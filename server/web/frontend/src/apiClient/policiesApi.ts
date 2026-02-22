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

/* ── Policy types ── */

export interface Policy {
  id: string;
  name: string;
  description: string;
  type: string;
  content: string;
  version: number;
  state: "draft" | "released" | "archived";
  deprecated_at?: string | null;
  deprecation_message?: string | null;
  replacement_policy_id?: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface CreatePolicyRequest {
  name: string;
  description: string;
  type: string;
  content: string;
}

export interface UpdatePolicyRequest {
  name?: string;
  description?: string;
  type?: string;
  content?: string;
}

export interface SetPolicyStateRequest {
  state: string;
}

export interface DeprecatePolicyRequest {
  message?: string;
  replacement_policy_id?: string;
}

/* ── API methods ── */

export async function fetchAllPolicies(): Promise<Policy[]> {
  return apiRequest<Policy[]>("/api/v1/policies/all", {
    headers: authHeaders(),
  });
}

export async function fetchPolicy(id: string): Promise<Policy> {
  return apiRequest<Policy>(`/api/v1/policies/all/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  });
}

export async function createPolicy(req: CreatePolicyRequest): Promise<Policy> {
  return apiRequest<Policy>("/api/v1/policies/all", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updatePolicy(id: string, req: UpdatePolicyRequest): Promise<Policy> {
  return apiRequest<Policy>(`/api/v1/policies/all/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function setPolicyState(id: string, req: SetPolicyStateRequest): Promise<Policy> {
  return apiRequest<Policy>(`/api/v1/policies/all/${encodeURIComponent(id)}/state`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deprecatePolicy(id: string, req: DeprecatePolicyRequest): Promise<Policy> {
  return apiRequest<Policy>(`/api/v1/policies/all/${encodeURIComponent(id)}/deprecate`, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deletePolicy(id: string): Promise<void> {
  const res = await fetch(`/api/v1/policies/all/${encodeURIComponent(id)}`, {
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
