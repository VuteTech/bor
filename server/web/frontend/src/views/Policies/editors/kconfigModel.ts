// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * KDE Plasma (Kconfig) policy content model: the JSON shape the editor
 * writes (generic settings plus control-module and URL restrictions), the
 * upgrade of content written by older editors, the INI preview and import,
 * presets, and the KIO protocol and KCM module tables used by the special
 * editors.
 *
 * Settings follow KDE's conventions as they are: Kiosk restriction keys
 * store `false` to restrict, `[$i]` is the `immutable` flag, `[$e]` is
 * `expand`, `[$d]` is `delete`.
 */

import {
  KCONFIG_VALUE_PLACEHOLDER,
  findCatalogueEntry,
  findCatalogueEntryByTarget,
  groupPathLabel,
  targetKey,
  type KConfigCatalogueEntry,
} from "./kconfigCatalogue";

/* ── Content shape (camelCase protojson of KConfigPolicy) ── */

export interface KConfigSettingJson {
  file: string;
  groupPath: string[];
  key: string;
  value?: string;
  immutable?: boolean;
  expand?: boolean;
  delete?: boolean;
  locale?: string;
  catalogueId?: string;
}

export type KcmMode = "KCM_HIDE_LISTED" | "KCM_SHOW_ONLY_LISTED";

export interface UrlRestrictionRule {
  action: "open" | "list" | "redirect";
  referrerProtocol: string;
  referrerHost: string;
  referrerPath: string;
  protocol: string;
  host: string;
  path: string;
  enabled: boolean;
}

export interface KConfigContent {
  settings: KConfigSettingJson[];
  kcmRestrictions: string[];
  kcmMode: KcmMode;
  urlRestrictions: UrlRestrictionRule[];
  fileImmutableKdeglobals: boolean;
}

export const EMPTY_KCONFIG_CONTENT: KConfigContent = {
  settings: [],
  kcmRestrictions: [],
  kcmMode: "KCM_HIDE_LISTED",
  urlRestrictions: [],
  fileImmutableKdeglobals: false,
};

/** Result of loading content: the normalised model plus what the upgrade did. */
export interface KConfigLoadResult {
  content: KConfigContent;
  /** Typed fields from older content that became generic settings. */
  convertedFields: string[];
  /** Legacy resource-restriction fields that were dropped (no effect on Plasma 5/6). */
  droppedFields: string[];
  /** True when serialising `content` would differ from the input. */
  changed: boolean;
}

/* ── Older typed content ── */

type LegacyTarget =
  | { catalogueId: string; kind: "bool" | "int" | "string" }
  | { file: string; groupPath: string[]; key: string; kind: "bool" | "int" | "string" };

const APPLETS_RC = "plasma-org.kde.plasma.desktop-appletsrc";

/** Typed fields older editors wrote, and where they live now. */
const LEGACY_TYPED_FIELDS: Record<string, LegacyTarget> = {
  shellAccess: { catalogueId: "kiosk.shell_access", kind: "bool" },
  runCommand: { catalogueId: "kiosk.run_command", kind: "bool" },
  actionLogout: { catalogueId: "kiosk.logout", kind: "bool" },
  actionFileNew: { catalogueId: "kiosk.action.file_new", kind: "bool" },
  actionFileOpen: { catalogueId: "kiosk.action.file_open", kind: "bool" },
  actionFileSave: { catalogueId: "kiosk.action.file_save", kind: "bool" },
  plasmoidUnlockedDesktop: { catalogueId: "kiosk.plasma_unlocked_desktop", kind: "bool" },
  allowConfigureWhenLocked: { catalogueId: "kiosk.plasma_allow_configure_when_locked", kind: "bool" },
  borderlessMaximizedWindows: { catalogueId: "kwin.BorderlessMaximizedWindows", kind: "bool" },
  autoLock: { catalogueId: "screenlocker.Autolock", kind: "bool" },
  lockOnResume: { catalogueId: "screenlocker.LockOnResume", kind: "bool" },
  lockOnStart: { catalogueId: "screenlocker.LockOnStart", kind: "bool" },
  lockTimeout: { catalogueId: "screenlocker.Timeout", kind: "int" },
  lockGrace: { catalogueId: "screenlocker.LockGrace", kind: "int" },
  lockScreenImage: { catalogueId: "screenlocker.Greeter.Image", kind: "string" },
  iconTheme: { catalogueId: "kde.IconTheme", kind: "string" },
  wallpaperPlugin: { file: APPLETS_RC, groupPath: ["Containments", "1"], key: "wallpaperplugin", kind: "string" },
  wallpaperImage: { file: APPLETS_RC, groupPath: ["Containments", "1", "Wallpaper", "org.kde.image", "General"], key: "Image", kind: "string" },
  wallpaperFillMode: { file: APPLETS_RC, groupPath: ["Containments", "1", "Wallpaper", "org.kde.image", "General"], key: "FillMode", kind: "string" },
  wallpaperColor: { file: APPLETS_RC, groupPath: ["Containments", "1", "Wallpaper", "org.kde.image", "General"], key: "Color", kind: "string" },
};

/** Legacy [KDE Resource Restrictions] fields: KDE Plasma 5 and 6 never read them. */
const LEGACY_DROPPED_FIELDS = ["restrictWallpaper", "restrictIcons", "restrictAutostart", "restrictColors", "restrictCursors"];

function asString(v: unknown): string {
  if (typeof v === "boolean") return v ? "true" : "false";
  if (v === null || v === undefined) return "";
  return String(v);
}

function normaliseSetting(raw: Record<string, unknown>): KConfigSettingJson | null {
  const file = asString(raw.file);
  const key = asString(raw.key);
  const groupPath = Array.isArray(raw.groupPath) ? (raw.groupPath as unknown[]).map(asString) : [];
  if (!file || !key) return null;
  const s: KConfigSettingJson = { file, groupPath, key };
  if (raw.value !== undefined && raw.value !== null) s.value = asString(raw.value);
  if (raw.immutable === true) s.immutable = true;
  if (raw.expand === true) s.expand = true;
  if (raw.delete === true) s.delete = true;
  if (typeof raw.locale === "string" && raw.locale) s.locale = raw.locale;
  if (typeof raw.catalogueId === "string" && raw.catalogueId) s.catalogueId = raw.catalogueId;
  return s;
}

/** Identity of a setting: file, group path, key and locale. */
export function settingKey(s: Pick<KConfigSettingJson, "file" | "groupPath" | "key" | "locale">): string {
  return targetKey(s.file, s.groupPath, s.key) + "\u0000" + (s.locale ?? "");
}

function dedupeSettings(settings: KConfigSettingJson[]): KConfigSettingJson[] {
  const seen = new Set<string>();
  const out: KConfigSettingJson[] = [];
  for (const s of settings) {
    const k = settingKey(s);
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(s);
  }
  return out;
}

/**
 * Parses policy content. Content written by older editors (typed fields
 * plus `enforcedFields`) is converted into generic settings; the legacy
 * resource restrictions are dropped and reported.
 */
export function parseKConfigContent(raw: string): KConfigLoadResult {
  let parsed: Record<string, unknown> = {};
  try {
    const p = JSON.parse(raw || "{}");
    if (p && typeof p === "object" && !Array.isArray(p)) parsed = p as Record<string, unknown>;
  } catch {
    /* treat unparsable content as empty */
  }

  const settings: KConfigSettingJson[] = [];
  if (Array.isArray(parsed.settings)) {
    for (const s of parsed.settings as unknown[]) {
      if (s && typeof s === "object") {
        const n = normaliseSetting(s as Record<string, unknown>);
        if (n) settings.push(n);
      }
    }
  }

  const enforcedFields = Array.isArray(parsed.enforcedFields) ? (parsed.enforcedFields as unknown[]).map(asString) : [];
  const convertedFields: string[] = [];
  const droppedFields: string[] = [];

  for (const [field, target] of Object.entries(LEGACY_TYPED_FIELDS)) {
    if (!(field in parsed) || parsed[field] === null || parsed[field] === undefined) continue;
    const value = asString(parsed[field]);
    const immutable = enforcedFields.includes(field);
    if ("catalogueId" in target) {
      const entry = findCatalogueEntry(target.catalogueId);
      if (entry) {
        settings.push(...settingsForEntry(entry, value, immutable));
        convertedFields.push(field);
        continue;
      }
    }
    if ("file" in target) {
      settings.push({ file: target.file, groupPath: target.groupPath, key: target.key, value, ...(immutable ? { immutable: true } : {}) });
      convertedFields.push(field);
    }
  }
  for (const field of LEGACY_DROPPED_FIELDS) {
    if (field in parsed && parsed[field] !== null && parsed[field] !== undefined) droppedFields.push(field);
  }

  const content: KConfigContent = {
    settings: dedupeSettings(settings),
    kcmRestrictions: Array.isArray(parsed.kcmRestrictions) ? (parsed.kcmRestrictions as unknown[]).map(asString).filter(Boolean) : [],
    kcmMode: parsed.kcmMode === "KCM_SHOW_ONLY_LISTED" || parsed.kcmMode === 1 ? "KCM_SHOW_ONLY_LISTED" : "KCM_HIDE_LISTED",
    urlRestrictions: parseUrlRestrictionRules(raw),
    fileImmutableKdeglobals: parsed.fileImmutableKdeglobals === true,
  };

  const changed = convertedFields.length > 0 || droppedFields.length > 0 || "enforcedFields" in parsed;
  return { content, convertedFields, droppedFields, changed };
}

/** Serialises the model to the JSON the server stores (only what is set). */
export function serializeKConfigContent(c: KConfigContent): string {
  const out: Record<string, unknown> = {};
  if (c.settings.length > 0) {
    out.settings = c.settings.map((s) => {
      const o: Record<string, unknown> = { file: s.file, groupPath: s.groupPath, key: s.key };
      if (s.delete) o.delete = true;
      else o.value = s.value ?? "";
      if (s.immutable) o.immutable = true;
      if (s.expand) o.expand = true;
      if (s.locale) o.locale = s.locale;
      if (s.catalogueId) o.catalogueId = s.catalogueId;
      return o;
    });
  }
  if (c.kcmRestrictions.length > 0) out.kcmRestrictions = c.kcmRestrictions;
  if (c.kcmMode === "KCM_SHOW_ONLY_LISTED") out.kcmMode = c.kcmMode;
  if (c.urlRestrictions.length > 0) {
    out.urlRestrictions = c.urlRestrictions.map((r) => ({
      action: r.action,
      referrerProtocol: r.referrerProtocol,
      referrerHost: r.referrerHost,
      referrerPath: r.referrerPath,
      protocol: r.protocol,
      host: r.host,
      path: r.path,
      enabled: r.enabled,
    }));
  }
  if (c.fileImmutableKdeglobals) out.fileImmutableKdeglobals = true;
  return JSON.stringify(out, null, 2);
}

export function hasAnyKConfigContent(c: KConfigContent): boolean {
  return c.settings.length > 0 || c.kcmRestrictions.length > 0 || c.urlRestrictions.length > 0 || c.fileImmutableKdeglobals;
}

/* ── Catalogue-backed settings ── */

/** The main setting plus the entry's extra writes, all tagged with the entry id. */
export function settingsForEntry(entry: KConfigCatalogueEntry, value: string, immutable: boolean, expand = false): KConfigSettingJson[] {
  const main: KConfigSettingJson = { file: entry.file, groupPath: entry.groupPath, key: entry.key, value, catalogueId: entry.id };
  if (immutable) main.immutable = true;
  if (expand) main.expand = true;
  const extras = (entry.extraWrites ?? []).map((w) => {
    const s: KConfigSettingJson = {
      file: w.file,
      groupPath: w.groupPath,
      key: w.key,
      value: w.value === KCONFIG_VALUE_PLACEHOLDER ? value : w.value,
      catalogueId: entry.id,
    };
    if (w.immutable || immutable) s.immutable = true;
    if (w.expand) s.expand = true;
    if (w.delete) s.delete = true;
    if (w.locale) s.locale = w.locale;
    return s;
  });
  return [main, ...extras];
}

/** The main setting of a catalogue entry in the content, if configured. */
export function findEntrySetting(c: KConfigContent, entry: KConfigCatalogueEntry): KConfigSettingJson | undefined {
  const k = targetKey(entry.file, entry.groupPath, entry.key);
  return c.settings.find((s) => targetKey(s.file, s.groupPath, s.key) === k && !s.locale);
}

export function upsertEntry(c: KConfigContent, entry: KConfigCatalogueEntry, value: string, immutable: boolean, expand = false): KConfigContent {
  const fresh = settingsForEntry(entry, value, immutable, expand);
  const freshKeys = new Set(fresh.map(settingKey));
  const kept = c.settings.filter((s) => s.catalogueId !== entry.id && !freshKeys.has(settingKey(s)));
  return { ...c, settings: [...kept, ...fresh] };
}

export function removeEntry(c: KConfigContent, entry: KConfigCatalogueEntry): KConfigContent {
  const mainKey = targetKey(entry.file, entry.groupPath, entry.key);
  return {
    ...c,
    settings: c.settings.filter((s) => s.catalogueId !== entry.id && targetKey(s.file, s.groupPath, s.key) !== mainKey),
  };
}

/** Catalogue entry a setting belongs to: by id, else by its target. */
export function catalogueEntryForSetting(s: KConfigSettingJson): KConfigCatalogueEntry | undefined {
  return (s.catalogueId ? findCatalogueEntry(s.catalogueId) : undefined) ?? findCatalogueEntryByTarget(s.file, s.groupPath, s.key);
}

/** Settings the catalogue does not describe (shown under "Custom entries"). */
export function customSettings(c: KConfigContent): KConfigSettingJson[] {
  return c.settings.filter((s) => catalogueEntryForSetting(s) === undefined);
}

/** True for settings that only exist as another entry's extra write. */
export function isExtraWrite(s: KConfigSettingJson): boolean {
  const entry = s.catalogueId ? findCatalogueEntry(s.catalogueId) : undefined;
  return entry !== undefined && targetKey(entry.file, entry.groupPath, entry.key) !== targetKey(s.file, s.groupPath, s.key);
}

export function upsertCustomSetting(c: KConfigContent, setting: KConfigSettingJson, replaceKey?: string): KConfigContent {
  const k = settingKey(setting);
  const kept = c.settings.filter((s) => settingKey(s) !== k && (replaceKey === undefined || settingKey(s) !== replaceKey));
  return { ...c, settings: [...kept, setting] };
}

export function removeSettingByKey(c: KConfigContent, key: string): KConfigContent {
  return { ...c, settings: c.settings.filter((s) => settingKey(s) !== key) };
}

/* ── Presets ── */

export interface KConfigPresetSetting {
  id: string;
  value: string;
  immutable: boolean;
}

export interface KConfigPreset {
  id: string;
  label: string;
  description: string;
  settings: KConfigPresetSetting[];
  kcmRestrictions?: string[];
}

const SHARED_LAB_SETTINGS: KConfigPresetSetting[] = [
  { id: "kiosk.shell_access", value: "false", immutable: true },
  { id: "kiosk.run_command", value: "false", immutable: true },
  { id: "kiosk.plasma_unlocked_desktop", value: "false", immutable: true },
  { id: "kiosk.plasma_scripting_console", value: "false", immutable: true },
  { id: "kiosk.lineedit_text_completion", value: "false", immutable: true },
  { id: "ksmserver.loginMode", value: "emptySession", immutable: true },
  { id: "ksmserver.confirmLogout", value: "false", immutable: true },
  { id: "krunner.historyBehavior", value: "Disabled", immutable: true },
  { id: "dolphin.RememberOpenedTabs", value: "false", immutable: true },
  { id: "activity.what-to-remember", value: "2", immutable: true },
  { id: "klipper.KeepClipboardContents", value: "false", immutable: true },
  { id: "screenlocker.Autolock", value: "true", immutable: true },
  { id: "screenlocker.Timeout", value: "10", immutable: true },
  { id: "screenlocker.LockGrace", value: "0", immutable: true },
  { id: "screenlocker.LockOnResume", value: "true", immutable: true },
];

export const KCONFIG_PRESETS: KConfigPreset[] = [
  {
    id: "security-baseline",
    label: "Security baseline",
    description: "Screen lock with timeout and lock on resume, no automatic mounting of removable media, RDP server off, password reveal and store downloads restricted, wallet closes when idle, X11 keystroke snooping off.",
    settings: [
      { id: "screenlocker.Autolock", value: "true", immutable: true },
      { id: "screenlocker.Timeout", value: "10", immutable: true },
      { id: "screenlocker.LockGrace", value: "5", immutable: true },
      { id: "screenlocker.LockOnResume", value: "true", immutable: true },
      { id: "screenlocker.RequirePassword", value: "true", immutable: true },
      { id: "automount.AutomountEnabled", value: "false", immutable: true },
      { id: "automount.AutomountOnLogin", value: "false", immutable: true },
      { id: "automount.AutomountOnPlugin", value: "false", immutable: true },
      { id: "automount.AutomountUnknownDevices", value: "false", immutable: true },
      { id: "krdp.Autostart", value: "false", immutable: true },
      { id: "kiosk.lineedit_reveal_password", value: "false", immutable: true },
      { id: "kiosk.ghns", value: "false", immutable: true },
      { id: "kwallet.CloseWhenIdle", value: "true", immutable: true },
      { id: "kwallet.IdleTimeout", value: "10", immutable: true },
      { id: "kwin.XwaylandEavesdrops", value: "None", immutable: true },
    ],
    kcmRestrictions: ["kcm_krdpserver", "kcm_device_automounter"],
  },
  {
    id: "shared-lab",
    label: "Shared lab workstation",
    description: "No shell or KRunner commands, locked desktop layout, clean session at every login, no history of searches, documents or clipboard, screen lock after 10 minutes, administrative System Settings pages hidden.",
    settings: SHARED_LAB_SETTINGS,
    kcmRestrictions: ["kcm_autostart", "kcm_kded", "kcm_cron", "kcm_krdpserver", "kcm_kaccounts", "kcm_device_automounter"],
  },
  {
    id: "public-kiosk",
    label: "Public kiosk",
    description: "Everything in the shared lab preset plus no logout, lock, user switching or new sessions, locked toolbars, no application configuration dialogs, no untrusted launchers and no Open With.",
    settings: [
      ...SHARED_LAB_SETTINGS,
      { id: "kiosk.logout", value: "false", immutable: true },
      { id: "kiosk.lock_screen", value: "false", immutable: true },
      { id: "kiosk.switch_user", value: "false", immutable: true },
      { id: "kiosk.start_new_session", value: "false", immutable: true },
      { id: "kiosk.movable_toolbars", value: "false", immutable: true },
      { id: "kiosk.run_desktop_files", value: "false", immutable: true },
      { id: "kiosk.open_with", value: "false", immutable: true },
      { id: "kiosk.editfiletype", value: "false", immutable: true },
      { id: "kiosk.ghns", value: "false", immutable: true },
      { id: "kiosk.action.options_configure", value: "false", immutable: true },
      { id: "kiosk.action.options_configure_keybinding", value: "false", immutable: true },
      { id: "kiosk.action.options_configure_toolbars", value: "false", immutable: true },
      { id: "kiosk.action.help_report_bug", value: "false", immutable: true },
    ],
    kcmRestrictions: ["kcm_autostart", "kcm_kded", "kcm_cron", "kcm_krdpserver", "kcm_kaccounts", "kcm_device_automounter"],
  },
  {
    id: "branding",
    label: "Branding defaults",
    description: "Global theme, colour scheme, icon theme and Plasma style as defaults users may still change, with the splash screen disabled. Edit the values to match your corporate theme.",
    settings: [
      { id: "kde.LookAndFeelPackage", value: "org.kde.breeze.desktop", immutable: false },
      { id: "kde.ColorScheme", value: "BreezeLight", immutable: false },
      { id: "kde.IconTheme", value: "breeze", immutable: false },
      { id: "plasma.Theme", value: "default", immutable: false },
      { id: "ksplash.Theme", value: "None", immutable: false },
      { id: "ksplash.Engine", value: "none", immutable: false },
    ],
  },
];

/** Adds a preset's settings that are not configured yet; existing values stay. */
export function applyKConfigPreset(c: KConfigContent, preset: KConfigPreset): { content: KConfigContent; added: number } {
  let next = c;
  let added = 0;
  for (const p of preset.settings) {
    const entry = findCatalogueEntry(p.id);
    if (!entry || findEntrySetting(next, entry)) continue;
    next = upsertEntry(next, entry, p.value, p.immutable);
    added++;
  }
  if (preset.kcmRestrictions) {
    const missing = preset.kcmRestrictions.filter((m) => !next.kcmRestrictions.includes(m));
    if (missing.length > 0) {
      next = { ...next, kcmRestrictions: [...next.kcmRestrictions, ...missing] };
      added += missing.length;
    }
  }
  return { content: next, added };
}

/* ── INI preview (mirrors the agent's renderer) ── */

interface PreviewEntry {
  key: string;
  value: string;
  immutable: boolean;
  expand: boolean;
  del: boolean;
  locale: string;
}

function renderKey(e: PreviewEntry, groupImmutable: boolean): string {
  let k = e.key;
  if (e.locale) k += `[${e.locale}]`;
  let flags = "";
  if (e.immutable && !groupImmutable) flags += "i";
  if (e.expand) flags += "e";
  if (e.del) flags += "d";
  if (flags) k += `[$${flags}]`;
  return k;
}

/** The files the agent would write for this policy alone, rendered like the agent does. */
export function renderKConfigPreview(c: KConfigContent): { file: string; content: string; note?: string }[] {
  const files = new Map<string, Map<string, PreviewEntry[]>>();
  const add = (file: string, group: string, e: PreviewEntry) => {
    let groups = files.get(file);
    if (!groups) {
      groups = new Map();
      files.set(file, groups);
    }
    const arr = groups.get(group) ?? [];
    arr.push(e);
    groups.set(group, arr);
  };

  for (const s of c.settings) {
    add(s.file, s.groupPath.join("]["), {
      key: s.key,
      value: s.value ?? "",
      immutable: s.immutable === true,
      expand: s.expand === true,
      del: s.delete === true,
      locale: s.locale ?? "",
    });
  }
  c.urlRestrictions.forEach((r, i) => {
    const value = [r.action, r.referrerProtocol, r.referrerHost, r.referrerPath, r.protocol, r.host, r.path, r.enabled ? "true" : "false"].join(",");
    add("kdeglobals", "KDE URL Restrictions", { key: `rule_${i + 1}`, value, immutable: true, expand: false, del: false, locale: "" });
  });
  if (c.urlRestrictions.length > 0) {
    add("kdeglobals", "KDE URL Restrictions", { key: "rule_count", value: String(c.urlRestrictions.length), immutable: true, expand: false, del: false, locale: "" });
  }
  for (const mod of c.kcmRestrictions) {
    add("kdeglobals", "KDE Control Module Restrictions", {
      key: mod,
      value: c.kcmMode === "KCM_SHOW_ONLY_LISTED" ? "true" : "false",
      immutable: true,
      expand: false,
      del: false,
      locale: "",
    });
  }
  if (c.fileImmutableKdeglobals && !files.has("kdeglobals")) files.set("kdeglobals", new Map());

  const out: { file: string; content: string; note?: string }[] = [];
  for (const file of Array.from(files.keys()).sort()) {
    const groups = files.get(file)!;
    const lines: string[] = [];
    if (file === "kdeglobals" && c.fileImmutableKdeglobals) lines.push("[$i]", "");
    const groupNames = Array.from(groups.keys()).sort();
    groupNames.forEach((group, gi) => {
      const entries = [...groups.get(group)!].sort((a, b) => (a.key === b.key ? a.locale.localeCompare(b.locale) : a.key < b.key ? -1 : 1));
      const allImmutable = entries.length > 0 && entries.every((e) => e.immutable);
      if (gi > 0) lines.push("");
      lines.push(`[${group}]${allImmutable ? "[$i]" : ""}`);
      for (const e of entries) {
        const k = renderKey(e, allImmutable);
        lines.push(e.del ? k : `${k}=${e.value}`);
      }
    });
    const note =
      file === "kdeglobals" && c.kcmMode === "KCM_SHOW_ONLY_LISTED" && c.kcmRestrictions.length > 0
        ? "Show-only mode: every other module installed on a node is written as hidden there; only the listed modules are shown here."
        : undefined;
    out.push({ file, content: lines.join("\n") + "\n", note });
  }
  return out;
}

/* ── INI import ── */

export interface KConfigImportResult {
  content: KConfigContent;
  imported: number;
  warnings: string[];
}

/**
 * Parses KConfig INI text and merges it into the content as settings for
 * `file`. Known restriction groups go to the URL / module lists; keys the
 * catalogue knows are tagged with their entry.
 */
export function importKConfigIni(c: KConfigContent, file: string, ini: string): KConfigImportResult {
  const warnings: string[] = [];
  let next: KConfigContent = { ...c, settings: [...c.settings], kcmRestrictions: [...c.kcmRestrictions], urlRestrictions: [...c.urlRestrictions] };
  let imported = 0;
  let groupPath: string[] = [];
  let groupImmutable = false;

  const lines = ini.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line || line.startsWith("#") || line.startsWith(";")) continue;

    if (line.startsWith("[")) {
      if (line === "[$i]") {
        if (file === "kdeglobals") next = { ...next, fileImmutableKdeglobals: true };
        else warnings.push(`line ${i + 1}: file-level [$i] is only supported for kdeglobals`);
        continue;
      }
      const parts = Array.from(line.matchAll(/\[([^\]]*)\]/g)).map((m) => m[1]);
      groupImmutable = parts.length > 0 && parts[parts.length - 1] === "$i";
      groupPath = groupImmutable ? parts.slice(0, -1) : parts;
      if (groupPath.length === 0) warnings.push(`line ${i + 1}: empty group header`);
      continue;
    }

    if (groupPath.length === 0) {
      warnings.push(`line ${i + 1}: key before any group header was skipped`);
      continue;
    }

    const eq = line.indexOf("=");
    const left = (eq >= 0 ? line.slice(0, eq) : line).trim();
    const value = eq >= 0 ? line.slice(eq + 1) : "";
    const m = /^([^[]+)((?:\[[^\]]*\])*)$/.exec(left);
    if (!m) {
      warnings.push(`line ${i + 1}: could not parse "${line}"`);
      continue;
    }
    const key = m[1].trim();
    let immutable = groupImmutable;
    let expand = false;
    let del = false;
    let locale = "";
    for (const mark of Array.from(m[2].matchAll(/\[([^\]]*)\]/g)).map((x) => x[1])) {
      if (mark.startsWith("$")) {
        if (mark.includes("i")) immutable = true;
        if (mark.includes("e")) expand = true;
        if (mark.includes("d")) del = true;
      } else {
        locale = mark;
      }
    }

    const group = groupPath.join("][");
    if (file === "kdeglobals" && group === "KDE URL Restrictions") {
      if (key === "rule_count") continue;
      const fields = value.split(",");
      if (fields.length !== 8 || !["open", "list", "redirect"].includes(fields[0])) {
        warnings.push(`line ${i + 1}: URL rule "${key}" does not have 8 fields and was skipped`);
        continue;
      }
      next.urlRestrictions.push({
        action: fields[0] as UrlRestrictionRule["action"],
        referrerProtocol: fields[1],
        referrerHost: fields[2],
        referrerPath: fields[3],
        protocol: fields[4],
        host: fields[5],
        path: fields[6],
        enabled: fields[7].trim().toLowerCase() === "true",
      });
      imported++;
      continue;
    }
    if (file === "kdeglobals" && group === "KDE Control Module Restrictions") {
      if (value.trim() === "false" && !next.kcmRestrictions.includes(key)) {
        next.kcmRestrictions.push(key);
        imported++;
      }
      continue;
    }

    const setting: KConfigSettingJson = { file, groupPath: [...groupPath], key };
    if (del) setting.delete = true;
    else setting.value = value;
    if (immutable) setting.immutable = true;
    if (expand) setting.expand = true;
    if (locale) setting.locale = locale;
    const entry = findCatalogueEntryByTarget(file, groupPath, key);
    if (entry) setting.catalogueId = entry.id;
    next = upsertCustomSetting(next, setting);
    imported++;
  }

  return { content: next, imported, warnings };
}

/** Parses "[Containments][1]" or "Containments/1" into a group path. */
export function parseGroupPathInput(text: string): string[] {
  const t = text.trim();
  if (!t) return [];
  if (t.startsWith("[")) return Array.from(t.matchAll(/\[([^\]]*)\]/g)).map((m) => m[1]).filter(Boolean);
  return t.split("/").map((p) => p.trim()).filter(Boolean);
}

/** "[A][B]" for display. */
export const groupPathText = groupPathLabel;

/* ── URL restriction helpers ── */

export function parseUrlRestrictionRules(content: string): UrlRestrictionRule[] {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    if (!Array.isArray(parsed.urlRestrictions)) return [];
    return (parsed.urlRestrictions as Record<string, unknown>[]).map((r) => ({
      action: (r.action as UrlRestrictionRule["action"]) || "open",
      referrerProtocol: (r.referrerProtocol as string) || "",
      referrerHost: (r.referrerHost as string) || "",
      referrerPath: (r.referrerPath as string) || "",
      protocol: (r.protocol as string) || "",
      host: (r.host as string) || "",
      path: (r.path as string) || "",
      enabled: r.enabled === true,
    }));
  } catch {
    return [];
  }
}

export const DEFAULT_URL_RULE: UrlRestrictionRule = {
  action: "open",
  referrerProtocol: "",
  referrerHost: "",
  referrerPath: "",
  protocol: "",
  host: "",
  path: "",
  enabled: true,
};

export interface UrlRulePreset {
  id: string;
  label: string;
  description: string;
  rules: UrlRestrictionRule[];
}

const rule = (action: UrlRestrictionRule["action"], protocol: string, path: string, enabled: boolean): UrlRestrictionRule => ({
  ...DEFAULT_URL_RULE,
  action,
  protocol,
  path,
  enabled,
});

export const URL_RULE_PRESETS: UrlRulePreset[] = [
  {
    id: "home-only",
    label: "Home and temp folders only",
    description: "KDE applications can open and list files only below the user's home directory and the temp directory.",
    rules: [rule("open", "file", "", false), rule("list", "file", "", false), rule("open", "file", "$HOME", true), rule("list", "file", "$HOME", true), rule("open", "file", "$TMP", true), rule("list", "file", "$TMP", true)],
  },
  {
    id: "no-smb",
    label: "No Windows shares",
    description: "Browsing and opening smb:// locations is denied.",
    rules: [rule("list", "smb", "", false), rule("open", "smb", "", false)],
  },
  {
    id: "no-remote-fs",
    label: "No FTP, SFTP or FISH",
    description: "Remote file access through ftp://, sftp:// and fish:// is denied.",
    rules: [rule("open", "ftp", "", false), rule("list", "ftp", "", false), rule("open", "sftp", "", false), rule("list", "sftp", "", false), rule("open", "fish", "", false), rule("list", "fish", "", false)],
  },
];

// KIO protocols available for URL restriction rules.
export const KIO_PROTOCOLS = [
  "bzip", "bzip2", "cifs", "dav", "davs", "file", "fish", "ftp", "gdrive",
  "gopher", "gzip", "help", "http", "https", "info", "ldap", "ldaps", "lzma",
  "man", "nfs", "recentlyused", "sftp", "smb", "tar", "thumbnail", "webdav",
  "webdavs", "xz", "zstd",
];

// KDE Control Modules (KCMs) available for System Settings restrictions.
// Sorted alphabetically by module ID. Label is the human-readable description.
export const KCM_MODULES: { id: string; label: string }[] = [
  { id: "kcm_about-distro", label: "Information About This System" },
  { id: "kcm_access", label: "Accessibility Options" },
  { id: "kcm_activities", label: "Activities" },
  { id: "kcm_animations", label: "Animation Speed and Style" },
  { id: "kcm_app-permissions", label: "Application Permissions" },
  { id: "kcm_audio_information", label: "Audio Device Information" },
  { id: "kcm_autostart", label: "Autostart Applications" },
  { id: "kcm_baloofile", label: "File Search" },
  { id: "kcm_block_devices", label: "Block Devices" },
  { id: "kcm_bluetooth", label: "Bluetooth Devices" },
  { id: "kcm_cddb", label: "CDDB Retrieval" },
  { id: "kcm_cellular_network", label: "Cellular Networks" },
  { id: "kcm_clock", label: "Date and Time" },
  { id: "kcm_colors", label: "Colour Scheme" },
  { id: "kcm_componentchooser", label: "Default Applications" },
  { id: "kcm_cpu", label: "Advanced CPU Information" },
  { id: "kcm_cron", label: "Task Scheduler (Cron)" },
  { id: "kcm_cursortheme", label: "Cursor Theme" },
  { id: "kcm_desktoppaths", label: "Personal File Locations" },
  { id: "kcm_desktoptheme", label: "Plasma Style" },
  { id: "kcm_device_automounter", label: "Device Automounting" },
  { id: "kcm_edid", label: "Display EDID Information" },
  { id: "kcm_egl", label: "EGL Information" },
  { id: "kcm_energyinfo", label: "Energy Consumption Statistics" },
  { id: "kcm_feedback", label: "User Feedback Settings" },
  { id: "kcm_filetypes", label: "File Associations" },
  { id: "kcm_firmware_security", label: "Firmware Security" },
  { id: "kcm_fontinst", label: "Font Management" },
  { id: "kcm_fonts", label: "UI Fonts" },
  { id: "kcm_gamecontroller", label: "Game Controllers" },
  { id: "kcm_glx", label: "GLX Information" },
  { id: "kcm_icons", label: "Icon Theme" },
  { id: "kcm_interrupts", label: "Interrupt Information" },
  { id: "kcm_kaccounts", label: "Online Accounts" },
  { id: "kcm_kamera", label: "Camera Configuration" },
  { id: "kcm_kded", label: "Background Services" },
  { id: "kcm_keyboard", label: "Keyboard Hardware and Layout" },
  { id: "kcm_keys", label: "Keyboard Shortcuts" },
  { id: "kcm_kgamma", label: "Monitor Calibration" },
  { id: "kcm_krdpserver", label: "Remote Desktop" },
  { id: "kcm_kscreen", label: "Display Configuration" },
  { id: "kcm_kwallet5", label: "KDE Wallet" },
  { id: "kcm_kwin_effects", label: "Desktop Effects" },
  { id: "kcm_kwin_scripts", label: "KWin Scripts" },
  { id: "kcm_kwin_virtualdesktops", label: "Virtual Desktops" },
  { id: "kcm_kwindecoration", label: "Window Decorations" },
  { id: "kcm_kwinoptions", label: "Window Behaviour" },
  { id: "kcm_kwinrules", label: "Window Rules" },
  { id: "kcm_kwinscreenedges", label: "Screen Edges" },
  { id: "kcm_kwinsupportinfo", label: "KWin Support Information" },
  { id: "kcm_kwintabbox", label: "Task Switcher" },
  { id: "kcm_kwintouchscreen", label: "Touch Screen Gestures" },
  { id: "kcm_kwinxwayland", label: "Legacy X11 App Compatibility" },
  { id: "kcm_landingpage", label: "Landing Page" },
  { id: "kcm_lookandfeel", label: "Global Theme" },
  { id: "kcm_memory", label: "Memory Information" },
  { id: "kcm_mobile_hotspot", label: "WiFi Hotspot" },
  { id: "kcm_mobile_power", label: "Power Management (Mobile)" },
  { id: "kcm_mobile_wifi", label: "Wireless Network (Mobile)" },
  { id: "kcm_mouse", label: "Mouse Settings" },
  { id: "kcm_netpref", label: "Network Preferences" },
  { id: "kcm_network", label: "Network Information" },
  { id: "kcm_networkmanagement", label: "Network Connections" },
  { id: "kcm_nightlight", label: "Night Light" },
  { id: "kcm_nighttime", label: "Day-Night Cycle" },
  { id: "kcm_notifications", label: "Notifications" },
  { id: "kcm_opencl", label: "OpenCL Information" },
  { id: "kcm_pci", label: "PCI Information" },
  { id: "kcm_plasmasearch", label: "Search Settings" },
  { id: "kcm_plasmakeyboard", label: "Plasma Keyboard" },
  { id: "kcm_powerdevilprofilesconfig", label: "Power Management" },
  { id: "kcm_printer_manager", label: "Printer Management" },
  { id: "kcm_proxy", label: "Proxy Settings" },
  { id: "kcm_pulseaudio", label: "Audio Volume" },
  { id: "kcm_push_notifications", label: "Push Notifications" },
  { id: "kcm_qtquicksettings", label: "Qt Quick Settings" },
  { id: "kcm_recentFiles", label: "File Activity History" },
  { id: "kcm_regionandlang", label: "Language and Formats" },
  { id: "kcm_samba", label: "Samba Status" },
  { id: "kcm_screenlocker", label: "Screen Locking" },
  { id: "kcm_sddm", label: "Login Manager (SDDM)" },
  { id: "kcm_sensors", label: "Sensors" },
  { id: "kcm_smserver", label: "Desktop Session" },
  { id: "kcm_solid_actions", label: "Device Actions" },
  { id: "kcm_soundtheme", label: "Sound Theme" },
  { id: "kcm_splashscreen", label: "Splash Screen" },
  { id: "kcm_style", label: "Application Style" },
  { id: "kcm_tablet", label: "Drawing Tablet" },
  { id: "kcm_touchpad", label: "Touchpad" },
  { id: "kcm_touchscreen", label: "Touchscreen" },
  { id: "kcm_updates", label: "Software Updates" },
  { id: "kcm_usb", label: "USB Devices" },
  { id: "kcm_users", label: "User Accounts" },
  { id: "kcm_virtualkeyboard", label: "Virtual Keyboard" },
  { id: "kcm_vulkan", label: "Vulkan Information" },
  { id: "kcm_wallpaper", label: "Wallpaper" },
  { id: "kcm_wayland", label: "Wayland Compositor Information" },
  { id: "kcm_webshortcuts", label: "Web Search Keywords" },
  { id: "kcm_workspace", label: "Workspace Behaviour" },
  { id: "kcm_xserver", label: "X-Server Information" },
  { id: "kcmspellchecking", label: "Spell Checker" },
  { id: "kcm_audiocd", label: "Audiocd IO Worker" },
];

// Convert KDE "R,G,B" color string to hex "#rrggbb".
export function rgbToHex(rgb: string): string {
  const parts = rgb.split(",").map((s) => parseInt(s.trim(), 10));
  if (parts.length < 3 || parts.slice(0, 3).some(isNaN)) return "#000000";
  return "#" + parts.slice(0, 3).map((v) => Math.max(0, Math.min(255, v)).toString(16).padStart(2, "0")).join("");
}

// Convert hex "#rrggbb" to KDE "R,G,B" color string.
export function hexToRgb(hex: string): string {
  const m = hex.replace("#", "");
  if (m.length !== 6) return "0,0,0";
  const r = parseInt(m.substring(0, 2), 16);
  const g = parseInt(m.substring(2, 4), 16);
  const b = parseInt(m.substring(4, 6), 16);
  return `${r},${g},${b}`;
}
