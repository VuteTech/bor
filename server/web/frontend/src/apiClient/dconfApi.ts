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
    } catch { /* swallow */ }
    throw new Error(detail);
  }
  return res.json();
}

/* ── DConf schema catalogue types ── */

export interface DConfEnumValue {
  nick: string;
  value: number;
}

export interface DConfKey {
  name: string;
  type: string;
  summary?: string;
  description?: string;
  default_value?: string;
  enum_values?: DConfEnumValue[];
  range_min?: string;
  range_max?: string;
  choices?: string[];
}

export interface DConfSchema {
  schema_id: string;
  path: string;
  relocatable: boolean;
  source: string;
  keys: DConfKey[];
}

/* ── Compliance types ── */

export type ComplianceStatus = "unknown" | "compliant" | "non_compliant" | "inapplicable" | "error";

export interface ComplianceItem {
  schema_id: string;
  key: string;
  status: ComplianceStatus;
  message?: string;
}

export interface ComplianceResult {
  node_id: string;
  node_name: string;
  policy_id: string;
  policy_name: string;
  status: ComplianceStatus;
  message?: string;
  items?: ComplianceItem[];
  reported_at: string;
}

/* ── DConf policy content types (stored as JSON in policy.content) ── */

export interface DConfEntry {
  schema_id: string;
  path: string;
  key: string;
  value: string;
  lock: boolean;
}

export interface DConfPolicyContent {
  entries: DConfEntry[];
  db_name: string;
}

/* ── API calls ── */

export async function fetchDConfSchemas(nodeId?: string): Promise<DConfSchema[]> {
  const qs = nodeId ? `?node_id=${encodeURIComponent(nodeId)}` : "";
  return apiRequest<DConfSchema[]>(`/api/v1/dconf/schemas${qs}`, {
    headers: authHeaders(),
  });
}

export interface ComplianceListParams {
  page?: number;
  per_page?: number;
  search?: string;
  status?: string;
  sort_field?: string;
  sort_order?: "asc" | "desc";
}

export interface ComplianceListResponse {
  items: ComplianceResult[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
  status_counts: Record<string, number>;
}

// fetchCompliancePaged returns one page of compliance results plus the total
// count and an overall per-status distribution (all applied server-side).
export async function fetchCompliancePaged(
  params: ComplianceListParams = {},
): Promise<ComplianceListResponse> {
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== "") qs.set(key, String(value));
  }
  const query = qs.toString();
  return apiRequest<ComplianceListResponse>(`/api/v1/compliance${query ? `?${query}` : ""}`, {
    headers: authHeaders(),
  });
}
