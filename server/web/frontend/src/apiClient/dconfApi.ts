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

export async function fetchComplianceResults(): Promise<ComplianceResult[]> {
  return apiRequest<ComplianceResult[]>("/api/v1/compliance", {
    headers: authHeaders(),
  });
}
