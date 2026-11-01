// SPDX-License-Identifier: AGPL-3.0-only
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
  /** Asks for a value first; it is sent as the JSON body {"panel_input": ...}. */
  input?: PanelInput;
}

export interface PanelInput {
  label: string;
  help?: string;
  max_bytes?: number;
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
  value?: string,
): Promise<{ token?: string; message?: string }> {
  const init: RequestInit = { method: action.method, headers: authHeaders() };
  if (value !== undefined) init.body = JSON.stringify({ panel_input: value });
  const res = await apiRequest<{ token?: string; message?: string }>(checkURL(action.url), init);
  // An extension action may change what the edition enables (a license, for
  // example); the shell re-reads the session to pick that up.
  window.dispatchEvent(new Event(SESSION_CHANGED_EVENT));
  return res;
}

/** Dispatched on window when the signed-in session's data may have changed. */
export const SESSION_CHANGED_EVENT = "bor:session-changed";
