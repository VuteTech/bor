// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared content model for the SELinux policy editor: the protojson
 * (camelCase, enum names as strings) shape stored in `Policy.content`, a
 * defensive parser, a compact serializer and the structural checks the
 * server applies too. Mirrors proto/policy/selinux.proto and the rules in
 * server/pkg/macrules/macrules.go.
 */

export type SelinuxMode = "SELINUX_MODE_UNSPECIFIED" | "SELINUX_MODE_ENFORCING" | "SELINUX_MODE_PERMISSIVE";

export type SelinuxFileKind =
  | "SELINUX_FILE_KIND_UNSPECIFIED"
  | "SELINUX_FILE_KIND_FILE"
  | "SELINUX_FILE_KIND_DIR"
  | "SELINUX_FILE_KIND_CHAR"
  | "SELINUX_FILE_KIND_BLOCK"
  | "SELINUX_FILE_KIND_SOCKET"
  | "SELINUX_FILE_KIND_PIPE"
  | "SELINUX_FILE_KIND_SYMLINK";

export interface SelinuxBooleanEntry {
  name: string;
  value: boolean;
  description: string;
}

export interface SelinuxModuleEntry {
  name: string;
  enabled: boolean;
}

export interface SelinuxCustomModuleEntry {
  name: string;
  cil: string;
  description: string;
}

export interface SelinuxFileContextEntry {
  pathRegex: string;
  type: string;
  kind: SelinuxFileKind;
  restorecon: boolean;
}

export interface SelinuxLoginEntry {
  login: string;
  seuser: string;
  range: string;
}

export interface SelinuxContent {
  mode: SelinuxMode;
  requireActive: boolean;
  prepareDisabledNodes: boolean;
  booleans: SelinuxBooleanEntry[];
  permissiveDomains: string[];
  modules: SelinuxModuleEntry[];
  customModules: SelinuxCustomModuleEntry[];
  fileContexts: SelinuxFileContextEntry[];
  logins: SelinuxLoginEntry[];
}

export const SELINUX_MODE_OPTIONS: { value: SelinuxMode; label: string; help: string }[] = [
  { value: "SELINUX_MODE_UNSPECIFIED", label: "Keep the node's mode", help: "The policy does not change the enforcement mode." },
  { value: "SELINUX_MODE_ENFORCING", label: "Enforcing", help: "Denials are enforced. The runtime switch and /etc/selinux/config are set." },
  { value: "SELINUX_MODE_PERMISSIVE", label: "Permissive", help: "Denials are only logged. Use it on a pilot group before enforcing." },
];

export const SELINUX_FILE_KIND_OPTIONS: { value: SelinuxFileKind; label: string }[] = [
  { value: "SELINUX_FILE_KIND_UNSPECIFIED", label: "Any" },
  { value: "SELINUX_FILE_KIND_FILE", label: "Regular file" },
  { value: "SELINUX_FILE_KIND_DIR", label: "Directory" },
  { value: "SELINUX_FILE_KIND_SYMLINK", label: "Symbolic link" },
  { value: "SELINUX_FILE_KIND_CHAR", label: "Character device" },
  { value: "SELINUX_FILE_KIND_BLOCK", label: "Block device" },
  { value: "SELINUX_FILE_KIND_SOCKET", label: "Socket" },
  { value: "SELINUX_FILE_KIND_PIPE", label: "Named pipe" },
];

const MODE_BY_NUMBER: SelinuxMode[] = ["SELINUX_MODE_UNSPECIFIED", "SELINUX_MODE_ENFORCING", "SELINUX_MODE_PERMISSIVE"];
const KIND_BY_NUMBER: SelinuxFileKind[] = [
  "SELINUX_FILE_KIND_UNSPECIFIED",
  "SELINUX_FILE_KIND_FILE",
  "SELINUX_FILE_KIND_DIR",
  "SELINUX_FILE_KIND_CHAR",
  "SELINUX_FILE_KIND_BLOCK",
  "SELINUX_FILE_KIND_SOCKET",
  "SELINUX_FILE_KIND_PIPE",
  "SELINUX_FILE_KIND_SYMLINK",
];

function asMode(v: unknown): SelinuxMode {
  if (typeof v === "number") return MODE_BY_NUMBER[v] ?? "SELINUX_MODE_UNSPECIFIED";
  if (typeof v === "string" && (MODE_BY_NUMBER as string[]).includes(v)) return v as SelinuxMode;
  return "SELINUX_MODE_UNSPECIFIED";
}

function asKind(v: unknown): SelinuxFileKind {
  if (typeof v === "number") return KIND_BY_NUMBER[v] ?? "SELINUX_FILE_KIND_UNSPECIFIED";
  if (typeof v === "string" && (KIND_BY_NUMBER as string[]).includes(v)) return v as SelinuxFileKind;
  return "SELINUX_FILE_KIND_UNSPECIFIED";
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const bool = (v: unknown): boolean => v === true;
const arr = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const obj = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

export function emptySelinuxContent(): SelinuxContent {
  return { mode: "SELINUX_MODE_UNSPECIFIED", requireActive: false, prepareDisabledNodes: false, booleans: [], permissiveDomains: [], modules: [], customModules: [], fileContexts: [], logins: [] };
}

/** Parses stored content; anything malformed yields an empty, editable model. */
export function parseSelinuxContent(raw: string): SelinuxContent {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return emptySelinuxContent();
  }
  const o = obj(parsed);
  return {
    mode: asMode(o.mode),
    requireActive: bool(o.requireActive),
    prepareDisabledNodes: bool(o.prepareDisabledNodes),
    booleans: arr(o.booleans).map((b) => ({ name: str(obj(b).name), value: bool(obj(b).value), description: str(obj(b).description) })),
    permissiveDomains: arr(o.permissiveDomains).map(str).filter(Boolean),
    modules: arr(o.modules).map((m) => ({ name: str(obj(m).name), enabled: bool(obj(m).enabled) })),
    customModules: arr(o.customModules).map((m) => ({ name: str(obj(m).name), cil: str(obj(m).cil), description: str(obj(m).description) })),
    fileContexts: arr(o.fileContexts).map((f) => ({ pathRegex: str(obj(f).pathRegex), type: str(obj(f).type), kind: asKind(obj(f).kind), restorecon: bool(obj(f).restorecon) })),
    logins: arr(o.logins).map((l) => ({ login: str(obj(l).login), seuser: str(obj(l).seuser), range: str(obj(l).range) })),
  };
}

/** Serializes the model without empty fields, in the protojson form the server stores. */
export function serializeSelinuxContent(c: SelinuxContent): string {
  const out: Record<string, unknown> = {};
  if (c.mode !== "SELINUX_MODE_UNSPECIFIED") out.mode = c.mode;
  if (c.requireActive) out.requireActive = true;
  if (c.prepareDisabledNodes) out.prepareDisabledNodes = true;
  if (c.booleans.length) out.booleans = c.booleans.map((b) => ({ name: b.name, value: b.value, ...(b.description ? { description: b.description } : {}) }));
  if (c.permissiveDomains.length) out.permissiveDomains = c.permissiveDomains;
  if (c.modules.length) out.modules = c.modules.map((m) => ({ name: m.name, enabled: m.enabled }));
  if (c.customModules.length) out.customModules = c.customModules.map((m) => ({ name: m.name, cil: m.cil, ...(m.description ? { description: m.description } : {}) }));
  if (c.fileContexts.length) {
    out.fileContexts = c.fileContexts.map((f) => ({
      pathRegex: f.pathRegex,
      type: f.type,
      ...(f.kind !== "SELINUX_FILE_KIND_UNSPECIFIED" ? { kind: f.kind } : {}),
      ...(f.restorecon ? { restorecon: true } : {}),
    }));
  }
  if (c.logins.length) out.logins = c.logins.map((l) => ({ login: l.login, seuser: l.seuser, ...(l.range ? { range: l.range } : {}) }));
  return JSON.stringify(out, null, 2);
}

/* ── rules mirrored from server/pkg/macrules ── */

const BOOLEAN_RE = /^[a-z][a-z0-9_]{0,127}$/;
const TYPE_RE = /^[a-z][a-z0-9_]{0,126}_t$/;
const CUSTOM_NAME_RE = /^[a-z0-9][a-z0-9_.-]{0,63}$/;
const MODULE_RE = /^[a-z0-9][a-z0-9_.-]{0,63}$/;
const SEUSER_RE = /^[a-z][a-z0-9_]{0,63}$/;
const LOGIN_RE = /^%?[a-z_][a-z0-9_.@-]{0,31}\$?$/;
const RANGE_RE = /^s[0-9]{1,4}(-s[0-9]{1,4})?(:c[0-9]{1,4}(\.c[0-9]{1,4})?(,c[0-9]{1,4}(\.c[0-9]{1,4})?)*)?$/;

export const PROTECTED_SELINUX_BOOLEANS = new Set(["secure_mode_policyload"]);

const RISKY_BOOLEAN_VALUES: Record<string, boolean> = {
  unconfined_login: false,
  deny_execmem: true,
  deny_ptrace: true,
  selinuxuser_execstack: false,
  selinuxuser_execmod: false,
  secure_mode_insmod: true,
  domain_kernel_load_modules: false,
  ssh_sysadm_login: true,
};

/** Setting the boolean to this value can lock users out or break desktop applications. */
export function isRiskySelinuxBoolean(name: string, value: boolean): boolean {
  if (name in RISKY_BOOLEAN_VALUES && RISKY_BOOLEAN_VALUES[name] === value) return true;
  if (name.startsWith("xguest_")) return true;
  if (name.startsWith("use_") && name.endsWith("_home_dirs") && !value) return true;
  return false;
}

export const RISKY_SELINUX_PERMISSIVE = new Set(["init_t", "kernel_t", "unconfined_t", "sshd_t", "xdm_t", "sudo_t", "login_t", "systemd_logind_t", "polkit_t"]);
export const RISKY_SELINUX_MODULES = new Set(["unconfined", "xserver", "systemd", "init", "dbus", "polkit", "sudo", "su", "login", "ssh", "sshd", "networkmanager", "selinuxutil", "userdomain", "authlogin", "kernel", "logging", "staff", "sysadm", "unprivuser", "xdm", "gdm", "sddm"]);

export function booleanNameProblem(name: string): string | null {
  if (!name) return "Enter a boolean name";
  if (!BOOLEAN_RE.test(name)) return "Boolean names are lowercase letters, digits and underscores";
  if (PROTECTED_SELINUX_BOOLEANS.has(name)) return "This boolean cannot be set by a policy: it would lock the policy against every later change";
  return null;
}

export function domainProblem(name: string): string | null {
  if (!name) return "Enter a type name";
  if (!TYPE_RE.test(name)) return "Type names are lowercase and end in _t (for example corpapp_t)";
  return null;
}

export function moduleNameProblem(name: string): string | null {
  if (!name) return "Enter a module name";
  if (!MODULE_RE.test(name) || name.includes("..")) return "Module names are lowercase letters, digits, '_', '.' and '-'";
  if (name.startsWith("bor-")) return "Bor modules are managed as custom modules";
  return null;
}

export function customNameProblem(name: string): string | null {
  if (!name) return "Enter a name";
  if (!CUSTOM_NAME_RE.test(name) || name.includes("..")) return "Names are lowercase letters, digits, '_', '.' and '-' (at most 64 characters)";
  if (name.startsWith("bor-")) return "The bor- prefix is added automatically";
  return null;
}

/** A light structural check of CIL: balanced parentheses, no loose text. The server checks the statement list. */
export function cilProblem(text: string): string | null {
  if (!text.trim()) return "Enter the module source";
  if (text.length > 64 * 1024) return "The module is larger than 64 KiB";
  let depth = 0;
  let inString = false;
  let inComment = false;
  let statements = 0;
  for (const c of text) {
    if (inComment) {
      if (c === "\n") inComment = false;
      continue;
    }
    if (inString) {
      if (c === "\"") inString = false;
      continue;
    }
    if (c === ";") {
      inComment = true;
      continue;
    }
    if (c === "\"") {
      inString = true;
      continue;
    }
    if (c === "(") {
      depth++;
      if (depth === 1) statements++;
    } else if (c === ")") {
      depth--;
      if (depth < 0) return "Unbalanced parentheses: ')' without '('";
    } else if (depth === 0 && !/\s/.test(c)) {
      return "Text outside of a statement";
    }
  }
  if (inString) return "Unterminated string";
  if (depth !== 0) return `Unbalanced parentheses: ${depth} '(' without ')'`;
  if (statements === 0) return "The module has no statement";
  return null;
}

const FILE_CONTEXT_ROOTS = new Set(["/", "/usr", "/etc", "/var", "/boot", "/dev", "/proc", "/sys", "/run", "/lib", "/lib64", "/bin", "/sbin", "/root", "/home", "/tmp", "/var/lib", "/var/log", "/usr/bin", "/usr/lib", "/usr/lib64", "/usr/sbin", "/usr/share", "/etc/selinux"]);

/** The literal prefix of a path expression (what restorecon walks). */
export function literalPrefix(expr: string): string {
  const m = /[.^$*+?()[\]{}|\\]/.exec(expr);
  if (!m) return expr;
  let prefix = expr.slice(0, m.index);
  if (!expr.slice(m.index).startsWith("(/")) {
    const i = prefix.lastIndexOf("/");
    if (i >= 0) prefix = prefix.slice(0, i + 1);
  }
  return prefix || "/";
}

export function fileContextProblem(f: SelinuxFileContextEntry): string | null {
  if (!f.pathRegex) return "Enter a path expression";
  if (!f.pathRegex.startsWith("/")) return "The path expression must be absolute";
  try {
    new RegExp(`^${f.pathRegex}$`);
  } catch {
    return "The path expression is not a valid regular expression";
  }
  const clean = literalPrefix(f.pathRegex).replace(/\/+$/, "") || "/";
  if (FILE_CONTEXT_ROOTS.has(clean)) return `The expression would relabel ${clean} as a whole`;
  if (domainProblem(f.type)) return "Enter the SELinux type (for example bin_t)";
  return null;
}

export function loginProblem(l: SelinuxLoginEntry): string | null {
  if (!l.login) return "Enter a login or %group";
  if (l.login === "__default__" || l.login === "root") return `The ${l.login} mapping cannot be changed by a policy`;
  if (!LOGIN_RE.test(l.login)) return "Login names are lowercase; groups start with %";
  if (!l.seuser) return "Enter the SELinux user (for example user_u)";
  if (!SEUSER_RE.test(l.seuser) || l.seuser === "system_u") return "SELinux user names are lowercase (user_u, staff_u); system_u is not allowed";
  if (l.range && !RANGE_RE.test(l.range)) return "The range looks like s0 or s0-s0:c0.c1023";
  return null;
}

export function hasAnySelinuxSetting(c: SelinuxContent): boolean {
  return c.mode !== "SELINUX_MODE_UNSPECIFIED" || c.requireActive || c.prepareDisabledNodes || c.booleans.length > 0 || c.permissiveDomains.length > 0 || c.modules.length > 0 || c.customModules.length > 0 || c.fileContexts.length > 0 || c.logins.length > 0;
}

/** The save-time check: the first problem, or null when the content is valid. */
export function validateSelinuxContent(c: SelinuxContent): string | null {
  if (!hasAnySelinuxSetting(c)) return "Set a mode, a requirement, a boolean, an exception, a module, a file context or a login mapping before saving";
  if (c.prepareDisabledNodes && c.mode === "SELINUX_MODE_UNSPECIFIED") return "Preparing disabled nodes needs a mode the node is brought to";
  const seen = new Set<string>();
  for (const b of c.booleans) {
    const p = booleanNameProblem(b.name);
    if (p) return `Boolean ${b.name || "(unnamed)"}: ${p}`;
    if (seen.has(b.name)) return `Boolean ${b.name} is listed twice`;
    seen.add(b.name);
  }
  seen.clear();
  for (const d of c.permissiveDomains) {
    const p = domainProblem(d);
    if (p) return `Permissive domain ${d || "(empty)"}: ${p}`;
    if (seen.has(d)) return `Permissive domain ${d} is listed twice`;
    seen.add(d);
  }
  seen.clear();
  for (const m of c.modules) {
    const p = moduleNameProblem(m.name);
    if (p) return `Module ${m.name || "(unnamed)"}: ${p}`;
    if (seen.has(m.name)) return `Module ${m.name} is listed twice`;
    seen.add(m.name);
  }
  seen.clear();
  for (const m of c.customModules) {
    const p = customNameProblem(m.name) ?? cilProblem(m.cil);
    if (p) return `Custom module ${m.name || "(unnamed)"}: ${p}`;
    if (seen.has(m.name)) return `Custom module ${m.name} is listed twice`;
    seen.add(m.name);
  }
  seen.clear();
  for (const f of c.fileContexts) {
    const p = fileContextProblem(f);
    if (p) return `File context ${f.pathRegex || "(empty)"}: ${p}`;
    const key = `${f.pathRegex}\u0000${f.kind}`;
    if (seen.has(key)) return `File context ${f.pathRegex} is listed twice`;
    seen.add(key);
  }
  seen.clear();
  for (const l of c.logins) {
    const p = loginProblem(l);
    if (p) return `Login ${l.login || "(empty)"}: ${p}`;
    if (seen.has(l.login)) return `Login ${l.login} is mapped twice`;
    seen.add(l.login);
  }
  return null;
}

/* ── templates ── */

export interface SelinuxBooleanTemplate {
  id: string;
  label: string;
  description: string;
  booleans: SelinuxBooleanEntry[];
}

export const SELINUX_BOOLEAN_TEMPLATES: SelinuxBooleanTemplate[] = [
  {
    id: "hardening",
    label: "Process hardening",
    description: "The boolean half of the desktop hardening benchmarks: no executable stacks or heaps for user programs, no ptrace, no kernel module loading after boot.",
    booleans: [
      { name: "selinuxuser_execstack", value: false, description: "No executable stack for user programs" },
      { name: "selinuxuser_execheap", value: false, description: "No executable heap for user programs" },
      { name: "deny_ptrace", value: true, description: "No ptrace (debuggers, strace) for anyone" },
      { name: "secure_mode_insmod", value: true, description: "No kernel module loading after boot" },
    ],
  },
  {
    id: "nfs-homes",
    label: "Network home directories (NFS)",
    description: "Home directories on NFS or autofs mounts.",
    booleans: [{ name: "use_nfs_home_dirs", value: true, description: "Home directories on NFS" }],
  },
  {
    id: "smb-homes",
    label: "Network home directories (Samba)",
    description: "Home directories on CIFS/SMB shares.",
    booleans: [{ name: "use_samba_home_dirs", value: true, description: "Home directories on CIFS/SMB" }],
  },
  {
    id: "kiosk",
    label: "Kiosk and guest accounts",
    description: "Locks the xguest kiosk user down: no network, no removable media, no Bluetooth, no running downloaded content.",
    booleans: [
      { name: "xguest_connect_network", value: false, description: "Kiosk user: no network" },
      { name: "xguest_mount_media", value: false, description: "Kiosk user: no removable media" },
      { name: "xguest_use_bluetooth", value: false, description: "Kiosk user: no Bluetooth" },
      { name: "xguest_exec_content", value: false, description: "Kiosk user: no downloaded programs" },
    ],
  },
  {
    id: "developer",
    label: "Developer workstation",
    description: "Virtual machines on USB and NFS storage, containers that manage cgroups and connect anywhere.",
    booleans: [
      { name: "virt_use_usb", value: true, description: "VMs may use USB devices" },
      { name: "virt_use_nfs", value: true, description: "VM images on NFS" },
      { name: "container_manage_cgroup", value: true, description: "Containers may manage cgroups" },
      { name: "container_connect_any", value: true, description: "Containers may connect to any port" },
    ],
  },
];

export interface SelinuxCilTemplate {
  id: string;
  label: string;
  description: string;
  cil: string;
}

export const SELINUX_CIL_TEMPLATES: SelinuxCilTemplate[] = [
  {
    id: "read-dir",
    label: "Allow a domain to read a directory",
    description: "Lets one domain read and search files of one type. Replace the two types; find them in the denials of the node page.",
    cil: "; Allow corpapp_t to read files labelled corpapp_data_t.\n(allow corpapp_t corpapp_data_t (dir (read search open getattr)))\n(allow corpapp_t corpapp_data_t (file (read open getattr map)))\n",
  },
  {
    id: "connect-port",
    label: "Allow a domain to connect to a TCP port type",
    description: "Lets one domain connect to sockets of one port type (http_port_t, ssh_port_t, ...).",
    cil: "; Allow corpapp_t to connect to HTTP ports.\n(allow corpapp_t http_port_t (tcp_socket (name_connect)))\n",
  },
  {
    id: "dontaudit",
    label: "Silence a harmless denial",
    description: "Stops logging a denial that is expected and harmless, without allowing it.",
    cil: "; Do not log corpapp_t probing /proc entries it does not need.\n(dontaudit corpapp_t proc_t (file (read getattr)))\n",
  },
];

/** A short description of a policy for summaries. */
export function describeSelinuxContent(c: SelinuxContent): string[] {
  const parts: string[] = [];
  if (c.mode !== "SELINUX_MODE_UNSPECIFIED") parts.push(c.mode === "SELINUX_MODE_ENFORCING" ? "enforcing" : "permissive");
  if (c.requireActive) parts.push("SELinux required");
  if (c.prepareDisabledNodes) parts.push("prepares disabled nodes");
  if (c.booleans.length) parts.push(`${c.booleans.length} boolean${c.booleans.length === 1 ? "" : "s"}`);
  if (c.permissiveDomains.length) parts.push(`${c.permissiveDomains.length} permissive domain${c.permissiveDomains.length === 1 ? "" : "s"}`);
  if (c.modules.length) parts.push(`${c.modules.length} module switch${c.modules.length === 1 ? "" : "es"}`);
  if (c.customModules.length) parts.push(`${c.customModules.length} custom module${c.customModules.length === 1 ? "" : "s"}`);
  if (c.fileContexts.length) parts.push(`${c.fileContexts.length} file context${c.fileContexts.length === 1 ? "" : "s"}`);
  if (c.logins.length) parts.push(`${c.logins.length} login mapping${c.logins.length === 1 ? "" : "s"}`);
  return parts;
}
