// SPDX-License-Identifier: LGPL-3.0-or-later
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

/* ── Polkit types ── */

export interface PolkitAction {
  action_id: string;
  description?: string;
  message?: string;
  vendor?: string;
  default_any?: string;
  default_inactive?: string;
  default_active?: string;
}

export interface PolkitSubjectFilter {
  in_group?: string;
  negate_group?: boolean;
  is_user?: string;
  require_local?: boolean;
  require_active?: boolean;
  system_unit?: string;
}

export type PolkitResultValue =
  | "POLKIT_RESULT_NOT_SET"
  | "POLKIT_RESULT_NO"
  | "POLKIT_RESULT_YES"
  | "POLKIT_RESULT_AUTH_SELF"
  | "POLKIT_RESULT_AUTH_SELF_KEEP"
  | "POLKIT_RESULT_AUTH_ADMIN"
  | "POLKIT_RESULT_AUTH_ADMIN_KEEP";

export interface PolkitActionCondition {
  key: string;
  value: string;
  negate?: boolean;
}

export interface PolkitRule {
  description: string;
  action_ids: string[];
  action_prefixes: string[];
  subject?: PolkitSubjectFilter;
  result: PolkitResultValue;
  action_conditions?: PolkitActionCondition[];
}

export interface PolkitPolicyContent {
  rules: PolkitRule[];
}

/* ── API calls ── */

export async function fetchPolkitActions(nodeId?: string): Promise<PolkitAction[]> {
  const qs = nodeId ? `?node_id=${encodeURIComponent(nodeId)}` : "";
  return apiRequest<PolkitAction[]>(`/api/v1/polkit/actions${qs}`, {
    headers: authHeaders(),
  });
}

/* ── Content helpers ── */

export function parsePolkitContent(raw: string): PolkitPolicyContent {
  try {
    const parsed = JSON.parse(raw || "{}") as Partial<PolkitPolicyContent> & { file_prefix?: string };
    const rules = Array.isArray(parsed.rules) ? parsed.rules : [];
    return {
      // Normalize sparse rules: content created via the API (protojson-style)
      // legitimately omits empty arrays, and the editor assumes they exist.
      rules: rules.map((r) => ({
        ...r,
        action_ids: r.action_ids ?? [],
        action_prefixes: r.action_prefixes ?? [],
        action_conditions: r.action_conditions ?? [],
      })),
    };
  } catch {
    return { rules: [] };
  }
}

export function serializePolkitContent(content: PolkitPolicyContent): string {
  return JSON.stringify(content, null, 2);
}

/* ── JS preview generator ── */

const RESULT_JS_MAP: Record<PolkitResultValue, string> = {
  POLKIT_RESULT_NOT_SET:        "polkit.Result.NOT_SET",
  POLKIT_RESULT_NO:             "polkit.Result.NO",
  POLKIT_RESULT_YES:            "polkit.Result.YES",
  POLKIT_RESULT_AUTH_SELF:      "polkit.Result.AUTH_SELF",
  POLKIT_RESULT_AUTH_SELF_KEEP: "polkit.Result.AUTH_SELF_KEEP",
  POLKIT_RESULT_AUTH_ADMIN:     "polkit.Result.AUTH_ADMIN",
  POLKIT_RESULT_AUTH_ADMIN_KEEP:"polkit.Result.AUTH_ADMIN_KEEP",
};

export function polkitContentToJS(content: PolkitPolicyContent): string {
  // Header matches the agent's polkitManagedHeader (timestamp added at apply time).
  const lines: string[] = [
    "// This file is managed by Bor. Do not edit manually.",
    "// Changes will be overwritten by policy enforcement.",
    "// Generated: <timestamp added by agent>",
    "",
  ];

  for (const rule of content.rules) {
    if (rule.description) {
      lines.push(`// ${rule.description}`);
    }

    // Collect action parts — any one must match (OR).
    const actionParts: string[] = [];
    for (const id of (rule.action_ids ?? []).filter(Boolean)) {
      actionParts.push(`action.id === ${JSON.stringify(id)}`);
    }
    for (const prefix of (rule.action_prefixes ?? []).filter(Boolean)) {
      actionParts.push(`action.id.indexOf(${JSON.stringify(prefix)}) === 0`);
    }

    // Collect subject conditions — all must match (AND).
    const subjectConds: string[] = [];
    const s = rule.subject;
    if (s) {
      if (s.in_group) {
        const check = `subject.isInGroup(${JSON.stringify(s.in_group)})`;
        subjectConds.push(s.negate_group ? `!${check}` : check);
      }
      if (s.is_user)       { subjectConds.push(`subject.user === ${JSON.stringify(s.is_user)}`); }
      if (s.require_local)  { subjectConds.push("subject.local === true"); }
      if (s.require_active) { subjectConds.push("subject.active === true"); }
      if (s.system_unit)   { subjectConds.push(`subject.system_unit === ${JSON.stringify(s.system_unit)}`); }
    }

    // Build outer if condition — mirrors Go writeRuleJS exactly.
    // Multiple action parts are wrapped in parens so || binds before && with subject.
    const outerParts: string[] = [];
    if (actionParts.length === 0) {
      outerParts.push("    true /* no action filter */");
    } else if (actionParts.length === 1) {
      outerParts.push(`    ${actionParts[0]}`);
    } else {
      const orLines = actionParts.map(p => `      ${p}`).join(" ||\n");
      outerParts.push(`    (\n${orLines}\n    )`);
    }
    for (const sc of subjectConds) {
      outerParts.push(`    ${sc}`);
    }

    const resultVal = RESULT_JS_MAP[rule.result] ?? "polkit.Result.NOT_SET";

    lines.push("polkit.addRule(function(action, subject) {");
    lines.push(`  if (\n${outerParts.join(" &&\n")}\n  ) {`);

    // Action variable guards — early-return statements matching Go generator output.
    for (const cond of (rule.action_conditions ?? [])) {
      if (!cond.key) continue;
      const k = JSON.stringify(cond.key);
      const v = JSON.stringify(cond.value ?? "");
      if (cond.negate) {
        lines.push(`    if (action.lookup(${k}) === ${v}) { return; }`);
      } else {
        lines.push(`    if (action.lookup(${k}) !== ${v}) { return; }`);
      }
    }

    lines.push(`    return ${resultVal};`);
    lines.push("  }");
    lines.push("});");
    lines.push("");
  }

  return lines.join("\n");
}
