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

export type UpdatesEngine = "none" | "dnf-automatic" | "unattended-upgrades" | "os-update" | "rpm-ostree" | "";

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

export function updatesEngineLabel(engine: string): string {
  switch (engine) {
    case "dnf-automatic": return "dnf-automatic";
    case "unattended-upgrades": return "unattended-upgrades";
    case "os-update": return "os-update";
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
