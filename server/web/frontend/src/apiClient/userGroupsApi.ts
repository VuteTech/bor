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
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

/* ── Types ── */

export interface UserGroup {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface CreateUserGroupRequest {
  name: string;
  description: string;
}

export interface UpdateUserGroupRequest {
  name?: string;
  description?: string;
}

/* ── API calls ── */

export async function fetchUserGroups(): Promise<UserGroup[]> {
  return apiRequest<UserGroup[]>("/api/v1/user-groups", {
    headers: authHeaders(),
  });
}

export async function fetchUserGroup(id: string): Promise<UserGroup> {
  return apiRequest<UserGroup>(`/api/v1/user-groups/${id}`, {
    headers: authHeaders(),
  });
}

export async function createUserGroup(req: CreateUserGroupRequest): Promise<UserGroup> {
  return apiRequest<UserGroup>("/api/v1/user-groups", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateUserGroup(
  id: string,
  req: UpdateUserGroupRequest
): Promise<UserGroup> {
  return apiRequest<UserGroup>(`/api/v1/user-groups/${id}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteUserGroup(id: string): Promise<void> {
  return apiRequest<void>(`/api/v1/user-groups/${id}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

/* ── Group Members ── */

export interface UserGroupMember {
  id: string;
  group_id: string;
  user_id: string;
  created_at: string;
}

export async function fetchGroupMembers(groupId: string): Promise<UserGroupMember[]> {
  return apiRequest<UserGroupMember[]>(
    `/api/v1/user-groups/${encodeURIComponent(groupId)}/members`,
    { headers: authHeaders() }
  );
}

export async function addGroupMember(
  groupId: string,
  userId: string
): Promise<UserGroupMember> {
  return apiRequest<UserGroupMember>(
    `/api/v1/user-groups/${encodeURIComponent(groupId)}/members`,
    {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify({ user_id: userId }),
    }
  );
}

export async function removeGroupMember(
  groupId: string,
  memberId: string
): Promise<void> {
  return apiRequest<void>(
    `/api/v1/user-groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(memberId)}`,
    { method: "DELETE", headers: authHeaders() }
  );
}

/* ── Group Role Bindings ── */

export interface UserGroupRoleBinding {
  id: string;
  group_id: string;
  role_id: string;
  scope_type: string;
  scope_id?: string;
  created_at: string;
}

export async function fetchGroupRoleBindings(
  groupId: string
): Promise<UserGroupRoleBinding[]> {
  return apiRequest<UserGroupRoleBinding[]>(
    `/api/v1/user-groups/${encodeURIComponent(groupId)}/role-bindings`,
    { headers: authHeaders() }
  );
}

export async function addGroupRoleBinding(
  groupId: string,
  roleId: string,
  scopeType: string,
  scopeId?: string
): Promise<UserGroupRoleBinding> {
  return apiRequest<UserGroupRoleBinding>(
    `/api/v1/user-groups/${encodeURIComponent(groupId)}/role-bindings`,
    {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify({
        role_id: roleId,
        scope_type: scopeType,
        scope_id: scopeId,
      }),
    }
  );
}

export async function removeGroupRoleBinding(
  groupId: string,
  bindingId: string
): Promise<void> {
  return apiRequest<void>(
    `/api/v1/user-groups/${encodeURIComponent(groupId)}/role-bindings/${encodeURIComponent(bindingId)}`,
    { method: "DELETE", headers: authHeaders() }
  );
}
