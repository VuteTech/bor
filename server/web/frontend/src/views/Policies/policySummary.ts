// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Read-only "what does this policy set" rows, shared by the edit page's
 * Overview tab and the create wizard's Review step.
 */

import { FIREFOX_ALL_POLICIES } from "../../generated/proto/firefox_ui";
import { THUNDERBIRD_ALL_POLICIES } from "../../generated/proto/thunderbird_ui";
import { CHROME_ALL_POLICIES } from "../../generated/proto/chrome_ui";
import { EDGE_ALL_POLICIES } from "../../generated/proto/edge_ui";
import { KCM_MODULES, KCONFIG_ALL_POLICIES } from "./editors/kconfigModel";

export interface SettingsRow {
  setting: string;
  value: string;
  locked: string | null; // null = not applicable for this row
}

export function formatDisplayValue(val: unknown): string {
  if (val === undefined || val === null) return "—";
  if (typeof val === "boolean") return val ? "Yes" : "No";
  if (Array.isArray(val)) return val.length > 0 ? val.join(", ") : "(empty)";
  if (typeof val === "object") return JSON.stringify(val);
  return String(val);
}

export function buildSettingsRows(policyType: string, content: string): SettingsRow[] {
  let raw: unknown;
  try {
    raw = JSON.parse(content || "{}");
  } catch {
    return [];
  }

  const rows: SettingsRow[] = [];

  if (policyType === "Firefox") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of FIREFOX_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];

      if (policyDef.type === "object" && typeof val === "object" && val !== null) {
        const objVal = val as Record<string, unknown>;
        const lockedVal = "Locked" in objVal
          ? (objVal["Locked"] === true ? "Yes" : "No")
          : null;
        for (const field of policyDef.subFields || []) {
          if (field.key === "Locked") continue;
          rows.push({
            setting: `${policyDef.label} › ${field.label}`,
            value: formatDisplayValue(objVal[field.key]),
            locked: lockedVal,
          });
        }
      } else {
        rows.push({
          setting: policyDef.label,
          value: formatDisplayValue(val),
          locked: null,
        });
      }
    }
    return rows;
  }

  if (policyType === "Thunderbird") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of THUNDERBIRD_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];

      if (policyDef.type === "object" && typeof val === "object" && val !== null) {
        const objVal = val as Record<string, unknown>;
        const lockedVal = "Locked" in objVal
          ? (objVal["Locked"] === true ? "Yes" : "No")
          : null;
        for (const field of policyDef.subFields || []) {
          if (field.key === "Locked") continue;
          rows.push({
            setting: `${policyDef.label} › ${field.label}`,
            value: formatDisplayValue(objVal[field.key]),
            locked: lockedVal,
          });
        }
      } else {
        rows.push({
          setting: policyDef.label,
          value: formatDisplayValue(val),
          locked: null,
        });
      }
    }
    return rows;
  }

  if (policyType === "Chrome") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of CHROME_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];
      let displayVal: string;
      if (policyDef.type === "list" && Array.isArray(val)) {
        displayVal = val.length > 0 ? val.join(", ") : "(empty)";
      } else if (policyDef.type === "integer-enum" && policyDef.intOptions) {
        const opt = policyDef.intOptions.find(o => o.value === val);
        displayVal = opt ? opt.label : formatDisplayValue(val);
      } else if (policyDef.type === "string-enum" && policyDef.stringOptions) {
        const opt = policyDef.stringOptions.find(o => o === val);
        displayVal = opt ? opt : formatDisplayValue(val);
      } else if (policyDef.type === "json") {
        displayVal = typeof val === "string" ? val : JSON.stringify(val);
      } else {
        displayVal = formatDisplayValue(val);
      }
      rows.push({
        setting: policyDef.label,
        value: displayVal,
        locked: null,
      });
    }
    return rows;
  }

  if (policyType === "Edge") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of EDGE_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];
      let displayVal: string;
      if (policyDef.type === "list" && Array.isArray(val)) {
        displayVal = val.length > 0 ? val.join(", ") : "(empty)";
      } else if (policyDef.type === "integer-enum" && policyDef.intOptions) {
        const opt = policyDef.intOptions.find(o => o.value === val);
        displayVal = opt ? opt.label : formatDisplayValue(val);
      } else if (policyDef.type === "string-enum" && policyDef.stringOptions) {
        const opt = policyDef.stringOptions.find(o => o === val);
        displayVal = opt ? opt : formatDisplayValue(val);
      } else if (policyDef.type === "json") {
        displayVal = typeof val === "string" ? val : JSON.stringify(val);
      } else {
        displayVal = formatDisplayValue(val);
      }
      rows.push({
        setting: policyDef.label,
        value: displayVal,
        locked: null,
      });
    }
    return rows;
  }

  if (policyType === "Kconfig") {
    const parsed = raw as Record<string, unknown>;
    const enforcedFields = Array.isArray(parsed.enforcedFields) ? (parsed.enforcedFields as string[]) : [];

    // URL restrictions
    if (Array.isArray(parsed.urlRestrictions)) {
      (parsed.urlRestrictions as Record<string, unknown>[]).forEach((r, i) => {
        const proto = (r.protocol as string) || "*";
        const host = (r.host as string) || "*";
        const path = (r.path as string) ? `/${r.path}` : "";
        const summary = `${r.action} ${proto}://${host}${path} → ${r.enabled ? "allow" : "deny"}`;
        rows.push({ setting: `Security › URL Restrictions › rule_${i + 1}`, value: summary, locked: "Yes" });
      });
    }

    // KCM restrictions
    if (Array.isArray(parsed.kcmRestrictions)) {
      for (const modId of parsed.kcmRestrictions as string[]) {
        const mod = KCM_MODULES.find(m => m.id === modId);
        rows.push({
          setting: `System Settings Restrictions › ${mod ? mod.label : modId}`,
          value: "Restricted",
          locked: "Yes",
        });
      }
    }

    // All other typed fields
    for (const def of KCONFIG_ALL_POLICIES) {
      if (def.type === "url-restrictions" || def.type === "kcm-restrictions") continue;
      if (!(def.key in parsed) || parsed[def.key] === null || parsed[def.key] === undefined) continue;
      rows.push({
        setting: `${def.group} › ${def.label}`,
        value: formatDisplayValue(parsed[def.key]),
        locked: enforcedFields.includes(def.key) ? "Yes" : "No",
      });
    }

    return rows;
  }

  if (policyType === "Polkit") {
    type PkRule = {
      description?: string;
      action_ids?: string[];
      action_prefixes?: string[];
      result?: string;
      subject?: { in_group?: string; negate_group?: boolean; is_user?: string; require_local?: boolean; require_active?: boolean };
    };
    const pk = raw as { rules?: PkRule[] };
    const pkRules = Array.isArray(pk?.rules) ? pk.rules : [];
    for (const [idx, rule] of pkRules.entries()) {
      const actions = [
        ...(rule.action_ids ?? []),
        ...(rule.action_prefixes ?? []).map((pfx) => `${pfx}*`),
      ];
      const result = (rule.result ?? "").replace("POLKIT_RESULT_", "").toLowerCase().replace(/_/g, " ") || "not set";
      const subj = rule.subject ?? {};
      const who: string[] = [];
      if (subj.in_group) who.push(`${subj.negate_group ? "not in group" : "group"} ${subj.in_group}`);
      if (subj.is_user) who.push(`user ${subj.is_user}`);
      if (subj.require_local) who.push("local only");
      if (subj.require_active) who.push("active session only");
      rows.push({
        setting: `Rule ${idx + 1} › ${rule.description || actions.join(", ") || "(no actions)"}`,
        value: `${result} — ${who.length > 0 ? who.join(", ") : "everyone"}${rule.description && actions.length > 0 ? ` (${actions.join(", ")})` : ""}`,
        locked: null,
      });
    }
    return rows;
  }

  if (policyType === "Firewalld") {
    type FwPortLike = { port?: string | number; protocol?: string; toPort?: string | number; toAddr?: string };
    const fw = raw as {
      zone?: string; target?: string; services?: string[]; ports?: FwPortLike[]; sourcePorts?: FwPortLike[];
      protocols?: string[]; icmpBlocks?: string[]; icmpBlockInversion?: boolean; masquerade?: boolean;
      forwardPorts?: FwPortLike[]; richRules?: string[];
    };
    const fmtPort = (p: FwPortLike) => `${p.port ?? "?"}/${p.protocol ?? "?"}`;
    if (fw.zone) rows.push({ setting: "Zone", value: fw.zone, locked: null });
    if (fw.target) rows.push({ setting: "Target", value: fw.target, locked: null });
    if (fw.services?.length) rows.push({ setting: "Services", value: fw.services.join(", "), locked: null });
    if (fw.ports?.length) rows.push({ setting: "Ports", value: fw.ports.map(fmtPort).join(", "), locked: null });
    if (fw.sourcePorts?.length) rows.push({ setting: "Source ports", value: fw.sourcePorts.map(fmtPort).join(", "), locked: null });
    if (fw.protocols?.length) rows.push({ setting: "Protocols", value: fw.protocols.join(", "), locked: null });
    if (fw.icmpBlocks?.length) rows.push({ setting: "ICMP blocks", value: fw.icmpBlocks.join(", "), locked: null });
    if (fw.icmpBlockInversion !== undefined) rows.push({ setting: "ICMP block inversion", value: fw.icmpBlockInversion ? "Yes" : "No", locked: null });
    if (fw.masquerade !== undefined) rows.push({ setting: "Masquerade", value: fw.masquerade ? "Yes" : "No", locked: null });
    if (fw.forwardPorts?.length) {
      rows.push({
        setting: "Forward ports",
        value: fw.forwardPorts.map((p) => `${fmtPort(p)} → ${p.toAddr ?? ""}${p.toPort !== undefined ? `:${p.toPort}` : ""}`).join(", "),
        locked: null,
      });
    }
    for (const [i, rr] of (fw.richRules ?? []).entries()) {
      rows.push({ setting: `Rich rule ${i + 1}`, value: rr, locked: null });
    }
    return rows;
  }

  if (policyType === "SessionAccess") {
    type SaWin = { days?: string[]; start?: string; end?: string };
    type SaRule = { description?: string; users?: string[]; groups?: string[]; windows?: SaWin[]; endAction?: string; warnMinutes?: number[] };
    const sa = raw as { rules?: SaRule[]; enforcePam?: boolean; includeSsh?: boolean };
    for (const [idx, rule] of (sa.rules ?? []).entries()) {
      const targets = [
        ...(rule.users ?? []).map((u) => `user ${u}`),
        ...(rule.groups ?? []).map((g) => `group ${g}`),
      ];
      const windows = (rule.windows ?? []).map((w) => `${(w.days ?? []).join("/")} ${w.start ?? "?"}–${w.end ?? "?"}`);
      const tail = rule.endAction ? `, then ${rule.endAction.toLowerCase().replace(/_/g, " ")}` : "";
      rows.push({
        setting: `Rule ${idx + 1} › ${rule.description || targets.join(", ") || "(no users or groups)"}`,
        value: `${windows.length > 0 ? windows.join("; ") : "no allowed periods"}${tail}`,
        locked: null,
      });
    }
    if (sa.enforcePam !== undefined) rows.push({ setting: "PAM enforcement (pam_time)", value: sa.enforcePam ? "Yes" : "No", locked: null });
    if (sa.includeSsh !== undefined) rows.push({ setting: "Apply to SSH logins", value: sa.includeSsh ? "Yes" : "No", locked: null });
    return rows;
  }

  // Normalize to array for multi-setting support (backward compat with single-object)
  const items: Record<string, unknown>[] = Array.isArray(raw)
    ? raw
    : [raw as Record<string, unknown>];

  if (policyType === "Dconf") {
    // DConf content shape: { entries: [{schema_id, key, value, lock, path?},...], db_name }
    const dconfContent = raw as { entries?: Record<string, unknown>[]; db_name?: string };
    const entries = Array.isArray(dconfContent?.entries) ? dconfContent.entries : [];
    for (const [idx, entry] of entries.entries()) {
      const schemaId = String(entry["schema_id"] ?? "");
      const keyName  = String(entry["key"]       ?? "");
      const label    = schemaId ? `${schemaId} › ${keyName}` : (keyName || `Entry ${idx + 1}`);
      const lockedVal = entry["lock"] !== undefined
        ? (entry["lock"] === "true" || entry["lock"] === true ? "Yes" : "No")
        : null;
      rows.push({
        setting: label,
        value: formatDisplayValue(entry["value"]),
        locked: lockedVal,
      });
    }
    return rows;
  }

  if (policyType === "Package") {
    const pkgContent = raw as { repositories?: { id?: string; type?: string; enabled?: boolean }[]; packages?: { name?: string; state?: string; version?: string }[]; updateCache?: boolean; allowDowngrade?: boolean };
    const pkgRepos = Array.isArray(pkgContent?.repositories) ? pkgContent.repositories : [];
    const pkgPkgs  = Array.isArray(pkgContent?.packages)     ? pkgContent.packages     : [];
    rows.push({ setting: "Repositories", value: `${pkgRepos.length} defined`, locked: null });
    for (const repo of pkgRepos) {
      const typeShort = (repo.type ?? "").replace("REPOSITORY_TYPE_", "");
      rows.push({ setting: `  Repo › ${repo.id ?? "(unnamed)"}`, value: `${typeShort}${repo.enabled === false ? " (disabled)" : ""}`, locked: null });
    }
    for (const pkg of pkgPkgs) {
      const stateShort = (pkg.state ?? "").replace("PACKAGE_STATE_", "").toLowerCase();
      const versionPart = pkg.version ? ` @ ${pkg.version}` : "";
      rows.push({ setting: `Package › ${pkg.name ?? "(unnamed)"}`, value: stateShort + versionPart, locked: null });
    }
    if (pkgContent.updateCache !== undefined) {
      rows.push({ setting: "Options › Refresh cache", value: pkgContent.updateCache ? "yes" : "no", locked: null });
    }
    return rows;
  }

  // Custom / unknown: show raw key-value pairs
  for (const [idx, parsed] of items.entries()) {
    const prefix = items.length > 1 ? `Setting ${idx + 1} › ` : "";
    for (const [key, val] of Object.entries(parsed)) {
      rows.push({
        setting: prefix + key,
        value: formatDisplayValue(val),
        locked: null,
      });
    }
  }

  return rows;
}
