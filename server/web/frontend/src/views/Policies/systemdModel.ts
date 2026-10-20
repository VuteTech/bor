// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared content model for the Systemd ("Services & timers") policy editor:
 * the protojson (camelCase, enum names as strings) shape stored in
 * `Policy.content`, a defensive parser, a compact serializer and the
 * structural checks the server applies too. Mirrors proto/policy/systemd.proto
 * and the name rules in server/pkg/systemdunits/systemdunits.go.
 */

export type SystemdEnablement =
  | "SYSTEMD_ENABLEMENT_UNSPECIFIED"
  | "SYSTEMD_ENABLEMENT_ENABLED"
  | "SYSTEMD_ENABLEMENT_DISABLED"
  | "SYSTEMD_ENABLEMENT_MASKED";

export type SystemdActivity =
  | "SYSTEMD_ACTIVITY_UNSPECIFIED"
  | "SYSTEMD_ACTIVITY_RUNNING"
  | "SYSTEMD_ACTIVITY_STOPPED";

/** Which manager a unit or task belongs to: the system manager, or every user's manager. */
export type SystemdScope =
  | "SYSTEMD_SCOPE_UNSPECIFIED"
  | "SYSTEMD_SCOPE_SYSTEM"
  | "SYSTEMD_SCOPE_GLOBAL";

export function isGlobalScope(s: SystemdScope): boolean {
  return s === "SYSTEMD_SCOPE_GLOBAL";
}

export type SystemdUnitType = "service" | "timer" | "socket" | "target" | "path";

export const SYSTEMD_UNIT_TYPES: SystemdUnitType[] = ["service", "timer", "socket", "target", "path"];

export interface SystemdTimerOverrides {
  onCalendar: string[];
  onBootSec: string;
  onStartupSec: string;
  onActiveSec: string;
  onUnitActiveSec: string;
  onUnitInactiveSec: string;
  persistent?: boolean;
  randomizedDelaySec: string;
  accuracySec: string;
  wakeSystem?: boolean;
  fixedRandomDelay?: boolean;
  onClockChange?: boolean;
  onTimezoneChange?: boolean;
}

export interface SystemdServiceOverrides {
  restart: string;
  restartSec: string;
  timeoutStartSec: string;
  timeoutStopSec: string;
  environment: string[];
  memoryMax: string;
  cpuQuota: string;
  tasksMax: string;
  nice: string;
  noNewPrivileges?: boolean;
  protectSystem: string;
  protectHome: string;
  privateTmp?: boolean;
}

/** One raw unit-file directive of a drop-in. */
export interface SystemdDirectiveEntry {
  /** Unit | Service | Timer | Socket | Path | Install */
  section: string;
  key: string;
  /** One line per value; an empty list writes "Key=" alone (reset). */
  values: string[];
}

export interface SystemdUnitEntry {
  name: string;
  scope: SystemdScope;
  enablement: SystemdEnablement;
  activity: SystemdActivity;
  restartOnChange: boolean;
  requireLastRunSuccess: boolean;
  description: string;
  timer: SystemdTimerOverrides;
  service: SystemdServiceOverrides;
  raw: SystemdDirectiveEntry[];
}

export type SystemdTaskTrigger =
  | "SYSTEMD_TASK_TRIGGER_UNSPECIFIED"
  | "SYSTEMD_TASK_TRIGGER_TIMER"
  | "SYSTEMD_TASK_TRIGGER_BOOT";

/** A Bor-deployed scheduled task: bor-<name>.service, its timer and script. */
export interface SystemdTaskEntry {
  name: string;
  description: string;
  scope: SystemdScope;
  trigger: SystemdTaskTrigger;
  schedule: SystemdTimerOverrides;
  /** Whether the task runs a script (written to /etc/bor/systemd) or a program (argv). */
  mode: "script" | "program";
  script: string;
  execStart: string[];
  user: string;
  group: string;
  workingDirectory: string;
  environment: string[];
  timeoutStartSec: string;
  hardened: boolean;
  requireLastRunSuccess: boolean;
  enabled: boolean;
  /** AppArmorProfile= of the service: the loaded name of the profile the task runs under. */
  apparmorProfile: string;
  /** SELinuxContext= of the service: user:role:type[:level]. */
  selinuxContext: string;
}

export interface SystemdContent {
  units: SystemdUnitEntry[];
  tasks: SystemdTaskEntry[];
  writePresets: boolean;
}

export const SCOPE_OPTIONS: { value: SystemdScope; label: string; description: string }[] = [
  { value: "SYSTEMD_SCOPE_SYSTEM", label: "System", description: "The system manager (systemctl)" },
  { value: "SYSTEMD_SCOPE_GLOBAL", label: "All users", description: "Every user's manager (systemctl --global), applied in each logged-in session" },
];

export const ENABLEMENT_OPTIONS: { value: SystemdEnablement; label: string }[] = [
  { value: "SYSTEMD_ENABLEMENT_UNSPECIFIED", label: "Keep" },
  { value: "SYSTEMD_ENABLEMENT_ENABLED", label: "Enabled" },
  { value: "SYSTEMD_ENABLEMENT_DISABLED", label: "Disabled" },
  { value: "SYSTEMD_ENABLEMENT_MASKED", label: "Masked" },
];

export const ACTIVITY_OPTIONS: { value: SystemdActivity; label: string }[] = [
  { value: "SYSTEMD_ACTIVITY_UNSPECIFIED", label: "Keep" },
  { value: "SYSTEMD_ACTIVITY_RUNNING", label: "Running" },
  { value: "SYSTEMD_ACTIVITY_STOPPED", label: "Stopped" },
];

export const RESTART_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Keep" },
  { value: "no", label: "no" },
  { value: "on-success", label: "on-success" },
  { value: "on-failure", label: "on-failure" },
  { value: "on-abnormal", label: "on-abnormal" },
  { value: "on-abort", label: "on-abort" },
  { value: "on-watchdog", label: "on-watchdog" },
  { value: "always", label: "always" },
];

export const PROTECT_SYSTEM_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Keep" },
  { value: "no", label: "no" },
  { value: "yes", label: "yes (read-only /usr and /boot)" },
  { value: "full", label: "full (also /etc)" },
  { value: "strict", label: "strict (whole file system)" },
];

export const PROTECT_HOME_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "Keep" },
  { value: "no", label: "no" },
  { value: "yes", label: "yes (inaccessible)" },
  { value: "read-only", label: "read-only" },
  { value: "tmpfs", label: "tmpfs" },
];

/** Schedule presets offered by the Timer overrides. */
export const SCHEDULE_PRESETS: { id: string; label: string; expression: string }[] = [
  { id: "hourly", label: "Hourly", expression: "hourly" },
  { id: "daily", label: "Daily at 03:00", expression: "*-*-* 03:00:00" },
  { id: "weekdays", label: "Weekdays at 07:00", expression: "Mon..Fri *-*-* 07:00:00" },
  { id: "weekly", label: "Weekly, Saturday 03:00", expression: "Sat *-*-* 03:00:00" },
  { id: "monthly", label: "Monthly, first day 03:00", expression: "*-*-01 03:00:00" },
  { id: "custom", label: "Custom expression", expression: "" },
];

export const TASK_TRIGGER_OPTIONS: { value: SystemdTaskTrigger; label: string }[] = [
  { value: "SYSTEMD_TASK_TRIGGER_TIMER", label: "On a schedule (timer)" },
  { value: "SYSTEMD_TASK_TRIGGER_BOOT", label: "At boot" },
];

export const DIRECTIVE_SECTIONS = ["Unit", "Service", "Timer", "Socket", "Path", "Install"] as const;

/** Sandboxing directives the hardened switch of a task adds. */
export const TASK_HARDENED_DIRECTIVES = [
  "NoNewPrivileges=yes",
  "PrivateTmp=yes",
  "ProtectKernelTunables=yes",
  "ProtectControlGroups=yes",
  "RestrictSUIDSGID=yes",
  "ProtectClock=yes",
  "ProtectHostname=yes",
];

export const MAX_TASK_SCRIPT_BYTES = 64 * 1024;

/** Unit presets the editor can add in one click; content only, no mechanism. */
export interface SystemdTemplate {
  id: string;
  label: string;
  description: string;
  units: SystemdUnitEntry[];
}

export function systemdTemplates(): SystemdTemplate[] {
  const mask = (name: string, description: string) =>
    newUnitEntry({ name, enablement: "SYSTEMD_ENABLEMENT_MASKED", description });
  const window = (name: string, description: string) =>
    newUnitEntry({
      name,
      enablement: "SYSTEMD_ENABLEMENT_ENABLED",
      activity: "SYSTEMD_ACTIVITY_RUNNING",
      description,
      timer: { ...emptyTimerOverrides(), onCalendar: ["Sat *-*-* 03:00:00"], persistent: true, randomizedDelaySec: "1h" },
    });
  return [
    {
      id: "desktop-hardening",
      label: "Desktop hardening",
      description: "Masks zero-configuration networking and printer discovery, disables Bluetooth and rpcbind, and makes Ctrl+Alt+Del do nothing.",
      units: [
        mask("avahi-daemon.service", "mDNS/DNS-SD is not used on the corporate network"),
        mask("avahi-daemon.socket", "socket activation of avahi-daemon"),
        mask("cups-browsed.service", "printers come from the print server, not from discovery"),
        newUnitEntry({ name: "bluetooth.service", enablement: "SYSTEMD_ENABLEMENT_DISABLED", activity: "SYSTEMD_ACTIVITY_STOPPED", description: "no Bluetooth peripherals" }),
        newUnitEntry({ name: "rpcbind.socket", enablement: "SYSTEMD_ENABLEMENT_DISABLED", activity: "SYSTEMD_ACTIVITY_STOPPED", description: "no NFS or NIS clients" }),
        mask("ctrl-alt-del.target", "Ctrl+Alt+Del must not reboot a desktop"),
      ],
    },
    {
      id: "maintenance-window",
      label: "Maintenance window",
      description: "Moves the weekly TRIM, the man-db rebuild and the package metadata refresh timers to Saturday 03:00 with a one-hour random spread, catching up runs missed while the node was off.",
      units: [
        window("fstrim.timer", "weekly TRIM in the Saturday maintenance window"),
        window("man-db.timer", "man-db rebuild in the maintenance window"),
        window("dnf-makecache.timer", "package metadata refresh (Fedora, RHEL) in the maintenance window"),
        window("apt-daily.timer", "package metadata refresh (Debian, Ubuntu) in the maintenance window"),
      ],
    },
  ];
}

/* ── unit name rules (mirror server/pkg/systemdunits/systemdunits.go) ── */

const UNIT_NAME_RE = /^[A-Za-z0-9:_.\\@-]{1,255}$/;

/** Units no policy may manage; the server rejects them. */
export const PROTECTED_UNITS: string[] = [
  "bor-agent.service",
  "dbus.service",
  "dbus.socket",
  "dbus-broker.service",
  "systemd-logind.service",
  "polkit.service",
  "init.scope",
  "-.slice",
  "-.mount",
  "user@.service",
  "user-runtime-dir@.service",
  "systemd-user-sessions.service",
  "getty.target",
  "basic.target",
  "sysinit.target",
  "multi-user.target",
  "graphical.target",
  "default.target",
  "rescue.target",
  "emergency.target",
  "shutdown.target",
  "final.target",
  "umount.target",
  "reboot.target",
  "poweroff.target",
  "halt.target",
  "kexec.target",
  "systemd-journald.service",
  "systemd-journald.socket",
  "systemd-journald-dev-log.socket",
  "systemd-udevd.service",
  "systemd-udevd-control.socket",
  "systemd-udevd-kernel.socket",
];

const PROTECTED_PREFIXES = ["systemd-journald", "systemd-udevd", "user@", "user-runtime-dir@"];

/** User-manager units no policy may manage in the "all users" scope, on top of PROTECTED_UNITS. */
export const PROTECTED_GLOBAL_UNITS: string[] = [
  "exit.target",
  "sockets.target",
  "timers.target",
  "paths.target",
  "graphical-session.target",
  "graphical-session-pre.target",
  "xdg-desktop-autostart.target",
  "systemd-exit.service",
  "systemd-tmpfiles-setup.service",
];

/** User-manager units whose stop can break every user's desktop session. */
const RISKY_GLOBAL_PREFIXES = ["pipewire", "wireplumber", "pulseaudio", "xdg-desktop-portal", "plasma-", "gnome-session", "gnome-shell", "dbus-"];

/** Units whose stop or mask can cut users off; the editor asks for a confirmation. */
export const RISKY_UNITS: string[] = [
  "NetworkManager.service",
  "systemd-resolved.service",
  "sshd.service",
  "ssh.service",
  "sshd.socket",
  "ssh.socket",
  "gdm.service",
  "sddm.service",
  "lightdm.service",
  "display-manager.service",
  "firewalld.service",
  "chronyd.service",
  "chrony.service",
  "systemd-timesyncd.service",
  "auditd.service",
  "cron.service",
  "crond.service",
  "cronie.service",
  "wpa_supplicant.service",
  "iwd.service",
];

const RISKY_PREFIXES = ["systemd-networkd", "systemd-resolved", "systemd-timesyncd"];

export function unitType(name: string): SystemdUnitType | "" {
  const i = name.lastIndexOf(".");
  if (i < 0) return "";
  const suffix = name.slice(i + 1);
  return (SYSTEMD_UNIT_TYPES as string[]).includes(suffix) ? (suffix as SystemdUnitType) : "";
}

export function isProtectedUnit(name: string): boolean {
  return PROTECTED_UNITS.includes(name) || PROTECTED_PREFIXES.some((p) => name.startsWith(p));
}

export function isRiskyUnit(name: string): boolean {
  return RISKY_UNITS.includes(name) || RISKY_PREFIXES.some((p) => name.startsWith(p));
}

export function isProtectedGlobalUnit(name: string): boolean {
  return isProtectedUnit(name) || PROTECTED_GLOBAL_UNITS.includes(name);
}

export function isRiskyGlobalUnit(name: string): boolean {
  return isRiskyUnit(name) || RISKY_GLOBAL_PREFIXES.some((p) => name.startsWith(p));
}

/** Whether a unit entry needs the risky-unit confirmation in its scope. */
export function isRiskyUnitEntry(u: SystemdUnitEntry): boolean {
  return isGlobalScope(u.scope) ? isRiskyGlobalUnit(u.name) : isRiskyUnit(u.name);
}

export function isTemplateUnit(name: string): boolean {
  const i = name.indexOf("@");
  return i >= 0 && name.slice(i + 1).startsWith(".");
}

/** Why a unit name cannot be used, or null when it is acceptable. */
export function unitNameProblem(name: string): string | null {
  if (!name) return "Unit name is required";
  if (name === "." || name === ".." || !UNIT_NAME_RE.test(name)) {
    return "Unit name contains characters that are not permitted";
  }
  const t = unitType(name);
  if (!t) return `Unit name must end in .${SYSTEMD_UNIT_TYPES.join(", .")}`;
  if (name.length === t.length + 1) return "Unit name has no name before the suffix";
  if (isProtectedUnit(name)) return "This unit is protected and cannot be managed by a policy";
  if (isTemplateUnit(name)) return `Template units cannot be managed; name an instance such as ${name.replace("@.", "@instance.")}`;
  return null;
}

/* ── value syntax (mirror systemdunits.ValidateTimeSpan / ValidateCalendar and services/systemd.go) ── */

const TIME_SPAN_RE =
  /^(infinity|(\d+(\.\d+)?\s*(usec|us|nsec|ns|msec|ms|seconds|second|sec|s|minutes|minute|min|m|hours|hour|hr|h|days|day|d|weeks|week|w|months|month|M|years|year|y)?\s*)+)$/;
const CALENDAR_RE = /^[A-Za-z0-9 *,.:~/@-]+$/;
const SIZE_RE = /^(\d+(\.\d+)?[KMGTPE]?|\d+(\.\d+)?%|infinity)$/;
const PERCENT_RE = /^\d+(\.\d+)?%$/;
const ENV_RE = /^[A-Za-z_][A-Za-z0-9_]*=/;

const TASK_NAME_RE = /^[a-z0-9][a-z0-9-]{0,47}$/;
const USER_NAME_RE = /^[a-z_][a-z0-9_-]{0,31}$/;
const DIRECTIVE_KEY_RE = /^[A-Z][A-Za-z0-9]*$/;

/** Why a task name cannot be used, or null. */
export function taskNameProblem(name: string): string | null {
  if (!name) return "Task name is required";
  if (!TASK_NAME_RE.test(name)) return "Use 1-48 lower-case letters, digits or dashes, starting with a letter or digit";
  if (name.startsWith("bor-")) return "Do not type the bor- prefix; it is added automatically";
  return null;
}

export function userNameProblem(name: string): string | null {
  if (!name) return null;
  if (!USER_NAME_RE.test(name)) return "Use 1-32 lower-case letters, digits, underscores or dashes, starting with a letter or underscore";
  return null;
}

export function directiveKeyProblem(key: string): string | null {
  if (!key) return "Directive name is required";
  if (key.length > 64 || !DIRECTIVE_KEY_RE.test(key)) return "Use a CamelCase systemd directive such as IPAddressDeny";
  return null;
}

/** Exec* directives run commands and make the policy need systemd:exec. */
export function isExecDirective(key: string): boolean {
  return key.startsWith("Exec");
}

export function timeSpanProblem(value: string): string | null {
  const v = value.trim();
  if (!v) return "Time span must not be empty";
  if (v.length > 64 || !TIME_SPAN_RE.test(v)) return "Use systemd syntax such as 15min, 1h 30min or 90s";
  return null;
}

export function calendarProblem(value: string): string | null {
  const v = value.trim();
  if (!v) return "Calendar expression must not be empty";
  if (v.length > 128) return "Calendar expression is longer than 128 characters";
  if (!CALENDAR_RE.test(v)) return "Use systemd syntax such as daily, Sat *-*-* 03:00:00 or Mon..Fri 07:00";
  return null;
}

/* ── defaults ── */

export function emptyTimerOverrides(): SystemdTimerOverrides {
  return {
    onCalendar: [],
    onBootSec: "",
    onStartupSec: "",
    onActiveSec: "",
    onUnitActiveSec: "",
    onUnitInactiveSec: "",
    randomizedDelaySec: "",
    accuracySec: "",
  };
}

export function emptyServiceOverrides(): SystemdServiceOverrides {
  return {
    restart: "",
    restartSec: "",
    timeoutStartSec: "",
    timeoutStopSec: "",
    environment: [],
    memoryMax: "",
    cpuQuota: "",
    tasksMax: "",
    nice: "",
    protectSystem: "",
    protectHome: "",
  };
}

export function newUnitEntry(partial: Partial<SystemdUnitEntry> = {}): SystemdUnitEntry {
  return {
    name: "",
    scope: "SYSTEMD_SCOPE_SYSTEM",
    enablement: "SYSTEMD_ENABLEMENT_UNSPECIFIED",
    activity: "SYSTEMD_ACTIVITY_UNSPECIFIED",
    restartOnChange: false,
    requireLastRunSuccess: false,
    description: "",
    timer: emptyTimerOverrides(),
    service: emptyServiceOverrides(),
    raw: [],
    ...partial,
  };
}

export function newTaskEntry(partial: Partial<SystemdTaskEntry> = {}): SystemdTaskEntry {
  return {
    name: "",
    description: "",
    scope: "SYSTEMD_SCOPE_SYSTEM",
    trigger: "SYSTEMD_TASK_TRIGGER_TIMER",
    schedule: emptyTimerOverrides(),
    mode: "script",
    script: "#!/bin/sh\nset -eu\n",
    execStart: [],
    user: "",
    group: "",
    workingDirectory: "",
    environment: [],
    timeoutStartSec: "",
    hardened: true,
    apparmorProfile: "",
    selinuxContext: "",
    requireLastRunSuccess: true,
    enabled: true,
    ...partial,
  };
}

export function newDirectiveEntry(partial: Partial<SystemdDirectiveEntry> = {}): SystemdDirectiveEntry {
  return { section: "Service", key: "", values: [""], ...partial };
}

/** Whether the schedule carries a trigger (Persistent alone schedules nothing). */
export function timerHasTrigger(t: SystemdTimerOverrides): boolean {
  return t.onCalendar.length > 0 || !!t.onBootSec || !!t.onStartupSec || !!t.onActiveSec || !!t.onUnitActiveSec || !!t.onUnitInactiveSec;
}

export function timerIsEmpty(t: SystemdTimerOverrides): boolean {
  return (
    t.onCalendar.length === 0 && !t.onBootSec && !t.onStartupSec && !t.onActiveSec &&
    !t.onUnitActiveSec && !t.onUnitInactiveSec && t.persistent === undefined &&
    !t.randomizedDelaySec && !t.accuracySec && t.wakeSystem === undefined &&
    t.fixedRandomDelay === undefined && t.onClockChange === undefined && t.onTimezoneChange === undefined
  );
}

export function serviceIsEmpty(s: SystemdServiceOverrides): boolean {
  return (
    !s.restart && !s.restartSec && !s.timeoutStartSec && !s.timeoutStopSec && s.environment.length === 0 &&
    !s.memoryMax && !s.cpuQuota && !s.tasksMax && !s.nice && s.noNewPrivileges === undefined &&
    !s.protectSystem && !s.protectHome && s.privateTmp === undefined
  );
}

/* ── parse ── */

const ENABLEMENT_BY_NUMBER: SystemdEnablement[] = [
  "SYSTEMD_ENABLEMENT_UNSPECIFIED",
  "SYSTEMD_ENABLEMENT_ENABLED",
  "SYSTEMD_ENABLEMENT_DISABLED",
  "SYSTEMD_ENABLEMENT_MASKED",
];
const ACTIVITY_BY_NUMBER: SystemdActivity[] = [
  "SYSTEMD_ACTIVITY_UNSPECIFIED",
  "SYSTEMD_ACTIVITY_RUNNING",
  "SYSTEMD_ACTIVITY_STOPPED",
];

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
function strList(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}
function optBool(v: unknown): boolean | undefined {
  return typeof v === "boolean" ? v : undefined;
}

function parseScope(v: unknown): SystemdScope {
  return v === "SYSTEMD_SCOPE_GLOBAL" || v === 2 ? "SYSTEMD_SCOPE_GLOBAL" : "SYSTEMD_SCOPE_SYSTEM";
}

function parseEnablement(v: unknown): SystemdEnablement {
  if (typeof v === "number") return ENABLEMENT_BY_NUMBER[v] ?? "SYSTEMD_ENABLEMENT_UNSPECIFIED";
  if (typeof v === "string" && (ENABLEMENT_BY_NUMBER as string[]).includes(v)) return v as SystemdEnablement;
  return "SYSTEMD_ENABLEMENT_UNSPECIFIED";
}

function parseActivity(v: unknown): SystemdActivity {
  if (typeof v === "number") return ACTIVITY_BY_NUMBER[v] ?? "SYSTEMD_ACTIVITY_UNSPECIFIED";
  if (typeof v === "string" && (ACTIVITY_BY_NUMBER as string[]).includes(v)) return v as SystemdActivity;
  return "SYSTEMD_ACTIVITY_UNSPECIFIED";
}

function parseTimer(raw: unknown): SystemdTimerOverrides {
  const t = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  return {
    onCalendar: strList(t.onCalendar),
    onBootSec: str(t.onBootSec),
    onStartupSec: str(t.onStartupSec),
    onActiveSec: str(t.onActiveSec),
    onUnitActiveSec: str(t.onUnitActiveSec),
    onUnitInactiveSec: str(t.onUnitInactiveSec),
    persistent: optBool(t.persistent),
    randomizedDelaySec: str(t.randomizedDelaySec),
    accuracySec: str(t.accuracySec),
    wakeSystem: optBool(t.wakeSystem),
    fixedRandomDelay: optBool(t.fixedRandomDelay),
    onClockChange: optBool(t.onClockChange),
    onTimezoneChange: optBool(t.onTimezoneChange),
  };
}

function parseService(raw: unknown): SystemdServiceOverrides {
  const s = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;
  return {
    restart: str(s.restart),
    restartSec: str(s.restartSec),
    timeoutStartSec: str(s.timeoutStartSec),
    timeoutStopSec: str(s.timeoutStopSec),
    environment: strList(s.environment),
    memoryMax: str(s.memoryMax),
    cpuQuota: str(s.cpuQuota),
    tasksMax: str(s.tasksMax),
    nice: str(s.nice),
    noNewPrivileges: optBool(s.noNewPrivileges),
    protectSystem: str(s.protectSystem),
    protectHome: str(s.protectHome),
    privateTmp: optBool(s.privateTmp),
  };
}

const TRIGGER_BY_NUMBER: SystemdTaskTrigger[] = [
  "SYSTEMD_TASK_TRIGGER_UNSPECIFIED",
  "SYSTEMD_TASK_TRIGGER_TIMER",
  "SYSTEMD_TASK_TRIGGER_BOOT",
];

function parseTrigger(v: unknown): SystemdTaskTrigger {
  if (typeof v === "number") return TRIGGER_BY_NUMBER[v] ?? "SYSTEMD_TASK_TRIGGER_UNSPECIFIED";
  if (typeof v === "string" && (TRIGGER_BY_NUMBER as string[]).includes(v)) return v as SystemdTaskTrigger;
  return "SYSTEMD_TASK_TRIGGER_UNSPECIFIED";
}

function parseRaw(raw: unknown): SystemdDirectiveEntry[] {
  if (!Array.isArray(raw)) return [];
  return raw.map((d) => {
    const e = (d && typeof d === "object" ? d : {}) as Record<string, unknown>;
    return { section: str(e.section), key: str(e.key), values: strList(e.values) };
  });
}

function parseTask(t: unknown): SystemdTaskEntry {
  const e = (t && typeof t === "object" ? t : {}) as Record<string, unknown>;
  const execStart = strList(e.execStart);
  const script = str(e.script);
  return newTaskEntry({
    name: str(e.name),
    description: str(e.description),
    scope: parseScope(e.scope),
    trigger: parseTrigger(e.trigger),
    schedule: parseTimer(e.schedule),
    mode: !script && execStart.length > 0 ? "program" : "script",
    script,
    execStart,
    user: str(e.user),
    group: str(e.group),
    workingDirectory: str(e.workingDirectory),
    environment: strList(e.environment),
    timeoutStartSec: str(e.timeoutStartSec),
    hardened: e.hardened !== false,
    apparmorProfile: str(e.apparmorProfile),
    selinuxContext: str(e.selinuxContext),
    requireLastRunSuccess: e.requireLastRunSuccess === true,
    enabled: e.enabled !== false,
  });
}

export function parseSystemdContent(raw: string): SystemdContent {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw || "{}");
  } catch {
    return { units: [], tasks: [], writePresets: false };
  }
  const obj = (parsed && typeof parsed === "object" ? parsed : {}) as Record<string, unknown>;
  const units = Array.isArray(obj.units) ? obj.units : [];
  const tasks = Array.isArray(obj.tasks) ? obj.tasks : [];
  return {
    units: units.map((u) => {
      const e = (u && typeof u === "object" ? u : {}) as Record<string, unknown>;
      const dropIn = (e.dropIn && typeof e.dropIn === "object" ? e.dropIn : {}) as Record<string, unknown>;
      return newUnitEntry({
        name: str(e.name),
        scope: parseScope(e.scope),
        enablement: parseEnablement(e.enablement),
        activity: parseActivity(e.activity),
        restartOnChange: e.restartOnChange === true,
        requireLastRunSuccess: e.requireLastRunSuccess === true,
        description: str(e.description),
        timer: parseTimer(dropIn.timer),
        service: parseService(dropIn.service),
        raw: parseRaw(dropIn.raw),
      });
    }),
    tasks: tasks.map(parseTask),
    writePresets: obj.writePresets === true,
  };
}

/* ── serialize ── */

function compactTimer(t: SystemdTimerOverrides): Record<string, unknown> | undefined {
  if (timerIsEmpty(t)) return undefined;
  const out: Record<string, unknown> = {};
  if (t.onCalendar.length) out.onCalendar = t.onCalendar;
  if (t.onBootSec) out.onBootSec = t.onBootSec;
  if (t.onStartupSec) out.onStartupSec = t.onStartupSec;
  if (t.onActiveSec) out.onActiveSec = t.onActiveSec;
  if (t.onUnitActiveSec) out.onUnitActiveSec = t.onUnitActiveSec;
  if (t.onUnitInactiveSec) out.onUnitInactiveSec = t.onUnitInactiveSec;
  if (t.persistent !== undefined) out.persistent = t.persistent;
  if (t.randomizedDelaySec) out.randomizedDelaySec = t.randomizedDelaySec;
  if (t.accuracySec) out.accuracySec = t.accuracySec;
  if (t.wakeSystem !== undefined) out.wakeSystem = t.wakeSystem;
  if (t.fixedRandomDelay !== undefined) out.fixedRandomDelay = t.fixedRandomDelay;
  if (t.onClockChange !== undefined) out.onClockChange = t.onClockChange;
  if (t.onTimezoneChange !== undefined) out.onTimezoneChange = t.onTimezoneChange;
  return out;
}

function compactService(s: SystemdServiceOverrides): Record<string, unknown> | undefined {
  if (serviceIsEmpty(s)) return undefined;
  const out: Record<string, unknown> = {};
  if (s.restart) out.restart = s.restart;
  if (s.restartSec) out.restartSec = s.restartSec;
  if (s.timeoutStartSec) out.timeoutStartSec = s.timeoutStartSec;
  if (s.timeoutStopSec) out.timeoutStopSec = s.timeoutStopSec;
  if (s.environment.length) out.environment = s.environment;
  if (s.memoryMax) out.memoryMax = s.memoryMax;
  if (s.cpuQuota) out.cpuQuota = s.cpuQuota;
  if (s.tasksMax) out.tasksMax = s.tasksMax;
  if (s.nice) out.nice = s.nice;
  if (s.noNewPrivileges !== undefined) out.noNewPrivileges = s.noNewPrivileges;
  if (s.protectSystem) out.protectSystem = s.protectSystem;
  if (s.protectHome) out.protectHome = s.protectHome;
  if (s.privateTmp !== undefined) out.privateTmp = s.privateTmp;
  return out;
}

function compactTask(t: SystemdTaskEntry): Record<string, unknown> {
  const out: Record<string, unknown> = { name: t.name };
  if (t.description) out.description = t.description;
  if (isGlobalScope(t.scope)) out.scope = t.scope;
  out.trigger = t.trigger;
  if (t.trigger === "SYSTEMD_TASK_TRIGGER_TIMER") {
    const schedule = compactTimer(t.schedule);
    if (schedule) out.schedule = schedule;
  }
  if (t.mode === "program") out.execStart = t.execStart;
  else out.script = t.script;
  if (t.user) out.user = t.user;
  if (t.group) out.group = t.group;
  if (t.workingDirectory) out.workingDirectory = t.workingDirectory;
  if (t.environment.length) out.environment = t.environment;
  if (t.timeoutStartSec) out.timeoutStartSec = t.timeoutStartSec;
  if (t.apparmorProfile) out.apparmorProfile = t.apparmorProfile;
  if (t.selinuxContext) out.selinuxContext = t.selinuxContext;
  if (!t.hardened) out.hardened = false;
  if (t.requireLastRunSuccess) out.requireLastRunSuccess = true;
  if (!t.enabled) out.enabled = false;
  return out;
}

export function serializeSystemdContent(c: SystemdContent): string {
  const units = c.units.map((u) => {
    const out: Record<string, unknown> = { name: u.name };
    if (isGlobalScope(u.scope)) out.scope = u.scope;
    if (u.enablement !== "SYSTEMD_ENABLEMENT_UNSPECIFIED") out.enablement = u.enablement;
    if (u.activity !== "SYSTEMD_ACTIVITY_UNSPECIFIED") out.activity = u.activity;
    if (u.restartOnChange) out.restartOnChange = true;
    if (u.requireLastRunSuccess) out.requireLastRunSuccess = true;
    if (u.description) out.description = u.description;
    const timer = unitType(u.name) === "timer" ? compactTimer(u.timer) : undefined;
    const service = unitType(u.name) === "service" ? compactService(u.service) : undefined;
    const raw = u.raw.length ? u.raw.map((d) => ({ section: d.section, key: d.key, values: d.values })) : undefined;
    if (timer || service || raw) {
      const dropIn: Record<string, unknown> = {};
      if (timer) dropIn.timer = timer;
      if (service) dropIn.service = service;
      if (raw) dropIn.raw = raw;
      out.dropIn = dropIn;
    }
    return out;
  });
  const out: Record<string, unknown> = { units };
  if (c.tasks.length) out.tasks = c.tasks.map(compactTask);
  if (c.writePresets) out.writePresets = true;
  return JSON.stringify(out, null, 2);
}

/* ── validation (mirror services/systemd.go) ── */

const RESTART_VALUES = RESTART_OPTIONS.map((o) => o.value).filter(Boolean);
const PROTECT_SYSTEM_VALUES = PROTECT_SYSTEM_OPTIONS.map((o) => o.value).filter(Boolean);
const PROTECT_HOME_VALUES = PROTECT_HOME_OPTIONS.map((o) => o.value).filter(Boolean);

function timerProblem(t: SystemdTimerOverrides): string | null {
  if (t.onCalendar.length > 16) return "At most 16 calendar expressions";
  for (const expr of t.onCalendar) {
    const p = calendarProblem(expr);
    if (p) return `OnCalendar: ${p}`;
  }
  const spans: [string, string][] = [
    ["OnBootSec", t.onBootSec],
    ["OnStartupSec", t.onStartupSec],
    ["OnActiveSec", t.onActiveSec],
    ["OnUnitActiveSec", t.onUnitActiveSec],
    ["OnUnitInactiveSec", t.onUnitInactiveSec],
    ["RandomizedDelaySec", t.randomizedDelaySec],
    ["AccuracySec", t.accuracySec],
  ];
  for (const [label, value] of spans) {
    if (!value) continue;
    const p = timeSpanProblem(value);
    if (p) return `${label}: ${p}`;
  }
  return null;
}

function serviceProblem(s: SystemdServiceOverrides): string | null {
  if (s.restart && !RESTART_VALUES.includes(s.restart)) return `Restart must be one of ${RESTART_VALUES.join(", ")}`;
  const spans: [string, string][] = [
    ["RestartSec", s.restartSec],
    ["TimeoutStartSec", s.timeoutStartSec],
    ["TimeoutStopSec", s.timeoutStopSec],
  ];
  for (const [label, value] of spans) {
    if (!value) continue;
    const p = timeSpanProblem(value);
    if (p) return `${label}: ${p}`;
  }
  if (s.environment.length > 64) return "At most 64 environment entries";
  for (const e of s.environment) {
    if (e.length > 1024) return "An environment entry is longer than 1024 characters";
    if (!ENV_RE.test(e) || /[\n\r\0]/.test(e)) return `Environment entry "${e}" must be KEY=value on one line`;
  }
  if (s.memoryMax && !SIZE_RE.test(s.memoryMax)) return "MemoryMax must be a size such as 512M, a percentage such as 20%, or infinity";
  if (s.cpuQuota && !PERCENT_RE.test(s.cpuQuota)) return "CPUQuota must be a percentage such as 50%";
  if (s.tasksMax && !SIZE_RE.test(s.tasksMax)) return "TasksMax must be a number, a percentage or infinity";
  if (s.nice) {
    const n = Number(s.nice);
    if (!/^-?\d+$/.test(s.nice) || n < -20 || n > 19) return "Nice must be an integer between -20 and 19";
  }
  if (s.protectSystem && !PROTECT_SYSTEM_VALUES.includes(s.protectSystem)) return "ProtectSystem must be one of yes, no, full, strict";
  if (s.protectHome && !PROTECT_HOME_VALUES.includes(s.protectHome)) return "ProtectHome must be one of yes, no, read-only, tmpfs";
  return null;
}

function environmentProblem(env: string[]): string | null {
  if (env.length > 64) return "At most 64 environment entries";
  for (const e of env) {
    if (e.length > 1024) return "An environment entry is longer than 1024 characters";
    if (!ENV_RE.test(e) || /[\n\r\0]/.test(e)) return `Environment entry "${e}" must be KEY=value on one line`;
  }
  return null;
}

/** Why one raw directive cannot be saved, or null. */
export function directiveProblem(d: SystemdDirectiveEntry, unitTypeName: string): string | null {
  if (!(DIRECTIVE_SECTIONS as readonly string[]).includes(d.section)) return `Section must be one of ${DIRECTIVE_SECTIONS.join(", ")}`;
  if (["Service", "Timer", "Socket", "Path"].includes(d.section) && d.section.toLowerCase() !== unitTypeName) {
    return `A [${d.section}] directive applies to .${d.section.toLowerCase()} units only`;
  }
  const keyProblem = directiveKeyProblem(d.key);
  if (keyProblem) return keyProblem;
  if (d.values.length > 16) return `Directive ${d.key} has more than 16 values`;
  for (const v of d.values) {
    if (v.length > 1024) return `Directive ${d.key}: a value is longer than 1024 characters`;
    if (/[\n\r\0]/.test(v)) return `Directive ${d.key}: a value must be a single line`;
  }
  return null;
}

/** Why one task cannot be saved, or null. */
export function taskEntryProblem(t: SystemdTaskEntry): string | null {
  const nameProblem = taskNameProblem(t.name);
  if (nameProblem) return nameProblem;
  if (t.description.length > 512) return "Description is longer than 512 characters";
  if (t.trigger === "SYSTEMD_TASK_TRIGGER_TIMER") {
    if (!timerHasTrigger(t.schedule)) return "A timer task needs a schedule: a calendar expression or a monotonic trigger such as OnBootSec";
    const p = timerProblem(t.schedule);
    if (p) return p;
  } else if (t.trigger !== "SYSTEMD_TASK_TRIGGER_BOOT") {
    return "Set a trigger: timer or boot";
  }
  if (t.mode === "script") {
    if (!t.script) return "Set a script to run";
    if (!t.script.startsWith("#!")) return "The script must start with an interpreter line such as #!/bin/sh";
    if (new TextEncoder().encode(t.script).length > MAX_TASK_SCRIPT_BYTES) return "The script is larger than 64 KiB";
  } else {
    const argv = t.execStart.filter((a) => a !== "");
    if (argv.length === 0) return "Set the program to run";
    if (!argv[0].startsWith("/")) return "The program must be an absolute path";
    for (const a of argv) {
      if (a.length > 1024) return "A program argument is longer than 1024 characters";
      if (/[\n\r\0]/.test(a)) return "A program argument must be a single line";
    }
  }
  if (isGlobalScope(t.scope) && (t.user || t.group)) {
    return "A task for all users runs as the logged-in user; leave user and group empty";
  }
  const user = userNameProblem(t.user);
  if (user) return `User: ${user}`;
  const group = userNameProblem(t.group);
  if (group) return `Group: ${group}`;
  if (t.workingDirectory && (!t.workingDirectory.startsWith("/") || /[\n\r\0]/.test(t.workingDirectory))) {
    return "Working directory must be an absolute path";
  }
  const env = environmentProblem(t.environment);
  if (env) return env;
  if (t.timeoutStartSec) {
    const p = timeSpanProblem(t.timeoutStartSec);
    if (p) return `TimeoutStartSec: ${p}`;
  }
  if (t.apparmorProfile) {
    const p = apparmorProfileProblem(t.apparmorProfile);
    if (p) return `AppArmorProfile: ${p}`;
  }
  if (t.selinuxContext) {
    const p = selinuxContextProblem(t.selinuxContext);
    if (p) return `SELinuxContext: ${p}`;
  }
  return null;
}

/** Why one unit entry cannot be saved, or null. */
export function unitEntryProblem(u: SystemdUnitEntry): string | null {
  const nameProblem = unitNameProblem(u.name);
  if (nameProblem) return nameProblem;
  if (isGlobalScope(u.scope) && isProtectedGlobalUnit(u.name)) {
    return "This unit is protected in the user scope and cannot be managed by a policy";
  }
  const t = unitType(u.name);
  const hasTimer = t === "timer" && !timerIsEmpty(u.timer);
  const hasService = t === "service" && !serviceIsEmpty(u.service);
  const hasRaw = u.raw.length > 0;
  if (
    u.enablement === "SYSTEMD_ENABLEMENT_UNSPECIFIED" &&
    u.activity === "SYSTEMD_ACTIVITY_UNSPECIFIED" &&
    !hasTimer && !hasService && !hasRaw && !u.requireLastRunSuccess
  ) {
    return "Set an enablement, an activity, an override or the last-run check";
  }
  if (u.enablement === "SYSTEMD_ENABLEMENT_MASKED" && u.activity === "SYSTEMD_ACTIVITY_RUNNING") {
    return "A masked unit cannot be running";
  }
  if (t === "target" && u.activity !== "SYSTEMD_ACTIVITY_UNSPECIFIED") {
    return "Targets can only be enabled, disabled or masked";
  }
  if (u.requireLastRunSuccess && t !== "timer" && t !== "service") {
    return "The last-run check applies to timers and services only";
  }
  if (u.restartOnChange && t !== "service") return "Restart on change applies to services only";
  if (u.description.length > 512) return "Description is longer than 512 characters";
  if (hasTimer) {
    const p = timerProblem(u.timer);
    if (p) return p;
  }
  if (hasService) {
    const p = serviceProblem(u.service);
    if (p) return p;
  }
  if (u.raw.length > 32) return "At most 32 raw directives per unit";
  for (const d of u.raw) {
    const p = directiveProblem(d, t);
    if (p) return p;
  }
  return null;
}

/** Why the whole policy cannot be saved, or null. */
/** AppArmorProfile= value: a loaded profile name, optionally prefixed with "-" (tolerate a missing profile). */
export function apparmorProfileProblem(v: string): string | null {
  return /^-?[A-Za-z0-9_][A-Za-z0-9_.:/+-]{0,254}$/.test(v)
    ? null
    : "Use the name the profile is loaded with (letters, digits, _ . : / + -), optionally prefixed with -";
}

/** SELinuxContext= value: user:role:type with an optional level, optionally prefixed with "-". */
export function selinuxContextProblem(v: string): string | null {
  return /^-?[a-z0-9_]{1,64}:[a-z0-9_]{1,64}:[a-z0-9_]{1,128}(:[a-z0-9_.,:-]{1,128})?$/.test(v)
    ? null
    : "Use user:role:type or user:role:type:level, optionally prefixed with -";
}

export function validateSystemdContent(c: SystemdContent): string | null {
  if (c.units.length === 0 && c.tasks.length === 0) return "Add at least one unit or task before saving";
  if (c.units.length > 200) return "A policy can manage at most 200 units";
  if (c.tasks.length > 50) return "A policy can deploy at most 50 tasks";
  const seen = new Set<string>();
  let hasEnablement = false;
  for (const u of c.units) {
    const p = unitEntryProblem(u);
    if (p) return `${u.name || "(unnamed unit)"}: ${p}`;
    // The same unit may appear once per scope.
    const key = `${isGlobalScope(u.scope) ? "global/" : ""}${u.name}`;
    if (seen.has(key)) return `Unit "${u.name}" is listed twice`;
    seen.add(key);
    if (u.enablement !== "SYSTEMD_ENABLEMENT_UNSPECIFIED") hasEnablement = true;
  }
  const seenTasks = new Set<string>();
  for (const t of c.tasks) {
    const p = taskEntryProblem(t);
    if (p) return `${t.name ? `task ${t.name}` : "(unnamed task)"}: ${p}`;
    if (seenTasks.has(t.name)) return `Task "${t.name}" is listed twice`;
    seenTasks.add(t.name);
  }
  if (c.writePresets && !hasEnablement) return "The preset file needs at least one unit with an enablement";
  return null;
}

/** Whether the policy runs commands on nodes (tasks or raw Exec* directives) and needs systemd:exec. */
export function contentRunsCommands(c: SystemdContent): boolean {
  if (c.tasks.length > 0) return true;
  return c.units.some((u) => u.raw.some((d) => isExecDirective(d.key)));
}

/* ── labels ── */

export function enablementLabel(e: SystemdEnablement): string {
  return ENABLEMENT_OPTIONS.find((o) => o.value === e)?.label ?? e;
}

export function activityLabel(a: SystemdActivity): string {
  return ACTIVITY_OPTIONS.find((o) => o.value === a)?.label ?? a;
}

/** One-line schedule description shared by units and tasks. */
export function describeSchedule(tm: SystemdTimerOverrides): string {
  const sched: string[] = [];
  if (tm.onCalendar.length) sched.push(`schedule ${tm.onCalendar.join(" | ")}`);
  if (tm.onBootSec) sched.push(`${tm.onBootSec} after boot`);
  if (tm.onStartupSec) sched.push(`${tm.onStartupSec} after startup`);
  if (tm.onActiveSec) sched.push(`${tm.onActiveSec} after activation`);
  if (tm.onUnitActiveSec) sched.push(`every ${tm.onUnitActiveSec}`);
  if (tm.onUnitInactiveSec) sched.push(`${tm.onUnitInactiveSec} after the last run`);
  if (tm.persistent !== undefined) sched.push(tm.persistent ? "persistent" : "not persistent");
  if (tm.randomizedDelaySec) sched.push(`random delay ${tm.randomizedDelaySec}`);
  if (tm.accuracySec) sched.push(`accuracy ${tm.accuracySec}`);
  if (tm.wakeSystem !== undefined) sched.push(tm.wakeSystem ? "wakes the system" : "does not wake the system");
  if (tm.fixedRandomDelay !== undefined) sched.push(tm.fixedRandomDelay ? "fixed random delay" : "varying random delay");
  if (tm.onClockChange !== undefined) sched.push(tm.onClockChange ? "runs on clock change" : "ignores clock changes");
  if (tm.onTimezoneChange !== undefined) sched.push(tm.onTimezoneChange ? "runs on timezone change" : "ignores timezone changes");
  return sched.join(", ");
}

/** One-line description of a task, for summaries. */
export function describeTaskEntry(t: SystemdTaskEntry): string {
  const parts: string[] = [];
  if (isGlobalScope(t.scope)) parts.push("all users");
  parts.push(t.trigger === "SYSTEMD_TASK_TRIGGER_BOOT" ? (isGlobalScope(t.scope) ? "at login" : "at boot") : describeSchedule(t.schedule) || "timer");
  parts.push(t.mode === "program" ? `runs ${t.execStart.join(" ")}` : `script (${new TextEncoder().encode(t.script).length} bytes)`);
  if (t.user) parts.push(`as ${t.user}${t.group ? `:${t.group}` : ""}`);
  if (!t.hardened) parts.push("sandboxing off");
  if (t.requireLastRunSuccess) parts.push("last run must succeed");
  if (!t.enabled) parts.push("files only, not enabled");
  return parts.join("; ");
}

/** One-line description of what a unit entry sets, for summaries. */
export function describeUnitEntry(u: SystemdUnitEntry): string {
  const parts: string[] = [];
  if (isGlobalScope(u.scope)) parts.push("all users");
  if (u.enablement !== "SYSTEMD_ENABLEMENT_UNSPECIFIED") parts.push(enablementLabel(u.enablement).toLowerCase());
  if (u.activity !== "SYSTEMD_ACTIVITY_UNSPECIFIED") parts.push(activityLabel(u.activity).toLowerCase());
  if (u.requireLastRunSuccess) parts.push("last run must succeed");
  if (u.restartOnChange) parts.push("restart after override changes");
  const t = unitType(u.name);
  if (t === "timer" && !timerIsEmpty(u.timer)) {
    parts.push(describeSchedule(u.timer));
  }
  if (t === "service" && !serviceIsEmpty(u.service)) {
    const sv = u.service;
    const svc: string[] = [];
    if (sv.restart) svc.push(`Restart=${sv.restart}`);
    if (sv.restartSec) svc.push(`RestartSec=${sv.restartSec}`);
    if (sv.timeoutStartSec) svc.push(`TimeoutStartSec=${sv.timeoutStartSec}`);
    if (sv.timeoutStopSec) svc.push(`TimeoutStopSec=${sv.timeoutStopSec}`);
    if (sv.environment.length) svc.push(`${sv.environment.length} environment ${sv.environment.length === 1 ? "entry" : "entries"}`);
    if (sv.memoryMax) svc.push(`MemoryMax=${sv.memoryMax}`);
    if (sv.cpuQuota) svc.push(`CPUQuota=${sv.cpuQuota}`);
    if (sv.tasksMax) svc.push(`TasksMax=${sv.tasksMax}`);
    if (sv.nice) svc.push(`Nice=${sv.nice}`);
    if (sv.noNewPrivileges !== undefined) svc.push(`NoNewPrivileges=${sv.noNewPrivileges ? "yes" : "no"}`);
    if (sv.protectSystem) svc.push(`ProtectSystem=${sv.protectSystem}`);
    if (sv.protectHome) svc.push(`ProtectHome=${sv.protectHome}`);
    if (sv.privateTmp !== undefined) svc.push(`PrivateTmp=${sv.privateTmp ? "yes" : "no"}`);
    parts.push(svc.join(", "));
  }
  if (u.raw.length) {
    parts.push(u.raw.map((d) => `[${d.section}] ${d.key}=${d.values.join(" | ") || "(reset)"}`).join(", "));
  }
  return parts.join("; ");
}
