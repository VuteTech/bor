// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Settings panels contributed by server extensions (pkg/ext). The UI renders
 * the panel JSON generically; it knows nothing about the extension.
 */

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
  if (res.status === 204) return {} as T;
  return res.json();
}

export interface ExtSettingsPanel {
  id: string;
  title: string;
  data_url: string;
}

export interface PanelAction {
  label: string;
  method: string;
  url: string;
  danger?: boolean;
  confirm_phrase?: string;
  confirm?: string;
  /** "secret": show the response's token once; "message": show its message. */
  result?: "secret" | "message" | "";
}

export interface PanelField {
  label: string;
  value: string;
  copy?: boolean;
}

export interface PanelTable {
  title: string;
  columns: string[];
  rows: { cells: string[]; actions?: PanelAction[] }[];
  empty?: string;
}

export interface PanelSection {
  title: string;
  subtitle?: string;
  status?: "ok" | "warning" | "error" | "disabled";
  status_text?: string;
  fields?: PanelField[];
  tables?: PanelTable[];
  actions?: PanelAction[];
}

export interface Panel {
  description?: string;
  sections: PanelSection[];
}

/** Extension URLs are confined to the extension API. */
function checkURL(url: string): string {
  if (!url.startsWith("/api/v1/ext/") || url.includes("..")) {
    throw new Error("Invalid extension URL");
  }
  return url;
}

export async function fetchExtPanels(): Promise<ExtSettingsPanel[]> {
  return apiRequest<ExtSettingsPanel[]>("/api/v1/ext-panels", { headers: authHeaders() });
}

export async function fetchPanel(dataUrl: string): Promise<Panel> {
  return apiRequest<Panel>(checkURL(dataUrl), { headers: authHeaders() });
}

export async function runPanelAction(
  action: PanelAction,
): Promise<{ token?: string; message?: string }> {
  return apiRequest<{ token?: string; message?: string }>(checkURL(action.url), {
    method: action.method,
    headers: authHeaders(),
  });
}
