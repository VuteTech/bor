// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * The single frontend registry of policy types.
 *
 * `Policy.type` is a free string on the wire; the server accepts unknown types
 * for forward compatibility. Everything the UI knows *about* a type — label,
 * category, copy for the create wizard, icon, default content and the
 * "has at least one setting" rule — lives here so the wizard, the policies
 * list, the filters, the editor and the dashboard cannot drift apart.
 *
 * Field definitions (which keys a Firefox policy has, etc.) are NOT here: they
 * are generated from the protos (`generated/proto/*_ui.ts`). This file is the
 * hand-written "which types exist" list that `docs/proto-driven-ui-plan.md`
 * deliberately keeps manual.
 */

import type { ComponentType } from "react";
import type { SVGIconProps } from "@patternfly/react-icons/dist/esm/createIcon";
import EdgeIcon from "@patternfly/react-icons/dist/esm/icons/edge-icon";
import SlidersHIcon from "@patternfly/react-icons/dist/esm/icons/sliders-h-icon";
import KeyIcon from "@patternfly/react-icons/dist/esm/icons/key-icon";
import ShieldAltIcon from "@patternfly/react-icons/dist/esm/icons/shield-alt-icon";
import UserClockIcon from "@patternfly/react-icons/dist/esm/icons/user-clock-icon";
import CubesIcon from "@patternfly/react-icons/dist/esm/icons/cubes-icon";
import CogIcon from "@patternfly/react-icons/dist/esm/icons/cog-icon";

import { FirefoxMarkIcon } from "../icons/policyTypes/FirefoxMarkIcon";
import { ThunderbirdMarkIcon } from "../icons/policyTypes/ThunderbirdMarkIcon";
import { ChromeMarkIcon } from "../icons/policyTypes/ChromeMarkIcon";
import { KdeMarkIcon } from "../icons/policyTypes/KdeMarkIcon";
import { FlatpakMarkIcon } from "../icons/policyTypes/FlatpakMarkIcon";

export type PolicyTypeId =
  | "Firefox"
  | "Thunderbird"
  | "Chrome"
  | "Edge"
  | "Kconfig"
  | "Dconf"
  | "Polkit"
  | "Firewalld"
  | "SessionAccess"
  | "Package"
  | "Flatpak";

export type PolicyTypeCategory = "browsers" | "desktop" | "system";

export interface PolicyTypeDef {
  /** Wire value stored in `Policy.type`. Never shown alone when a label exists. */
  id: PolicyTypeId;
  /** Human label ("KDE Plasma"). */
  label: string;
  /** Technical name shown as a subtitle when it differs from the label ("Kconfig"). */
  technicalName?: string;
  category: PolicyTypeCategory;
  /** One line for the tile body (≤ 60 characters). */
  tagline: string;
  /** Two or three sentences for the description panel. */
  description: string;
  /** What the agent writes or controls on the node. */
  manages: string[];
  /** Which desktops / applications the type applies to. */
  appliesTo: string[];
  /** Two or three example uses. */
  examples: string[];
  /** User documentation, when it exists. */
  docsHref?: string;
  /** Flat single-colour icon, PatternFly-icon compatible. */
  Icon: ComponentType<SVGIconProps>;
  /** Empty content a freshly-created policy of this type starts from. */
  defaultContent: () => string;
  /**
   * Returns null when the content is complete enough to be saved, otherwise the
   * message to show. Mirrors the server's structural checks so the admin sees
   * the problem before a round trip. "At least one setting" is the rule for
   * every type: the server never accepts an empty policy.
   */
  validateContent: (content: string) => string | null;
}

export const POLICY_TYPE_CATEGORIES: { id: PolicyTypeCategory; label: string }[] = [
  { id: "browsers", label: "Browsers & mail" },
  { id: "desktop", label: "Desktop environment" },
  { id: "system", label: "System & security" },
];

const DOCS_BASE = "https://github.com/VuteTech/Bor/blob/master/docs";

const pretty = (v: unknown) => JSON.stringify(v, null, 2);

function parseObject(content: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(content || "{}");
    return parsed && typeof parsed === "object" && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

/** "At least one key" rule shared by the browser policy types. */
const requireAnyKey =
  (product: string) =>
  (content: string): string | null => {
    const parsed = parseObject(content);
    if (!parsed) return `${product} policy content is not valid JSON`;
    if (Object.keys(parsed).length === 0) {
      return `At least one ${product} policy setting must be selected and configured before saving`;
    }
    return null;
  };

/** "At least one element in `field`" rule for the rule/entry-based types. */
const requireNonEmptyList =
  (product: string, field: string, message: string) =>
  (content: string): string | null => {
    const parsed = parseObject(content);
    if (!parsed) return `${product} policy content is not valid JSON`;
    const list = parsed[field];
    if (!Array.isArray(list) || list.length === 0) return message;
    return null;
  };

function validateSessionAccess(content: string): string | null {
  type SaWin = { days?: string[]; start?: string; end?: string };
  type SaRule = { users?: string[]; groups?: string[]; windows?: SaWin[] };
  const parsed = parseObject(content) as { rules?: SaRule[] } | null;
  if (!parsed) return "Session access policy content is not valid JSON";
  if (!Array.isArray(parsed.rules) || parsed.rules.length === 0) return "Add at least one rule before saving";
  for (const [i, rule] of parsed.rules.entries()) {
    const targets = [...(rule.users ?? []), ...(rule.groups ?? [])].filter((t) => t.trim() !== "");
    if (targets.length === 0) return `Rule ${i + 1}: add at least one user or group`;
    const windows = rule.windows ?? [];
    if (windows.length === 0) return `Rule ${i + 1}: add at least one allowed period`;
    for (const [w, win] of windows.entries()) {
      if (!win.days || win.days.length === 0) return `Rule ${i + 1}, period ${w + 1}: select at least one day`;
      if (!win.start || !win.end) return `Rule ${i + 1}, period ${w + 1}: set both a start and an end time`;
    }
  }
  return null;
}

export const POLICY_TYPES: Record<PolicyTypeId, PolicyTypeDef> = {
  Firefox: {
    id: "Firefox",
    label: "Firefox",
    category: "browsers",
    tagline: "Browser policies: updates, privacy, security, restrictions",
    description:
      "Enterprise policies for Firefox ESR, delivered as policies.json. Control updates, privacy and telemetry, security settings, extensions and what users are allowed to change.",
    manages: [
      "/etc/firefox/policies/policies.json",
      "Flatpak Firefox system-config extension (org.mozilla.firefox.systemconfig)",
    ],
    appliesTo: ["Firefox ESR from distribution packages or Flatpak"],
    examples: [
      "Disable telemetry and Firefox Studies",
      "Pin the update channel and block manual updates",
      "Block or force-install extensions",
    ],
    Icon: FirefoxMarkIcon,
    defaultContent: () => "{}",
    validateContent: requireAnyKey("Firefox"),
  },
  Thunderbird: {
    id: "Thunderbird",
    label: "Thunderbird",
    category: "browsers",
    tagline: "Mail client policies: updates, privacy, security",
    description:
      "Enterprise policies for Thunderbird, using the same policies.json mechanism as Firefox. Control updates, privacy, security and which features users may change.",
    manages: [
      "/etc/thunderbird/policies/policies.json",
      "Flatpak Thunderbird system-config extension (net.thunderbird.Thunderbird.systemconfig)",
    ],
    appliesTo: ["Thunderbird from distribution packages or Flatpak"],
    examples: [
      "Disable telemetry and data reporting",
      "Lock the update channel",
      "Restrict add-on installation",
    ],
    Icon: ThunderbirdMarkIcon,
    defaultContent: () => "{}",
    validateContent: requireAnyKey("Thunderbird"),
  },
  Chrome: {
    id: "Chrome",
    label: "Chrome",
    technicalName: "Chrome and Chromium",
    category: "browsers",
    tagline: "Browser policies for Google Chrome and Chromium",
    description:
      "Managed policies for Google Chrome and Chromium on Linux. Control sign-in and sync, extensions, URL block and allow lists, updates, privacy and security.",
    manages: [
      "/etc/opt/chrome/policies/managed",
      "/etc/chromium/policies/managed and /etc/chromium-browser/policies/managed",
      "Flatpak Chromium system-policies extension",
    ],
    appliesTo: ["Google Chrome and Chromium from packages or Flatpak"],
    examples: [
      "Block URLs and set the home and start pages",
      "Force-install or block extensions",
      "Turn off password saving and browser sync",
    ],
    Icon: ChromeMarkIcon,
    defaultContent: () => "{}",
    validateContent: requireAnyKey("Chrome"),
  },
  Edge: {
    id: "Edge",
    label: "Microsoft Edge",
    category: "browsers",
    tagline: "Browser policies for Microsoft Edge on Linux",
    description:
      "Managed policies for Microsoft Edge on Linux. The catalogue contains the settings that apply to Linux; Windows-only groups are left out.",
    manages: ["/etc/opt/edge/policies/managed"],
    appliesTo: ["Microsoft Edge for Linux"],
    examples: [
      "Block URLs and set the home page",
      "Control extensions and sign-in",
      "Disable telemetry and personalisation",
    ],
    Icon: EdgeIcon,
    defaultContent: () => "{}",
    validateContent: requireAnyKey("Edge"),
  },
  Kconfig: {
    id: "Kconfig",
    label: "KDE Plasma",
    technicalName: "Kconfig",
    category: "desktop",
    tagline: "Kiosk restrictions, System Settings lockdown, look and feel",
    description:
      "KDE Plasma configuration and Kiosk restrictions written as immutable system-wide settings. Lock down actions and System Settings modules, restrict KIO URL schemes, and control appearance and screen locking.",
    manages: [
      "System-wide KDE configuration under /etc/bor/xdg (immutable [$i] entries)",
      "/etc/profile.d/99-bor.sh, which prepends that directory to XDG_CONFIG_DIRS",
    ],
    appliesTo: ["KDE Plasma 5 and 6"],
    examples: [
      "Hide System Settings modules from users",
      "Disable the run command and terminal access",
      "Enforce screen locking after inactivity",
    ],
    Icon: KdeMarkIcon,
    defaultContent: () => "{}",
    // Kconfig content is keyed by catalogue entries; "any key" is the structural
    // minimum here, the editor performs the catalogue-aware check before saving.
    validateContent: (content) => {
      const parsed = parseObject(content);
      if (!parsed) return "KConfig policy content is not valid JSON";
      if (Object.keys(parsed).length === 0) {
        return "At least one KDE Kiosk policy setting must be selected and configured before saving";
      }
      return null;
    },
  },
  Dconf: {
    id: "Dconf",
    label: "Dconf",
    category: "desktop",
    tagline: "Mandatory GSettings keys for GNOME and other dconf desktops",
    description:
      "System-wide dconf settings with optional locks, for GNOME and every other desktop or application that keeps its configuration in GSettings. Keys are picked from the schemas installed on the server.",
    manages: ["/etc/dconf/db/<database>.d/ keyfiles and locks", "dconf database update after each change"],
    appliesTo: ["GNOME and other GSettings-based desktops and applications"],
    examples: [
      "Set and lock the desktop background and lock screen",
      "Disable user switching or removable-media automount",
      "Pre-configure GNOME Shell favourites",
    ],
    docsHref: `${DOCS_BASE}/dconf.md`,
    Icon: SlidersHIcon,
    defaultContent: () => pretty({ entries: [], db_name: "local" }),
    validateContent: requireNonEmptyList(
      "Dconf",
      "entries",
      "At least one dconf entry must be configured before saving",
    ),
  },
  Polkit: {
    id: "Polkit",
    label: "Polkit",
    category: "system",
    tagline: "Who may run privileged actions",
    description:
      "PolicyKit authorization rules: decide which users and groups may run privileged actions such as mounting drives, managing services or installing packages. Rules are evaluated in order and the first match wins.",
    manages: ["/etc/polkit-1/rules.d/ JavaScript rules"],
    appliesTo: ["Any desktop that uses polkit (GNOME, KDE Plasma and most others)"],
    examples: [
      "Let a helpdesk group manage network connections",
      "Deny package installation for standard users",
      "Require administrator authentication for power actions",
    ],
    docsHref: `${DOCS_BASE}/polkit.md`,
    Icon: KeyIcon,
    defaultContent: () => pretty({ rules: [] }),
    validateContent: requireNonEmptyList(
      "Polkit",
      "rules",
      "At least one polkit rule must be configured before saving",
    ),
  },
  Firewalld: {
    id: "Firewalld",
    label: "Firewall",
    technicalName: "firewalld",
    category: "system",
    tagline: "Zone services, ports, rich rules and target",
    description:
      "Permanent firewalld zone configuration: allowed services and ports, rich rules and the zone target. Policies bound to the same node are merged into one zone definition.",
    manages: ["/etc/firewalld/zones/<zone>.xml", "firewalld reload after each change"],
    appliesTo: ["Nodes running firewalld"],
    examples: [
      "Allow SSH and a remote-support port",
      "Block everything except the corporate VPN",
      "Open a printer port only on the office zone",
    ],
    Icon: ShieldAltIcon,
    defaultContent: () => "{}",
    validateContent: (content) => {
      const parsed = parseObject(content);
      if (!parsed) return "Firewalld policy content is not valid JSON";
      if (Object.keys(parsed).length === 0) {
        return "Configure at least one firewall setting (service, port, or rich rule) before saving";
      }
      return null;
    },
  },
  SessionAccess: {
    id: "SessionAccess",
    label: "Session Access",
    category: "system",
    tagline: "When users and groups may use the desktop",
    description:
      "Weekly time windows per user or group. When a window ends the session is locked or logged off after warnings, and PAM (optionally SSH too) keeps the machine closed until the next allowed period.",
    manages: [
      "systemd-logind session lock and terminate",
      "/etc/security/time.conf (pam_time) and the PAM stack",
      "sshd, through PAM, when SSH enforcement is enabled",
    ],
    appliesTo: ["GNOME and KDE Plasma sessions; PAM for logins and SSH"],
    examples: [
      "Computer-lab hours for student accounts",
      "Lock shared kiosks outside opening hours",
      "Log contractors off at the end of their shift",
    ],
    docsHref: `${DOCS_BASE}/session-access.md`,
    Icon: UserClockIcon,
    defaultContent: () => pretty({ rules: [], enforcePam: true, includeSsh: false }),
    validateContent: validateSessionAccess,
  },
  Package: {
    id: "Package",
    label: "Package",
    category: "system",
    tagline: "Repositories and packages to install or remove",
    description:
      "Software repositories (deb822, PPA, COPR, .ymp) and the packages that must be installed or removed, applied through PackageKit on every supported distribution.",
    manages: [
      "/etc/apt/sources.list.d and /etc/apt/trusted.gpg.d",
      "/etc/yum.repos.d and /etc/pki/rpm-gpg",
      "/etc/zypp/repos.d",
      "Package install and removal through PackageKit",
    ],
    appliesTo: ["Debian, Ubuntu, Fedora, RHEL-family and openSUSE nodes"],
    examples: [
      "Install the corporate VPN client everywhere",
      "Remove games and other unwanted packages",
      "Add a vendor repository together with its signing key",
    ],
    Icon: CubesIcon,
    defaultContent: () =>
      pretty({ repositories: [], packages: [], updateCache: true, allowDowngrade: false }),
    validateContent: (content) => {
      const parsed = parseObject(content) as { repositories?: unknown[]; packages?: unknown[] } | null;
      if (!parsed) return "Package policy content is not valid JSON";
      const repos = Array.isArray(parsed.repositories) ? parsed.repositories.length : 0;
      const pkgs = Array.isArray(parsed.packages) ? parsed.packages.length : 0;
      if (repos + pkgs === 0) return "Add at least one repository or package before saving";
      return null;
    },
  },
  Flatpak: {
    id: "Flatpak",
    label: "Flatpak apps",
    technicalName: "flatpak",
    category: "system",
    tagline: "Remotes, apps and update settings for Flatpak",
    description:
      "Flatpak remotes to configure on each node (Flathub or your own repositories, with subsets and allow/deny filters) and the applications that must be present, kept up to date or removed. Apps are picked from the server-indexed catalog.",
    manages: [
      "Flatpak remotes in the system installation",
      "/etc/bor/flatpak/*.flatpakrepo, *.filter and *.gpg",
      "Installed Flatpak applications and periodic updates",
    ],
    appliesTo: ["Nodes with flatpak installed (inapplicable elsewhere)"],
    examples: [
      "Install Firefox and LibreOffice from Flathub everywhere",
      "Allow only verified Flathub apps on shared desktops",
      "Add an internal remote and keep its apps updated nightly",
    ],
    docsHref: `${DOCS_BASE}/flatpak.md`,
    Icon: FlatpakMarkIcon,
    defaultContent: () => pretty({ remotes: [], apps: [], autoUpdate: false, uninstallUnused: false }),
    validateContent: (content) => {
      const parsed = parseObject(content) as { remotes?: unknown[]; apps?: unknown[] } | null;
      if (!parsed) return "Flatpak policy content is not valid JSON";
      const remotes = Array.isArray(parsed.remotes) ? parsed.remotes.length : 0;
      const apps = Array.isArray(parsed.apps) ? parsed.apps.length : 0;
      if (remotes + apps === 0) return "Add at least one remote or application before saving";
      return null;
    },
  },
};

/** Display order: by category, then the order used across the UI. */
export const POLICY_TYPE_ORDER: PolicyTypeId[] = [
  "Firefox",
  "Thunderbird",
  "Chrome",
  "Edge",
  "Kconfig",
  "Dconf",
  "Polkit",
  "Firewalld",
  "SessionAccess",
  "Package",
  "Flatpak",
];

export const POLICY_TYPE_LIST: PolicyTypeDef[] = POLICY_TYPE_ORDER.map((id) => POLICY_TYPES[id]);

/** Icon for a type the registry does not know (forward compatibility). */
export const UNKNOWN_POLICY_TYPE_ICON: ComponentType<SVGIconProps> = CogIcon;

export function isPolicyTypeId(value: string): value is PolicyTypeId {
  return Object.prototype.hasOwnProperty.call(POLICY_TYPES, value);
}

export function getPolicyType(id: string): PolicyTypeDef | undefined {
  return isPolicyTypeId(id) ? POLICY_TYPES[id] : undefined;
}

/** Label for any type string; unknown types fall back to the raw identifier. */
export function policyTypeLabel(id: string): string {
  return getPolicyType(id)?.label ?? id;
}

/** Types grouped for the tile grid, in category order. */
export function policyTypesByCategory(): { category: { id: PolicyTypeCategory; label: string }; types: PolicyTypeDef[] }[] {
  return POLICY_TYPE_CATEGORIES.map((category) => ({
    category,
    types: POLICY_TYPE_LIST.filter((t) => t.category === category.id),
  })).filter((g) => g.types.length > 0);
}

/**
 * Empty content for a type. Unknown types get a generic single-entry list so
 * the legacy structured form still has something to render.
 */
export function defaultContentForType(type: string): string {
  return getPolicyType(type)?.defaultContent() ?? pretty([{}]);
}

/** Save-time structural check; unknown types only need valid JSON. */
export function validatePolicyContent(type: string, content: string): string | null {
  const def = getPolicyType(type);
  if (def) return def.validateContent(content);
  try {
    JSON.parse(content);
    return null;
  } catch {
    return "Policy content must be valid JSON";
  }
}
