// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// csrfToken reads the bor_csrf cookie for double-submit CSRF protection.
function csrfToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)bor_csrf=([^;]*)/);
  return match ? match[1] : "";
}

// authHeaders returns headers for authenticated API requests.
// Authentication is handled via httpOnly session cookies set by the server.
// The X-CSRF-Token header is included for CSRF protection on mutating requests.
export function authHeaders(): Record<string, string> {
  const hdrs: Record<string, string> = { "Content-Type": "application/json" };
  const csrf = csrfToken();
  if (csrf) hdrs["X-CSRF-Token"] = csrf;
  return hdrs;
}

// tryRefresh attempts to refresh the access token using the refresh cookie.
// Returns true if the refresh succeeded and a new session cookie was set.
let refreshInFlight: Promise<boolean> | null = null;

async function tryRefresh(): Promise<boolean> {
  // Coalesce concurrent refresh attempts into a single request.
  if (refreshInFlight) return refreshInFlight;
  refreshInFlight = (async () => {
    try {
      const res = await fetch("/api/v1/auth/refresh", {
        method: "POST",
        credentials: "same-origin",
      });
      return res.ok;
    } catch {
      return false;
    } finally {
      refreshInFlight = null;
    }
  })();
  return refreshInFlight;
}

async function apiRequest<T>(url: string, init?: RequestInit): Promise<T> {
  let res = await fetch(url, { credentials: "same-origin", ...init });
  if (res.status === 401) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      res = await fetch(url, { credentials: "same-origin", ...init });
    }
  }
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch {
      /* swallow */
    }
    throw new Error(detail);
  }
  return res.json();
}

export async function refreshSession(): Promise<boolean> {
  return tryRefresh();
}

/* ── Auth ── */

export interface LoginResult {
  token: string;
  user: UserInfo;
}

export interface UserInfo {
  id: number;
  username: string;
  email: string;
  full_name: string;
  permissions?: string[];
  source?: string;
}

export async function login(
  username: string,
  password: string
): Promise<LoginResult> {
  return apiRequest<LoginResult>("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
}

/* ── New multi-step auth flow ── */

export interface AuthBeginResponse {
  session_token: string;
  next: "mfa" | "password";
  mfa_methods: string[]; // e.g. ["webauthn", "totp"]
}

export interface AuthStepResponse {
  session_token?: string;
  next?: string;
  token?: string;
  user?: UserInfo;
}

export async function authBegin(username: string): Promise<AuthBeginResponse> {
  return apiRequest<AuthBeginResponse>("/api/v1/auth/begin", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username }),
  });
}

export async function authStep(
  sessionToken: string,
  type: "totp" | "password",
  credential: string
): Promise<AuthStepResponse> {
  return apiRequest<AuthStepResponse>("/api/v1/auth/step", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_token: sessionToken, type, credential }),
  });
}

/* ── MFA ── */

export interface MFAStatus {
  enabled: boolean;
  algorithm?: string;
  mfa_required: boolean;
}

export interface MFASetupBeginResult {
  secret: string;
  algorithm: string;
}

export interface MFASetupFinishResult {
  backup_codes: string[];
}

export interface MFASettings {
  mfa_required: boolean;
  totp_algorithm: "SHA256" | "SHA512";
}

export async function getMFAStatus(): Promise<MFAStatus> {
  return apiRequest<MFAStatus>("/api/v1/users/me/mfa", {
    headers: authHeaders(),
  });
}

export async function mfaSetupBegin(): Promise<MFASetupBeginResult> {
  return apiRequest<MFASetupBeginResult>("/api/v1/users/me/mfa/setup/begin", {
    method: "POST",
    headers: authHeaders(),
  });
}

export async function mfaSetupFinish(code: string): Promise<MFASetupFinishResult> {
  return apiRequest<MFASetupFinishResult>("/api/v1/users/me/mfa/setup/finish", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ code }),
  });
}

export async function mfaDisable(password: string): Promise<void> {
  const res = await fetch("/api/v1/users/me/mfa/disable", {
    method: "POST",
    headers: authHeaders(),
    credentials: "same-origin",
    body: JSON.stringify({ password }),
  });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch {
      /* swallow */
    }
    throw new Error(detail);
  }
}

export async function getMFASettings(): Promise<MFASettings> {
  return apiRequest<MFASettings>("/api/v1/settings/mfa", {
    headers: authHeaders(),
  });
}

export async function updateMFASettings(settings: MFASettings): Promise<MFASettings> {
  return apiRequest<MFASettings>("/api/v1/settings/mfa", {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(settings),
  });
}

export async function checkSession(): Promise<UserInfo> {
  return apiRequest<UserInfo>("/api/v1/auth/me", {
    headers: authHeaders(),
  });
}

/* ── Public server config ── */

export interface PublicConfig {
  privacy_policy_url: string;
}

export async function getPublicConfig(): Promise<PublicConfig> {
  const res = await fetch("/api/v1/config");
  if (!res.ok) return { privacy_policy_url: "" };
  return res.json();
}

export async function logout(): Promise<void> {
  await fetch("/api/v1/auth/logout", {
    method: "POST",
    credentials: "same-origin",
  });
}

/* ── WebAuthn ── */

export interface WebAuthnCredential {
  id: string;
  name: string;
  aaguid?: string;
  transports?: string[];
  created_at: string;
  last_used_at?: string;
}

export async function webAuthnRegisterBegin(): Promise<{ publicKey: unknown }> {
  return apiRequest<{ publicKey: unknown }>(
    "/api/v1/users/me/webauthn/register/begin",
    { method: "POST", headers: authHeaders() }
  );
}

export async function webAuthnRegisterFinish(
  name: string,
  credential: unknown
): Promise<WebAuthnCredential> {
  return apiRequest<WebAuthnCredential>(
    "/api/v1/users/me/webauthn/register/finish",
    {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify({ name, credential }),
    }
  );
}

export async function listWebAuthnCredentials(): Promise<WebAuthnCredential[]> {
  return apiRequest<WebAuthnCredential[]>(
    "/api/v1/users/me/webauthn/credentials",
    { headers: authHeaders() }
  );
}

export async function renameWebAuthnCredential(
  id: string,
  name: string
): Promise<void> {
  const res = await fetch(`/api/v1/users/me/webauthn/credentials/${id}`, {
    method: "PUT",
    headers: authHeaders(),
    credentials: "same-origin",
    body: JSON.stringify({ name }),
  });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch {
      /* swallow */
    }
    throw new Error(detail);
  }
}

export async function deleteWebAuthnCredential(id: string): Promise<void> {
  const res = await fetch(`/api/v1/users/me/webauthn/credentials/${id}`, {
    method: "DELETE",
    headers: authHeaders(),
    credentials: "same-origin",
  });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch {
      /* swallow */
    }
    throw new Error(detail);
  }
}

export async function webAuthnAuthBegin(
  sessionToken: string
): Promise<{ publicKey: unknown }> {
  return apiRequest<{ publicKey: unknown }>("/api/v1/auth/webauthn/begin", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_token: sessionToken }),
  });
}

export async function webAuthnAuthFinish(
  sessionToken: string,
  credential: unknown
): Promise<AuthStepResponse> {
  return apiRequest<AuthStepResponse>(
    "/api/v1/auth/webauthn/finish",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_token: sessionToken, credential }),
    }
  );
}
