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
