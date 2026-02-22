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

/* ── Types ── */

export interface AuditLog {
  id: string;
  user_id?: string;
  username: string;
  action: string;
  resource_type: string;
  resource_id: string;
  details: string;
  ip_address: string;
  created_at: string;
}

export interface AuditLogListResponse {
  items: AuditLog[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface AuditLogListParams {
  page?: number;
  per_page?: number;
  resource_type?: string;
  action?: string;
  username?: string;
}

/* ── API methods ── */

export async function fetchAuditLogs(
  params?: AuditLogListParams
): Promise<AuditLogListResponse> {
  const qp = new URLSearchParams();
  if (params?.page) qp.set("page", String(params.page));
  if (params?.per_page) qp.set("per_page", String(params.per_page));
  if (params?.resource_type) qp.set("resource_type", params.resource_type);
  if (params?.action) qp.set("action", params.action);
  if (params?.username) qp.set("username", params.username);

  const qs = qp.toString();
  const url = `/api/v1/audit-logs${qs ? "?" + qs : ""}`;
  return apiRequest<AuditLogListResponse>(url, { headers: authHeaders() });
}

export async function exportAuditLogs(
  format: "csv" | "json",
  params?: AuditLogListParams
): Promise<void> {
  const qp = new URLSearchParams();
  qp.set("format", format);
  if (params?.resource_type) qp.set("resource_type", params.resource_type);
  if (params?.action) qp.set("action", params.action);
  if (params?.username) qp.set("username", params.username);

  const tk = localStorage.getItem(TOKEN_STORAGE_KEY);
  const hdrs: Record<string, string> = {};
  if (tk) hdrs["Authorization"] = `Bearer ${tk}`;

  const res = await fetch(`/api/v1/audit-logs/export?${qp.toString()}`, {
    headers: hdrs,
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

  // Download the file
  const blob = await res.blob();
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `audit_logs.${format}`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  window.URL.revokeObjectURL(url);
}
