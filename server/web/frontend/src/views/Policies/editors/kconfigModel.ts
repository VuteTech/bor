// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * KDE Plasma (Kconfig) policy model: the hand-written catalogue of Kiosk
 * settings, the KIO protocol and KCM module tables, and the pure content
 * helpers used by `KconfigPolicyEditor` and the settings summary.
 * (The Kconfig proto carries no UI annotations, so unlike the browser types
 * this catalogue is not generated.)
 *
 * Kiosk restriction booleans follow KDE's convention: `false` restricts.
 * Defs with `inverted: true` present that as a "Restricted / Allowed" switch
 * and store `false` when restricted, so the stored content stays what the
 * agent and KDE expect.
 */

export interface KConfigPolicyDef {
  key: string;  // camelCase JSON field name (matches protojson output)
  label: string;
  group: string;
  type: "boolean" | "string" | "select" | "int" | "color" | "url-restrictions" | "kcm-restrictions";
  selectOptions?: string[];
  defaultValue?: string;
  /** KDE semantics: false restricts. Shown as a Restricted / Allowed switch. */
  inverted?: boolean;
  /** Switch labels for booleans. Defaults: inverted → Restricted / Allowed, else Enabled / Disabled. */
  onLabel?: string;
  offLabel?: string;
  /** Where the agent writes the setting, as "file · [Group] · key". */
  target?: string;
  /** One or two sentences shown under the title. */
  help?: string;
  /** Setting with no effect on Plasma 5/6; the editor only offers to remove it. */
  legacy?: boolean;
}

/* ── URL Restriction rule model (KDE Kiosk) ── */

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

const ACTIONS = "kdeglobals · [KDE Action Restrictions]";
const LOCKER = "kscreenlockerrc · [Daemon]";

export const KCONFIG_ALL_POLICIES: KConfigPolicyDef[] = [
  // Session & shell
  {
    key: "shellAccess", label: "Restrict shell access", group: "Session & Shell", type: "boolean", inverted: true,
    target: `${ACTIONS} · shell_access`,
    help: "Removes terminal access and arbitrary command execution (KRunner, Dolphin's Open Terminal) and disables the user's autostart folder.",
  },
  {
    key: "runCommand", label: "Restrict KRunner command execution", group: "Session & Shell", type: "boolean", inverted: true,
    target: `${ACTIONS} · run_command, action/run_command`,
    help: "Disables KRunner (Alt+F2 / Alt+Space) and hides the Run Command entry from the desktop menu.",
  },
  {
    key: "actionLogout", label: "Restrict logout, shutdown and restart", group: "Session & Shell", type: "boolean", inverted: true,
    target: `${ACTIONS} · logout, action/logout`,
    help: "Removes the leave actions from the launcher and the desktop menu. Combine with Lock desktop layout for kiosk desktops.",
  },
  {
    key: "plasmoidUnlockedDesktop", label: "Lock desktop layout", group: "Session & Shell", type: "boolean", inverted: true,
    onLabel: "Locked", offLabel: "Unlocked",
    target: `${ACTIONS} · plasma/plasmashell/unlockedDesktop`,
    help: "Users cannot move, add or remove panels and widgets or edit launcher favourites (Plasma 5 and 6).",
  },
  {
    key: "allowConfigureWhenLocked", label: "Restrict configuring widgets while locked (Plasma 5)", group: "Session & Shell", type: "boolean", inverted: true,
    target: `${ACTIONS} · plasma/allow_configure_when_locked`,
    help: "Documented Kiosk key for Plasma 5. Plasma 6 does not read it; use Lock desktop layout there.",
  },
  // Application menu actions
  {
    key: "actionFileNew", label: "Restrict File › New", group: "Application Menu Actions", type: "boolean", inverted: true,
    target: `${ACTIONS} · action/file_new`, help: "Applies to every KDE application that uses the standard action.",
  },
  {
    key: "actionFileOpen", label: "Restrict File › Open", group: "Application Menu Actions", type: "boolean", inverted: true,
    target: `${ACTIONS} · action/file_open`, help: "Applies to every KDE application that uses the standard action.",
  },
  {
    key: "actionFileSave", label: "Restrict File › Save", group: "Application Menu Actions", type: "boolean", inverted: true,
    target: `${ACTIONS} · action/file_save`, help: "Applies to every KDE application that uses the standard action.",
  },
  // System Settings Restrictions
  { key: "kcmRestrictions", label: "System Settings Modules", group: "System Settings Restrictions", type: "kcm-restrictions" },
  // Window Manager
  {
    key: "borderlessMaximizedWindows", label: "Borderless maximized windows", group: "Window Manager", type: "boolean",
    target: "kwinrc · [Windows] · BorderlessMaximizedWindows",
  },
  // Screen Lock
  {
    key: "autoLock", label: "Lock automatically after inactivity", group: "Screen Lock", type: "boolean",
    target: `${LOCKER} · Autolock`,
  },
  {
    key: "lockTimeout", label: "Inactivity timeout (minutes)", group: "Screen Lock", type: "int", defaultValue: "5",
    target: `${LOCKER} · Timeout`, help: "Minutes of inactivity before the screen locks. KDE's default is 5.",
  },
  {
    key: "lockGrace", label: "Grace period before a password is required (seconds)", group: "Screen Lock", type: "int", defaultValue: "5",
    target: `${LOCKER} · LockGrace`, help: "Seconds after locking during which the screen unlocks without a password. 0 asks for the password immediately.",
  },
  {
    key: "lockOnResume", label: "Lock on resume from sleep", group: "Screen Lock", type: "boolean",
    target: `${LOCKER} · LockOnResume`,
  },
  {
    key: "lockOnStart", label: "Lock the screen immediately at login", group: "Screen Lock", type: "boolean",
    target: `${LOCKER} · LockOnStart`,
  },
  {
    key: "lockScreenImage", label: "Lock screen wallpaper image path", group: "Screen Lock", type: "string",
    target: "kscreenlockerrc · [Greeter][Wallpaper][org.kde.image][General] · Image",
    help: "Absolute path readable by every user, for example /usr/share/backgrounds/corporate-lock.png. Cascades reliably from system configuration.",
  },
  // Appearance
  { key: "iconTheme", label: "Icon theme", group: "Appearance", type: "string", target: "kdeglobals · [Icons] · Theme" },
  {
    key: "wallpaperPlugin", label: "Wallpaper plugin", group: "Appearance", type: "string", defaultValue: "org.kde.image",
    target: "plasma-org.kde.plasma.desktop-appletsrc · [Containments][1] · wallpaperplugin",
  },
  {
    key: "wallpaperImage", label: "Wallpaper image path", group: "Appearance", type: "string",
    target: "plasma-org.kde.plasma.desktop-appletsrc · [Containments][1][Wallpaper][org.kde.image][General] · Image",
    help: "Writes containment 1 of the desktop layout file, which does not match every desktop. Not reliable; prefer the lock screen wallpaper until the desktop layout feature ships.",
  },
  {
    key: "wallpaperFillMode", label: "Wallpaper fill mode", group: "Appearance", type: "select", selectOptions: ["0", "1", "2", "3", "6"], defaultValue: "2",
    target: "plasma-org.kde.plasma.desktop-appletsrc · [Containments][1][Wallpaper][org.kde.image][General] · FillMode",
  },
  {
    key: "wallpaperColor", label: "Wallpaper background color", group: "Appearance", type: "color",
    target: "plasma-org.kde.plasma.desktop-appletsrc · [Containments][1][Wallpaper][org.kde.image][General] · Color",
  },
  // Security
  { key: "urlRestrictions", label: "URL Restrictions", group: "Security", type: "url-restrictions" },
];

/**
 * Settings older policies may still contain. KDE dropped
 * [KDE Resource Restrictions] with Plasma 5 (it was a KDE 4 KStandardDirs
 * feature), so the agent ignores them; the editor only offers removal.
 */
const LEGACY_GROUP = "Legacy (no effect)";
const LEGACY_HELP =
  "KDE Plasma 5 and 6 do not read [KDE Resource Restrictions]; the agent ignores this setting and it should be removed from the policy. To stop wallpaper or theme changes, enforce the Appearance settings or lock the desktop layout instead.";

export const KCONFIG_LEGACY_POLICIES: KConfigPolicyDef[] = [
  { key: "restrictWallpaper", label: "Wallpaper Changes", group: LEGACY_GROUP, type: "boolean", legacy: true, help: LEGACY_HELP },
  { key: "restrictIcons", label: "Icon Changes", group: LEGACY_GROUP, type: "boolean", legacy: true, help: LEGACY_HELP },
  { key: "restrictAutostart", label: "Autostart Changes", group: LEGACY_GROUP, type: "boolean", legacy: true, help: LEGACY_HELP },
  { key: "restrictColors", label: "Color Scheme Changes", group: LEGACY_GROUP, type: "boolean", legacy: true, help: LEGACY_HELP },
  { key: "restrictCursors", label: "Cursor Theme Changes", group: LEGACY_GROUP, type: "boolean", legacy: true, help: LEGACY_HELP },
];

/** Looks a def up by its content key, including legacy defs. */
export function findKConfigDef(key: string): KConfigPolicyDef | undefined {
  return KCONFIG_ALL_POLICIES.find((p) => p.key === key) ?? KCONFIG_LEGACY_POLICIES.find((p) => p.key === key);
}

/** Value a freshly selected setting previews with: restrictions start restricted. */
export function defaultKConfigValue(def: KConfigPolicyDef): string {
  if (def.type === "boolean") return def.inverted ? "false" : "true";
  if (def.defaultValue !== undefined) return def.defaultValue;
  return def.type === "int" ? "0" : "";
}

/** Human-readable state of a boolean setting's stored value. */
export function kconfigBoolDisplay(def: KConfigPolicyDef, value: string): string {
  if (def.inverted) {
    return value === "false" ? (def.onLabel ?? "Restricted") : (def.offLabel ?? "Allowed");
  }
  return value === "true" ? (def.onLabel ?? "Enabled") : (def.offLabel ?? "Disabled");
}

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
  const parts = rgb.split(",").map(s => parseInt(s.trim(), 10));
  if (parts.length !== 3 || parts.some(isNaN)) return "#000000";
  return "#" + parts.map(v => Math.max(0, Math.min(255, v)).toString(16).padStart(2, "0")).join("");
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

// FillMode display labels for the select dropdown.
export const FILL_MODE_LABELS: Record<string, string> = {
  "0": "0 — Stretch",
  "1": "1 — Preserve Aspect Fit",
  "2": "2 — Preserve Aspect Crop",
  "3": "3 — Tile",
  "6": "6 — Pad",
};

/**
 * Tree groups for the editor: every current setting, plus the legacy group
 * when the policy content still carries a legacy key (so it can be removed).
 */
export function buildKConfigTree(configuredKeys: string[] = []): Map<string, KConfigPolicyDef[]> {
  const groups = new Map<string, KConfigPolicyDef[]>();
  for (const p of KCONFIG_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  const legacy = KCONFIG_LEGACY_POLICIES.filter((p) => configuredKeys.includes(p.key));
  if (legacy.length > 0) groups.set(LEGACY_GROUP, legacy);
  return groups;
}

// Detect which KConfig policy def keys are configured in the content JSON.
// Returns the camelCase JSON key for each present field.
export function detectKConfigConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    const result: string[] = [];
    for (const def of [...KCONFIG_ALL_POLICIES, ...KCONFIG_LEGACY_POLICIES]) {
      if (def.type === "url-restrictions") {
        if (Array.isArray(parsed.urlRestrictions) && (parsed.urlRestrictions as unknown[]).length > 0) {
          result.push("urlRestrictions");
        }
        continue;
      }
      if (def.type === "kcm-restrictions") {
        if (Array.isArray(parsed.kcmRestrictions) && (parsed.kcmRestrictions as unknown[]).length > 0) {
          result.push("kcmRestrictions");
        }
        continue;
      }
      if (def.key in parsed && parsed[def.key] !== null && parsed[def.key] !== undefined) {
        result.push(def.key);
      }
    }
    return result;
  } catch { return []; }
}

// Extract the value + enforced state for a KConfig policy by its camelCase key.
// Returns the value as a string (booleans → "true"/"false", numbers → string).
export function extractKConfigEntry(content: string, defKey: string): { value: string; enforced: boolean } | undefined {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    if (!(defKey in parsed) || parsed[defKey] === null || parsed[defKey] === undefined) return undefined;
    const rawVal = parsed[defKey];
    const value = typeof rawVal === "boolean" ? (rawVal ? "true" : "false") : String(rawVal);
    const enforced = Array.isArray(parsed.enforcedFields) && (parsed.enforcedFields as string[]).includes(defKey);
    return { value, enforced };
  } catch { return undefined; }
}

// Build KConfig content JSON by setting/updating a single field.
// Booleans and ints are stored as their native JSON types; strings as strings.
export function buildKConfigContent(policyDef: KConfigPolicyDef, value: string, enforced: boolean, existingContent?: string): string {
  const parsed: Record<string, unknown> = {};
  try { Object.assign(parsed, JSON.parse(existingContent || "{}")); } catch { /* ignore */ }

  if (policyDef.type === "boolean") {
    parsed[policyDef.key] = value === "true";
  } else if (policyDef.type === "int") {
    const n = parseInt(value, 10);
    parsed[policyDef.key] = isNaN(n) ? 0 : n;
  } else {
    parsed[policyDef.key] = value;
  }

  let enforcedFields: string[] = Array.isArray(parsed.enforcedFields) ? (parsed.enforcedFields as string[]) : [];
  if (enforced) {
    if (!enforcedFields.includes(policyDef.key)) enforcedFields = [...enforcedFields, policyDef.key];
  } else {
    enforcedFields = enforcedFields.filter(f => f !== policyDef.key);
  }
  if (enforcedFields.length > 0) {
    parsed.enforcedFields = enforcedFields;
  } else {
    delete parsed.enforcedFields;
  }

  return JSON.stringify(parsed, null, 2);
}

// Remove a KConfig field from the content JSON by its camelCase key.
export function removeKConfigContentKey(defKey: string, existingContent: string): string {
  const parsed: Record<string, unknown> = {};
  try { Object.assign(parsed, JSON.parse(existingContent || "{}")); } catch { /* ignore */ }

  delete parsed[defKey];

  if (Array.isArray(parsed.enforcedFields)) {
    const filtered = (parsed.enforcedFields as string[]).filter(f => f !== defKey);
    if (filtered.length > 0) { parsed.enforcedFields = filtered; } else { delete parsed.enforcedFields; }
  }

  return JSON.stringify(parsed, null, 2);
}

// Parse URL restriction rules from the KConfig content JSON.
export function parseUrlRestrictionRules(content: string): UrlRestrictionRule[] {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    if (!Array.isArray(parsed.urlRestrictions)) return [];
    return (parsed.urlRestrictions as Record<string, unknown>[]).map(r => ({
      action: (r.action as UrlRestrictionRule["action"]) || "open",
      referrerProtocol: (r.referrerProtocol as string) || "",
      referrerHost: (r.referrerHost as string) || "",
      referrerPath: (r.referrerPath as string) || "",
      protocol: (r.protocol as string) || "",
      host: (r.host as string) || "",
      path: (r.path as string) || "",
      enabled: r.enabled === true,
    }));
  } catch { return []; }
}

// Build URL restriction content into the KConfig JSON (replaces urlRestrictions array).
export function buildUrlRestrictionContent(rules: UrlRestrictionRule[], existingContent: string): string {
  const parsed: Record<string, unknown> = {};
  try { Object.assign(parsed, JSON.parse(existingContent || "{}")); } catch { /* ignore */ }

  if (rules.length > 0) {
    parsed.urlRestrictions = rules.map(r => ({
      action: r.action,
      referrerProtocol: r.referrerProtocol,
      referrerHost: r.referrerHost,
      referrerPath: r.referrerPath,
      protocol: r.protocol,
      host: r.host,
      path: r.path,
      enabled: r.enabled,
    }));
  } else {
    delete parsed.urlRestrictions;
  }

  return JSON.stringify(parsed, null, 2);
}

// Parse KCM restriction module IDs from the KConfig content JSON.
export function parseKcmRestrictions(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    if (Array.isArray(parsed.kcmRestrictions)) return parsed.kcmRestrictions as string[];
    return [];
  } catch { return []; }
}

// Build KCM restriction content into the KConfig JSON (replaces kcmRestrictions array).
export function buildKcmRestrictionContent(modules: string[], existingContent: string): string {
  const parsed: Record<string, unknown> = {};
  try { Object.assign(parsed, JSON.parse(existingContent || "{}")); } catch { /* ignore */ }

  if (modules.length > 0) {
    parsed.kcmRestrictions = modules;
  } else {
    delete parsed.kcmRestrictions;
  }

  return JSON.stringify(parsed, null, 2);
}
