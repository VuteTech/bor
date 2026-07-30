// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import { authHeaders } from "./authApi";

async function apiRequest<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: "same-origin", ...init });
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
  cert_serial?: string;
  cert_not_after?: string;
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

export interface NodeListParams {
  page?: number;
  per_page?: number;
  search?: string;
  status?: string;
  os?: string;
  desktop?: string;
  agent_version?: string;
  sort_field?: string;
  sort_order?: "asc" | "desc";
}

export interface NodeListResponse {
  items: Node[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface NodeFilterOptions {
  os: string[];
  desktops: string[];
  agent_versions: string[];
}

/* ── API calls ── */

// fetchNodesPaged returns a single page of nodes plus the total count, with all
// filtering and sorting applied server-side.
export async function fetchNodesPaged(params: NodeListParams = {}): Promise<NodeListResponse> {
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== "") qs.set(key, String(value));
  }
  const query = qs.toString();
  return apiRequest<NodeListResponse>(`/api/v1/nodes${query ? `?${query}` : ""}`, {
    headers: authHeaders(),
  });
}

// fetchNodeFilterOptions returns the distinct values for the filter dropdowns.
export async function fetchNodeFilterOptions(): Promise<NodeFilterOptions> {
  return apiRequest<NodeFilterOptions>("/api/v1/nodes/filter-options", {
    headers: authHeaders(),
  });
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

export async function deleteNode(id: string): Promise<void> {
  const res = await fetch(`/api/v1/nodes/${id}`, {
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

export async function revokeNodeCertificate(id: string, reason?: string): Promise<void> {
  await apiRequest<{ status: string }>(`/api/v1/nodes/${id}/revoke`, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ reason: reason || "manually revoked" }),
  });
}
