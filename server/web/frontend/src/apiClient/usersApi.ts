// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

const TOKEN_STORAGE_KEY = "bor_token";

function authHeaders(): Record<string, string> {
  const tk = localStorage.getItem(TOKEN_STORAGE_KEY);
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

async function apiRequestNoBody(url: string, init?: RequestInit): Promise<void> {
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
}

/* ── Types ── */

export interface User {
  id: string;
  username: string;
  email: string;
  full_name: string;
  source: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateUserRequest {
  username: string;
  password: string;
  email: string;
  full_name: string;
}

export interface UpdateUserRequest {
  email?: string;
  full_name?: string;
  enabled?: boolean;
}

export interface UserRoleBinding {
  id: string;
  user_id: string;
  role_id: string;
  scope_type: string;
  scope_id?: string;
  created_at: string;
}

export interface CreateBindingRequest {
  user_id: string;
  role_id: string;
  scope_type: string;
  scope_id?: string;
}

/* ── API methods ── */

export async function fetchUsers(): Promise<User[]> {
  return apiRequest<User[]>("/api/v1/users", { headers: authHeaders() });
}

export async function fetchUser(id: string): Promise<User> {
  return apiRequest<User>(`/api/v1/users/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  });
}

export async function createUser(req: CreateUserRequest): Promise<User> {
  return apiRequest<User>("/api/v1/users", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateUser(
  id: string,
  req: UpdateUserRequest
): Promise<void> {
  return apiRequestNoBody(`/api/v1/users/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteUser(id: string): Promise<void> {
  return apiRequestNoBody(`/api/v1/users/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

export async function fetchUserBindings(
  userId: string
): Promise<UserRoleBinding[]> {
  return apiRequest<UserRoleBinding[]>(
    `/api/v1/user-role-bindings?user_id=${encodeURIComponent(userId)}`,
    { headers: authHeaders() }
  );
}

export async function createBinding(
  req: CreateBindingRequest
): Promise<UserRoleBinding> {
  return apiRequest<UserRoleBinding>("/api/v1/user-role-bindings", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteBinding(id: string): Promise<void> {
  return apiRequestNoBody(
    `/api/v1/user-role-bindings/${encodeURIComponent(id)}`,
    { method: "DELETE", headers: authHeaders() }
  );
}
