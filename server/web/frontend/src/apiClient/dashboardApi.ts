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

/* ── Raw API types ── */

interface RawNode {
  id: string;
  name: string;
  status: string;
  os_name?: string;
  os_version?: string;
  desktop_env?: string;
  agent_version?: string;
  node_group_id?: string;
  node_group_name?: string;
  last_seen?: string;
}

interface RawNodeGroup {
  id: string;
  name: string;
  description: string;
  node_count: number;
  created_at: string;
  updated_at: string;
}

interface RawPolicy {
  id: string;
  name: string;
  type: string;
  version: number;
  state: "draft" | "released" | "archived";
  updated_at: string;
}

interface RawBinding {
  id: string;
  policy_id: string;
  group_id: string;
  policy_name: string;
  policy_state: string;
  group_name: string;
  node_count: number;
  state: "enabled" | "disabled";
  priority: number;
}

/* ── Dashboard data types ── */

export interface FleetOverview {
  totalNodes: number;
  online: number;
  offline: number;
  unknown: number;
  agentVersions: Record<string, number>;
  osDistribution: Record<string, number>;
  desktopEnvironment: Record<string, number>;
}

export interface GroupSummary {
  id: string;
  name: string;
  description: string;
  totalNodes: number;
  onlineNodes: number;
  offlineNodes: number;
  enabledPolicies: number;
  disabledPolicies: number;
}

export interface NodesGroupsOverview {
  totalGroups: number;
  nodesWithoutGroup: number;
  groups: GroupSummary[];
}

export interface PoliciesOverview {
  totalPolicies: number;
  released: number;
  draft: number;
  archived: number;
  byType: Record<string, number>;
}

export interface BindingEntry {
  id: string;
  policyName: string;
  policyState: string;
  groupName: string;
  nodeCount: number;
  state: "enabled" | "disabled";
  priority: number;
}

export interface BindingsOverview {
  totalBindings: number;
  enabledBindings: number;
  disabledBindings: number;
  groupsWithBindings: number;
  groupsWithoutBindings: number;
  bindings: BindingEntry[];
}

export interface DashboardData {
  fleet: FleetOverview;
  nodesGroups: NodesGroupsOverview;
  policies: PoliciesOverview;
  bindings: BindingsOverview;
}

/* ── Fetch ── */

export async function fetchDashboardData(): Promise<DashboardData> {
  const hdrs = { headers: authHeaders() };

  const [nodesRes, groupsRes, policiesRes, bindingsRes] = await Promise.allSettled([
    apiRequest<RawNode[]>("/api/v1/nodes", hdrs),
    apiRequest<RawNodeGroup[]>("/api/v1/node-groups", hdrs),
    apiRequest<RawPolicy[]>("/api/v1/policies/all", hdrs),
    apiRequest<RawBinding[]>("/api/v1/policy-bindings", hdrs),
  ]);

  const rawNodes: RawNode[] = nodesRes.status === "fulfilled" ? nodesRes.value : [];
  const rawGroups: RawNodeGroup[] = groupsRes.status === "fulfilled" ? groupsRes.value : [];
  const rawPolicies: RawPolicy[] = policiesRes.status === "fulfilled" ? policiesRes.value : [];
  const rawBindings: RawBinding[] = bindingsRes.status === "fulfilled" ? bindingsRes.value : [];

  /* Fleet */
  const totalNodes = rawNodes.length;
  const online = rawNodes.filter((n) => n.status === "online").length;
  const offline = rawNodes.filter((n) => n.status === "offline").length;
  const unknown = rawNodes.filter((n) => n.status === "unknown").length;

  const agentVersions: Record<string, number> = {};
  const osDistribution: Record<string, number> = {};
  const desktopEnvironment: Record<string, number> = {};

  for (const node of rawNodes) {
    const ver = node.agent_version || "unknown";
    agentVersions[ver] = (agentVersions[ver] || 0) + 1;

    const osParts = [node.os_name, node.os_version].filter(Boolean);
    if (osParts.length > 0) {
      const key = osParts.join(" ");
      osDistribution[key] = (osDistribution[key] || 0) + 1;
    }

    if (node.desktop_env) {
      for (const de of node.desktop_env.split(",").map((s) => s.trim()).filter(Boolean)) {
        desktopEnvironment[de] = (desktopEnvironment[de] || 0) + 1;
      }
    }
  }

  /* Nodes & Groups */
  const nodesWithoutGroup = rawNodes.filter((n) => !n.node_group_id).length;

  const groupOnline: Record<string, number> = {};
  const groupOffline: Record<string, number> = {};
  const groupTotal: Record<string, number> = {};
  for (const node of rawNodes) {
    if (!node.node_group_id) continue;
    const gid = node.node_group_id;
    groupTotal[gid] = (groupTotal[gid] || 0) + 1;
    if (node.status === "online") {
      groupOnline[gid] = (groupOnline[gid] || 0) + 1;
    } else {
      groupOffline[gid] = (groupOffline[gid] || 0) + 1;
    }
  }

  const groupEnabledPolicies: Record<string, number> = {};
  const groupDisabledPolicies: Record<string, number> = {};
  for (const b of rawBindings) {
    if (b.state === "enabled") {
      groupEnabledPolicies[b.group_id] = (groupEnabledPolicies[b.group_id] || 0) + 1;
    } else {
      groupDisabledPolicies[b.group_id] = (groupDisabledPolicies[b.group_id] || 0) + 1;
    }
  }

  const groups: GroupSummary[] = rawGroups.map((g) => ({
    id: g.id,
    name: g.name,
    description: g.description,
    totalNodes: groupTotal[g.id] ?? g.node_count,
    onlineNodes: groupOnline[g.id] || 0,
    offlineNodes: groupOffline[g.id] || 0,
    enabledPolicies: groupEnabledPolicies[g.id] || 0,
    disabledPolicies: groupDisabledPolicies[g.id] || 0,
  }));

  /* Policies */
  const released = rawPolicies.filter((p) => p.state === "released").length;
  const draft = rawPolicies.filter((p) => p.state === "draft").length;
  const archived = rawPolicies.filter((p) => p.state === "archived").length;

  const byType: Record<string, number> = {};
  for (const p of rawPolicies) {
    byType[p.type] = (byType[p.type] || 0) + 1;
  }

  /* Bindings */
  const enabledBindings = rawBindings.filter((b) => b.state === "enabled").length;
  const disabledBindings = rawBindings.filter((b) => b.state === "disabled").length;
  const bindingGroupIds = new Set(rawBindings.map((b) => b.group_id));
  const groupsWithBindings = bindingGroupIds.size;
  const groupsWithoutBindings = Math.max(0, rawGroups.length - groupsWithBindings);

  const bindings: BindingEntry[] = rawBindings
    .slice()
    .sort((a, b) => a.group_name.localeCompare(b.group_name) || a.priority - b.priority)
    .map((b) => ({
      id: b.id,
      policyName: b.policy_name,
      policyState: b.policy_state,
      groupName: b.group_name,
      nodeCount: b.node_count,
      state: b.state,
      priority: b.priority,
    }));

  return {
    fleet: { totalNodes, online, offline, unknown, agentVersions, osDistribution, desktopEnvironment },
    nodesGroups: { totalGroups: rawGroups.length, nodesWithoutGroup, groups },
    policies: { totalPolicies: rawPolicies.length, released, draft, archived, byType },
    bindings: {
      totalBindings: rawBindings.length,
      enabledBindings,
      disabledBindings,
      groupsWithBindings,
      groupsWithoutBindings,
      bindings,
    },
  };
}
