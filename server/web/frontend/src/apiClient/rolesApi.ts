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

export interface Role {
  id: string;
  name: string;
  description: string;
  permission_count: number;
  created_at: string;
  updated_at: string;
}

export interface Permission {
  id: string;
  resource: string;
  action: string;
}

export interface CreateRoleRequest {
  name: string;
  description: string;
}

export interface UpdateRoleRequest {
  name?: string;
  description?: string;
}

/* ── API methods ── */

export async function fetchRoles(): Promise<Role[]> {
  return apiRequest<Role[]>("/api/v1/roles", { headers: authHeaders() });
}

export async function fetchRole(id: string): Promise<Role> {
  return apiRequest<Role>(`/api/v1/roles/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  });
}

export async function createRole(req: CreateRoleRequest): Promise<Role> {
  return apiRequest<Role>("/api/v1/roles", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateRole(
  id: string,
  req: UpdateRoleRequest
): Promise<void> {
  return apiRequestNoBody(`/api/v1/roles/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteRole(id: string): Promise<void> {
  return apiRequestNoBody(`/api/v1/roles/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

export async function fetchAllPermissions(): Promise<Permission[]> {
  return apiRequest<Permission[]>("/api/v1/permissions", {
    headers: authHeaders(),
  });
}

export async function fetchRolePermissions(
  roleId: string
): Promise<Permission[]> {
  return apiRequest<Permission[]>(
    `/api/v1/roles/${encodeURIComponent(roleId)}/permissions`,
    { headers: authHeaders() }
  );
}

export async function setRolePermissions(
  roleId: string,
  permissionIds: string[]
): Promise<void> {
  return apiRequestNoBody(
    `/api/v1/roles/${encodeURIComponent(roleId)}/permissions`,
    {
      method: "PUT",
      headers: authHeaders(),
      body: JSON.stringify({ permission_ids: permissionIds }),
    }
  );
}
