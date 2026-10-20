// SPDX-License-Identifier: AGPL-3.0-only
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
  if (res.status === 204) return undefined as unknown as T;
  return res.json();
}

/* ── Types ── */

export type ReportTrigger = "manual" | "scheduled" | "daily_summary" | "api";

export interface ReportRef {
  id: string;
  name: string;
}

export interface ReportScope {
  all_nodes: boolean;
  node_groups: ReportRef[];
  policy_types: string[];
  policies: ReportRef[];
}

export interface ReportStatusCounts {
  compliant: number;
  non_compliant: number;
  error: number;
  inapplicable: number;
  unknown: number;
}

/** Result of checking a stored report when it is opened. */
export interface ReportIntegrity {
  sha256: string;
  /** ok | mismatch | missing */
  hash: string;
  /** unsigned | valid | invalid | unverified */
  signature: string;
  signature_format?: string;
  signature_key_id?: string;
}

export interface ReportSummary {
  id: string;
  generated_at: string;
  trigger: ReportTrigger | string;
  kind: string;
  generated_by: string;
  generated_by_user_id?: string;
  label: string;
  notes: string;
  scope: ReportScope;
  nodes: number;
  nodes_online: number;
  nodes_unreachable: number;
  results: number;
  results_stale: number;
  by_status: ReportStatusCounts;
  score_pct: number | null;
  pinned: boolean;
  bundle_bytes: number;
  sha256?: string;
  signed: boolean;
  /** Set when the report was opened, not in lists. */
  integrity?: ReportIntegrity;
  /** Download formats available for this report (json, csv, jws, plus renderer formats). */
  formats?: string[];
}

export interface PagedResponse<T> {
  items: T[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface ReportListParams {
  page?: number;
  per_page?: number;
  q?: string;
  trigger?: string;
  group?: string;
  /** full (default), summary or all */
  kind?: string;
}

export interface CreateReportRequest {
  scope: {
    all_nodes: boolean;
    node_group_ids: string[];
    policy_types: string[];
  };
  label: string;
  notes: string;
  include_items: boolean;
}

export interface ReportNodeRow {
  node_id: string;
  node_name: string;
  os_name?: string;
  os_version?: string;
  agent_version?: string;
  status: string;
  last_seen?: string;
  unreachable: boolean;
  groups: string[];
  lsm?: string;
  lsm_mode?: string;
  encrypted?: boolean;
  escrowed?: boolean;
  pending_security?: number;
  reboot_required?: boolean;
  by_status: ReportStatusCounts;
}

export interface ReportNodeParams {
  page?: number;
  per_page?: number;
  status?: string;
  unreachable?: boolean;
  q?: string;
}

export interface ReportItemControl {
  framework: string;
  id: string;
}

export interface ReportItem {
  schema_id: string;
  key: string;
  status: string;
  message?: string;
  controls?: ReportItemControl[];
}

export interface ReportResultRow {
  node_id: string;
  node_name: string;
  policy_id: string;
  policy_name: string;
  policy_type: string;
  policy_version: number;
  status: string;
  message?: string;
  reported_at?: string;
  stale: boolean;
  /** Number of per-setting items the result carried. */
  items: number;
  items_failed: number;
  /** The items, when the page was requested with include_items. */
  item_list?: ReportItem[];
}

export interface ReportResultParams {
  page?: number;
  per_page?: number;
  status?: string;
  policy?: string;
  type?: string;
  node?: string;
  stale?: boolean;
  q?: string;
  include_items?: boolean;
}

export interface ReportPolicyRow {
  policy_id: string;
  policy_name: string;
  policy_type: string;
  policy_version: number;
  nodes: number;
  by_status: ReportStatusCounts;
}

/* ── API calls ── */

type QueryValue = string | number | boolean | undefined;

function buildQuery(params: object): string {
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(params as Record<string, QueryValue>)) {
    if (value === undefined || value === null || value === "") continue;
    qs.set(key, typeof value === "boolean" ? (value ? "1" : "0") : String(value));
  }
  const s = qs.toString();
  return s ? `?${s}` : "";
}

export async function listReports(params: ReportListParams = {}): Promise<PagedResponse<ReportSummary>> {
  return apiRequest<PagedResponse<ReportSummary>>(`/api/v1/compliance/reports${buildQuery(params)}`, {
    headers: authHeaders(),
  });
}

export async function getReport(id: string): Promise<ReportSummary> {
  return apiRequest<ReportSummary>(`/api/v1/compliance/reports/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  });
}

export async function createReport(body: CreateReportRequest): Promise<ReportSummary> {
  return apiRequest<ReportSummary>("/api/v1/compliance/reports", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(body),
  });
}

export async function fetchReportNodes(id: string, params: ReportNodeParams = {}): Promise<PagedResponse<ReportNodeRow>> {
  const { unreachable, ...rest } = params;
  const query: Record<string, QueryValue> = { ...rest };
  if (unreachable !== undefined) query.unreachable = unreachable;
  return apiRequest<PagedResponse<ReportNodeRow>>(
    `/api/v1/compliance/reports/${encodeURIComponent(id)}/nodes${buildQuery(query)}`,
    { headers: authHeaders() },
  );
}

export async function fetchReportResults(id: string, params: ReportResultParams = {}): Promise<PagedResponse<ReportResultRow>> {
  const { stale, include_items, ...rest } = params;
  const query: Record<string, QueryValue> = { ...rest };
  if (stale !== undefined) query.stale = stale;
  if (include_items) query.include_items = true;
  return apiRequest<PagedResponse<ReportResultRow>>(
    `/api/v1/compliance/reports/${encodeURIComponent(id)}/results${buildQuery(query)}`,
    { headers: authHeaders() },
  );
}

export async function fetchReportPolicies(id: string): Promise<ReportPolicyRow[]> {
  return apiRequest<ReportPolicyRow[]>(`/api/v1/compliance/reports/${encodeURIComponent(id)}/policies`, {
    headers: authHeaders(),
  });
}

export async function deleteReport(id: string): Promise<void> {
  await apiRequest<void>(`/api/v1/compliance/reports/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

/** Download a report in one format (json, csv with a part, jws, or a renderer format) as a file. */
export async function downloadReport(id: string, format: string, part?: string): Promise<string> {
  const query = buildQuery({ format, part });
  const res = await fetch(`/api/v1/compliance/reports/${encodeURIComponent(id)}/download${query}`, {
    credentials: "same-origin",
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
  const blob = await res.blob();
  const disposition = res.headers.get("Content-Disposition") ?? "";
  const match = disposition.match(/filename="?([^";]+)"?/);
  const filename = match?.[1] ?? `compliance-report.${format === "csv" ? "csv" : format}`;
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
  return filename;
}
