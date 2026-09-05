// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared content model for the Flatpak policy editor: the protojson
 * (camelCase, enum names as strings) shape stored in `Policy.content`, a
 * defensive parser and a compact serializer. Mirrors proto/policy/flatpak.proto.
 */

import {
  isValidFlatpakAppId,
  isValidFlatpakBranch,
  isValidFlatpakFilterRef,
  isValidFlatpakRemoteName,
  isValidFlatpakRemoteUrl,
} from "../../apiClient/flatpakApi";

export type FlatpakFilterMode =
  | "FLATPAK_FILTER_MODE_NONE"
  | "FLATPAK_FILTER_MODE_ALLOWLIST"
  | "FLATPAK_FILTER_MODE_DENYLIST";

export type FlatpakAppState =
  | "FLATPAK_APP_STATE_PRESENT"
  | "FLATPAK_APP_STATE_ABSENT"
  | "FLATPAK_APP_STATE_LATEST";

export type FlatpakScope = "FLATPAK_SCOPE_SYSTEM" | "FLATPAK_SCOPE_USER";

export interface FlatpakRemoteEntry {
  name: string;
  url: string;
  title: string;
  enabled: boolean;
  gpgVerify: boolean;
  /** base64 keyring, "" when none. */
  gpgKeyData: string;
  gpgKeyId: string;
  subset: string;
  defaultBranch: string;
  priority: number;
  filterMode: FlatpakFilterMode;
  filterRefs: string[];
  noEnumerate: boolean;
  noUseForDeps: boolean;
  collectionId: string;
  homepage: string;
  comment: string;
}

export interface FlatpakAppEntry {
  appId: string;
  remote: string;
  branch: string;
  state: FlatpakAppState;
  scope: FlatpakScope;
  optional: boolean;
  deleteData: boolean;
  displayName: string;
}

export interface FlatpakContent {
  remotes: FlatpakRemoteEntry[];
  apps: FlatpakAppEntry[];
  installation: string;
  autoUpdate: boolean;
  autoUpdateIntervalHours: number;
  uninstallUnused: boolean;
  operationTimeoutMinutes: number;
}

export const DEFAULT_AUTO_UPDATE_HOURS = 24;
export const DEFAULT_OPERATION_TIMEOUT_MIN = 30;

export const FILTER_MODE_OPTIONS: { value: FlatpakFilterMode; label: string; description: string }[] = [
  { value: "FLATPAK_FILTER_MODE_NONE", label: "No filter", description: "Every ref the remote offers is available" },
  { value: "FLATPAK_FILTER_MODE_ALLOWLIST", label: "Allow list", description: "Deny everything except runtimes and the listed refs" },
  { value: "FLATPAK_FILTER_MODE_DENYLIST", label: "Deny list", description: "Allow everything except the listed refs" },
];

export const APP_STATE_OPTIONS: { value: FlatpakAppState; label: string; description: string }[] = [
  { value: "FLATPAK_APP_STATE_PRESENT", label: "Present", description: "Install if missing" },
  { value: "FLATPAK_APP_STATE_LATEST", label: "Latest", description: "Install and update on every sync" },
  { value: "FLATPAK_APP_STATE_ABSENT", label: "Absent", description: "Uninstall if installed" },
];

export const SUBSET_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "All apps" },
  { value: "verified", label: "verified — apps published by their upstream developers" },
  { value: "floss", label: "floss — open-source apps only" },
  { value: "verified_floss", label: "verified_floss — verified and open-source" },
];

export function newRemoteEntry(partial: Partial<FlatpakRemoteEntry> = {}): FlatpakRemoteEntry {
  return {
    name: "",
    url: "",
    title: "",
    enabled: true,
    gpgVerify: true,
    gpgKeyData: "",
    gpgKeyId: "",
    subset: "",
    defaultBranch: "",
    priority: 0,
    filterMode: "FLATPAK_FILTER_MODE_NONE",
    filterRefs: [],
    noEnumerate: false,
    noUseForDeps: false,
    collectionId: "",
    homepage: "",
    comment: "",
    ...partial,
  };
}

export function newAppEntry(partial: Partial<FlatpakAppEntry> = {}): FlatpakAppEntry {
  return {
    appId: "",
    remote: "",
    branch: "",
    state: "FLATPAK_APP_STATE_PRESENT",
    scope: "FLATPAK_SCOPE_SYSTEM",
    optional: false,
    deleteData: false,
    displayName: "",
    ...partial,
  };
}

/* ── enum coercion (accept names or protobuf numbers) ── */

function coerceFilterMode(v: unknown): FlatpakFilterMode {
  if (v === 1 || v === "FLATPAK_FILTER_MODE_ALLOWLIST") return "FLATPAK_FILTER_MODE_ALLOWLIST";
  if (v === 2 || v === "FLATPAK_FILTER_MODE_DENYLIST") return "FLATPAK_FILTER_MODE_DENYLIST";
  return "FLATPAK_FILTER_MODE_NONE";
}

function coerceAppState(v: unknown): FlatpakAppState {
  if (v === 2 || v === "FLATPAK_APP_STATE_ABSENT") return "FLATPAK_APP_STATE_ABSENT";
  if (v === 3 || v === "FLATPAK_APP_STATE_LATEST") return "FLATPAK_APP_STATE_LATEST";
  return "FLATPAK_APP_STATE_PRESENT";
}

function coerceScope(v: unknown): FlatpakScope {
  if (v === 2 || v === "FLATPAK_SCOPE_USER") return "FLATPAK_SCOPE_USER";
  return "FLATPAK_SCOPE_SYSTEM";
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const strList = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);

export function parseFlatpakContent(raw: string): FlatpakContent {
  let p: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(raw || "{}");
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) p = parsed as Record<string, unknown>;
  } catch {
    /* fall through to empty content */
  }
  const remotes = (Array.isArray(p.remotes) ? p.remotes : [])
    .filter((r): r is Record<string, unknown> => !!r && typeof r === "object")
    .map((r) =>
      newRemoteEntry({
        name: str(r.name),
        url: str(r.url),
        title: str(r.title),
        enabled: r.enabled === undefined ? true : !!r.enabled,
        gpgVerify: r.gpgVerify === undefined ? true : !!r.gpgVerify,
        gpgKeyData: str(r.gpgKeyData),
        gpgKeyId: str(r.gpgKeyId),
        subset: str(r.subset),
        defaultBranch: str(r.defaultBranch),
        priority: num(r.priority),
        filterMode: coerceFilterMode(r.filterMode),
        filterRefs: strList(r.filterRefs),
        noEnumerate: !!r.noEnumerate,
        noUseForDeps: !!r.noUseForDeps,
        collectionId: str(r.collectionId),
        homepage: str(r.homepage),
        comment: str(r.comment),
      }),
    );
  const apps = (Array.isArray(p.apps) ? p.apps : [])
    .filter((a): a is Record<string, unknown> => !!a && typeof a === "object")
    .map((a) =>
      newAppEntry({
        appId: str(a.appId),
        remote: str(a.remote),
        branch: str(a.branch),
        state: coerceAppState(a.state),
        scope: coerceScope(a.scope),
        optional: !!a.optional,
        deleteData: !!a.deleteData,
        displayName: str(a.displayName),
      }),
    );
  return {
    remotes,
    apps,
    installation: str(p.installation),
    autoUpdate: !!p.autoUpdate,
    autoUpdateIntervalHours: num(p.autoUpdateIntervalHours),
    uninstallUnused: !!p.uninstallUnused,
    operationTimeoutMinutes: num(p.operationTimeoutMinutes),
  };
}

/** Emits only the fields that carry data; protojson on the server accepts the camelCase keys. */
export function serializeFlatpakContent(c: FlatpakContent): string {
  const out: Record<string, unknown> = {};
  if (c.remotes.length) {
    out.remotes = c.remotes.map((r) => {
      const o: Record<string, unknown> = { name: r.name, url: r.url, enabled: r.enabled, gpgVerify: r.gpgVerify };
      if (r.title) o.title = r.title;
      if (r.gpgKeyData) o.gpgKeyData = r.gpgKeyData;
      if (r.gpgKeyId) o.gpgKeyId = r.gpgKeyId;
      if (r.subset) o.subset = r.subset;
      if (r.defaultBranch) o.defaultBranch = r.defaultBranch;
      if (r.priority) o.priority = r.priority;
      if (r.filterMode !== "FLATPAK_FILTER_MODE_NONE") {
        o.filterMode = r.filterMode;
        o.filterRefs = r.filterRefs.map((x) => x.trim()).filter((x) => x !== "");
      }
      if (r.noEnumerate) o.noEnumerate = true;
      if (r.noUseForDeps) o.noUseForDeps = true;
      if (r.collectionId) o.collectionId = r.collectionId;
      if (r.homepage) o.homepage = r.homepage;
      if (r.comment) o.comment = r.comment;
      return o;
    });
  }
  if (c.apps.length) {
    out.apps = c.apps.map((a) => {
      const o: Record<string, unknown> = { appId: a.appId, state: a.state };
      if (a.remote) o.remote = a.remote;
      if (a.branch) o.branch = a.branch;
      if (a.scope !== "FLATPAK_SCOPE_SYSTEM") o.scope = a.scope;
      if (a.optional) o.optional = true;
      if (a.deleteData) o.deleteData = true;
      if (a.displayName) o.displayName = a.displayName;
      return o;
    });
  }
  if (c.installation) out.installation = c.installation;
  if (c.autoUpdate) {
    out.autoUpdate = true;
    if (c.autoUpdateIntervalHours) out.autoUpdateIntervalHours = c.autoUpdateIntervalHours;
  }
  if (c.uninstallUnused) out.uninstallUnused = true;
  if (c.operationTimeoutMinutes) out.operationTimeoutMinutes = c.operationTimeoutMinutes;
  return JSON.stringify(out, null, 2);
}

/* ── validation (messages mirror server/internal/services/flatpak.go) ── */

export function validateRemoteEntry(r: FlatpakRemoteEntry): string | null {
  if (!r.name.trim()) return "Remote name is required";
  if (!isValidFlatpakRemoteName(r.name)) {
    return "Remote name must start with a letter or digit and contain only letters, digits, dots, underscores or hyphens (max 64 characters)";
  }
  if (!r.url.trim()) return "Repository URL is required";
  if (!isValidFlatpakRemoteUrl(r.url)) return "Repository URL must start with https:// or oci+https://";
  if (r.gpgVerify && !r.gpgKeyData) return "GPG verification is enabled but no key was provided — upload the remote's public key or disable verification";
  if (r.gpgKeyId && !/^[0-9A-Fa-f]{40}$/.test(r.gpgKeyId)) return "Key fingerprint must be 40 hexadecimal characters";
  if (r.defaultBranch && !isValidFlatpakBranch(r.defaultBranch)) return "Default branch may contain only letters, digits, dots, underscores or hyphens";
  if (r.priority < 0 || r.priority > 1000) return "Priority must be between 0 and 1000";
  if (r.subset && !/^[A-Za-z0-9_-]{1,64}$/.test(r.subset)) return "Subset may contain only letters, digits, underscores or hyphens";
  if (r.collectionId && !/^[A-Za-z0-9._-]{1,128}$/.test(r.collectionId)) return "Collection ID may contain only letters, digits, dots, underscores or hyphens";
  if (r.filterMode !== "FLATPAK_FILTER_MODE_NONE") {
    for (const ref of r.filterRefs) {
      const t = ref.trim();
      if (t === "") continue;
      if (!isValidFlatpakFilterRef(t)) return `Filter ref "${t}" may contain only letters, digits, dots, underscores, hyphens, slashes and *`;
    }
  }
  return null;
}

export function validateAppEntry(a: FlatpakAppEntry, remotes: FlatpakRemoteEntry[]): string | null {
  if (!isValidFlatpakAppId(a.appId)) return `"${a.appId}" is not a valid application ID (expected reverse-DNS, e.g. org.mozilla.firefox)`;
  if (a.remote && !isValidFlatpakRemoteName(a.remote)) return `"${a.remote}" is not a valid remote name`;
  if (!isValidFlatpakBranch(a.branch)) return `Branch "${a.branch}" may contain only letters, digits, dots, underscores or hyphens`;
  void remotes;
  return null;
}

/** First problem that would make the content unsaveable, or null. */
export function validateFlatpakContent(c: FlatpakContent): string | null {
  const seenRemotes = new Set<string>();
  for (const r of c.remotes) {
    const err = validateRemoteEntry(r);
    if (err) return `Remote ${r.name || "(unnamed)"}: ${err}`;
    if (seenRemotes.has(r.name)) return `Remote "${r.name}" is listed twice`;
    seenRemotes.add(r.name);
  }
  const seenApps = new Set<string>();
  for (const a of c.apps) {
    const err = validateAppEntry(a, c.remotes);
    if (err) return err;
    const key = `${a.appId}|${a.scope}`;
    if (seenApps.has(key)) return `Application "${a.appId}" is listed twice`;
    seenApps.add(key);
  }
  if (c.installation && !isValidFlatpakRemoteName(c.installation)) return "Installation name may contain only letters, digits, dots, underscores or hyphens";
  if (c.autoUpdateIntervalHours && (c.autoUpdateIntervalHours < 1 || c.autoUpdateIntervalHours > 168)) return "Auto-update interval must be between 1 and 168 hours";
  if (c.operationTimeoutMinutes && (c.operationTimeoutMinutes < 5 || c.operationTimeoutMinutes > 240)) return "Operation timeout must be between 5 and 240 minutes";
  return null;
}

export function filterModeLabel(m: FlatpakFilterMode): string {
  return FILTER_MODE_OPTIONS.find((o) => o.value === m)?.label ?? "No filter";
}

export function appStateLabel(s: FlatpakAppState): string {
  return APP_STATE_OPTIONS.find((o) => o.value === s)?.label ?? s;
}
