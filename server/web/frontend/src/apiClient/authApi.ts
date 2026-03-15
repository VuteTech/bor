// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

const TOKEN_STORAGE_KEY = "bor_token";

function storedToken(): string | null {
  return localStorage.getItem(TOKEN_STORAGE_KEY);
}

function authHeaders(): Record<string, string> {
  const tk = storedToken();
  const hdrs: Record<string, string> = { "Content-Type": "application/json" };
  if (tk) hdrs["Authorization"] = `Bearer ${tk}`;
  return hdrs;
}

async function apiRequest<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
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
  const result = await apiRequest<LoginResult>("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (result.token) {
    localStorage.setItem(TOKEN_STORAGE_KEY, result.token);
  }
  return result;
}

/* ── New multi-step auth flow ── */

export interface AuthBeginResponse {
  session_token: string;
  next: "totp" | "password";
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
  const result = await apiRequest<AuthStepResponse>("/api/v1/auth/step", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_token: sessionToken, type, credential }),
  });
  if (result.token) {
    localStorage.setItem(TOKEN_STORAGE_KEY, result.token);
  }
  return result;
}

/* ── MFA ── */

export interface MFAStatus {
  enabled: boolean;
  algorithm?: string;
  mfa_required: boolean;
}

export interface MFASetupBeginResult {
  secret: string;
  qr_code_url: string;
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

export function logout(): void {
  localStorage.removeItem(TOKEN_STORAGE_KEY);
}

export function getStoredToken(): string | null {
  return storedToken();
}
