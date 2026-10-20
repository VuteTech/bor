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
    } catch { /* swallow */ }
    throw new Error(detail);
  }
  return res.json();
}

export type UpdatesEngine = "none" | "dnf-automatic" | "unattended-upgrades" | "os-update" | "transactional-update" | "rpm-ostree" | "";

export interface UpdatesRun {
  time?: string;
  success: boolean;
  packages: number;
  message: string;
}

export interface UpdatesRebootState {
  required: boolean;
  since?: string;
  reasons: string[];
  staged: boolean;
}

/** The inventory a node reported (protojson, without the pending list). */
export interface UpdatesInventory {
  engine: UpdatesEngine;
  engineInstalled: boolean;
  engineTimerEnabled: boolean;
  /** bor | vendor | foreign | "" */
  engineManagedBy: string;
  /** off | check | security | all | "" */
  effectiveMode: string;
  lastCheck?: string;
  lastRun?: UpdatesRun;
  pendingTotal: number;
  pendingSecurity: number;
  reboot?: UpdatesRebootState;
  bootTime?: string;
  runningKernel: string;
  newestKernel: string;
  foreignMechanisms: string[];
  lastPolicyReboot?: string;
  /** When the agent first saw the oldest pending security update. */
  oldestSecuritySince?: string;
  /** Pending firmware updates (fwupd), not counted in pendingTotal. */
  pendingFirmware: number;
}

export interface UpdatesNodeStatus {
  reportedAt?: string;
  inventory?: Partial<UpdatesInventory>;
}

export interface UpdatesPendingRow {
  kind: "package" | "firmware";
  name: string;
  version: string;
  advisory: string;
  severity: string;
  security: boolean;
  cves: string[];
}

/** The update state a node reported last. */
export async function fetchNodeUpdates(nodeId: string): Promise<UpdatesNodeStatus> {
  return apiRequest<UpdatesNodeStatus>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/updates`, { headers: authHeaders() });
}

/** The pending updates of a node, security first. */
export async function fetchNodeUpdatesPending(nodeId: string, opts: { q?: string; security?: boolean; limit?: number } = {}): Promise<UpdatesPendingRow[]> {
  const qs = new URLSearchParams();
  if (opts.q) qs.set("q", opts.q);
  if (opts.security) qs.set("security", "1");
  if (opts.limit) qs.set("limit", String(opts.limit));
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiRequest<UpdatesPendingRow[]>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/updates/pending${suffix}`, { headers: authHeaders() });
}

export type UpdatesActionKind = "check" | "install" | "reboot";

export interface UpdatesActionResult {
  id: string;
  kind: UpdatesActionKind;
  force: boolean;
  success: boolean;
  message: string;
  /** The tail of the engine's output. */
  text: string;
}

/** Ask the node to refresh its metadata, run its engine once or reboot; waits for the outcome. */
export async function runUpdatesAction(nodeId: string, kind: UpdatesActionKind, force = false): Promise<UpdatesActionResult> {
  return apiRequest<UpdatesActionResult>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/updates/actions`, {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify({ kind, force }),
  });
}

/** Fleet counts by update state, within the caller's scope. */
export interface UpdatesFleetSummary {
  nodes: number;
  reported: number;
  securityPending: number;
  rebootPending: number;
  engineMissing: number;
}

/** Download the pending updates of the scope (or one node) as CSV. */
export async function downloadUpdatesExport(opts: { security?: boolean; nodeId?: string; group?: string; q?: string } = {}): Promise<number> {
  const qs = new URLSearchParams();
  if (opts.security) qs.set("security", "1");
  if (opts.nodeId) qs.set("node", opts.nodeId);
  if (opts.group) qs.set("group", opts.group);
  if (opts.q) qs.set("q", opts.q);
  const res = await fetch(`/api/v1/updates/export?${qs.toString()}`, { credentials: "same-origin", headers: authHeaders() });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch { /* swallow */ }
    throw new Error(detail);
  }
  const text = await res.text();
  const blob = new Blob([text], { type: "text/csv" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = res.headers.get("Content-Disposition")?.match(/filename=([^;]+)/)?.[1] ?? "pending-updates.csv";
  a.click();
  URL.revokeObjectURL(url);
  return Math.max(0, text.split("\n").filter((l) => l.trim()).length - 1);
}

export async function fetchUpdatesSummary(): Promise<UpdatesFleetSummary> {
  return apiRequest<UpdatesFleetSummary>("/api/v1/updates/summary", { headers: authHeaders() });
}

export function updatesEngineLabel(engine: string): string {
  switch (engine) {
    case "dnf-automatic": return "dnf-automatic";
    case "unattended-upgrades": return "unattended-upgrades";
    case "os-update": return "os-update";
    case "transactional-update": return "transactional-update";
    case "rpm-ostree": return "rpm-ostree";
    case "none": return "No update engine";
    default: return "";
  }
}

export function updatesModeLabel(mode: string): string {
  switch (mode) {
    case "off": return "Unattended updates off";
    case "check": return "Check only";
    case "security": return "Security updates";
    case "all": return "All updates";
    default: return "Not managed";
  }
}
