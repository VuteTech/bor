// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Pure helpers for policy types whose content is a flat JSON object keyed by
 * policy name (Firefox, Thunderbird, Chrome, Edge). Content is always passed
 * around as the raw string so the editor never owns a parsed copy that could
 * drift from what is saved.
 */

/** Parse content into an object; anything that isn't a JSON object yields `{}`. */
export function parseContentObject(content: string): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(content || "{}");
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

/** Group a flat field catalogue by its `group` for the tree panel. */
export function buildFieldTree<T extends { group: string }>(fields: readonly T[]): Map<string, T[]> {
  const groups = new Map<string, T[]>();
  for (const f of fields) {
    const arr = groups.get(f.group) || [];
    arr.push(f);
    groups.set(f.group, arr);
  }
  return groups;
}

/** Keys from the catalogue that are present in the content, in catalogue order. */
export function detectConfiguredKeys(fields: readonly { key: string }[], content: string): string[] {
  const parsed = parseContentObject(content);
  return fields.filter((f) => f.key in parsed).map((f) => f.key);
}

/** Value of one key, or `undefined` when absent / content is not valid JSON. */
export function extractContentValue(content: string, key: string): unknown {
  return parseContentObject(content)[key];
}

/** Set (or add) one key, preserving all other keys. */
export function setContentKey(key: string, value: unknown, content: string): string {
  const parsed = parseContentObject(content);
  parsed[key] = value ?? null;
  return JSON.stringify(parsed, null, 2);
}

/** Remove one key, preserving all other keys. */
export function removeContentKey(key: string, content: string): string {
  const parsed = parseContentObject(content);
  delete parsed[key];
  return JSON.stringify(parsed, null, 2);
}

/**
 * Canonical string form of policy content, so cosmetic JSON formatting
 * differences don't register as unsaved edits. Falls back to the raw string
 * when the content isn't valid JSON (mid-edit in a raw editor).
 */
export function normalizePolicyContent(content: string): string {
  try {
    return JSON.stringify(JSON.parse(content || "{}"));
  } catch {
    return content;
  }
}
