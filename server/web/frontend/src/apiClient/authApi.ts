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
