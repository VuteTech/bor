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

/** One systemd unit from the catalogue nodes report. */
export interface SystemdCatalogueUnit {
  name: string;
  description: string;
  /** service | timer | socket | target | path */
  type: string;
  /** enabled | disabled | static | masked | indirect | generated | ... */
  unitFileState: string;
  /** enabled | disabled | "" */
  preset: string;
  activeState: string;
  subState: string;
  hasInstall: boolean;
  onCalendar: string[];
  /** Timers, sockets and paths: the unit they activate. */
  triggers: string;
  fragmentPath: string;
  serviceType: string;
  /** Services: the exposure level systemd-analyze security computed ("9.6"), "" when unknown. */
  securityExposure: string;
  /** Services: SAFE | OK | MEDIUM | EXPOSED | UNSAFE, "" when unknown. */
  securityPredicate: string;
  /** The server flags units whose stop or mask can cut users off. */
  risky: boolean;
  /** How many nodes reported a unit of this name (1 in a per-node listing). */
  nodeCount: number;
  reportedAt: string;
}

export interface SystemdUnitsQuery {
  /** Substring of the name or description. */
  q?: string;
  /** service | timer | socket | target | path */
  type?: string;
  /** One node's units instead of the union over every node. */
  nodeId?: string;
}

/**
 * The systemd units nodes reported: the union over every node, or the
 * units of one node with its current states.
 */
export async function fetchSystemdUnits(query: SystemdUnitsQuery = {}): Promise<SystemdCatalogueUnit[]> {
  const qs = new URLSearchParams();
  if (query.q) qs.set("q", query.q);
  if (query.type) qs.set("type", query.type);
  if (query.nodeId) qs.set("node_id", query.nodeId);
  const s = qs.toString();
  return apiRequest<SystemdCatalogueUnit[]>(`/api/v1/systemd/units${s ? `?${s}` : ""}`, {
    headers: authHeaders(),
  });
}

/** Result of the server-side calendar expression preview. */
export interface SystemdCalendarPreview {
  /** False when the server has no systemd-analyze; nothing else is set then. */
  available: boolean;
  normalized?: string;
  /** Next elapse times as systemd prints them, in the server's time zone. */
  next: string[];
}

/**
 * Evaluates a calendar expression with the server's systemd-analyze. A
 * malformed expression rejects with the parser's message.
 */
export async function previewSystemdCalendar(expression: string, iterations = 5): Promise<SystemdCalendarPreview> {
  return apiRequest<SystemdCalendarPreview>("/api/v1/systemd/calendar-preview", {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify({ expression, iterations }),
  });
}

/**
 * The units one node reported, for the Services & timers section of the
 * node details (a node view; the catalogue endpoint above is a policy view).
 */
export async function fetchNodeSystemdUnits(nodeId: string, query: Omit<SystemdUnitsQuery, "nodeId"> = {}): Promise<SystemdCatalogueUnit[]> {
  const qs = new URLSearchParams();
  if (query.q) qs.set("q", query.q);
  if (query.type) qs.set("type", query.type);
  const s = qs.toString();
  return apiRequest<SystemdCatalogueUnit[]>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/systemd/units${s ? `?${s}` : ""}`, {
    headers: authHeaders(),
  });
}

export type SystemdUnitActionVerb = "start" | "stop" | "restart";

/** The outcome of an on-demand unit action once the agent answered. */
export interface SystemdUnitActionResult {
  id: string;
  unit: string;
  action: SystemdUnitActionVerb;
  success: boolean;
  /** The job result or error, possibly with a note that a bound policy will reconcile the unit. */
  message: string;
  /** The unit after the action, when the agent reported it. */
  unitState?: SystemdCatalogueUnit;
}

/**
 * Starts, stops or restarts a unit on a node now. The request waits for
 * the agent's answer; it rejects with the server's message when the node
 * is not connected (503), the agent did not answer in time (504) or the
 * caller lacks systemd:operate (403).
 */
export async function runSystemdUnitAction(nodeId: string, unit: string, action: SystemdUnitActionVerb): Promise<SystemdUnitActionResult> {
  return apiRequest<SystemdUnitActionResult>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/systemd/actions`, {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify({ unit, action }),
  });
}
