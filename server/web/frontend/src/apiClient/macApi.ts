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

export type MacLsm = "selinux" | "apparmor" | "none" | "";
export type MacItemKind = "boolean" | "module" | "profile";

/** One SELinux boolean as a node reported it. */
export interface SelinuxBooleanInfo {
  name: string;
  current: boolean;
  persistent: boolean;
  description?: string;
}

/** One SELinux policy module as a node reported it. */
export interface SelinuxModuleInfo {
  name: string;
  priority: number;
  enabled: boolean;
  lang: string;
}

/** One AppArmor profile as a node reported it (a file, a loaded profile or both). */
export interface ApparmorProfileInfo {
  name: string;
  file: string;
  /** enforce | complain | unconfined | kill | prompt | "" (not loaded) */
  loadedMode: string;
  /** The mode flag inside the file; "" for enforce. */
  fileMode: string;
  /** vendor | bor | snapd | docker | lxd | libvirt | runtime */
  managedBy: string;
  hasLocalInclude: boolean;
  attachedProcesses: number;
  unconfinedWithProfile: boolean;
  attachments: string[];
  foreignDisableLink: boolean;
  foreignComplainLink: boolean;
  attachmentConflict: boolean;
}

/** One catalogue or node inventory row: the reported item plus the server's curated knowledge. */
export interface MacCatalogueItem {
  lsm: MacLsm;
  kind: MacItemKind;
  name: string;
  nodeCount: number;
  reportedAt?: string;
  info: Partial<SelinuxBooleanInfo & SelinuxModuleInfo & ApparmorProfileInfo>;
  group?: string;
  description?: string;
  restrictive?: boolean;
  riskyOn?: boolean;
  riskyOff?: boolean;
  risky?: boolean;
  protected?: boolean;
  dynamic?: boolean;
  /** From the server's curated list only; no node reported it. */
  curated?: boolean;
}

export interface MacCatalogueQuery {
  lsm: "selinux" | "apparmor";
  kind: MacItemKind;
  q?: string;
  nodeId?: string;
  limit?: number;
}

/** SELinux booleans and modules or AppArmor profiles: the union over every node plus the curated lists, or one node's view. */
export async function fetchMacCatalogue(query: MacCatalogueQuery): Promise<MacCatalogueItem[]> {
  const qs = new URLSearchParams({ lsm: query.lsm, kind: query.kind });
  if (query.q) qs.set("q", query.q);
  if (query.nodeId) qs.set("node_id", query.nodeId);
  if (query.limit) qs.set("limit", String(query.limit));
  return apiRequest<MacCatalogueItem[]>(`/api/v1/mac/catalogue?${qs.toString()}`, { headers: authHeaders() });
}

export interface SelinuxStatus {
  active: boolean;
  mode: string;
  configMode: string;
  policyType: string;
  policyVersion: number;
  booleanCount: number;
  moduleCount: number;
  semanagePresent: boolean;
  audit2AllowPresent: boolean;
  setroubleshootPresent: boolean;
  policyloadLocked: boolean;
  storeRoot: string;
  bootFlags: string[];
  relabelPending: boolean;
  toolsVersion: string;
}

export interface ApparmorStatus {
  enabled: boolean;
  inLsmList: boolean;
  parserVersion: string;
  usernsMediation: boolean;
  serviceActive: boolean;
  serviceEnabled: boolean;
  enforceCount: number;
  complainCount: number;
  unconfinedCount: number;
  killCount: number;
  /** -1 when the kernel has no such switch. */
  restrictUserns: number;
  restrictUnconfined: number;
  bootFlags: string[];
  profileFileCount: number;
}

/** Denials of one subject over the report window, redacted on the node. */
export interface MacDenialGroup {
  subject: string;
  operation: string;
  class: string;
  target: string;
  count: number;
  lastSeen?: string;
  example: string;
  permissive: boolean;
  /** AppArmor: the executable that hit the user namespace restriction. */
  execpath: string;
}

export interface MacNodeStatus {
  lsm: MacLsm;
  mode: string;
  reportedAt?: string;
  denialsSince?: string;
  selinux?: SelinuxStatus;
  apparmor?: ApparmorStatus;
  denials: MacDenialGroup[];
}

/** The mandatory access control status and denial summary of one node. */
export async function fetchNodeMac(nodeId: string): Promise<MacNodeStatus> {
  return apiRequest<MacNodeStatus>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/mac`, { headers: authHeaders() });
}

/** The booleans, modules or profiles one node reported. */
export async function fetchNodeMacItems(nodeId: string, query: { kind?: MacItemKind; q?: string; limit?: number } = {}): Promise<MacCatalogueItem[]> {
  const qs = new URLSearchParams();
  if (query.kind) qs.set("kind", query.kind);
  if (query.q) qs.set("q", query.q);
  if (query.limit) qs.set("limit", String(query.limit));
  const s = qs.toString();
  return apiRequest<MacCatalogueItem[]>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/mac/items${s ? `?${s}` : ""}`, { headers: authHeaders() });
}

export type MacActionKind = "selinux_propose_module" | "apparmor_propose_rules";

export interface MacActionResult {
  id: string;
  kind: MacActionKind;
  subject: string;
  success: boolean;
  message: string;
  /** The proposed policy source. */
  text: string;
}

/**
 * Asks a node for a policy proposal built from its recent denials: an
 * audit2allow module for an SELinux domain, or AppArmor rules for a
 * profile. Nothing is applied on the node. Needs mac:operate.
 */
export async function runMacAction(nodeId: string, kind: MacActionKind, subject: string, windowHours = 24): Promise<MacActionResult> {
  return apiRequest<MacActionResult>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/mac/actions`, {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify({ kind, subject, windowHours }),
  });
}

/** Human label of an LSM id. */
export function macLsmLabel(lsm: MacLsm | undefined): string {
  switch (lsm) {
    case "selinux": return "SELinux";
    case "apparmor": return "AppArmor";
    case "none": return "None";
    default: return "";
  }
}
