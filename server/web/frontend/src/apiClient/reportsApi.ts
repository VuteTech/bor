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
  /** Failed results whose failures are all covered by waivers. */
  results_waived: number;
  by_status: ReportStatusCounts;
  score_pct: number | null;
  schedule_id?: string;
  pinned: boolean;
  pinned_by?: string;
  pinned_at?: string;
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

/** What this server offers for reports (extension modules add capabilities). */
export interface ReportFramework {
  name: string;
  short: string;
  version: string;
  url?: string;
}

export interface ReportCapabilities {
  /** e.g. "schedules", "waivers", "attestations", "frameworks" */
  capabilities: string[];
  formats: string[];
  frameworks: ReportFramework[];
  frameworks_version: string;
  /** Fresh-evidence collection is available. */
  refresh: boolean;
  daily_summary: boolean;
  stale_after_seconds: number;
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
  /** Ask the online agents in scope to re-check before the snapshot. */
  refresh?: boolean;
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
  /** Id of the waiver that covers the item's failure. */
  waiver?: string;
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
  /** Every failure of the result is covered by a waiver. */
  waived: boolean;
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
  waived?: boolean;
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
  return apiRequest<PagedResponse<ReportSummary>>(`/api/v1/ext/reports/reports${buildQuery(params)}`, {
    headers: authHeaders(),
  });
}

export async function getReport(id: string): Promise<ReportSummary> {
  return apiRequest<ReportSummary>(`/api/v1/ext/reports/reports/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  });
}

export async function createReport(body: CreateReportRequest): Promise<ReportSummary> {
  return apiRequest<ReportSummary>("/api/v1/ext/reports/reports", {
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
    `/api/v1/ext/reports/reports/${encodeURIComponent(id)}/nodes${buildQuery(query)}`,
    { headers: authHeaders() },
  );
}

export async function fetchReportResults(id: string, params: ReportResultParams = {}): Promise<PagedResponse<ReportResultRow>> {
  const { stale, include_items, ...rest } = params;
  const query: Record<string, QueryValue> = { ...rest };
  if (stale !== undefined) query.stale = stale;
  if (include_items) query.include_items = true;
  return apiRequest<PagedResponse<ReportResultRow>>(
    `/api/v1/ext/reports/reports/${encodeURIComponent(id)}/results${buildQuery(query)}`,
    { headers: authHeaders() },
  );
}

export interface ReportControlRow {
  framework: string;
  id: string;
  title: string;
  pass: number;
  fail: number;
  error: number;
  inapplicable: number;
  nodes_failing: number;
}

export interface ControlFinding {
  node_id: string;
  node: string;
  policy_id: string;
  policy_name: string;
  policy_type: string;
  schema_id?: string;
  key?: string;
  status: string;
  message?: string;
  stale: boolean;
  waiver?: string;
}

export interface ReportChange {
  node_id: string;
  node_name: string;
  policy_id: string;
  policy_name: string;
  policy_type: string;
  before: string;
  after: string;
}

export interface ReportComparison {
  base: ReportSummary;
  target: ReportSummary;
  new_failures: ReportChange[];
  new_failures_total: number;
  fixed: ReportChange[];
  fixed_total: number;
  still_failing: number;
  nodes_added: { node_id: string; node_name: string }[];
  nodes_removed: { node_id: string; node_name: string }[];
  policies_added: { policy_id: string; policy_name: string; policy_type: string }[];
  policies_removed: { policy_id: string; policy_name: string; policy_type: string }[];
}

export interface ReportTrendPoint {
  report_id: string;
  generated_at: string;
  score_pct: number | null;
  nodes: number;
  nodes_unreachable: number;
  results: number;
  results_stale: number;
  by_status: ReportStatusCounts;
}

export async function fetchReportCapabilities(): Promise<ReportCapabilities> {
  return apiRequest<ReportCapabilities>("/api/v1/ext/reports/capabilities", { headers: authHeaders() });
}

export async function fetchReportControls(id: string): Promise<ReportControlRow[]> {
  return apiRequest<ReportControlRow[]>(`/api/v1/ext/reports/reports/${encodeURIComponent(id)}/controls`, { headers: authHeaders() });
}

export async function fetchControlFindings(id: string, framework: string, control: string, failingOnly = false): Promise<ControlFinding[]> {
  return apiRequest<ControlFinding[]>(
    `/api/v1/ext/reports/reports/${encodeURIComponent(id)}/controls/findings${buildQuery({ framework, control, failing: failingOnly || undefined })}`,
    { headers: authHeaders() },
  );
}

export async function setReportPinned(id: string, pinned: boolean): Promise<ReportSummary> {
  return apiRequest<ReportSummary>(`/api/v1/ext/reports/reports/${encodeURIComponent(id)}/pin`, {
    method: pinned ? "POST" : "DELETE",
    headers: authHeaders(),
  });
}

export async function compareReports(targetId: string, baseId: string): Promise<ReportComparison> {
  return apiRequest<ReportComparison>(
    `/api/v1/ext/reports/reports/${encodeURIComponent(targetId)}/compare${buildQuery({ base: baseId })}`,
    { headers: authHeaders() },
  );
}

/** Daily summaries of the fleet (no group) or of one node group. */
export async function fetchComplianceTrend(days = 30, group?: string): Promise<ReportTrendPoint[]> {
  return apiRequest<ReportTrendPoint[]>(`/api/v1/ext/reports/trend${buildQuery({ days, group })}`, { headers: authHeaders() });
}

/* ── Bor Enterprise programme (shown when the server advertises the capability) ── */

export type ScheduleCadence = "daily" | "weekly" | "monthly";

export interface ReportSchedule {
  id: string;
  name: string;
  enabled: boolean;
  all_nodes: boolean;
  node_group_ids: string[];
  policy_types: string[];
  include_items: boolean;
  refresh: boolean;
  cadence: ScheduleCadence;
  at_minute: number;
  weekday?: number;
  month_day?: number;
  time_zone: string;
  label_template: string;
  notes: string;
  webhook_url: string;
  webhook_secret_set: boolean;
  email_to: string[];
  attach_formats: string[];
  created_by: string;
  created_at: string;
  updated_at: string;
  next_run_at?: string;
  running_since?: string;
  last_run_at?: string;
  last_status?: string;
  last_error?: string;
  last_report_id?: string;
  last_delivery_status?: string;
  last_delivery_error?: string;
}

export interface ScheduleInput {
  name: string;
  enabled: boolean;
  all_nodes: boolean;
  node_group_ids: string[];
  policy_types: string[];
  include_items: boolean;
  refresh: boolean;
  cadence: ScheduleCadence;
  at_minute: number;
  weekday?: number;
  month_day?: number;
  time_zone: string;
  label_template: string;
  notes: string;
  webhook_url: string;
  webhook_secret?: string;
  clear_webhook_secret?: boolean;
  email_to: string[];
  attach_formats: string[];
}

export interface SchedulesResponse {
  items: ReportSchedule[];
  delivery: { email: boolean; http_webhooks: boolean; formats: string[] };
}

export interface ScheduleRunOutcome {
  status: string;
  error?: string;
  report_id?: string;
  delivery_status?: string;
  delivery_error?: string;
}

const EXT_REPORTS = "/api/v1/ext/reports";

export async function listSchedules(): Promise<SchedulesResponse> {
  return apiRequest<SchedulesResponse>(`${EXT_REPORTS}/schedules`, { headers: authHeaders() });
}

export async function createSchedule(body: ScheduleInput): Promise<ReportSchedule> {
  return apiRequest<ReportSchedule>(`${EXT_REPORTS}/schedules`, { method: "POST", headers: authHeaders(), body: JSON.stringify(body) });
}

export async function updateSchedule(id: string, body: ScheduleInput): Promise<ReportSchedule> {
  return apiRequest<ReportSchedule>(`${EXT_REPORTS}/schedules/${encodeURIComponent(id)}`, { method: "PUT", headers: authHeaders(), body: JSON.stringify(body) });
}

export async function deleteSchedule(id: string): Promise<void> {
  await apiRequest<void>(`${EXT_REPORTS}/schedules/${encodeURIComponent(id)}`, { method: "DELETE", headers: authHeaders() });
}

export async function runSchedule(id: string): Promise<ScheduleRunOutcome> {
  return apiRequest<ScheduleRunOutcome>(`${EXT_REPORTS}/schedules/${encodeURIComponent(id)}/run`, { method: "POST", headers: authHeaders() });
}

export interface Waiver {
  id: string;
  policy_id: string;
  policy_name: string;
  policy_type: string;
  node_id?: string;
  node_name?: string;
  schema_id?: string;
  key?: string;
  reason: string;
  approved_by: string;
  approved_at: string;
  expires_at: string;
  revoked_at?: string;
  revoked_by?: string;
  /** active | expired | revoked */
  state: string;
}

export interface WaiverInput {
  policy_id: string;
  node_id?: string;
  schema_id?: string;
  key?: string;
  reason: string;
  expires_at: string;
}

export async function listWaivers(includeInactive = false): Promise<{ items: Waiver[]; max_days: number }> {
  return apiRequest<{ items: Waiver[]; max_days: number }>(`${EXT_REPORTS}/waivers${includeInactive ? "?all=1" : ""}`, { headers: authHeaders() });
}

export async function createWaiver(body: WaiverInput): Promise<{ id: string }> {
  return apiRequest<{ id: string }>(`${EXT_REPORTS}/waivers`, { method: "POST", headers: authHeaders(), body: JSON.stringify(body) });
}

export async function revokeWaiver(id: string): Promise<void> {
  await apiRequest<void>(`${EXT_REPORTS}/waivers/${encodeURIComponent(id)}`, { method: "DELETE", headers: authHeaders() });
}

export interface Attestation {
  id: string;
  decision: "approved" | "rejected";
  comment: string;
  attested_by: string;
  attested_at: string;
  key_id: string;
  /** valid | invalid */
  signature: string;
}

export interface AttestationSummary {
  report_id: string;
  /** pending | approved | rejected */
  state: string;
  approvals: number;
  required_approvals: number;
  generated_by_user_id?: string;
  items: Attestation[];
  /** The requesting user generated the report (and cannot attest it). */
  self_generated: boolean;
  /** The requesting user has already attested the report. */
  self_attested: boolean;
}

export async function fetchAttestations(reportId: string): Promise<AttestationSummary> {
  return apiRequest<AttestationSummary>(`${EXT_REPORTS}/attestations/${encodeURIComponent(reportId)}`, { headers: authHeaders() });
}

export async function attestReport(reportId: string, decision: "approved" | "rejected", comment: string): Promise<Attestation> {
  return apiRequest<Attestation>(`${EXT_REPORTS}/attest/${encodeURIComponent(reportId)}`, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ decision, comment }),
  });
}

export async function fetchReportPolicies(id: string): Promise<ReportPolicyRow[]> {
  return apiRequest<ReportPolicyRow[]>(`/api/v1/ext/reports/reports/${encodeURIComponent(id)}/policies`, {
    headers: authHeaders(),
  });
}

export async function deleteReport(id: string): Promise<void> {
  await apiRequest<void>(`/api/v1/ext/reports/reports/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

/** Download a report in one format (json, csv with a part, jws, or a renderer format) as a file. */
export async function downloadReport(id: string, format: string, part?: string): Promise<string> {
  const query = buildQuery({ format, part });
  const res = await fetch(`/api/v1/ext/reports/reports/${encodeURIComponent(id)}/download${query}`, {
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
