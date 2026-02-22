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

/* ── Node types ── */

export type NodeStatus = "online" | "offline" | "unknown";

export interface Node {
  id: string;
  name: string;
  fqdn?: string;
  machine_id?: string;
  ip_address?: string;
  os_name?: string;
  os_version?: string;
  desktop_env?: string;
  agent_version?: string;
  status: NodeStatus;
  status_reason?: string;
  groups?: string;
  notes?: string;
  node_group_ids?: string[];
  node_group_names?: string[];
  last_seen?: string;
  created_at: string;
  updated_at: string;
}

export interface UpdateNodeRequest {
  name?: string;
  groups?: string;
  notes?: string;
}

export interface NodeStatusCounts {
  online: number;
  offline: number;
  unknown: number;
}

/* ── API calls ── */

export async function fetchNodes(params?: {
  search?: string;
  status?: string;
}): Promise<Node[]> {
  const qs = new URLSearchParams();
  if (params?.search) qs.set("search", params.search);
  if (params?.status) qs.set("status", params.status);
  const query = qs.toString();
  const url = `/api/v1/nodes${query ? `?${query}` : ""}`;
  return apiRequest<Node[]>(url, { headers: authHeaders() });
}

export async function fetchNode(id: string): Promise<Node> {
  return apiRequest<Node>(`/api/v1/nodes/${id}`, { headers: authHeaders() });
}

export async function updateNode(id: string, req: UpdateNodeRequest): Promise<Node> {
  return apiRequest<Node>(`/api/v1/nodes/${id}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function fetchNodeStatusCounts(): Promise<NodeStatusCounts> {
  return apiRequest<NodeStatusCounts>("/api/v1/nodes/status-counts", {
    headers: authHeaders(),
  });
}

export async function refreshNodeMetadata(id: string): Promise<void> {
  await apiRequest<{ ok: boolean }>(`/api/v1/nodes/${id}/refresh-metadata`, {
    method: "POST",
    headers: authHeaders(),
  });
}

export async function addNodeToGroup(nodeId: string, groupId: string): Promise<Node> {
  return apiRequest<Node>(`/api/v1/nodes/${nodeId}/groups`, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ group_id: groupId }),
  });
}

export async function removeNodeFromGroup(nodeId: string, groupId: string): Promise<void> {
  const res = await fetch(`/api/v1/nodes/${nodeId}/groups/${groupId}`, {
    method: "DELETE",
    headers: authHeaders(),
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

export async function deleteNode(id: string): Promise<void> {
  const res = await fetch(`/api/v1/nodes/${id}`, {
    method: "DELETE",
    headers: authHeaders(),
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
