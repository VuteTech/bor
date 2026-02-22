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
  // 204 No Content
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

/* ── Types ── */

export interface NodeGroup {
  id: string;
  name: string;
  description: string;
  node_count: number;
  created_at: string;
  updated_at: string;
}

export interface CreateNodeGroupRequest {
  name: string;
  description: string;
}

export interface UpdateNodeGroupRequest {
  name?: string;
  description?: string;
}

export interface EnrollmentToken {
  token: string;
  node_group_id: string;
  expires_at: string;
}

/* ── API calls ── */

export async function fetchNodeGroups(): Promise<NodeGroup[]> {
  return apiRequest<NodeGroup[]>("/api/v1/node-groups", {
    headers: authHeaders(),
  });
}

export async function fetchNodeGroup(id: string): Promise<NodeGroup> {
  return apiRequest<NodeGroup>(`/api/v1/node-groups/${id}`, {
    headers: authHeaders(),
  });
}

export async function createNodeGroup(req: CreateNodeGroupRequest): Promise<NodeGroup> {
  return apiRequest<NodeGroup>("/api/v1/node-groups", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateNodeGroup(
  id: string,
  req: UpdateNodeGroupRequest
): Promise<NodeGroup> {
  return apiRequest<NodeGroup>(`/api/v1/node-groups/${id}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteNodeGroup(id: string): Promise<void> {
  return apiRequest<void>(`/api/v1/node-groups/${id}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

export async function generateEnrollmentToken(
  groupId: string
): Promise<EnrollmentToken> {
  return apiRequest<EnrollmentToken>(`/api/v1/node-groups/${groupId}/tokens`, {
    method: "POST",
    headers: authHeaders(),
  });
}
