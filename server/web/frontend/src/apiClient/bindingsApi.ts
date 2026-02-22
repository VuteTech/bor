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

export interface PolicyBinding {
  id: string;
  policy_id: string;
  group_id: string;
  state: "enabled" | "disabled";
  priority: number;
  policy_name: string;
  policy_state: string;
  group_name: string;
  node_count: number;
  created_at: string;
  updated_at: string;
}

export interface CreatePolicyBindingRequest {
  policy_id: string;
  group_id: string;
  priority: number;
}

export interface UpdatePolicyBindingRequest {
  state?: string;
  priority?: number;
}

/* ── API calls ── */

export async function fetchBindings(): Promise<PolicyBinding[]> {
  return apiRequest<PolicyBinding[]>("/api/v1/policy-bindings", {
    headers: authHeaders(),
  });
}

export async function fetchBinding(id: string): Promise<PolicyBinding> {
  return apiRequest<PolicyBinding>(`/api/v1/policy-bindings/${id}`, {
    headers: authHeaders(),
  });
}

export async function createBinding(
  req: CreatePolicyBindingRequest
): Promise<PolicyBinding> {
  return apiRequest<PolicyBinding>("/api/v1/policy-bindings", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateBinding(
  id: string,
  req: UpdatePolicyBindingRequest
): Promise<PolicyBinding> {
  return apiRequest<PolicyBinding>(`/api/v1/policy-bindings/${id}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteBinding(id: string): Promise<void> {
  return apiRequest<void>(`/api/v1/policy-bindings/${id}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}
