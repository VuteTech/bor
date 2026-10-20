// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Model of an `Updates` (Software updates) policy: parse and serialize the
 * protojson content (camelCase keys, enum names), validate it, and the
 * labels and templates the editor offers.
 */

export type UpdatesMode = "UPDATES_MODE_UNSPECIFIED" | "UPDATES_MODE_OFF" | "UPDATES_MODE_CHECK" | "UPDATES_MODE_SECURITY" | "UPDATES_MODE_ALL";
export type UpdatesRebootPolicy = "UPDATES_REBOOT_UNSPECIFIED" | "UPDATES_REBOOT_NEVER" | "UPDATES_REBOOT_WHEN_NEEDED" | "UPDATES_REBOOT_ALWAYS";

export interface UpdatesSchedule {
  onCalendar: string;
  randomizedDelay: string;
  /** null = unset (on). */
  persistent: boolean | null;
}

export interface UpdatesReboot {
  policy: UpdatesRebootPolicy;
  window: string;
  onlyWithoutSessions: boolean;
  forceAfterDays: number;
  warnMinutes: number;
  message: string;
}

/** Compliance thresholds in days; 0 leaves a threshold unchecked. */
export interface UpdatesLimits {
  securityMaxAgeDays: number;
  rebootMaxAgeDays: number;
  checkMaxAgeDays: number;
}

export interface UpdatesFirmware {
  /** null = keep the node's fwupd-refresh.timer state. */
  refreshMetadata: boolean | null;
  reportPending: boolean;
}

export interface UpdatesContent {
  mode: UpdatesMode;
  schedule: UpdatesSchedule;
  reboot: UpdatesReboot;
  hold: string[];
  removeUnusedDependencies: boolean;
  installEngine: boolean;
  requireEngine: boolean;
  limits: UpdatesLimits;
  firmware: UpdatesFirmware;
}

export const UPDATES_MAX_LIMIT_DAYS = 3650;

export const UPDATES_MODE_OPTIONS: { value: UpdatesMode; label: string; help: string }[] = [
  { value: "UPDATES_MODE_UNSPECIFIED", label: "Keep the node's configuration", help: "The policy does not change what the node installs on its own." },
  { value: "UPDATES_MODE_OFF", label: "No unattended updates", help: "The engine's timer is disabled. Updates are installed by hand or through another tool." },
  { value: "UPDATES_MODE_CHECK", label: "Check and report only", help: "Updates are downloaded and reported on the node page; nothing is installed. os-update has no check mode, so SUSE nodes report the pending updates without downloading." },
  { value: "UPDATES_MODE_SECURITY", label: "Install security updates", help: "Updates with a security advisory are installed on the schedule. Image systems (Kinoite, Silverblue) cannot select updates and report this as inapplicable." },
  { value: "UPDATES_MODE_ALL", label: "Install all updates", help: "Every available update is installed on the schedule." },
];

export const UPDATES_REBOOT_OPTIONS: { value: UpdatesRebootPolicy; label: string; help: string }[] = [
  { value: "UPDATES_REBOOT_UNSPECIFIED", label: "Keep the node's behaviour", help: "The policy says nothing about reboots; a pending reboot is reported." },
  { value: "UPDATES_REBOOT_NEVER", label: "Never reboot", help: "A pending reboot is reported on the node page and the Compliance page; the node reboots when someone restarts it." },
  { value: "UPDATES_REBOOT_WHEN_NEEDED", label: "Reboot when an update needs it", help: "A kernel, systemd or library update that needs a reboot restarts the node within the window, with the rules below." },
  { value: "UPDATES_REBOOT_ALWAYS", label: "Reboot after every update run", help: "The node restarts after any run that installed something, within the window." },
];

const MODE_BY_NUMBER: UpdatesMode[] = ["UPDATES_MODE_UNSPECIFIED", "UPDATES_MODE_OFF", "UPDATES_MODE_CHECK", "UPDATES_MODE_SECURITY", "UPDATES_MODE_ALL"];
const REBOOT_BY_NUMBER: UpdatesRebootPolicy[] = ["UPDATES_REBOOT_UNSPECIFIED", "UPDATES_REBOOT_NEVER", "UPDATES_REBOOT_WHEN_NEEDED", "UPDATES_REBOOT_ALWAYS"];

function asMode(v: unknown): UpdatesMode {
  if (typeof v === "number") return MODE_BY_NUMBER[v] ?? "UPDATES_MODE_UNSPECIFIED";
  return MODE_BY_NUMBER.includes(v as UpdatesMode) ? (v as UpdatesMode) : "UPDATES_MODE_UNSPECIFIED";
}

function asReboot(v: unknown): UpdatesRebootPolicy {
  if (typeof v === "number") return REBOOT_BY_NUMBER[v] ?? "UPDATES_REBOOT_UNSPECIFIED";
  return REBOOT_BY_NUMBER.includes(v as UpdatesRebootPolicy) ? (v as UpdatesRebootPolicy) : "UPDATES_REBOOT_UNSPECIFIED";
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? Math.trunc(v) : 0);
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

export function emptyUpdatesContent(): UpdatesContent {
  return {
    mode: "UPDATES_MODE_UNSPECIFIED",
    schedule: { onCalendar: "", randomizedDelay: "", persistent: null },
    reboot: { policy: "UPDATES_REBOOT_UNSPECIFIED", window: "", onlyWithoutSessions: true, forceAfterDays: 0, warnMinutes: 10, message: "" },
    hold: [],
    removeUnusedDependencies: false,
    installEngine: true,
    requireEngine: false,
    limits: { securityMaxAgeDays: 0, rebootMaxAgeDays: 0, checkMaxAgeDays: 0 },
    firmware: { refreshMetadata: null, reportPending: false },
  };
}

export function parseUpdatesContent(raw: string): UpdatesContent {
  const c = emptyUpdatesContent();
  let data: unknown;
  try {
    data = raw.trim() ? JSON.parse(raw) : {};
  } catch {
    return c;
  }
  const d = obj(data);
  c.mode = asMode(d.mode);
  const s = obj(d.schedule);
  c.schedule = { onCalendar: str(s.onCalendar), randomizedDelay: str(s.randomizedDelay), persistent: typeof s.persistent === "boolean" ? s.persistent : null };
  const r = obj(d.reboot);
  c.reboot = {
    policy: asReboot(r.policy),
    window: str(r.window),
    onlyWithoutSessions: r.onlyWithoutSessions !== false,
    forceAfterDays: num(r.forceAfterDays),
    warnMinutes: typeof r.warnMinutes === "number" ? num(r.warnMinutes) : 10,
    message: str(r.message),
  };
  c.hold = Array.isArray(d.hold) ? d.hold.filter((h): h is string => typeof h === "string") : [];
  c.removeUnusedDependencies = d.removeUnusedDependencies === true;
  c.installEngine = d.installEngine !== false;
  c.requireEngine = d.requireEngine === true;
  const l = obj(d.limits);
  c.limits = { securityMaxAgeDays: num(l.securityMaxAgeDays), rebootMaxAgeDays: num(l.rebootMaxAgeDays), checkMaxAgeDays: num(l.checkMaxAgeDays) };
  const f = obj(d.firmware);
  c.firmware = { refreshMetadata: typeof f.refreshMetadata === "boolean" ? f.refreshMetadata : null, reportPending: f.reportPending === true };
  return c;
}

export function serializeUpdatesContent(c: UpdatesContent): string {
  const out: Record<string, unknown> = {};
  if (c.mode !== "UPDATES_MODE_UNSPECIFIED") out.mode = c.mode;
  if (c.schedule.onCalendar) {
    const s: Record<string, unknown> = { onCalendar: c.schedule.onCalendar };
    if (c.schedule.randomizedDelay) s.randomizedDelay = c.schedule.randomizedDelay;
    if (c.schedule.persistent !== null) s.persistent = c.schedule.persistent;
    out.schedule = s;
  }
  if (c.reboot.policy !== "UPDATES_REBOOT_UNSPECIFIED") {
    const r: Record<string, unknown> = { policy: c.reboot.policy };
    if (c.reboot.window) r.window = c.reboot.window;
    if (!c.reboot.onlyWithoutSessions) r.onlyWithoutSessions = false;
    if (c.reboot.forceAfterDays > 0) r.forceAfterDays = c.reboot.forceAfterDays;
    if (c.reboot.warnMinutes > 0) r.warnMinutes = c.reboot.warnMinutes;
    if (c.reboot.message) r.message = c.reboot.message;
    out.reboot = r;
  }
  if (c.hold.length) out.hold = c.hold;
  if (c.removeUnusedDependencies) out.removeUnusedDependencies = true;
  if (!c.installEngine) out.installEngine = false;
  if (c.requireEngine) out.requireEngine = true;
  if (hasLimits(c.limits)) {
    const l: Record<string, unknown> = {};
    if (c.limits.securityMaxAgeDays > 0) l.securityMaxAgeDays = c.limits.securityMaxAgeDays;
    if (c.limits.rebootMaxAgeDays > 0) l.rebootMaxAgeDays = c.limits.rebootMaxAgeDays;
    if (c.limits.checkMaxAgeDays > 0) l.checkMaxAgeDays = c.limits.checkMaxAgeDays;
    out.limits = l;
  }
  if (c.firmware.refreshMetadata !== null || c.firmware.reportPending) {
    const f: Record<string, unknown> = {};
    if (c.firmware.refreshMetadata !== null) f.refreshMetadata = c.firmware.refreshMetadata;
    if (c.firmware.reportPending) f.reportPending = true;
    out.firmware = f;
  }
  return JSON.stringify(out, null, 2);
}

export function hasLimits(l: UpdatesLimits): boolean {
  return l.securityMaxAgeDays > 0 || l.rebootMaxAgeDays > 0 || l.checkMaxAgeDays > 0;
}

export function limitProblem(v: number): string | null {
  if (!Number.isFinite(v) || v < 0 || v > UPDATES_MAX_LIMIT_DAYS) return `Use 0 (not checked) to ${UPDATES_MAX_LIMIT_DAYS} days`;
  return null;
}

const HOLD_RE = /^[A-Za-z0-9][A-Za-z0-9._+*?-]{0,127}$/;
const WINDOW_RE = /^([01]\d|2[0-3]):[0-5]\d-([01]\d|2[0-3]):[0-5]\d$/;
const TIME_SPAN_RE = /^(\d+(\.\d+)?\s*(usec|us|msec|ms|seconds?|sec|s|minutes?|min|m|hours?|hr|h|days?|d|weeks?|w|months?|M|years?|y)\s*)+$/;

export function holdProblem(h: string): string | null {
  if (!h.trim()) return "Enter a package name or a pattern";
  return HOLD_RE.test(h) ? null : "Use a package name or a pattern with * and ?";
}

export function windowProblem(w: string): string | null {
  if (!w) return null;
  if (!WINDOW_RE.test(w)) return "Use HH:MM-HH:MM, for example 02:00-05:00";
  if (w.slice(0, 5) === w.slice(6)) return "Start and end are the same";
  return null;
}

export function delayProblem(v: string): string | null {
  if (!v) return null;
  return TIME_SPAN_RE.test(v.trim()) ? null : "Use a time span such as 30m or 1h";
}

export function calendarProblem(v: string): string | null {
  if (!v.trim()) return "Enter a calendar expression";
  if (/[\n\r\0]/.test(v)) return "One line only";
  return null;
}

export function hasUpdatesSettings(c: UpdatesContent): boolean {
  return c.mode !== "UPDATES_MODE_UNSPECIFIED" || !!c.schedule.onCalendar || c.reboot.policy !== "UPDATES_REBOOT_UNSPECIFIED" ||
    c.hold.length > 0 || c.removeUnusedDependencies || c.requireEngine || !c.installEngine ||
    hasLimits(c.limits) || c.firmware.refreshMetadata !== null || c.firmware.reportPending;
}

/** Why the content cannot be saved, or null. */
export function validateUpdatesContent(c: UpdatesContent): string | null {
  if (!hasUpdatesSettings(c)) return "Set a mode, a schedule, a reboot rule, a hold or a limit";
  if (c.schedule.onCalendar) {
    const p = calendarProblem(c.schedule.onCalendar);
    if (p) return `Schedule: ${p}`;
  } else if (c.schedule.randomizedDelay || c.schedule.persistent !== null) {
    return "Schedule: a calendar expression is required";
  }
  const dp = delayProblem(c.schedule.randomizedDelay);
  if (dp) return `Schedule: ${dp}`;
  const wp = windowProblem(c.reboot.window);
  if (wp) return `Reboot window: ${wp}`;
  if (c.reboot.forceAfterDays < 0 || c.reboot.forceAfterDays > 365) return "Force after days must be between 0 and 365";
  if (c.reboot.warnMinutes < 0 || c.reboot.warnMinutes > 1440) return "Warning minutes must be between 0 and 1440";
  if (c.reboot.message.length > 512) return "The reboot message is longer than 512 characters";
  for (const [name, v] of [["Security age limit", c.limits.securityMaxAgeDays], ["Reboot age limit", c.limits.rebootMaxAgeDays], ["Check age limit", c.limits.checkMaxAgeDays]] as const) {
    const p = limitProblem(v);
    if (p) return `${name}: ${p}`;
  }
  const seen = new Set<string>();
  for (const h of c.hold) {
    const p = holdProblem(h);
    if (p) return `Hold ${h || "(empty)"}: ${p}`;
    if (seen.has(h)) return `Hold ${h} is listed twice`;
    seen.add(h);
  }
  return null;
}

export function updatesModeLabel(m: UpdatesMode): string {
  return UPDATES_MODE_OPTIONS.find((o) => o.value === m)?.label ?? m;
}

export function updatesRebootLabel(r: UpdatesRebootPolicy): string {
  return UPDATES_REBOOT_OPTIONS.find((o) => o.value === r)?.label ?? r;
}

export interface UpdatesTemplate {
  id: string;
  label: string;
  description: string;
  content: UpdatesContent;
}

export const UPDATES_TEMPLATES: UpdatesTemplate[] = [
  {
    id: "workstation",
    label: "Workstations: security updates nightly, reboot in the window",
    description: "Security updates install at 02:00 with up to an hour of random delay. A reboot an update needs happens between 03:00 and 05:00 when nobody is logged in, and after seven days even with a user logged in, with a ten-minute warning. A security update pending longer than 14 days, a reboot pending longer than 10 or metadata older than 3 days is non-compliant.",
    content: {
      ...emptyUpdatesContent(),
      mode: "UPDATES_MODE_SECURITY",
      schedule: { onCalendar: "*-*-* 02:00", randomizedDelay: "1h", persistent: true },
      reboot: { policy: "UPDATES_REBOOT_WHEN_NEEDED", window: "03:00-05:00", onlyWithoutSessions: true, forceAfterDays: 7, warnMinutes: 10, message: "" },
      limits: { securityMaxAgeDays: 14, rebootMaxAgeDays: 10, checkMaxAgeDays: 3 },
    },
  },
  {
    id: "servers",
    label: "Servers: all updates, no automatic reboot",
    description: "Every update installs at 04:00. The node never reboots on its own; a pending reboot shows on the node page and the Compliance page. A security update pending longer than 30 days or metadata older than 3 days is non-compliant.",
    content: {
      ...emptyUpdatesContent(),
      mode: "UPDATES_MODE_ALL",
      schedule: { onCalendar: "*-*-* 04:00", randomizedDelay: "30m", persistent: true },
      reboot: { policy: "UPDATES_REBOOT_NEVER", window: "", onlyWithoutSessions: true, forceAfterDays: 0, warnMinutes: 0, message: "" },
      limits: { securityMaxAgeDays: 30, rebootMaxAgeDays: 0, checkMaxAgeDays: 3 },
    },
  },
  {
    id: "check",
    label: "Check only: report pending updates",
    description: "Nothing is installed; the node reports what it could install and whether a reboot is pending.",
    content: { ...emptyUpdatesContent(), mode: "UPDATES_MODE_CHECK" },
  },
];
