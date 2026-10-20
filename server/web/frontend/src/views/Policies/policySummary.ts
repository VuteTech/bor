// SPDX-License-Identifier: AGPL-3.0-only
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
import {
  FILL_MODE_LABELS,
  HISTORY_MODE_LABELS,
  KCM_MODULES,
  catalogueEntryForSetting,
  hasKonsoleProfileContent,
  hasLayoutContent,
  isExtraWrite,
  konsoleProfileFileName,
  layoutBaseTheme,
  parseKConfigContent,
} from "./editors/kconfigModel";
import { boolDisplay, groupPathLabel, targetLabel } from "./editors/kconfigCatalogue";
import { describeTaskEntry, describeUnitEntry, parseSystemdContent } from "./systemdModel";
import { parseSelinuxContent } from "./selinuxModel";
import { apparmorModeLabel, parseApparmorContent } from "./apparmorModel";
import { parseUpdatesContent, updatesModeLabel, updatesRebootLabel } from "./updatesModel";

export interface SettingsRow {
  setting: string;
  value: string;
  locked: string | null; // null = not applicable for this row
}

export function formatDisplayValue(val: unknown): string {
  if (val === undefined || val === null) return "-";
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
    // Older typed content is shown through the same upgrade the editor uses.
    const kc = parseKConfigContent(content).content;

    for (const s of kc.settings) {
      if (isExtraWrite(s)) continue; // written together with its main setting
      const entry = catalogueEntryForSetting(s);
      const setting = entry ? `${entry.category} › ${entry.label}` : `Custom › ${targetLabel(s.file, s.groupPath, s.key)}`;
      let value: string;
      if (s.delete) value = "Deleted ([$d])";
      else if (entry?.type === "Bool") value = boolDisplay(entry, s.value ?? "");
      else value = (s.value ?? "") === "" ? "(empty)" : (s.value as string);
      if (s.locale) value += ` [${s.locale}]`;
      if (s.expand) value += " [$e]";
      rows.push({ setting, value, locked: s.immutable ? "Yes" : "No" });
    }

    const showOnly = kc.kcmMode === "KCM_SHOW_ONLY_LISTED";
    for (const modId of kc.kcmRestrictions) {
      const mod = KCM_MODULES.find((m) => m.id === modId);
      rows.push({
        setting: `System Settings Modules › ${mod ? mod.label : modId}`,
        value: showOnly ? "Visible (every other module hidden)" : "Hidden",
        locked: "Yes",
      });
    }

    kc.urlRestrictions.forEach((r, i) => {
      const proto = r.protocol || "*";
      const host = r.host || "*";
      const path = r.path ? `/${r.path}` : "";
      rows.push({ setting: `URL Restrictions › rule_${i + 1}`, value: `${r.action} ${proto}://${host}${path} -> ${r.enabled ? "allow" : "deny"}`, locked: "Yes" });
    });

    if (kc.fileImmutableKdeglobals) {
      rows.push({ setting: `Advanced › kdeglobals ${groupPathLabel(["$i"])}`, value: "Whole file immutable", locked: "Yes" });
    }

    if (kc.unixGroups.length > 0) {
      rows.push({ setting: "Scope › Unix groups", value: kc.unixGroups.join(", "), locked: null });
    }

    if (hasKonsoleProfileContent(kc.konsoleProfile)) {
      const k = kc.konsoleProfile;
      const bits: string[] = [];
      if (k.command.trim()) bits.push(`command ${k.command.trim()}`);
      bits.push((HISTORY_MODE_LABELS[k.historyMode] ?? k.historyMode).toLowerCase() + (k.historyMode === "1" && k.historySize > 0 ? ` (${k.historySize} lines)` : ""));
      if (k.colorScheme.trim()) bits.push(`colour scheme ${k.colorScheme.trim()}`);
      if (k.font.trim()) bits.push(`font ${k.font.trim()}`);
      if (k.setDefault || k.lock) bits.push("default profile");
      rows.push({ setting: `Applications › Konsole profile ${konsoleProfileFileName(k)}`, value: bits.join(", "), locked: k.lock ? "Yes" : "No" });
    }

    if (kc.hiddenApplications.length > 0) {
      rows.push({ setting: "Applications › Hidden applications", value: kc.hiddenApplications.join(", "), locked: null });
    }

    if (hasLayoutContent(kc.layout)) {
      const l = kc.layout;
      if (l.wallpaperImage.trim()) {
        rows.push({ setting: "Desktop Layout › Wallpaper image", value: `${l.wallpaperImage.trim()} (${FILL_MODE_LABELS[l.wallpaperFillMode || "2"] ?? l.wallpaperFillMode})`, locked: l.lockPackage ? "Yes" : "No" });
      }
      if (l.layoutScript.trim()) {
        rows.push({ setting: "Desktop Layout › Panel layout script", value: `${l.layoutScript.trim().split("\n").length} line(s)`, locked: l.lockPackage ? "Yes" : "No" });
      }
      const base = layoutBaseTheme(kc);
      rows.push({ setting: "Desktop Layout › Base theme", value: base ? `${base.value} (defaults copied, wallpaper replaced)` : "Breeze fallback", locked: base ? (base.immutable ? "Yes" : "No") : null });
      const how: string[] = ["new users"];
      if (l.lockPackage) how.push("existing users at every login");
      else if (l.applyOnce) how.push("existing users once");
      if (l.applyLive) how.push("logged-in sessions now");
      rows.push({ setting: "Desktop Layout › Applied to", value: how.join(", "), locked: null });
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
        value: `${result} - ${who.length > 0 ? who.join(", ") : "everyone"}${rule.description && actions.length > 0 ? ` (${actions.join(", ")})` : ""}`,
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
        value: fw.forwardPorts.map((p) => `${fmtPort(p)} -> ${p.toAddr ?? ""}${p.toPort !== undefined ? `:${p.toPort}` : ""}`).join(", "),
        locked: null,
      });
    }
    for (const [i, rr] of (fw.richRules ?? []).entries()) {
      rows.push({ setting: `Rich rule ${i + 1}`, value: rr, locked: null });
    }
    return rows;
  }

  if (policyType === "Systemd") {
    const sd = parseSystemdContent(content);
    for (const u of sd.units) {
      const what = describeUnitEntry(u) || "(nothing set)";
      rows.push({
        setting: u.name || "(unnamed unit)",
        value: u.description ? `${what} (${u.description})` : what,
        locked: null,
      });
    }
    for (const t of sd.tasks) {
      const what = describeTaskEntry(t);
      rows.push({
        setting: `Task bor-${t.name || "?"}`,
        value: t.description ? `${what} (${t.description})` : what,
        locked: null,
      });
    }
    if (sd.writePresets) {
      rows.push({ setting: "Preset file", value: "10-bor.preset carries the enablement decisions for units installed later", locked: null });
    }
    return rows;
  }

  if (policyType === "Selinux") {
    const se = parseSelinuxContent(content);
    if (se.mode !== "SELINUX_MODE_UNSPECIFIED") rows.push({ setting: "Mode", value: se.mode === "SELINUX_MODE_ENFORCING" ? "enforcing" : "permissive", locked: null });
    if (se.requireActive) rows.push({ setting: "SELinux required", value: "nodes without SELinux are non-compliant", locked: null });
    if (se.prepareDisabledNodes) rows.push({ setting: "Disabled nodes", value: "prepared (config permissive, relabel at next boot)", locked: null });
    for (const b of se.booleans) rows.push({ setting: `Boolean ${b.name}`, value: `${b.value ? "on" : "off"}${b.description ? ` (${b.description})` : ""}`, locked: null });
    for (const d of se.permissiveDomains) rows.push({ setting: `Permissive domain ${d}`, value: "permissive", locked: null });
    for (const m of se.modules) rows.push({ setting: `Module ${m.name}`, value: m.enabled ? "enabled" : "disabled", locked: null });
    for (const m of se.customModules) rows.push({ setting: `Custom module bor-${m.name}`, value: m.description || `${m.cil.split("\n").filter((l) => l.trim() && !l.trim().startsWith(";")).length} CIL statements`, locked: null });
    for (const f of se.fileContexts) rows.push({ setting: `File context ${f.pathRegex}`, value: `${f.type}${f.restorecon ? ", relabelled" : ""}`, locked: null });
    for (const l of se.logins) rows.push({ setting: `Login ${l.login}`, value: `${l.seuser}${l.range ? ` ${l.range}` : ""}`, locked: null });
    return rows;
  }

  if (policyType === "Apparmor") {
    const aa = parseApparmorContent(content);
    if (aa.requireEnabled) rows.push({ setting: "AppArmor required", value: "nodes without AppArmor are non-compliant", locked: null });
    if (aa.ensureService) rows.push({ setting: "apparmor.service", value: "kept enabled and running", locked: null });
    if (aa.restrictUnprivilegedUserns !== "APPARMOR_SWITCH_UNSPECIFIED") rows.push({ setting: "Restrict unprivileged user namespaces", value: aa.restrictUnprivilegedUserns === "APPARMOR_SWITCH_ON" ? "on" : "off", locked: null });
    if (aa.restrictUnprivilegedUnconfined !== "APPARMOR_SWITCH_UNSPECIFIED") rows.push({ setting: "Restrict unprivileged unconfined", value: aa.restrictUnprivilegedUnconfined === "APPARMOR_SWITCH_ON" ? "on" : "off", locked: null });
    for (const p of aa.profiles) rows.push({ setting: `Profile ${p.match}`, value: `${apparmorModeLabel(p.mode).toLowerCase()}${p.editVendorFlags ? " (vendor flags edited)" : ""}`, locked: null });
    for (const c of aa.customProfiles) rows.push({ setting: `Custom profile bor-${c.name}`, value: c.description || `${c.text.split("\n").length} lines`, locked: null });
    for (const o of aa.localOverrides) rows.push({ setting: `Local override ${o.profileFile}`, value: `${o.text.split("\n").filter((l) => l.trim()).length} rules`, locked: null });
    if (aa.extraHomeDirs.length) rows.push({ setting: "Extra home directories", value: aa.extraHomeDirs.join(" "), locked: null });
    return rows;
  }

  if (policyType === "Updates") {
    const up = parseUpdatesContent(content);
    if (up.mode !== "UPDATES_MODE_UNSPECIFIED") rows.push({ setting: "Mode", value: updatesModeLabel(up.mode), locked: null });
    if (up.schedule.onCalendar) rows.push({ setting: "Schedule", value: `${up.schedule.onCalendar}${up.schedule.randomizedDelay ? ` (random delay up to ${up.schedule.randomizedDelay})` : ""}`, locked: null });
    if (up.reboot.policy !== "UPDATES_REBOOT_UNSPECIFIED") {
      const parts = [updatesRebootLabel(up.reboot.policy)];
      if (up.reboot.window) parts.push(`window ${up.reboot.window}`);
      if (up.reboot.policy !== "UPDATES_REBOOT_NEVER") {
        parts.push(up.reboot.onlyWithoutSessions ? "only without logged-in users" : "even with users logged in");
        if (up.reboot.forceAfterDays > 0) parts.push(`forced after ${up.reboot.forceAfterDays} days with a ${up.reboot.warnMinutes}-minute warning`);
      }
      rows.push({ setting: "Reboot", value: parts.join(", "), locked: null });
    }
    for (const h of up.hold) rows.push({ setting: `Hold ${h}`, value: "held back from updates", locked: null });
    if (up.removeUnusedDependencies) rows.push({ setting: "Unused dependencies", value: "removed after updates", locked: null });
    if (!up.installEngine) rows.push({ setting: "Engine", value: "not installed by the policy", locked: null });
    if (up.requireEngine) rows.push({ setting: "Engine required", value: "nodes without an engine are non-compliant", locked: null });
    return rows;
  }

  if (policyType === "Flatpak") {
    type FpRemote = { name?: string; url?: string; subset?: string; filterMode?: string | number; filterRefs?: string[]; enabled?: boolean; gpgVerify?: boolean; priority?: number };
    type FpApp = { appId?: string; displayName?: string; remote?: string; branch?: string; state?: string | number; scope?: string | number; optional?: boolean };
    const fp = raw as {
      remotes?: FpRemote[]; apps?: FpApp[]; installation?: string; autoUpdate?: boolean;
      autoUpdateIntervalHours?: number; uninstallUnused?: boolean; operationTimeoutMinutes?: number;
    };
    const remotes = Array.isArray(fp.remotes) ? fp.remotes : [];
    const apps = Array.isArray(fp.apps) ? fp.apps : [];
    const filterLabel = (m: string | number | undefined, refs: string[] | undefined) => {
      const mode = typeof m === "number" ? ["none", "allowlist", "denylist"][m] ?? "none" : String(m ?? "").replace("FLATPAK_FILTER_MODE_", "").toLowerCase() || "none";
      if (mode === "none") return "";
      return `, ${mode} (${(refs ?? []).length} refs)`;
    };
    const stateLabel = (s: string | number | undefined) =>
      typeof s === "number" ? ["unspecified", "present", "absent", "latest"][s] ?? "unspecified" : String(s ?? "").replace("FLATPAK_APP_STATE_", "").toLowerCase() || "unspecified";
    const scopeLabel = (s: string | number | undefined) => {
      const v = typeof s === "number" ? ["", "system", "user"][s] ?? "" : String(s ?? "").replace("FLATPAK_SCOPE_", "").toLowerCase();
      return v === "user" ? " [user]" : "";
    };
    rows.push({ setting: "Remotes", value: `${remotes.length} defined`, locked: null });
    for (const r of remotes) {
      const bits = [r.url ?? ""];
      if (r.subset) bits.push(`subset ${r.subset}`);
      if (r.enabled === false) bits.push("disabled");
      if (r.gpgVerify === false) bits.push("GPG verification off");
      if (r.priority) bits.push(`priority ${r.priority}`);
      rows.push({ setting: `  Remote › ${r.name ?? "(unnamed)"}`, value: bits.join(", ") + filterLabel(r.filterMode, r.filterRefs), locked: null });
    }
    for (const a of apps) {
      const target = a.remote ? ` from ${a.remote}` : "";
      const branch = a.branch ? `//${a.branch}` : "";
      const opt = a.optional ? " (optional)" : "";
      const label = a.displayName ? `${a.displayName} (${a.appId ?? ""})` : (a.appId ?? "(unnamed)");
      rows.push({ setting: `App › ${label}${scopeLabel(a.scope)}`, value: `${stateLabel(a.state)}${target}${branch}${opt}`, locked: null });
    }
    if (fp.installation) rows.push({ setting: "Options › Installation", value: fp.installation, locked: null });
    if (fp.autoUpdate !== undefined) {
      rows.push({
        setting: "Options › Automatic updates",
        value: fp.autoUpdate ? `every ${fp.autoUpdateIntervalHours || 24} h` : "off",
        locked: null,
      });
    }
    if (fp.uninstallUnused !== undefined) rows.push({ setting: "Options › Remove unused runtimes", value: fp.uninstallUnused ? "yes" : "no", locked: null });
    if (fp.operationTimeoutMinutes) rows.push({ setting: "Options › Operation timeout", value: `${fp.operationTimeoutMinutes} min`, locked: null });
    return rows;
  }

  if (policyType === "DiskEncryption") {
    type DeTangServer = { url?: string; thumbprint?: string; acceptedThumbprints?: string[] };
    const de = raw as {
      requireEncryption?: boolean;
      volumeScope?: string;
      mountpoints?: string[];
      minVolumeKeyBits?: number;
      allowedCiphers?: string[];
      requireTpm2?: boolean;
      requireSecureBoot?: boolean;
      tpm2?: { enabled?: boolean; pcrProfile?: string; pcrs?: number[]; autoReseal?: boolean };
      tang?: { enabled?: boolean; servers?: DeTangServer[]; threshold?: number };
      recovery?: {
        escrow?: boolean;
        rotationIntervalDays?: number;
        rotateAfterReveal?: boolean;
        serverAssistedRotation?: boolean;
      };
      initramfsMode?: string;
      adoptExisting?: boolean;
    };
    const scopeLabel =
      String(de.volumeScope ?? "").replace("LUKS_VOLUME_SCOPE_", "").toLowerCase().replace("_", " ") || "system";
    rows.push({ setting: "Require encryption", value: de.requireEncryption ? "Yes" : "No", locked: null });
    rows.push({ setting: "Volume scope", value: scopeLabel, locked: null });
    if (de.mountpoints?.length) rows.push({ setting: "Mountpoints", value: de.mountpoints.join(", "), locked: null });
    if (de.minVolumeKeyBits) rows.push({ setting: "Minimum volume key", value: `${de.minVolumeKeyBits} bits`, locked: null });
    if (de.allowedCiphers?.length) rows.push({ setting: "Allowed ciphers", value: de.allowedCiphers.join(", "), locked: null });
    if (de.requireTpm2) rows.push({ setting: "Require TPM 2.0", value: "Yes", locked: null });
    if (de.requireSecureBoot) rows.push({ setting: "Require Secure Boot", value: "Yes", locked: null });
    if (de.tpm2?.enabled) {
      const profile =
        de.tpm2.pcrProfile === "TPM_PCR_PROFILE_CUSTOM"
          ? `custom PCRs ${(de.tpm2.pcrs ?? []).join("+")}`
          : de.tpm2.pcrProfile === "TPM_PCR_PROFILE_SECURE_BOOT_SHIM"
            ? "Secure Boot + shim (PCR 7+14)"
            : "Secure Boot (PCR 7)";
      rows.push({
        setting: "Protector › TPM2",
        value: `${profile}${de.tpm2.autoReseal === false ? ", auto-reseal off" : ", auto-reseal on"}`,
        locked: null,
      });
    }
    if (de.tang?.enabled) {
      const servers = de.tang.servers ?? [];
      rows.push({
        setting: "Protector › Tang",
        value:
          servers.map((s) => s.url ?? "").join(", ") +
          (servers.length > 1 ? ` (threshold ${de.tang.threshold || 1})` : ""),
        locked: null,
      });
    }
    if (de.recovery?.escrow) {
      const days = de.recovery.rotationIntervalDays ?? 180;
      rows.push({
        setting: "Protector › Recovery key",
        value: `escrowed, ${days === 0 ? "never rotated" : `rotated every ${days} days`}${
          de.recovery.rotateAfterReveal === false ? "" : ", replaced after every reveal"
        }${de.recovery.serverAssistedRotation === false ? ", server-assisted rotation off" : ""}`,
        locked: null,
      });
    }
    rows.push({
      setting: "Initramfs",
      value: de.initramfsMode === "INITRAMFS_MODE_MANAGE" ? "managed (dracut drop-in + rebuild)" : "verify only",
      locked: null,
    });
    if (de.adoptExisting === false) rows.push({ setting: "Adopt existing enrollments", value: "No", locked: null });
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
      const windows = (rule.windows ?? []).map((w) => `${(w.days ?? []).join("/")} ${w.start ?? "?"}-${w.end ?? "?"}`);
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
