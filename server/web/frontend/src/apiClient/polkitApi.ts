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

export interface PolkitRule {
  description: string;
  action_ids: string[];
  action_prefixes: string[];
  subject?: PolkitSubjectFilter;
  result: PolkitResultValue;
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
    return {
      rules: Array.isArray(parsed.rules) ? parsed.rules : [],
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
  const lines: string[] = [
    "// This file is managed by Bor. Do not edit manually.",
    "// Changes will be overwritten on next policy apply.",
    "",
  ];

  for (const rule of content.rules) {
    if (rule.description) {
      lines.push(`// ${rule.description}`);
    }

    // Build action match condition
    const matchParts: string[] = [];

    if (rule.action_ids && rule.action_ids.length > 0) {
      const ids = rule.action_ids.filter(Boolean).map(id => JSON.stringify(id));
      if (ids.length === 1) {
        matchParts.push(`action.id == ${ids[0]}`);
      } else if (ids.length > 1) {
        matchParts.push(`[${ids.join(", ")}].indexOf(action.id) >= 0`);
      }
    }

    if (rule.action_prefixes && rule.action_prefixes.length > 0) {
      for (const prefix of rule.action_prefixes.filter(Boolean)) {
        matchParts.push(`action.id.startsWith(${JSON.stringify(prefix)})`);
      }
    }

    const actionMatch = matchParts.length > 0
      ? matchParts.join(" ||\n        ")
      : "true /* no action filter */";

    // Build subject condition
    const subjectParts: string[] = [];
    const s = rule.subject;
    if (s) {
      if (s.in_group) {
        const check = `subject.isInGroup(${JSON.stringify(s.in_group)})`;
        subjectParts.push(s.negate_group ? `!${check}` : check);
      }
      if (s.is_user) {
        subjectParts.push(`subject.user == ${JSON.stringify(s.is_user)}`);
      }
      if (s.require_local) {
        subjectParts.push("subject.local");
      }
      if (s.require_active) {
        subjectParts.push("subject.active");
      }
      if (s.system_unit) {
        subjectParts.push(`subject.systemUnit == ${JSON.stringify(s.system_unit)}`);
      }
    }

    const subjectMatch = subjectParts.length > 0
      ? subjectParts.join(" &&\n        ")
      : null;

    const resultVal = RESULT_JS_MAP[rule.result] ?? "polkit.Result.NOT_SET";

    lines.push("polkit.addRule(function(action, subject) {");
    lines.push(`    if (${actionMatch}) {`);
    if (subjectMatch) {
      lines.push(`        if (${subjectMatch}) {`);
      lines.push(`            return ${resultVal};`);
      lines.push("        }");
    } else {
      lines.push(`        return ${resultVal};`);
    }
    lines.push("    }");
    lines.push("});");
    lines.push("");
  }

  return lines.join("\n");
}
