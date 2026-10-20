// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared content model for the AppArmor policy editor: the protojson
 * (camelCase, enum names as strings) shape stored in `Policy.content`, a
 * defensive parser, a compact serializer and the structural checks the
 * server applies too. Mirrors proto/policy/apparmor.proto and the rules in
 * server/pkg/macrules/macrules.go.
 */

export type ApparmorSwitch = "APPARMOR_SWITCH_UNSPECIFIED" | "APPARMOR_SWITCH_ON" | "APPARMOR_SWITCH_OFF";
export type ApparmorMode = "APPARMOR_MODE_UNSPECIFIED" | "APPARMOR_MODE_ENFORCE" | "APPARMOR_MODE_COMPLAIN" | "APPARMOR_MODE_DISABLED";

export interface ApparmorProfileEntry {
  /** A profile file name or a glob with * and ?. */
  match: string;
  mode: ApparmorMode;
  editVendorFlags: boolean;
}

export interface ApparmorCustomProfileEntry {
  name: string;
  text: string;
  description: string;
}

export interface ApparmorLocalOverrideEntry {
  profileFile: string;
  text: string;
}

export interface ApparmorContent {
  requireEnabled: boolean;
  ensureService: boolean;
  restrictUnprivilegedUserns: ApparmorSwitch;
  restrictUnprivilegedUnconfined: ApparmorSwitch;
  profiles: ApparmorProfileEntry[];
  customProfiles: ApparmorCustomProfileEntry[];
  localOverrides: ApparmorLocalOverrideEntry[];
  extraHomeDirs: string[];
}

export const APPARMOR_SWITCH_OPTIONS: { value: ApparmorSwitch; label: string }[] = [
  { value: "APPARMOR_SWITCH_UNSPECIFIED", label: "Keep the node's value" },
  { value: "APPARMOR_SWITCH_ON", label: "On" },
  { value: "APPARMOR_SWITCH_OFF", label: "Off" },
];

export const APPARMOR_MODE_OPTIONS: { value: ApparmorMode; label: string; help: string }[] = [
  { value: "APPARMOR_MODE_ENFORCE", label: "Enforce", help: "Denials are enforced." },
  { value: "APPARMOR_MODE_COMPLAIN", label: "Complain", help: "Missing permissions are logged and allowed (explicit deny rules still apply)." },
  { value: "APPARMOR_MODE_DISABLED", label: "Disabled", help: "The profile is unloaded and skipped at boot." },
];

const SWITCH_BY_NUMBER: ApparmorSwitch[] = ["APPARMOR_SWITCH_UNSPECIFIED", "APPARMOR_SWITCH_ON", "APPARMOR_SWITCH_OFF"];
const MODE_BY_NUMBER: ApparmorMode[] = ["APPARMOR_MODE_UNSPECIFIED", "APPARMOR_MODE_ENFORCE", "APPARMOR_MODE_COMPLAIN", "APPARMOR_MODE_DISABLED"];

function asSwitch(v: unknown): ApparmorSwitch {
  if (typeof v === "number") return SWITCH_BY_NUMBER[v] ?? "APPARMOR_SWITCH_UNSPECIFIED";
  if (typeof v === "string" && (SWITCH_BY_NUMBER as string[]).includes(v)) return v as ApparmorSwitch;
  return "APPARMOR_SWITCH_UNSPECIFIED";
}

function asMode(v: unknown): ApparmorMode {
  if (typeof v === "number") return MODE_BY_NUMBER[v] ?? "APPARMOR_MODE_UNSPECIFIED";
  if (typeof v === "string" && (MODE_BY_NUMBER as string[]).includes(v)) return v as ApparmorMode;
  return "APPARMOR_MODE_UNSPECIFIED";
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const bool = (v: unknown): boolean => v === true;
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

export function emptyApparmorContent(): ApparmorContent {
  return { requireEnabled: false, ensureService: false, restrictUnprivilegedUserns: "APPARMOR_SWITCH_UNSPECIFIED", restrictUnprivilegedUnconfined: "APPARMOR_SWITCH_UNSPECIFIED", profiles: [], customProfiles: [], localOverrides: [], extraHomeDirs: [] };
}

/** Parses stored content; anything malformed yields an empty, editable model. */
export function parseApparmorContent(raw: string): ApparmorContent {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return emptyApparmorContent();
  }
  const o = obj(parsed);
  return {
    requireEnabled: bool(o.requireEnabled),
    ensureService: bool(o.ensureService),
    restrictUnprivilegedUserns: asSwitch(o.restrictUnprivilegedUserns),
    restrictUnprivilegedUnconfined: asSwitch(o.restrictUnprivilegedUnconfined),
    profiles: arr(o.profiles).map((p) => ({ match: str(obj(p).match), mode: asMode(obj(p).mode), editVendorFlags: bool(obj(p).editVendorFlags) })),
    customProfiles: arr(o.customProfiles).map((c) => ({ name: str(obj(c).name), text: str(obj(c).text), description: str(obj(c).description) })),
    localOverrides: arr(o.localOverrides).map((l) => ({ profileFile: str(obj(l).profileFile), text: str(obj(l).text) })),
    extraHomeDirs: arr(o.extraHomeDirs).map(str).filter(Boolean),
  };
}

/** Serializes the model without empty fields. */
export function serializeApparmorContent(c: ApparmorContent): string {
  const out: Record<string, unknown> = {};
  if (c.requireEnabled) out.requireEnabled = true;
  if (c.ensureService) out.ensureService = true;
  if (c.restrictUnprivilegedUserns !== "APPARMOR_SWITCH_UNSPECIFIED") out.restrictUnprivilegedUserns = c.restrictUnprivilegedUserns;
  if (c.restrictUnprivilegedUnconfined !== "APPARMOR_SWITCH_UNSPECIFIED") out.restrictUnprivilegedUnconfined = c.restrictUnprivilegedUnconfined;
  if (c.profiles.length) out.profiles = c.profiles.map((p) => ({ match: p.match, mode: p.mode, ...(p.editVendorFlags ? { editVendorFlags: true } : {}) }));
  if (c.customProfiles.length) out.customProfiles = c.customProfiles.map((p) => ({ name: p.name, text: p.text, ...(p.description ? { description: p.description } : {}) }));
  if (c.localOverrides.length) out.localOverrides = c.localOverrides.map((l) => ({ profileFile: l.profileFile, text: l.text }));
  if (c.extraHomeDirs.length) out.extraHomeDirs = c.extraHomeDirs;
  return JSON.stringify(out, null, 2);
}

/* ── rules mirrored from server/pkg/macrules ── */

const PROFILE_FILE_RE = /^[A-Za-z0-9][A-Za-z0-9_.@+-]{0,127}$/;
const GLOB_RE = /^[A-Za-z0-9_.@+*?-]{1,128}$/;
const CUSTOM_NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,63}$/;
const HOME_DIR_RE = /^\/[A-Za-z0-9_./-]*\/$/;
export const RESERVED_PROFILE_FILES = new Set(["abi", "abstractions", "tunables", "local", "disable", "force-complain", "cache", "cache.d", "README"]);
export const PROTECTED_APPARMOR_PROFILES = new Set(["unconfined", "unprivileged_userns", "bwrap-userns-restrict"]);

export function isGlob(match: string): boolean {
  return /[*?]/.test(match);
}

export function profileFileProblem(name: string): string | null {
  if (!name) return "Enter a profile file name";
  if (!PROFILE_FILE_RE.test(name) || name.includes("..")) return "Profile file names are letters, digits, '_', '.', '@', '+' and '-'";
  if (RESERVED_PROFILE_FILES.has(name)) return `${name} is not a profile file`;
  if (/\.(dpkg-new|dpkg-old|dpkg-dist|rpmnew|rpmsave)$|~$/.test(name)) return "That is a package manager leftover, not a profile";
  return null;
}

export function profileMatchProblem(match: string): string | null {
  if (!match) return "Enter a profile file name or a pattern";
  if (!isGlob(match)) return profileFileProblem(match);
  if (!GLOB_RE.test(match) || match.includes("/")) return "Patterns use letters, digits, '_', '.', '@', '+', '-', '*' and '?'";
  return null;
}

export function customNameProblem(name: string): string | null {
  if (!name) return "Enter a name";
  if (!CUSTOM_NAME_RE.test(name) || name.includes("..")) return "Names are lowercase letters, digits, '_', '.' and '-' (at most 64 characters)";
  if (name.startsWith("bor-")) return "The bor- prefix is added automatically";
  return null;
}

/** Balanced braces, ignoring comments and strings. */
export function bracesProblem(text: string, what: string): string | null {
  if (!text.trim()) return `Enter the ${what}`;
  if (text.length > 64 * 1024) return `The ${what} is larger than 64 KiB`;
  let depth = 0;
  let inString = false;
  let inComment = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (inComment) {
      if (c === "\n") inComment = false;
      continue;
    }
    if (inString) {
      if (c === "\"") inString = false;
      else if (c === "\\") i++;
      continue;
    }
    if (c === "#" && !text.startsWith("#include", i)) {
      inComment = true;
      continue;
    }
    if (c === "\"") {
      inString = true;
      continue;
    }
    if (c === "{") depth++;
    else if (c === "}") {
      depth--;
      if (depth < 0) return `Unbalanced braces in the ${what}: '}' without '{'`;
    }
  }
  if (inString) return `Unterminated string in the ${what}`;
  if (depth !== 0) return `Unbalanced braces in the ${what}: ${depth} '{' without '}'`;
  return null;
}

/** The top-level profile header must be "profile bor-<name> ...". */
export function profileTextProblem(name: string, text: string): string | null {
  const p = bracesProblem(text, "profile");
  if (p) return p;
  const want = `bor-${name}`;
  let depth = 0;
  let found = 0;
  for (const rawLine of text.split("\n")) {
    let line = rawLine.trim();
    const hash = line.indexOf("#");
    if (hash >= 0 && !line.startsWith("#include", hash)) line = line.slice(0, hash).trim();
    if (!line) continue;
    if (depth === 0) {
      const fields = line.split(/\s+/);
      if (fields[0] === "profile") {
        if (fields[1] !== want) return `A top-level profile must be declared as "profile ${want}", found "${line}"`;
        found++;
      } else if (fields[0].startsWith("/") || fields[0].startsWith("@{")) {
        return `Declare the profile as "profile ${want} ${fields[0]}" instead of a bare path`;
      } else if (!["abi", "include", "#include", "alias"].includes(fields[0]) && !fields[0].startsWith("@") && !fields[0].startsWith("$")) {
        return `Unexpected text outside of the profile: "${line}"`;
      }
    }
    for (const ch of line) {
      if (ch === "{") depth++;
      else if (ch === "}") depth--;
    }
  }
  if (found === 0) return `The profile text must declare "profile ${want}"`;
  if (found > 1) return `The profile text declares "profile ${want}" more than once`;
  return null;
}

export function homeDirProblem(dir: string): string | null {
  if (!dir) return "Enter a directory";
  if (!HOME_DIR_RE.test(dir) || dir.includes("..") || dir.includes("//")) return "An absolute path ending in '/' without special characters (for example /nfs/home/)";
  if (dir === "/") return "The root directory cannot be a home directory root";
  return null;
}

export function hasAnyApparmorSetting(c: ApparmorContent): boolean {
  return c.requireEnabled || c.ensureService || c.restrictUnprivilegedUserns !== "APPARMOR_SWITCH_UNSPECIFIED" || c.restrictUnprivilegedUnconfined !== "APPARMOR_SWITCH_UNSPECIFIED" || c.profiles.length > 0 || c.customProfiles.length > 0 || c.localOverrides.length > 0 || c.extraHomeDirs.length > 0;
}

/** The save-time check: the first problem, or null when the content is valid. */
export function validateApparmorContent(c: ApparmorContent): string | null {
  if (!hasAnyApparmorSetting(c)) return "Set a requirement, a restriction, a profile mode, a custom profile, a local override or a home directory before saving";
  const seen = new Set<string>();
  for (const p of c.profiles) {
    const problem = profileMatchProblem(p.match);
    if (problem) return `Profile ${p.match || "(empty)"}: ${problem}`;
    if (p.mode === "APPARMOR_MODE_UNSPECIFIED") return `Profile ${p.match}: choose a mode`;
    if (p.mode === "APPARMOR_MODE_DISABLED" && isGlob(p.match)) return `Profile ${p.match}: profiles are disabled one by one, not by pattern`;
    if (p.mode === "APPARMOR_MODE_DISABLED" && PROTECTED_APPARMOR_PROFILES.has(p.match)) return `Profile ${p.match} cannot be disabled: the user namespace restriction depends on it`;
    if (p.mode === "APPARMOR_MODE_DISABLED" && p.editVendorFlags) return `Profile ${p.match}: editing vendor flags applies to enforce and complain only`;
    if (p.match.startsWith("bor-")) return `Profile ${p.match} is a Bor profile; its mode is part of its text`;
    if (seen.has(p.match)) return `Profile ${p.match} is listed twice`;
    seen.add(p.match);
  }
  seen.clear();
  for (const cp of c.customProfiles) {
    const problem = customNameProblem(cp.name) ?? profileTextProblem(cp.name, cp.text);
    if (problem) return `Custom profile ${cp.name || "(unnamed)"}: ${problem}`;
    if (seen.has(cp.name)) return `Custom profile ${cp.name} is listed twice`;
    seen.add(cp.name);
  }
  seen.clear();
  for (const o of c.localOverrides) {
    const problem = profileFileProblem(o.profileFile) ?? bracesProblem(o.text, "override");
    if (problem) return `Local override ${o.profileFile || "(empty)"}: ${problem}`;
    if (o.profileFile.startsWith("bor-")) return `Local override ${o.profileFile}: a Bor profile is edited as a custom profile`;
    if (seen.has(o.profileFile)) return `Local override ${o.profileFile} is listed twice`;
    seen.add(o.profileFile);
  }
  seen.clear();
  for (const d of c.extraHomeDirs) {
    const problem = homeDirProblem(d);
    if (problem) return `Home directory ${d || "(empty)"}: ${problem}`;
    if (seen.has(d)) return `Home directory ${d} is listed twice`;
    seen.add(d);
  }
  return null;
}

/* ── templates ── */

/** The stub Ubuntu uses to exempt one application from the user namespace restriction. */
export function usernsStubTemplate(name: string, binary: string): string {
  const path = binary || "/opt/application/bin/application";
  const n = name || "application";
  return `# Allows ${path} to create user namespaces (sandboxes) while the\n# system-wide restriction is on. The profile confines nothing else.\n\nabi <abi/4.0>,\ninclude <tunables/global>\n\nprofile bor-${n} ${path} flags=(unconfined) {\n  userns,\n\n  # Site-specific additions and overrides. See local/README for details.\n  include if exists <local/bor-${n}>\n}\n`;
}

/** A confining starter profile in complain mode: run it, read the ALLOWED records, then tighten and switch to enforce. */
export function confineStarterTemplate(name: string, binary: string): string {
  const path = binary || "/opt/application/bin/application";
  const n = name || "application";
  return `# Confines ${path}. Starts in complain mode: every access the rules\n# below do not cover is logged as ALLOWED on the node page; add rules for\n# the legitimate ones, then remove the complain flag. Avoid deny rules\n# while in complain mode, they are enforced even then.\n\nabi <abi/4.0>,\ninclude <tunables/global>\n\nprofile bor-${n} ${path} flags=(complain) {\n  include <abstractions/base>\n  include <abstractions/fonts>\n  include <abstractions/nameservice>\n  include <abstractions/dbus-session-strict>\n  include <abstractions/X>\n\n  ${path} mr,\n  /etc/** r,\n  owner @{HOME}/.config/** rwk,\n  owner @{HOME}/.cache/** rwk,\n  owner @{HOME}/.local/share/** rwk,\n  owner @{HOME}/Documents/** rw,\n  owner @{HOME}/Downloads/** rw,\n  network inet stream,\n  network inet6 stream,\n\n  # Site-specific additions and overrides. See local/README for details.\n  include if exists <local/bor-${n}>\n}\n`;
}

/** Everything the application does is allowed, except the network. */
export function denyNetworkTemplate(name: string, binary: string): string {
  const path = binary || "/opt/application/bin/application";
  const n = name || "application";
  return `# ${path} may do anything except use the network.\n\nabi <abi/4.0>,\ninclude <tunables/global>\n\nprofile bor-${n} ${path} {\n  include <abstractions/base>\n  file,\n  capability,\n  unix,\n  dbus,\n  signal,\n  ptrace,\n  mount,\n  umount,\n  pivot_root,\n  change_profile,\n  userns,\n  deny network,\n\n  # Site-specific additions and overrides. See local/README for details.\n  include if exists <local/bor-${n}>\n}\n`;
}

export interface ApparmorProfileTemplate {
  id: string;
  label: string;
  description: string;
  render: (name: string, binary: string) => string;
}

export const APPARMOR_PROFILE_TEMPLATES: ApparmorProfileTemplate[] = [
  { id: "userns", label: "Allow user namespaces for an application", description: "For Electron and other sandboxed applications that Ubuntu's restriction blocks. Confines nothing else.", render: usernsStubTemplate },
  { id: "confine", label: "Confine an application, complain first", description: "A starter profile with the usual desktop abstractions in complain mode.", render: confineStarterTemplate },
  { id: "deny-network", label: "Deny network for an application", description: "Everything allowed except the network.", render: denyNetworkTemplate },
];

/** A short description of a policy for summaries. */
export function describeApparmorContent(c: ApparmorContent): string[] {
  const parts: string[] = [];
  if (c.requireEnabled) parts.push("AppArmor required");
  if (c.ensureService) parts.push("service kept running");
  if (c.restrictUnprivilegedUserns !== "APPARMOR_SWITCH_UNSPECIFIED") parts.push(`user namespace restriction ${c.restrictUnprivilegedUserns === "APPARMOR_SWITCH_ON" ? "on" : "off"}`);
  if (c.restrictUnprivilegedUnconfined !== "APPARMOR_SWITCH_UNSPECIFIED") parts.push(`unconfined restriction ${c.restrictUnprivilegedUnconfined === "APPARMOR_SWITCH_ON" ? "on" : "off"}`);
  if (c.profiles.length) parts.push(`${c.profiles.length} profile mode${c.profiles.length === 1 ? "" : "s"}`);
  if (c.customProfiles.length) parts.push(`${c.customProfiles.length} custom profile${c.customProfiles.length === 1 ? "" : "s"}`);
  if (c.localOverrides.length) parts.push(`${c.localOverrides.length} local override${c.localOverrides.length === 1 ? "" : "s"}`);
  if (c.extraHomeDirs.length) parts.push(`${c.extraHomeDirs.length} home director${c.extraHomeDirs.length === 1 ? "y" : "ies"}`);
  return parts;
}

export function apparmorModeLabel(mode: ApparmorMode): string {
  return APPARMOR_MODE_OPTIONS.find((o) => o.value === mode)?.label ?? "Keep";
}
