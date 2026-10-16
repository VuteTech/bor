// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Types and lookups for the KDE settings catalogue. The built-in entries
 * come from `generated/kconfig_catalogue.ts`, produced from the JSON the
 * server embeds (`npm run gen:kconfig-catalogue`). Entries that nodes
 * report (KConfigXT schemas) are added at runtime with
 * `registerCatalogueEntries`; a built-in entry always wins over a reported
 * one for the same file, group and key.
 */

import { KCONFIG_BUILTIN_CATALOGUE } from "../../../generated/kconfig_catalogue";

/** KConfigXT value types the editor knows how to render. */
export type KConfigValueType =
  | "Bool"
  | "Int"
  | "UInt"
  | "Double"
  | "String"
  | "StringList"
  | "Enum"
  | "Path"
  | "Url"
  | "Color"
  | "Font";

/** An entry written together with a catalogue setting (composite settings). */
export interface KConfigExtraWrite {
  file: string;
  groupPath: string[];
  key: string;
  /** Literal value, or `$VALUE` to mirror the main setting's value. */
  value: string;
  immutable?: boolean;
  expand?: boolean;
  delete?: boolean;
  locale?: string;
}

/** One known KDE setting (camelCase form of the KConfigCatalogueEntry proto). */
export interface KConfigCatalogueEntry {
  id: string;
  file: string;
  groupPath: string[];
  key: string;
  type: KConfigValueType;
  label: string;
  description?: string;
  defaultValue?: string;
  choices?: string[];
  rangeMin?: string;
  rangeMax?: string;
  category: string;
  /** KDE Kiosk restriction: false restricts, true allows. */
  inverted?: boolean;
  /** New entries start as enforced ([$i]). */
  immutableDefault?: boolean;
  /** Plasma major versions the setting applies to; empty means any. */
  plasmaMajor?: string[];
  reload?: string;
  windowsEquivalent?: string;
  /** "builtin" for curated entries, "kcfg" for entries reported by nodes. */
  source?: string;
  extraWrites?: KConfigExtraWrite[];
}

/** Placeholder in an extra write's value meaning "same as the main setting". */
export const KCONFIG_VALUE_PLACEHOLDER = "$VALUE";

const KNOWN_TYPES = new Set<string>(["Bool", "Int", "UInt", "Double", "String", "StringList", "Enum", "Path", "Url", "Color", "Font"]);

const entries: KConfigCatalogueEntry[] = [];
const byId = new Map<string, KConfigCatalogueEntry>();
const byTarget = new Map<string, KConfigCatalogueEntry>();

function addEntry(e: KConfigCatalogueEntry): boolean {
  if (!e.id || !e.file || !e.key || !KNOWN_TYPES.has(e.type)) return false;
  const groupPath = Array.isArray(e.groupPath) ? e.groupPath : [];
  if (groupPath.length === 0) return false;
  const target = targetKey(e.file, groupPath, e.key);
  if (byId.has(e.id) || byTarget.has(target)) return false;
  const n: KConfigCatalogueEntry = { ...e, groupPath, label: e.label || e.key, category: e.category || `Node: ${e.file}` };
  entries.push(n);
  byId.set(n.id, n);
  byTarget.set(target, n);
  return true;
}

for (const e of KCONFIG_BUILTIN_CATALOGUE) addEntry(e);

/** The curated, built-in entries (never changes at runtime). */
export const KCONFIG_CATALOGUE: readonly KConfigCatalogueEntry[] = KCONFIG_BUILTIN_CATALOGUE;

/** Every entry known right now: built-in ones first, then registered node entries. */
export function allCatalogueEntries(): readonly KConfigCatalogueEntry[] {
  return entries;
}

/** Category names in the order they first appear. */
export function allCategories(): string[] {
  return Array.from(new Set(entries.map((e) => e.category)));
}

/**
 * Adds entries a node reported. Entries whose id or target the catalogue
 * already knows are ignored, so curated entries keep their labels and
 * semantics. Returns how many entries were added.
 */
export function registerCatalogueEntries(extra: readonly KConfigCatalogueEntry[]): number {
  let added = 0;
  for (const e of extra) if (addEntry(e)) added++;
  return added;
}

export function findCatalogueEntry(id: string): KConfigCatalogueEntry | undefined {
  return byId.get(id);
}

/** Finds the catalogue entry that writes exactly this file, group path and key. */
export function findCatalogueEntryByTarget(file: string, groupPath: string[], key: string): KConfigCatalogueEntry | undefined {
  return byTarget.get(targetKey(file, groupPath, key));
}

export function targetKey(file: string, groupPath: string[], key: string): string {
  return `${file}\u0000${groupPath.join("][")}\u0000${key}`;
}

/** "[A][B]" for a group path. */
export function groupPathLabel(groupPath: string[]): string {
  return groupPath.map((g) => `[${g}]`).join("");
}

/** "file · [Group] · key", the way the editor shows a setting's target. */
export function targetLabel(file: string, groupPath: string[], key: string): string {
  return `${file} · ${groupPathLabel(groupPath)} · ${key}`;
}

/** Human-readable state of a Bool setting's stored value. */
export function boolDisplay(entry: Pick<KConfigCatalogueEntry, "inverted">, value: string): string {
  if (entry.inverted) return value === "false" ? "Restricted" : "Allowed";
  return value === "true" ? "Enabled" : "Disabled";
}

/** Value a freshly selected catalogue setting previews with. */
export function defaultCatalogueValue(entry: KConfigCatalogueEntry): string {
  if (entry.type === "Bool") {
    // Restrictions start restricted: that is why one adds them. Other
    // booleans start from the KDE default, or enabled.
    if (entry.inverted) return "false";
    return entry.defaultValue ?? "true";
  }
  if (entry.defaultValue !== undefined && entry.defaultValue !== "") return entry.defaultValue;
  if (entry.type === "Enum") return entry.choices?.[0] ?? "";
  if (entry.type === "Int" || entry.type === "UInt" || entry.type === "Double") return entry.rangeMin ?? "0";
  return "";
}
