// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useRef } from "react";
import {
  Modal,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalVariant,
  Button,
  Tabs,
  Tab,
  TabTitleText,
  Form,
  FormGroup,
  TextInput,
  TextArea,
  FormSelect,
  FormSelectOption,
  DescriptionList,
  DescriptionListGroup,
  DescriptionListTerm,
  DescriptionListDescription,
  Alert,
  Split,
  SplitItem,
  Label,
  CodeBlock,
  CodeBlockCode,
  Title,
  Switch,
  Flex,
  FlexItem,
  Card,
  CardBody,
  CardTitle,
} from "@patternfly/react-core";
import { Table, Thead, Tbody, Tr, Th, Td } from "@patternfly/react-table";

import type { Policy, CreatePolicyRequest, UpdatePolicyRequest } from "../../apiClient/policiesApi";
import { createPolicy, updatePolicy, setPolicyState, deletePolicy } from "../../apiClient/policiesApi";
import type { PolicyFieldDef } from "../../generated/proto/policy_ui_types";
import { FIREFOX_ALL_POLICIES } from "../../generated/proto/firefox_ui";
import { THUNDERBIRD_ALL_POLICIES } from "../../generated/proto/thunderbird_ui";
import { CHROME_ALL_POLICIES } from "../../generated/proto/chrome_ui";
import { EDGE_ALL_POLICIES } from "../../generated/proto/edge_ui";
import { DConfPolicyEditor } from "./DConfPolicyEditor";
import { PackagePolicyEditor } from "./PackagePolicyEditor";
import { PolkitPolicyEditor } from "./PolkitPolicyEditor";
import { FirewalldPolicyEditor } from "./FirewalldPolicyEditor";

/* ── Known policy types and their config schemas ── */

const POLICY_TYPES: { value: string; label: string; isDisabled?: boolean }[] = [
  { value: "Kconfig", label: "Kconfig" },
  { value: "Dconf", label: "Dconf" },
  { value: "Firefox", label: "Firefox" },
  { value: "Thunderbird", label: "Thunderbird" },
  { value: "Polkit", label: "Polkit" },
  { value: "Chrome", label: "Chrome" },
  { value: "Edge", label: "Edge" },
  { value: "Package", label: "Package" },
  { value: "Firewalld", label: "Firewall (firewalld)" },
];

interface PolicyTypeConfig {
  label: string;
  fields: { key: string; label: string; type: "text" | "textarea" | "checkbox" | "array" }[];
}

const TYPE_CONFIGS: Record<string, PolicyTypeConfig> = {
  Dconf: {
    label: "Desktop Configuration",
    fields: [
      { key: "schema", label: "Schema Path", type: "text" },
      { key: "key", label: "Key", type: "text" },
      { key: "value", label: "Value", type: "text" },
      { key: "lock", label: "Lock Setting", type: "checkbox" },
    ],
  },
  Polkit: {
    label: "PolicyKit Authorization",
    fields: [
      { key: "action_id", label: "Action ID", type: "text" },
      { key: "result_any", label: "Result (Any)", type: "text" },
      { key: "result_active", label: "Result (Active)", type: "text" },
      { key: "result_inactive", label: "Result (Inactive)", type: "text" },
    ],
  },
};

/* ── Firefox policy model (matches Protobuf schema) ── */

// Build category groups for the tree view
function buildFirefoxTree(): Map<string, PolicyFieldDef[]> {
  const groups = new Map<string, PolicyFieldDef[]>();
  for (const p of FIREFOX_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

/* ── Thunderbird policy model (matches Protobuf schema) ── */

// Build category groups for the Thunderbird tree view.
function buildThunderbirdTree(): Map<string, PolicyFieldDef[]> {
  const groups = new Map<string, PolicyFieldDef[]>();
  for (const p of THUNDERBIRD_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

/* ── Chrome/Chromium policy model ── */

function buildChromeTree(): Map<string, PolicyFieldDef[]> {
  const groups = new Map<string, PolicyFieldDef[]>();
  for (const p of CHROME_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

// Detect which Chrome policy keys are configured in the content JSON
function detectChromeConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    return CHROME_ALL_POLICIES.filter(p => p.key in parsed).map(p => p.key);
  } catch { return []; }
}

// Extract the value for a specific Chrome policy key from content JSON
function extractChromeValue(content: string, key: string): unknown {
  try {
    const parsed = JSON.parse(content || "{}");
    return parsed[key];
  } catch { return undefined; }
}

// Update or add a single key in the Chrome content JSON, preserving other keys
function buildChromeContent(key: string, value: unknown, existingContent?: string): string {
  let parsed: Record<string, unknown> = {};
  try { parsed = JSON.parse(existingContent || "{}"); } catch { /* ignore */ }
  parsed[key] = value ?? null;
  return JSON.stringify(parsed, null, 2);
}

// Remove a single key from the Chrome content JSON
function removeChromeContentKey(key: string, existingContent: string): string {
  let parsed: Record<string, unknown> = {};
  try { parsed = JSON.parse(existingContent || "{}"); } catch { /* ignore */ }
  delete parsed[key];
  return JSON.stringify(parsed, null, 2);
}

/* ── Microsoft Edge policy model ── */

function buildEdgeTree(): Map<string, PolicyFieldDef[]> {
  const groups = new Map<string, PolicyFieldDef[]>();
  for (const p of EDGE_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

function detectEdgeConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    return EDGE_ALL_POLICIES.filter(p => p.key in parsed).map(p => p.key);
  } catch { return []; }
}

function extractEdgeValue(content: string, key: string): unknown {
  try {
    const parsed = JSON.parse(content || "{}");
    return parsed[key];
  } catch { return undefined; }
}

function buildEdgeContent(key: string, value: unknown, existingContent?: string): string {
  let parsed: Record<string, unknown> = {};
  try { parsed = JSON.parse(existingContent || "{}"); } catch { /* ignore */ }
  parsed[key] = value ?? null;
  return JSON.stringify(parsed, null, 2);
}

function removeEdgeContentKey(key: string, existingContent: string): string {
  let parsed: Record<string, unknown> = {};
  try { parsed = JSON.parse(existingContent || "{}"); } catch { /* ignore */ }
  delete parsed[key];
  return JSON.stringify(parsed, null, 2);
}

/* ── KDE Kiosk (KConfig) policy model ── */

interface KConfigPolicyDef {
  key: string;  // camelCase JSON field name (matches protojson output)
  label: string;
  group: string;
  type: "boolean" | "string" | "select" | "int" | "color" | "url-restrictions" | "kcm-restrictions";
  selectOptions?: string[];
  defaultValue?: string;
}

/* ── URL Restriction rule model (KDE Kiosk) ── */

interface UrlRestrictionRule {
  action: "open" | "list" | "redirect";
  referrerProtocol: string;
  referrerHost: string;
  referrerPath: string;
  protocol: string;
  host: string;
  path: string;
  enabled: boolean;
}

const KCONFIG_ALL_POLICIES: KConfigPolicyDef[] = [
  // Action Restrictions
  { key: "shellAccess", label: "Shell Access", group: "Action Restrictions", type: "boolean" },
  { key: "runCommand", label: "Run Command (KRunner)", group: "Action Restrictions", type: "boolean" },
  { key: "actionLogout", label: "Logout Action", group: "Action Restrictions", type: "boolean" },
  { key: "actionFileNew", label: "File New Action", group: "Action Restrictions", type: "boolean" },
  { key: "actionFileOpen", label: "File Open Action", group: "Action Restrictions", type: "boolean" },
  { key: "actionFileSave", label: "File Save Action", group: "Action Restrictions", type: "boolean" },
  // System Settings Restrictions
  { key: "kcmRestrictions", label: "System Settings Modules", group: "System Settings Restrictions", type: "kcm-restrictions" },
  // Resource Restrictions
  { key: "restrictWallpaper", label: "Wallpaper Changes", group: "Resource Restrictions", type: "boolean" },
  { key: "restrictIcons", label: "Icon Changes", group: "Resource Restrictions", type: "boolean" },
  { key: "restrictAutostart", label: "Autostart Changes", group: "Resource Restrictions", type: "boolean" },
  { key: "restrictColors", label: "Color Scheme Changes", group: "Resource Restrictions", type: "boolean" },
  { key: "restrictCursors", label: "Cursor Theme Changes", group: "Resource Restrictions", type: "boolean" },
  // Window Manager
  { key: "borderlessMaximizedWindows", label: "Borderless Maximized Windows", group: "Window Manager", type: "boolean" },
  // Desktop
  { key: "plasmoidUnlockedDesktop", label: "Unlock Desktop Widgets", group: "Desktop", type: "boolean" },
  { key: "allowConfigureWhenLocked", label: "Configure When Locked", group: "Desktop", type: "boolean" },
  // Screen Lock
  { key: "autoLock", label: "Auto Lock", group: "Screen Lock", type: "boolean" },
  { key: "lockOnResume", label: "Lock on Resume", group: "Screen Lock", type: "boolean" },
  { key: "lockTimeout", label: "Lock Timeout (seconds)", group: "Screen Lock", type: "int" },
  // Appearance
  { key: "iconTheme", label: "Icon Theme", group: "Appearance", type: "string" },
  { key: "wallpaperPlugin", label: "Wallpaper Plugin", group: "Appearance", type: "string", defaultValue: "org.kde.image" },
  { key: "wallpaperImage", label: "Wallpaper Image Path", group: "Appearance", type: "string" },
  { key: "wallpaperFillMode", label: "Wallpaper Fill Mode", group: "Appearance", type: "select", selectOptions: ["0", "1", "2", "3", "6"], defaultValue: "2" },
  { key: "wallpaperColor", label: "Wallpaper Background Color", group: "Appearance", type: "color" },
  // Security
  { key: "urlRestrictions", label: "URL Restrictions", group: "Security", type: "url-restrictions" },
];

// KIO protocols available for URL restriction rules.
const KIO_PROTOCOLS = [
  "bzip", "bzip2", "cifs", "dav", "davs", "file", "fish", "ftp", "gdrive",
  "gopher", "gzip", "help", "http", "https", "info", "ldap", "ldaps", "lzma",
  "man", "nfs", "recentlyused", "sftp", "smb", "tar", "thumbnail", "webdav",
  "webdavs", "xz", "zstd",
];

// KDE Control Modules (KCMs) available for System Settings restrictions.
// Sorted alphabetically by module ID. Label is the human-readable description.
const KCM_MODULES: { id: string; label: string }[] = [
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

// Set for quick lookup of known KCM module IDs.
const KCM_MODULE_IDS = new Set(KCM_MODULES.map(m => m.id));

// Convert KDE "R,G,B" color string to hex "#rrggbb".
function rgbToHex(rgb: string): string {
  const parts = rgb.split(",").map(s => parseInt(s.trim(), 10));
  if (parts.length !== 3 || parts.some(isNaN)) return "#000000";
  return "#" + parts.map(v => Math.max(0, Math.min(255, v)).toString(16).padStart(2, "0")).join("");
}

// Convert hex "#rrggbb" to KDE "R,G,B" color string.
function hexToRgb(hex: string): string {
  const m = hex.replace("#", "");
  if (m.length !== 6) return "0,0,0";
  const r = parseInt(m.substring(0, 2), 16);
  const g = parseInt(m.substring(2, 4), 16);
  const b = parseInt(m.substring(4, 6), 16);
  return `${r},${g},${b}`;
}

// FillMode display labels for the select dropdown.
const FILL_MODE_LABELS: Record<string, string> = {
  "0": "0 — Stretch",
  "1": "1 — Preserve Aspect Fit",
  "2": "2 — Preserve Aspect Crop",
  "3": "3 — Tile",
  "6": "6 — Pad",
};

function buildKConfigTree(): Map<string, KConfigPolicyDef[]> {
  const groups = new Map<string, KConfigPolicyDef[]>();
  for (const p of KCONFIG_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

// Detect which KConfig policy def keys are configured in the content JSON.
// Returns the camelCase JSON key for each present field.
function detectKConfigConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    const result: string[] = [];
    for (const def of KCONFIG_ALL_POLICIES) {
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
function extractKConfigEntry(content: string, defKey: string): { value: string; enforced: boolean } | undefined {
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
function buildKConfigContent(policyDef: KConfigPolicyDef, value: string, enforced: boolean, existingContent?: string): string {
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
function removeKConfigContentKey(defKey: string, existingContent: string): string {
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
function parseUrlRestrictionRules(content: string): UrlRestrictionRule[] {
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
function buildUrlRestrictionContent(rules: UrlRestrictionRule[], existingContent: string): string {
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
function parseKcmRestrictions(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}") as Record<string, unknown>;
    if (Array.isArray(parsed.kcmRestrictions)) return parsed.kcmRestrictions as string[];
    return [];
  } catch { return []; }
}

// Build KCM restriction content into the KConfig JSON (replaces kcmRestrictions array).
function buildKcmRestrictionContent(modules: string[], existingContent: string): string {
  const parsed: Record<string, unknown> = {};
  try { Object.assign(parsed, JSON.parse(existingContent || "{}")); } catch { /* ignore */ }

  if (modules.length > 0) {
    parsed.kcmRestrictions = modules;
  } else {
    delete parsed.kcmRestrictions;
  }

  return JSON.stringify(parsed, null, 2);
}

/* ── Overview summary helpers ── */

interface SettingsRow {
  setting: string;
  value: string;
  locked: string | null; // null = not applicable for this row
}

function formatDisplayValue(val: unknown): string {
  if (val === undefined || val === null) return "—";
  if (typeof val === "boolean") return val ? "Yes" : "No";
  if (Array.isArray(val)) return val.length > 0 ? val.join(", ") : "(empty)";
  if (typeof val === "object") return JSON.stringify(val);
  return String(val);
}

function buildSettingsRows(policyType: string, content: string): SettingsRow[] {
  let raw: unknown;
  try {
    raw = JSON.parse(content || "{}");
  } catch {
    return [];
  }

  const rows: SettingsRow[] = [];

  if (policyType === "Firefox") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of FIREFOX_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];

      if (policyDef.type === "object" && typeof val === "object" && val !== null) {
        const objVal = val as Record<string, unknown>;
        const lockedVal = "Locked" in objVal
          ? (objVal["Locked"] === true ? "Yes" : "No")
          : null;
        for (const field of policyDef.subFields || []) {
          if (field.key === "Locked") continue;
          rows.push({
            setting: `${policyDef.label} › ${field.label}`,
            value: formatDisplayValue(objVal[field.key]),
            locked: lockedVal,
          });
        }
      } else {
        rows.push({
          setting: policyDef.label,
          value: formatDisplayValue(val),
          locked: null,
        });
      }
    }
    return rows;
  }

  if (policyType === "Thunderbird") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of THUNDERBIRD_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];

      if (policyDef.type === "object" && typeof val === "object" && val !== null) {
        const objVal = val as Record<string, unknown>;
        const lockedVal = "Locked" in objVal
          ? (objVal["Locked"] === true ? "Yes" : "No")
          : null;
        for (const field of policyDef.subFields || []) {
          if (field.key === "Locked") continue;
          rows.push({
            setting: `${policyDef.label} › ${field.label}`,
            value: formatDisplayValue(objVal[field.key]),
            locked: lockedVal,
          });
        }
      } else {
        rows.push({
          setting: policyDef.label,
          value: formatDisplayValue(val),
          locked: null,
        });
      }
    }
    return rows;
  }

  if (policyType === "Chrome") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of CHROME_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];
      let displayVal: string;
      if (policyDef.type === "list" && Array.isArray(val)) {
        displayVal = val.length > 0 ? val.join(", ") : "(empty)";
      } else if (policyDef.type === "integer-enum" && policyDef.intOptions) {
        const opt = policyDef.intOptions.find(o => o.value === val);
        displayVal = opt ? opt.label : formatDisplayValue(val);
      } else if (policyDef.type === "string-enum" && policyDef.stringOptions) {
        const opt = policyDef.stringOptions.find(o => o === val);
        displayVal = opt ? opt : formatDisplayValue(val);
      } else if (policyDef.type === "json") {
        displayVal = typeof val === "string" ? val : JSON.stringify(val);
      } else {
        displayVal = formatDisplayValue(val);
      }
      rows.push({
        setting: policyDef.label,
        value: displayVal,
        locked: null,
      });
    }
    return rows;
  }

  if (policyType === "Edge") {
    const parsed = raw as Record<string, unknown>;
    for (const policyDef of EDGE_ALL_POLICIES) {
      if (!(policyDef.key in parsed)) continue;
      const val = parsed[policyDef.key];
      let displayVal: string;
      if (policyDef.type === "list" && Array.isArray(val)) {
        displayVal = val.length > 0 ? val.join(", ") : "(empty)";
      } else if (policyDef.type === "integer-enum" && policyDef.intOptions) {
        const opt = policyDef.intOptions.find(o => o.value === val);
        displayVal = opt ? opt.label : formatDisplayValue(val);
      } else if (policyDef.type === "string-enum" && policyDef.stringOptions) {
        const opt = policyDef.stringOptions.find(o => o === val);
        displayVal = opt ? opt : formatDisplayValue(val);
      } else if (policyDef.type === "json") {
        displayVal = typeof val === "string" ? val : JSON.stringify(val);
      } else {
        displayVal = formatDisplayValue(val);
      }
      rows.push({
        setting: policyDef.label,
        value: displayVal,
        locked: null,
      });
    }
    return rows;
  }

  if (policyType === "Kconfig") {
    const parsed = raw as Record<string, unknown>;
    const enforcedFields = Array.isArray(parsed.enforcedFields) ? (parsed.enforcedFields as string[]) : [];

    // URL restrictions
    if (Array.isArray(parsed.urlRestrictions)) {
      (parsed.urlRestrictions as Record<string, unknown>[]).forEach((r, i) => {
        const proto = (r.protocol as string) || "*";
        const host = (r.host as string) || "*";
        const path = (r.path as string) ? `/${r.path}` : "";
        const summary = `${r.action} ${proto}://${host}${path} → ${r.enabled ? "allow" : "deny"}`;
        rows.push({ setting: `Security › URL Restrictions › rule_${i + 1}`, value: summary, locked: "Yes" });
      });
    }

    // KCM restrictions
    if (Array.isArray(parsed.kcmRestrictions)) {
      for (const modId of parsed.kcmRestrictions as string[]) {
        const mod = KCM_MODULES.find(m => m.id === modId);
        rows.push({
          setting: `System Settings Restrictions › ${mod ? mod.label : modId}`,
          value: "Restricted",
          locked: "Yes",
        });
      }
    }

    // All other typed fields
    for (const def of KCONFIG_ALL_POLICIES) {
      if (def.type === "url-restrictions" || def.type === "kcm-restrictions") continue;
      if (!(def.key in parsed) || parsed[def.key] === null || parsed[def.key] === undefined) continue;
      rows.push({
        setting: `${def.group} › ${def.label}`,
        value: formatDisplayValue(parsed[def.key]),
        locked: enforcedFields.includes(def.key) ? "Yes" : "No",
      });
    }

    return rows;
  }

  // Normalize to array for multi-setting support (backward compat with single-object)
  const items: Record<string, unknown>[] = Array.isArray(raw)
    ? raw
    : [raw as Record<string, unknown>];

  if (policyType === "Dconf") {
    // DConf content shape: { entries: [{schema_id, key, value, lock, path?},...], db_name }
    const dconfContent = raw as { entries?: Record<string, unknown>[]; db_name?: string };
    const entries = Array.isArray(dconfContent?.entries) ? dconfContent.entries : [];
    for (const [idx, entry] of entries.entries()) {
      const schemaId = String(entry["schema_id"] ?? "");
      const keyName  = String(entry["key"]       ?? "");
      const label    = schemaId ? `${schemaId} › ${keyName}` : (keyName || `Entry ${idx + 1}`);
      const lockedVal = entry["lock"] !== undefined
        ? (entry["lock"] === "true" || entry["lock"] === true ? "Yes" : "No")
        : null;
      rows.push({
        setting: label,
        value: formatDisplayValue(entry["value"]),
        locked: lockedVal,
      });
    }
    return rows;
  }

  if (policyType === "Package") {
    const pkgContent = raw as { repositories?: { id?: string; type?: string; enabled?: boolean }[]; packages?: { name?: string; state?: string; version?: string }[]; updateCache?: boolean; allowDowngrade?: boolean };
    const pkgRepos = Array.isArray(pkgContent?.repositories) ? pkgContent.repositories : [];
    const pkgPkgs  = Array.isArray(pkgContent?.packages)     ? pkgContent.packages     : [];
    rows.push({ setting: "Repositories", value: `${pkgRepos.length} defined`, locked: null });
    for (const repo of pkgRepos) {
      const typeShort = (repo.type ?? "").replace("REPOSITORY_TYPE_", "");
      rows.push({ setting: `  Repo › ${repo.id ?? "(unnamed)"}`, value: `${typeShort}${repo.enabled === false ? " (disabled)" : ""}`, locked: null });
    }
    for (const pkg of pkgPkgs) {
      const stateShort = (pkg.state ?? "").replace("PACKAGE_STATE_", "").toLowerCase();
      const versionPart = pkg.version ? ` @ ${pkg.version}` : "";
      rows.push({ setting: `Package › ${pkg.name ?? "(unnamed)"}`, value: stateShort + versionPart, locked: null });
    }
    if (pkgContent.updateCache !== undefined) {
      rows.push({ setting: "Options › Refresh cache", value: pkgContent.updateCache ? "yes" : "no", locked: null });
    }
    return rows;
  }

  // Known structured types (Polkit, Chrome)
  const config = TYPE_CONFIGS[policyType];
  if (config) {
    for (const [idx, parsed] of items.entries()) {
      const prefix = items.length > 1 ? `Setting ${idx + 1} › ` : "";
      for (const field of config.fields) {
        rows.push({
          setting: prefix + field.label,
          value: formatDisplayValue(parsed[field.key]),
          locked: null,
        });
      }
    }
    return rows;
  }

  // Custom / unknown: show raw key-value pairs
  for (const [idx, parsed] of items.entries()) {
    const prefix = items.length > 1 ? `Setting ${idx + 1} › ` : "";
    for (const [key, val] of Object.entries(parsed)) {
      rows.push({
        setting: prefix + key,
        value: formatDisplayValue(val),
        locked: null,
      });
    }
  }

  return rows;
}

// Detect all Firefox policy keys set in the content JSON
function detectFirefoxConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    return FIREFOX_ALL_POLICIES.filter(p => p.key in parsed).map(p => p.key);
  } catch { return []; }
}

// Detect all Thunderbird policy keys set in the content JSON. Thunderbird has
// its own catalogue, so filtering against the Firefox list would miss
// Thunderbird-only keys (no configured indicator, no auto-select).
function detectThunderbirdConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    return THUNDERBIRD_ALL_POLICIES.filter(p => p.key in parsed).map(p => p.key);
  } catch { return []; }
}

// Whether a policy's content JSON has at least one setting. Empty content is the
// object "{}" (a non-empty string), so a bare string-emptiness check would let
// an empty policy be released.
function policyHasSettings(content: string): boolean {
  try {
    const parsed = JSON.parse(content || "{}");
    return !!parsed && typeof parsed === "object" && Object.keys(parsed).length > 0;
  } catch { return false; }
}

// Canonical string form of policy content, so cosmetic JSON formatting
// differences don't register as unsaved edits. Falls back to the raw string
// when the content isn't valid JSON (mid-edit in the raw editor).
function normalizePolicyContent(content: string): string {
  try { return JSON.stringify(JSON.parse(content || "{}")); }
  catch { return content; }
}

// Default (empty) content a freshly-selected policy type starts from. Mirrors
// the per-type resets in handleTypeChange so the content baseline can move with
// the type without duplicating those literals inline.
function defaultContentForType(type: string): string {
  switch (type) {
    case "Dconf": return JSON.stringify({ entries: [], db_name: "local" }, null, 2);
    case "Package": return JSON.stringify({ repositories: [], packages: [], updateCache: true, allowDowngrade: false }, null, 2);
    case "Firefox":
    case "Thunderbird":
    case "Kconfig":
    case "Chrome":
    case "Edge":
    case "Firewalld": return "{}";
    default: return JSON.stringify([{}], null, 2);
  }
}

// Extract the value for a specific Firefox policy key from content JSON
function extractFirefoxValue(content: string, key: string): unknown {
  try {
    const parsed = JSON.parse(content || "{}");
    return parsed[key];
  } catch { /* invalid JSON in stored content — return undefined so UI shows defaults */ return undefined; }
}

// Update or add a single key in the Firefox content JSON, preserving other keys
function buildFirefoxContent(key: string, value: unknown, existingContent?: string): string {
  let parsed: Record<string, unknown> = {};
  try { parsed = JSON.parse(existingContent || "{}"); } catch { /* ignore */ }
  parsed[key] = value ?? null;
  return JSON.stringify(parsed, null, 2);
}

// Remove a single key from the Firefox content JSON
function removeFirefoxContentKey(key: string, existingContent: string): string {
  let parsed: Record<string, unknown> = {};
  try { parsed = JSON.parse(existingContent || "{}"); } catch { /* ignore */ }
  delete parsed[key];
  return JSON.stringify(parsed, null, 2);
}

/* ── Props ── */

interface PolicyDetailsModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSaved: () => void;
  onDeleted?: () => void;
  policy: Policy | null; // null = create mode
}

/* ── Component ── */

export const PolicyDetailsModal: React.FC<PolicyDetailsModalProps> = ({
  isOpen,
  onClose,
  onSaved,
  onDeleted,
  policy,
}) => {
  const isEditMode = policy !== null;

  // Form state
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [policyType, setPolicyType] = useState("Kconfig");
  const [status, setStatus] = useState("draft");
  const [contentRaw, setContentRaw] = useState("{}");
  const [structuredFieldsList, setStructuredFieldsList] = useState<Record<string, string>[]>([{}]);
  const [activeTab, setActiveTab] = useState(0);
  const [configMode, setConfigMode] = useState<"structured" | "raw">("structured");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [hoveredType, setHoveredType] = useState<string | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  // Track whether the policy was modified in this session (state transition or save)
  const [dirty, setDirty] = useState(false);

  // Baseline of the last-persisted form values, captured on open, on type change,
  // and after a save. Comparing the live form against it detects unsaved edits so
  // we can warn before discarding them (close/Escape) and block lifecycle
  // transitions that would otherwise silently drop them.
  const baselineRef = useRef({ name: "", description: "", policyType: "Kconfig", content: "{}" });
  const [showDiscardConfirm, setShowDiscardConfirm] = useState(false);
  // Pending policy-type switch awaiting confirmation (it would clear configured content).
  const [pendingTypeChange, setPendingTypeChange] = useState<string | null>(null);
  // Whether the currently-edited Chrome/Edge JSON value field holds invalid JSON.
  const [jsonFieldInvalid, setJsonFieldInvalid] = useState(false);

  // Derived: whether policy fields are editable (create mode or DRAFT state)
  const isEditable = !isEditMode || status === "draft";

  // Derived: does the live form differ from the last-persisted baseline?
  const hasUnsavedChanges =
    name !== baselineRef.current.name ||
    description !== baselineRef.current.description ||
    policyType !== baselineRef.current.policyType ||
    normalizePolicyContent(contentRaw) !== baselineRef.current.content;

  // Firefox-specific state: selected policy key + its value
  const [firefoxSelectedKey, setFirefoxSelectedKey] = useState<string | null>(null);
  const [firefoxValue, setFirefoxValue] = useState<unknown>(undefined);
  const [firefoxExpandedGroups, setFirefoxExpandedGroups] = useState<Set<string>>(new Set());

  // Thunderbird-specific state: selected policy key + its value
  const [thunderbirdSelectedKey, setThunderbirdSelectedKey] = useState<string | null>(null);
  const [thunderbirdValue, setThunderbirdValue] = useState<unknown>(undefined);
  const [thunderbirdExpandedGroups, setThunderbirdExpandedGroups] = useState<Set<string>>(new Set());

  // KConfig-specific state: selected policy key, value + enforced
  const [kconfigSelectedKey, setKconfigSelectedKey] = useState<string | null>(null);
  const [kconfigValue, setKconfigValue] = useState<string>("");
  const [kconfigEnforced, setKconfigEnforced] = useState<boolean>(false);
  const [kconfigExpandedGroups, setKconfigExpandedGroups] = useState<Set<string>>(new Set());
  const [urlRestrictionRules, setUrlRestrictionRules] = useState<UrlRestrictionRule[]>([]);
  const [customProtocolIndices, setCustomProtocolIndices] = useState<Set<number>>(new Set());
  const [kcmRestrictedModules, setKcmRestrictedModules] = useState<string[]>([]);
  const [kcmCustomInput, setKcmCustomInput] = useState("");

  // Chrome-specific state: selected policy key + its value
  const [chromeSelectedKey, setChromeSelectedKey] = useState<string | null>(null);
  const [chromeValue, setChromeValue] = useState<unknown>(undefined);
  const [chromeExpandedGroups, setChromeExpandedGroups] = useState<Set<string>>(new Set());

  // Edge-specific state: selected policy key + its value
  const [edgeSelectedKey, setEdgeSelectedKey] = useState<string | null>(null);
  const [edgeValue, setEdgeValue] = useState<unknown>(undefined);
  const [edgeExpandedGroups, setEdgeExpandedGroups] = useState<Set<string>>(new Set());

  // Dconf-specific state: contentRaw is shared with the raw editor;
  // the DConfPolicyEditor reads/writes via contentRaw directly.
  // (No extra state needed — DConfPolicyEditor is driven by contentRaw.)

  // Reset form when modal opens or policy changes
  useEffect(() => {
    if (!isOpen) return;

    if (policy) {
      setName(policy.name);
      setDescription(policy.description);
      setPolicyType(policy.type);
      setStatus(policy.state);
      setContentRaw(policy.content || "{}");
      if (policy.type === "Firefox") {
        const configuredKeys = detectFirefoxConfiguredKeys(policy.content);
        if (configuredKeys.length > 0) {
          setFirefoxSelectedKey(configuredKeys[0]);
          setFirefoxValue(extractFirefoxValue(policy.content, configuredKeys[0]));
          // Expand the groups containing configured keys
          const groups = new Set<string>();
          for (const key of configuredKeys) {
            const def = FIREFOX_ALL_POLICIES.find(p => p.key === key);
            if (def) groups.add(def.group);
          }
          setFirefoxExpandedGroups(groups);
        } else {
          setFirefoxSelectedKey(null);
          setFirefoxValue(undefined);
          setFirefoxExpandedGroups(new Set());
        }
      } else if (policy.type === "Kconfig") {
        const configuredKeys = detectKConfigConfiguredKeys(policy.content);
        if (configuredKeys.length > 0) {
          setKconfigSelectedKey(configuredKeys[0]);
          const entry = extractKConfigEntry(policy.content, configuredKeys[0]);
          setKconfigValue(entry?.value ?? "");
          setKconfigEnforced(entry?.enforced ?? false);
          const groups = new Set<string>();
          for (const key of configuredKeys) {
            const def = KCONFIG_ALL_POLICIES.find(p => p.key === key);
            if (def) groups.add(def.group);
          }
          setKconfigExpandedGroups(groups);
          // If url_restrictions is configured, load the rules
          if (configuredKeys.includes("urlRestrictions")) {
            const rules = parseUrlRestrictionRules(policy.content);
            setUrlRestrictionRules(rules);
            const customIdxs = new Set<number>();
            rules.forEach((r, i) => { if (r.protocol !== "" && !KIO_PROTOCOLS.includes(r.protocol)) customIdxs.add(i); });
            setCustomProtocolIndices(customIdxs);
          }
          if (configuredKeys.includes("kcmRestrictions")) {
            setKcmRestrictedModules(parseKcmRestrictions(policy.content));
          }
        } else {
          setKconfigSelectedKey(null);
          setKconfigValue("");
          setKconfigEnforced(false);
          setKconfigExpandedGroups(new Set());
          setUrlRestrictionRules([]);
          setCustomProtocolIndices(new Set());
          setKcmRestrictedModules([]);
          setKcmCustomInput("");
        }
      } else if (policy.type === "Chrome") {
        const configuredKeys = detectChromeConfiguredKeys(policy.content);
        if (configuredKeys.length > 0) {
          setChromeSelectedKey(configuredKeys[0]);
          setChromeValue(extractChromeValue(policy.content, configuredKeys[0]));
          const groups = new Set<string>();
          for (const key of configuredKeys) {
            const def = CHROME_ALL_POLICIES.find(p => p.key === key);
            if (def) groups.add(def.group);
          }
          setChromeExpandedGroups(groups);
        } else {
          setChromeSelectedKey(null);
          setChromeValue(undefined);
          setChromeExpandedGroups(new Set());
        }
      } else if (policy.type === "Edge") {
        const configuredKeys = detectEdgeConfiguredKeys(policy.content);
        if (configuredKeys.length > 0) {
          setEdgeSelectedKey(configuredKeys[0]);
          setEdgeValue(extractEdgeValue(policy.content, configuredKeys[0]));
          const groups = new Set<string>();
          for (const key of configuredKeys) {
            const def = EDGE_ALL_POLICIES.find(p => p.key === key);
            if (def) groups.add(def.group);
          }
          setEdgeExpandedGroups(groups);
        } else {
          setEdgeSelectedKey(null);
          setEdgeValue(undefined);
          setEdgeExpandedGroups(new Set());
        }
      } else if (policy.type === "Thunderbird") {
        const configuredKeys = detectThunderbirdConfiguredKeys(policy.content);
        if (configuredKeys.length > 0) {
          setThunderbirdSelectedKey(configuredKeys[0]);
          setThunderbirdValue(extractFirefoxValue(policy.content, configuredKeys[0]));
          const groups = new Set<string>();
          for (const key of configuredKeys) {
            const def = THUNDERBIRD_ALL_POLICIES.find(p => p.key === key);
            if (def) groups.add(def.group);
          }
          setThunderbirdExpandedGroups(groups);
        } else {
          setThunderbirdSelectedKey(null);
          setThunderbirdValue(undefined);
          setThunderbirdExpandedGroups(new Set());
        }
      } else if (policy.type === "Dconf") {
        // Dconf editor reads contentRaw directly — nothing extra to initialise.
      } else if (policy.type === "Polkit") {
        // Polkit editor reads contentRaw directly — nothing extra to initialise.
      } else if (policy.type === "Firewalld") {
        // Firewalld editor reads contentRaw directly — nothing extra to initialise.
      } else {
        try {
          const parsed = JSON.parse(policy.content || "{}");
          if (Array.isArray(parsed)) {
            setStructuredFieldsList(parsed.map(flattenForForm));
          } else {
            setStructuredFieldsList([flattenForForm(parsed)]);
          }
        } catch {
          setStructuredFieldsList([{}]);
        }
      }
    } else {
      setName("");
      setDescription("");
      setPolicyType("Kconfig");
      setStatus("draft");
      setContentRaw("{}");
      setStructuredFieldsList([{}]);
      setFirefoxSelectedKey(null);
      setFirefoxValue(undefined);
      setFirefoxExpandedGroups(new Set());
      setThunderbirdSelectedKey(null);
      setThunderbirdValue(undefined);
      setThunderbirdExpandedGroups(new Set());
      setKconfigSelectedKey(null);
      setKconfigValue("");
      setKconfigEnforced(false);
      setKconfigExpandedGroups(new Set());
      setUrlRestrictionRules([]);
      setCustomProtocolIndices(new Set());
      setKcmRestrictedModules([]);
      setKcmCustomInput("");
      setChromeSelectedKey(null);
      setChromeValue(undefined);
      setChromeExpandedGroups(new Set());
      setEdgeSelectedKey(null);
      setEdgeValue(undefined);
      setEdgeExpandedGroups(new Set());
    }
    setActiveTab(0);
    setConfigMode("structured");
    setError(null);
    setValidationError(null);
    setShowDeleteConfirm(false);
    setDirty(false);
    setShowDiscardConfirm(false);
    setPendingTypeChange(null);
    setJsonFieldInvalid(false);
    // Capture the persisted baseline these setters just established so
    // hasUnsavedChanges reads false until the user actually edits something.
    baselineRef.current = policy
      ? {
          name: policy.name,
          description: policy.description,
          policyType: policy.type,
          content: normalizePolicyContent(policy.content || "{}"),
        }
      : { name: "", description: "", policyType: "Kconfig", content: normalizePolicyContent("{}") };
  }, [isOpen, policy]);

  // Clear the Chrome/Edge JSON-invalid flag whenever the edited key changes so a
  // stale error doesn't linger on a different (or no) selection.
  useEffect(() => {
    setJsonFieldInvalid(false);
  }, [chromeSelectedKey, edgeSelectedKey]);

  // When policy type changes, reset content for the target type
  const applyTypeChange = (newType: string) => {
    setPolicyType(newType);
    // The new type starts from empty content, so clear any lingering JSON error
    // (otherwise Save could stay disabled with no field left to fix).
    setJsonFieldInvalid(false);
    if (newType === "Firefox") {
      setFirefoxSelectedKey(null);
      setFirefoxValue(undefined);
      setContentRaw("{}");
      setFirefoxExpandedGroups(new Set());
    } else if (newType === "Thunderbird") {
      setThunderbirdSelectedKey(null);
      setThunderbirdValue(undefined);
      setContentRaw("{}");
      setThunderbirdExpandedGroups(new Set());
    } else if (newType === "Kconfig") {
      setKconfigSelectedKey(null);
      setKconfigValue("");
      setKconfigEnforced(false);
      setContentRaw("{}");
      setKconfigExpandedGroups(new Set());
      setUrlRestrictionRules([]);
      setCustomProtocolIndices(new Set());
      setKcmRestrictedModules([]);
      setKcmCustomInput("");
    } else if (newType === "Chrome") {
      setChromeSelectedKey(null);
      setChromeValue(undefined);
      setContentRaw("{}");
      setChromeExpandedGroups(new Set());
    } else if (newType === "Edge") {
      setEdgeSelectedKey(null);
      setEdgeValue(undefined);
      setContentRaw("{}");
      setEdgeExpandedGroups(new Set());
    } else if (newType === "Dconf") {
      setContentRaw(JSON.stringify({ entries: [], db_name: "local" }, null, 2));
    } else if (newType === "Package") {
      setContentRaw(JSON.stringify({ repositories: [], packages: [], updateCache: true, allowDowngrade: false }, null, 2));
    } else if (newType === "Firewalld") {
      setContentRaw("{}");
    } else {
      setStructuredFieldsList([{}]);
      setContentRaw(JSON.stringify([{}], null, 2));
    }
    // Content is intentionally reset for the new type, so move the content/type
    // baseline with it — switching type shouldn't count as unsaved work. Any
    // name/description the user already entered stays in the baseline comparison.
    baselineRef.current = {
      ...baselineRef.current,
      policyType: newType,
      content: normalizePolicyContent(defaultContentForType(newType)),
    };
  };

  // Type-change entry point: switching type clears the configured content for
  // the current type, so confirm first when there is configured content to lose.
  const handleTypeChange = (newType: string) => {
    if (newType === policyType) return;
    if (policyHasSettings(contentRaw)) {
      setPendingTypeChange(newType);
      return;
    }
    applyTypeChange(newType);
  };

  // Keep content in sync (non-Firefox types)
  const syncContentFromStructuredList = (list: Record<string, string>[]) => {
    const json = JSON.stringify(list, null, 2);
    setContentRaw(json);
    setValidationError(null);
  };

  const syncContentFromRaw = (raw: string) => {
    setContentRaw(raw);
    try {
      const parsed = JSON.parse(raw);
      if (policyType !== "Firefox" && policyType !== "Thunderbird") {
        if (Array.isArray(parsed)) {
          setStructuredFieldsList(parsed.map(flattenForForm));
        } else {
          setStructuredFieldsList([flattenForForm(parsed)]);
        }
      }
      setValidationError(null);
    } catch {
      setValidationError("Invalid JSON format");
    }
  };

  const handleStructuredFieldChange = (settingIndex: number, key: string, value: string) => {
    const updated = [...structuredFieldsList];
    updated[settingIndex] = { ...updated[settingIndex], [key]: value };
    setStructuredFieldsList(updated);
    syncContentFromStructuredList(updated);
  };

  const handleAddSetting = () => {
    const updated = [...structuredFieldsList, {}];
    setStructuredFieldsList(updated);
    syncContentFromStructuredList(updated);
  };

  const handleRemoveSetting = (index: number) => {
    const updated = structuredFieldsList.filter((_, i) => i !== index);
    const result = updated.length === 0 ? [{}] : updated;
    setStructuredFieldsList(result);
    syncContentFromStructuredList(result);
  };

  // Firefox: select a policy from the tree and set its default value
  const handleFirefoxSelectPolicy = (policyDef: PolicyFieldDef) => {
    setFirefoxSelectedKey(policyDef.key);
    // If we already have a value for this key, load it for editing
    const existing = extractFirefoxValue(contentRaw, policyDef.key);
    if (existing !== undefined) {
      setFirefoxValue(existing);
    } else {
      // Set default value based on type and add to content
      let defaultValue: unknown;
      if (policyDef.type === "boolean") {
        defaultValue = true;
      } else if (policyDef.type === "string") {
        defaultValue = "";
      } else if (policyDef.type === "string-enum") {
        defaultValue = policyDef.stringOptions?.[0] || "";
      } else if (policyDef.type === "object") {
        const obj: Record<string, unknown> = {};
        for (const f of policyDef.subFields || []) {
          if (f.type === "boolean") obj[f.key] = false;
          else if (f.type === "string" || f.type === "string-enum") obj[f.key] = "";
          else if (f.type === "list") obj[f.key] = [];
        }
        defaultValue = obj;
      }
      setFirefoxValue(defaultValue);
      setContentRaw(buildFirefoxContent(policyDef.key, defaultValue, contentRaw));
    }
  };

  // Firefox: remove a policy from the content
  const handleFirefoxRemovePolicy = (key: string) => {
    const newContent = removeFirefoxContentKey(key, contentRaw);
    setContentRaw(newContent);
    if (firefoxSelectedKey === key) {
      setFirefoxSelectedKey(null);
      setFirefoxValue(undefined);
    }
  };

  // Firefox: update the value for the currently selected policy
  const updateFirefoxValue = (newValue: unknown) => {
    setFirefoxValue(newValue);
    if (firefoxSelectedKey) {
      setContentRaw(buildFirefoxContent(firefoxSelectedKey, newValue, contentRaw));
    }
  };

  // Firefox: toggle a group in the tree
  const toggleFirefoxGroup = (group: string) => {
    setFirefoxExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  // Thunderbird: select a policy from the tree and set its default value
  const handleThunderbirdSelectPolicy = (policyDef: PolicyFieldDef) => {
    setThunderbirdSelectedKey(policyDef.key);
    const existing = extractFirefoxValue(contentRaw, policyDef.key);
    if (existing !== undefined) {
      setThunderbirdValue(existing);
    } else {
      let defaultValue: unknown;
      if (policyDef.type === "boolean") {
        defaultValue = true;
      } else if (policyDef.type === "string") {
        defaultValue = "";
      } else if (policyDef.type === "string-enum") {
        defaultValue = policyDef.stringOptions?.[0] || "";
      } else if (policyDef.type === "list") {
        defaultValue = [];
      } else if (policyDef.type === "object") {
        const obj: Record<string, unknown> = {};
        for (const f of policyDef.subFields || []) {
          if (f.type === "boolean") obj[f.key] = false;
          else if (f.type === "string" || f.type === "string-enum") obj[f.key] = "";
          else if (f.type === "list") obj[f.key] = [];
        }
        defaultValue = obj;
      }
      setThunderbirdValue(defaultValue);
      setContentRaw(buildFirefoxContent(policyDef.key, defaultValue, contentRaw));
    }
  };

  // Thunderbird: remove a policy from the content
  const handleThunderbirdRemovePolicy = (key: string) => {
    const newContent = removeFirefoxContentKey(key, contentRaw);
    setContentRaw(newContent);
    if (thunderbirdSelectedKey === key) {
      setThunderbirdSelectedKey(null);
      setThunderbirdValue(undefined);
    }
  };

  // Thunderbird: update the value for the currently selected policy
  const updateThunderbirdValue = (newValue: unknown) => {
    setThunderbirdValue(newValue);
    if (thunderbirdSelectedKey) {
      setContentRaw(buildFirefoxContent(thunderbirdSelectedKey, newValue, contentRaw));
    }
  };

  // Thunderbird: toggle a group in the tree
  const toggleThunderbirdGroup = (group: string) => {
    setThunderbirdExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  // KConfig: select a policy from the tree
  const handleKconfigSelectPolicy = (policyDef: KConfigPolicyDef) => {
    setKconfigSelectedKey(policyDef.key);
    if (policyDef.type === "url-restrictions") {
      const rules = parseUrlRestrictionRules(contentRaw);
      if (rules.length > 0) {
        setUrlRestrictionRules(rules);
        const customIdxs = new Set<number>();
        rules.forEach((r, i) => { if (r.protocol !== "" && !KIO_PROTOCOLS.includes(r.protocol)) customIdxs.add(i); });
        setCustomProtocolIndices(customIdxs);
      } else {
        // Seed with one default rule
        const defaultRule: UrlRestrictionRule = { action: "open", referrerProtocol: "", referrerHost: "", referrerPath: "", protocol: "", host: "", path: "", enabled: true };
        setUrlRestrictionRules([defaultRule]);
        setCustomProtocolIndices(new Set());
        setContentRaw(buildUrlRestrictionContent([defaultRule], contentRaw));
      }
      return;
    }
    if (policyDef.type === "kcm-restrictions") {
      const modules = parseKcmRestrictions(contentRaw);
      setKcmRestrictedModules(modules);
      setKcmCustomInput("");
      return;
    }
    const existing = extractKConfigEntry(contentRaw, policyDef.key);
    if (existing !== undefined) {
      setKconfigValue(existing.value);
      setKconfigEnforced(existing.enforced);
    } else {
      // Set default and add to content
      const defaultVal = policyDef.type === "boolean" ? "true" : policyDef.type === "int" ? "0" : "";
      setKconfigValue(defaultVal);
      setKconfigEnforced(true);
      setContentRaw(buildKConfigContent(policyDef, defaultVal, true, contentRaw));
    }
  };

  // KConfig: remove a policy from the content
  const handleKconfigRemovePolicy = (key: string) => {
    const newContent = removeKConfigContentKey(key, contentRaw);
    setContentRaw(newContent);
    if (kconfigSelectedKey === key) {
      setKconfigSelectedKey(null);
      setKconfigValue("");
      setKconfigEnforced(false);
    }
  };

  // KConfig: update value for the currently selected policy
  const updateKconfigValue = (newValue: string) => {
    setKconfigValue(newValue);
    if (kconfigSelectedKey) {
      const def = KCONFIG_ALL_POLICIES.find(p => p.key === kconfigSelectedKey);
      if (def) setContentRaw(buildKConfigContent(def, newValue, kconfigEnforced, contentRaw));
    }
  };

  // KConfig: update enforced state for the currently selected policy
  const updateKconfigEnforced = (enforced: boolean) => {
    setKconfigEnforced(enforced);
    if (kconfigSelectedKey) {
      const def = KCONFIG_ALL_POLICIES.find(p => p.key === kconfigSelectedKey);
      if (def) setContentRaw(buildKConfigContent(def, kconfigValue, enforced, contentRaw));
    }
  };

  // KConfig: toggle a group in the tree
  const toggleKconfigGroup = (group: string) => {
    setKconfigExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  // Chrome: select a policy from the tree and set its default value
  const handleChromeSelectPolicy = (policyDef: PolicyFieldDef) => {
    setChromeSelectedKey(policyDef.key);
    const existing = extractChromeValue(contentRaw, policyDef.key);
    if (existing !== undefined) {
      setChromeValue(existing);
    } else {
      let defaultValue: unknown;
      if (policyDef.type === "boolean") {
        defaultValue = true;
      } else if (policyDef.type === "string" || policyDef.type === "string-enum") {
        defaultValue = policyDef.stringOptions ? policyDef.stringOptions[0] ?? "" : "";
      } else if (policyDef.type === "integer" || policyDef.type === "integer-enum") {
        defaultValue = policyDef.intOptions ? policyDef.intOptions[0]?.value ?? 0 : 0;
      } else if (policyDef.type === "list") {
        defaultValue = [];
      } else if (policyDef.type === "json") {
        defaultValue = {};
      }
      setChromeValue(defaultValue);
      setContentRaw(buildChromeContent(policyDef.key, defaultValue, contentRaw));
    }
  };

  // Chrome: remove a policy from the content
  const handleChromeRemovePolicy = (key: string) => {
    const newContent = removeChromeContentKey(key, contentRaw);
    setContentRaw(newContent);
    if (chromeSelectedKey === key) {
      setChromeSelectedKey(null);
      setChromeValue(undefined);
    }
  };

  // Chrome: update the value for the currently selected policy
  const updateChromeValue = (newValue: unknown) => {
    setChromeValue(newValue);
    if (chromeSelectedKey) {
      setContentRaw(buildChromeContent(chromeSelectedKey, newValue, contentRaw));
    }
  };

  // Chrome: toggle a group in the tree
  const toggleChromeGroup = (group: string) => {
    setChromeExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  // Edge: select a policy from the tree
  const handleEdgeSelectPolicy = (policyDef: PolicyFieldDef) => {
    setEdgeSelectedKey(policyDef.key);
    const existing = extractEdgeValue(contentRaw, policyDef.key);
    if (existing !== undefined) {
      setEdgeValue(existing);
    } else {
      let defaultValue: unknown;
      if (policyDef.type === "boolean") {
        defaultValue = true;
      } else if (policyDef.type === "string" || policyDef.type === "string-enum") {
        defaultValue = policyDef.stringOptions ? policyDef.stringOptions[0] ?? "" : "";
      } else if (policyDef.type === "integer" || policyDef.type === "integer-enum") {
        defaultValue = policyDef.intOptions ? policyDef.intOptions[0]?.value ?? 0 : 0;
      } else if (policyDef.type === "list") {
        defaultValue = [];
      } else if (policyDef.type === "json") {
        defaultValue = {};
      }
      setEdgeValue(defaultValue);
      setContentRaw(buildEdgeContent(policyDef.key, defaultValue, contentRaw));
    }
  };

  const handleEdgeRemovePolicy = (key: string) => {
    const newContent = removeEdgeContentKey(key, contentRaw);
    setContentRaw(newContent);
    if (edgeSelectedKey === key) {
      setEdgeSelectedKey(null);
      setEdgeValue(undefined);
    }
  };

  const updateEdgeValue = (newValue: unknown) => {
    setEdgeValue(newValue);
    if (edgeSelectedKey) {
      setContentRaw(buildEdgeContent(edgeSelectedKey, newValue, contentRaw));
    }
  };

  const toggleEdgeGroup = (group: string) => {
    setEdgeExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  const handleSave = async () => {
    setError(null);
    if (jsonFieldInvalid) {
      setError("Fix the invalid JSON value before saving.");
      return;
    }
    setSaving(true);

    try {
      let finalContent = contentRaw;

      if (policyType === "Firefox") {
        // Save the current selection into content before validating
        if (firefoxSelectedKey) {
          finalContent = buildFirefoxContent(firefoxSelectedKey, firefoxValue, contentRaw);
        }
        try {
          const parsed = JSON.parse(finalContent);
          if (Object.keys(parsed).length === 0) {
            setError("At least one Firefox policy setting must be selected and configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Firefox policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Thunderbird") {
        if (thunderbirdSelectedKey) {
          finalContent = buildFirefoxContent(thunderbirdSelectedKey, thunderbirdValue, contentRaw);
        }
        try {
          const parsed = JSON.parse(finalContent);
          if (Object.keys(parsed).length === 0) {
            setError("At least one Thunderbird policy setting must be selected and configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Thunderbird policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Kconfig") {
        // Save the current selection into content before validating
        if (kconfigSelectedKey === "urlRestrictions") {
          finalContent = buildUrlRestrictionContent(urlRestrictionRules, contentRaw);
        } else if (kconfigSelectedKey === "kcmRestrictions") {
          finalContent = buildKcmRestrictionContent(kcmRestrictedModules, contentRaw);
        } else if (kconfigSelectedKey) {
          const def = KCONFIG_ALL_POLICIES.find(p => p.key === kconfigSelectedKey);
          if (def) {
            finalContent = buildKConfigContent(def, kconfigValue, kconfigEnforced, contentRaw);
          }
        }
        try {
          const parsed = JSON.parse(finalContent);
          if (detectKConfigConfiguredKeys(finalContent).length === 0) {
            setError("At least one KDE Kiosk policy setting must be selected and configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("KConfig policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Chrome") {
        // Save the current selection into content before validating
        if (chromeSelectedKey) {
          finalContent = buildChromeContent(chromeSelectedKey, chromeValue, contentRaw);
        }
        try {
          const parsed = JSON.parse(finalContent);
          if (Object.keys(parsed).length === 0) {
            setError("At least one Chrome policy setting must be selected and configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Chrome policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Edge") {
        if (edgeSelectedKey) {
          finalContent = buildEdgeContent(edgeSelectedKey, edgeValue, contentRaw);
        }
        try {
          const parsed = JSON.parse(finalContent);
          if (Object.keys(parsed).length === 0) {
            setError("At least one Edge policy setting must be selected and configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Edge policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Dconf") {
        try {
          const parsed = JSON.parse(finalContent) as { entries?: unknown[] };
          if (!Array.isArray(parsed.entries) || parsed.entries.length === 0) {
            setError("At least one dconf entry must be configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Dconf policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Polkit") {
        try {
          const parsed = JSON.parse(finalContent) as { rules?: unknown[] };
          if (!Array.isArray(parsed.rules) || parsed.rules.length === 0) {
            setError("At least one polkit rule must be configured before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Polkit policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else if (policyType === "Firewalld") {
        try {
          const parsed = JSON.parse(finalContent) as Record<string, unknown>;
          if (!parsed || Object.keys(parsed).length === 0) {
            setError("Configure at least one firewall setting (service, port, or rich rule) before saving");
            setSaving(false);
            return;
          }
        } catch {
          setError("Firewalld policy content is not valid JSON");
          setSaving(false);
          return;
        }
      } else {
        try {
          JSON.parse(contentRaw);
        } catch {
          setError("Policy content must be valid JSON");
          setSaving(false);
          return;
        }
      }

      if (isEditMode && policy) {
        const req: UpdatePolicyRequest = {
          name,
          description,
          type: policyType,
          content: finalContent,
        };
        await updatePolicy(policy.id, req);
      } else {
        const req: CreatePolicyRequest = {
          name,
          description,
          type: policyType,
          content: finalContent,
        };
        await createPolicy(req);
      }

      setDirty(false);
      // The just-saved values are now the persisted baseline.
      baselineRef.current = { name, description, policyType, content: normalizePolicyContent(finalContent) };
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save policy");
    } finally {
      setSaving(false);
    }
  };

  /* ── State transition handler ── */
  const handleStateTransition = async (newState: string) => {
    if (!policy) return;
    try {
      setSaving(true);
      setError(null);
      const updated = await setPolicyState(policy.id, { state: newState });
      setStatus(updated.state);
      setDirty(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to change policy state");
    } finally {
      setSaving(false);
    }
  };

  /* ── Delete handler ── */
  const handleDelete = async () => {
    if (!policy) return;
    try {
      setSaving(true);
      setError(null);
      await deletePolicy(policy.id);
      setShowDeleteConfirm(false);
      if (onDeleted) onDeleted();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete policy");
      setShowDeleteConfirm(false);
    } finally {
      setSaving(false);
    }
  };

  /* ── Close handlers ── */
  // Actually close: refresh the parent list if a state transition happened.
  const finalizeClose = () => {
    setShowDiscardConfirm(false);
    if (dirty) onSaved();
    onClose();
  };

  // Close entry point (X button, Escape, Cancel): warn first if there are
  // unsaved edits so they aren't silently discarded.
  const handleClose = () => {
    if (hasUnsavedChanges) {
      setShowDiscardConfirm(true);
      return;
    }
    finalizeClose();
  };

  /* ── Firefox: property editor for selected policy ── */
  const renderFirefoxPropertyEditor = () => {
    if (!firefoxSelectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", color: "#6a6e73" }}>
          <Title headingLevel="h3" size="lg">Select a Firefox policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its properties.</p>
        </div>
      );
    }

    const policyDef = FIREFOX_ALL_POLICIES.find(p => p.key === firefoxSelectedKey);
    if (!policyDef) return null;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "1rem" }}>{policyDef.label}</Title>
        <Form>
          {policyDef.type === "boolean" && (
            <FormGroup label="Value" fieldId="ff-prop-bool">
              <Switch
                id="ff-prop-bool"
                isChecked={firefoxValue === true}
                onChange={(_ev, checked) => updateFirefoxValue(checked)}
                label="Enabled"
                labelOff="Disabled"
              />
            </FormGroup>
          )}
          {policyDef.type === "string" && (
            <FormGroup label="Value" fieldId="ff-prop-string">
              <TextInput
                id="ff-prop-string"
                value={(firefoxValue as string) || ""}
                onChange={(_ev, val) => updateFirefoxValue(val)}
              />
            </FormGroup>
          )}
          {policyDef.type === "string-enum" && (
            <FormGroup label="Value" fieldId="ff-prop-select">
              <FormSelect
                id="ff-prop-select"
                value={(firefoxValue as string) || ""}
                onChange={(_ev, val) => updateFirefoxValue(val)}
              >
                <FormSelectOption key="" value="" label="(not set)" />
                {(policyDef.stringOptions || []).map(v => (
                  <FormSelectOption key={v} value={v} label={v} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "object" && policyDef.subFields && (
            <>
              {policyDef.subFields.map(field => {
                const objVal = (firefoxValue as Record<string, unknown>) || {};
                return (
                  <FormGroup key={field.key} label={field.label} fieldId={`ff-prop-${field.key}`}>
                    {field.type === "boolean" && (
                      <Switch
                        id={`ff-prop-${field.key}`}
                        isChecked={objVal[field.key] === true}
                        onChange={(_ev, checked) => updateFirefoxValue({ ...objVal, [field.key]: checked })}
                        label="Yes"
                        labelOff="No"
                      />
                    )}
                    {field.type === "string" && (
                      <TextInput
                        id={`ff-prop-${field.key}`}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateFirefoxValue({ ...objVal, [field.key]: val })}
                      />
                    )}
                    {field.type === "string-enum" && (
                      <FormSelect
                        id={`ff-prop-${field.key}`}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateFirefoxValue({ ...objVal, [field.key]: val })}
                      >
                        <FormSelectOption key="" value="" label="(not set)" />
                        {(field.stringOptions || []).map(v => (
                          <FormSelectOption key={v} value={v} label={v} />
                        ))}
                      </FormSelect>
                    )}
                    {field.type === "list" && (
                      <TextArea
                        id={`ff-prop-${field.key}`}
                        value={((objVal[field.key] as string[]) || []).join("\n")}
                        onChange={(_ev, val) => updateFirefoxValue({ ...objVal, [field.key]: val.split("\n").filter(Boolean) })}
                        rows={3}
                        placeholder="One item per line"
                      />
                    )}
                  </FormGroup>
                );
              })}
            </>
          )}
        </Form>
      </div>
    );
  };

  /* ── Firefox: tree view + property editor layout ── */
  const renderFirefoxForm = () => {
    const tree = buildFirefoxTree();
    const configuredKeys = detectFirefoxConfiguredKeys(contentRaw);

    return (
      <div style={{ display: "flex", minHeight: "400px" }}>
        {/* Left panel: tree view */}
        <div style={{
          width: "260px",
          minWidth: "260px",
          borderRight: "1px solid var(--pf-t--global--border--color--default)",
          overflowY: "auto",
          paddingRight: "0",
        }}>
          {Array.from(tree.entries()).map(([group, policies]) => (
            <div key={group} style={{ marginBottom: "2px" }}>
              {/* Group header */}
              <div
                role="button"
                tabIndex={0}
                onClick={() => toggleFirefoxGroup(group)}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggleFirefoxGroup(group); } }}
                style={{
                  padding: "0.4rem 0.75rem",
                  cursor: "pointer",
                  fontWeight: 600,
                  fontSize: "0.8rem",
                  textTransform: "uppercase",
                  letterSpacing: "0.03em",
                  color: "var(--pf-t--global--text--color--regular)",
                  backgroundColor: "var(--pf-t--global--background--color--secondary--default)",
                  borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                  userSelect: "none",
                  display: "flex",
                  alignItems: "center",
                  gap: "0.4rem",
                }}
              >
                <span style={{
                  display: "inline-block",
                  width: 0,
                  height: 0,
                  borderStyle: "solid",
                  ...(firefoxExpandedGroups.has(group)
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "var(--pf-t--global--text--color--regular) transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent var(--pf-t--global--text--color--regular)" }),
                }} />
                {group}
              </div>
              {/* Policy items */}
              {firefoxExpandedGroups.has(group) && policies.map(p => {
                const isSelected = firefoxSelectedKey === p.key;
                const isConfigured = configuredKeys.includes(p.key);
                return (
                  <div
                    key={p.key}
                    role="button"
                    tabIndex={0}
                    onClick={() => handleFirefoxSelectPolicy(p)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleFirefoxSelectPolicy(p); } }}
                    style={{
                      padding: "0.35rem 0.75rem 0.35rem 1.5rem",
                      cursor: "pointer",
                      fontSize: "0.85rem",
                      backgroundColor: isSelected ? "#2d6a4f" : isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent",
                      color: isSelected ? "#fff" : "var(--pf-t--global--text--color--regular)",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.22)" : "rgba(45, 106, 79, 0.1)"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent"; }}
                  >
                    <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                      {isConfigured && <span style={{ color: isSelected ? "#fff" : "var(--pf-t--global--color--brand--200)", fontSize: "0.7rem" }}>●</span>}
                      {p.label}
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleFirefoxRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "var(--pf-t--global--text--color--subtle)",
                          padding: "0 0.25rem",
                          minWidth: "auto",
                        }}
                        aria-label={`Remove ${p.label}`}
                      >✕</Button>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {/* Right panel: property editor */}
        <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>
          {renderFirefoxPropertyEditor()}
        </div>
      </div>
    );
  };

  /* ── Thunderbird: property editor for selected policy ── */
  const renderThunderbirdPropertyEditor = () => {
    if (!thunderbirdSelectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", color: "#6a6e73" }}>
          <Title headingLevel="h3" size="lg">Select a Thunderbird policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its properties.</p>
        </div>
      );
    }

    const policyDef = THUNDERBIRD_ALL_POLICIES.find(p => p.key === thunderbirdSelectedKey);
    if (!policyDef) return null;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "1rem" }}>{policyDef.label}</Title>
        <Form>
          {policyDef.type === "boolean" && (
            <FormGroup label="Value" fieldId="tb-prop-bool">
              <Switch
                id="tb-prop-bool"
                isChecked={thunderbirdValue === true}
                onChange={(_ev, checked) => updateThunderbirdValue(checked)}
                label="Enabled"
                labelOff="Disabled"
              />
            </FormGroup>
          )}
          {policyDef.type === "string" && (
            <FormGroup label="Value" fieldId="tb-prop-string">
              <TextInput
                id="tb-prop-string"
                value={(thunderbirdValue as string) || ""}
                onChange={(_ev, val) => updateThunderbirdValue(val)}
              />
            </FormGroup>
          )}
          {policyDef.type === "string-enum" && (
            <FormGroup label="Value" fieldId="tb-prop-select">
              <FormSelect
                id="tb-prop-select"
                value={(thunderbirdValue as string) || ""}
                onChange={(_ev, val) => updateThunderbirdValue(val)}
              >
                <FormSelectOption key="" value="" label="(not set)" />
                {(policyDef.stringOptions || []).map(v => (
                  <FormSelectOption key={v} value={v} label={v} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "list" && (
            <FormGroup label="Values (one per line)" fieldId="tb-prop-stringlist">
              <TextArea
                id="tb-prop-stringlist"
                value={((thunderbirdValue as string[]) || []).join("\n")}
                onChange={(_ev, val) => updateThunderbirdValue(val.split("\n").filter(Boolean))}
                rows={4}
                placeholder="One item per line"
              />
            </FormGroup>
          )}
          {policyDef.type === "object" && policyDef.subFields && (
            <>
              {policyDef.subFields.map(field => {
                const objVal = (thunderbirdValue as Record<string, unknown>) || {};
                return (
                  <FormGroup key={field.key} label={field.label} fieldId={`tb-prop-${field.key}`}>
                    {field.type === "boolean" && (
                      <Switch
                        id={`tb-prop-${field.key}`}
                        isChecked={objVal[field.key] === true}
                        onChange={(_ev, checked) => updateThunderbirdValue({ ...objVal, [field.key]: checked })}
                        label="Yes"
                        labelOff="No"
                      />
                    )}
                    {field.type === "string" && (
                      <TextInput
                        id={`tb-prop-${field.key}`}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateThunderbirdValue({ ...objVal, [field.key]: val })}
                      />
                    )}
                    {field.type === "string-enum" && (
                      <FormSelect
                        id={`tb-prop-${field.key}`}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateThunderbirdValue({ ...objVal, [field.key]: val })}
                      >
                        <FormSelectOption key="" value="" label="(not set)" />
                        {(field.stringOptions || []).map(v => (
                          <FormSelectOption key={v} value={v} label={v} />
                        ))}
                      </FormSelect>
                    )}
                    {field.type === "list" && (
                      <TextArea
                        id={`tb-prop-${field.key}`}
                        value={((objVal[field.key] as string[]) || []).join("\n")}
                        onChange={(_ev, val) => updateThunderbirdValue({ ...objVal, [field.key]: val.split("\n").filter(Boolean) })}
                        rows={3}
                        placeholder="One item per line"
                      />
                    )}
                  </FormGroup>
                );
              })}
            </>
          )}
        </Form>
      </div>
    );
  };

  /* ── Thunderbird: tree view + property editor layout ── */
  const renderThunderbirdForm = () => {
    const tree = buildThunderbirdTree();
    const configuredKeys = detectThunderbirdConfiguredKeys(contentRaw);

    return (
      <div style={{ display: "flex", minHeight: "400px" }}>
        {/* Left panel: tree view */}
        <div style={{
          width: "260px",
          minWidth: "260px",
          borderRight: "1px solid var(--pf-t--global--border--color--default)",
          overflowY: "auto",
          paddingRight: "0",
        }}>
          {Array.from(tree.entries()).map(([group, policies]) => (
            <div key={group} style={{ marginBottom: "2px" }}>
              <div
                role="button"
                tabIndex={0}
                onClick={() => toggleThunderbirdGroup(group)}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggleThunderbirdGroup(group); } }}
                style={{
                  padding: "0.4rem 0.75rem",
                  cursor: "pointer",
                  fontWeight: 600,
                  fontSize: "0.8rem",
                  textTransform: "uppercase",
                  letterSpacing: "0.03em",
                  color: "var(--pf-t--global--text--color--regular)",
                  backgroundColor: "var(--pf-t--global--background--color--secondary--default)",
                  borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                  userSelect: "none",
                  display: "flex",
                  alignItems: "center",
                  gap: "0.4rem",
                }}
              >
                <span style={{
                  display: "inline-block",
                  width: 0,
                  height: 0,
                  borderStyle: "solid",
                  ...(thunderbirdExpandedGroups.has(group)
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "var(--pf-t--global--text--color--regular) transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent var(--pf-t--global--text--color--regular)" }),
                }} />
                {group}
              </div>
              {thunderbirdExpandedGroups.has(group) && policies.map(p => {
                const isSelected = thunderbirdSelectedKey === p.key;
                const isConfigured = configuredKeys.includes(p.key);
                return (
                  <div
                    key={p.key}
                    role="button"
                    tabIndex={0}
                    onClick={() => handleThunderbirdSelectPolicy(p)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleThunderbirdSelectPolicy(p); } }}
                    style={{
                      padding: "0.35rem 0.75rem 0.35rem 1.5rem",
                      cursor: "pointer",
                      fontSize: "0.85rem",
                      backgroundColor: isSelected ? "#2d6a4f" : isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent",
                      color: isSelected ? "#fff" : "var(--pf-t--global--text--color--regular)",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.22)" : "rgba(45, 106, 79, 0.1)"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent"; }}
                  >
                    <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                      {isConfigured && <span style={{ color: isSelected ? "#fff" : "var(--pf-t--global--color--brand--200)", fontSize: "0.7rem" }}>●</span>}
                      {p.label}
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleThunderbirdRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "var(--pf-t--global--text--color--subtle)",
                          padding: "0 0.25rem",
                          minWidth: "auto",
                        }}
                        aria-label={`Remove ${p.label}`}
                      >✕</Button>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {/* Right panel: property editor */}
        <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>
          {renderThunderbirdPropertyEditor()}
        </div>
      </div>
    );
  };

  /* ── KConfig: property editor for selected policy ── */
  /* ── KCM Restrictions: module multi-select editor ── */
  const renderKcmRestrictionsEditor = () => {
    const updateModules = (newModules: string[]) => {
      setKcmRestrictedModules(newModules);
      setContentRaw(buildKcmRestrictionContent(newModules, contentRaw));
    };

    const addModule = (moduleId: string) => {
      if (!moduleId || kcmRestrictedModules.includes(moduleId)) return;
      updateModules([...kcmRestrictedModules, moduleId]);
    };

    const removeModule = (moduleId: string) => {
      updateModules(kcmRestrictedModules.filter(m => m !== moduleId));
    };

    const addCustomModules = () => {
      const ids = kcmCustomInput.split(/[\n,]+/).map(s => s.trim()).filter(Boolean);
      const toAdd = ids.filter(id => !kcmRestrictedModules.includes(id));
      if (toAdd.length > 0) updateModules([...kcmRestrictedModules, ...toAdd]);
      setKcmCustomInput("");
    };

    // Available modules = all known modules not already added
    const availableModules = KCM_MODULES.filter(m => !kcmRestrictedModules.includes(m.id));

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>System Settings Module Restrictions</Title>
        <p style={{ color: "#6a6e73", fontSize: "0.85rem", marginBottom: "1rem" }}>
          Files: <code>/etc/kde5rc</code>, <code>/etc/kde6rc</code> &nbsp; Group: <code>[KDE Control Module Restrictions]</code>
          <br />
          Selected modules will be <strong>restricted</strong> (users will not be able to access them in System Settings).
        </p>

        {/* Module selector dropdown */}
        <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem", alignItems: "flex-end" }}>
          <FormGroup label="Add module" fieldId="kcm-add-select" style={{ flex: 1 }}>
            <FormSelect
              id="kcm-add-select"
              value=""
              onChange={(_ev, val) => { if (val) addModule(val); }}
            >
              <FormSelectOption value="" label={availableModules.length > 0 ? "Select a module to restrict..." : "(all known modules added)"} isDisabled />
              {availableModules.map(m => (
                <FormSelectOption key={m.id} value={m.id} label={`${m.label} (${m.id})`} />
              ))}
            </FormSelect>
          </FormGroup>
        </div>

        {/* Custom module input */}
        <details style={{ marginBottom: "1rem" }}>
          <summary style={{ cursor: "pointer", color: "#6a6e73", fontSize: "0.85rem" }}>Add custom modules</summary>
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.5rem", alignItems: "flex-end" }}>
            <FormGroup label="Custom module IDs" fieldId="kcm-custom-input" helperText="One per line or comma-separated" style={{ flex: 1 }}>
              <TextArea
                id="kcm-custom-input"
                value={kcmCustomInput}
                onChange={(_ev, val) => setKcmCustomInput(val)}
                rows={2}
                placeholder="kcm_example, kcm_other"
              />
            </FormGroup>
            <Button variant="secondary" size="sm" onClick={addCustomModules} style={{ marginBottom: "0.25rem" }}>Add</Button>
          </div>
        </details>

        {/* Restricted modules list */}
        {kcmRestrictedModules.length > 0 ? (
          <div>
            <Title headingLevel="h4" size="md" style={{ marginBottom: "0.5rem" }}>Restricted modules ({kcmRestrictedModules.length})</Title>
            {kcmRestrictedModules.map(moduleId => {
              const mod = KCM_MODULES.find(m => m.id === moduleId);
              return (
                <div
                  key={moduleId}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "0.4rem 0.75rem",
                    borderBottom: "1px solid #e8e8e8",
                    fontSize: "0.85rem",
                  }}
                >
                  <span>
                    <strong>{mod ? mod.label : moduleId}</strong>
                    {mod && <span style={{ color: "#6a6e73", marginLeft: "0.5rem" }}>({moduleId})</span>}
                    {!mod && <Label isCompact color="orange" style={{ marginLeft: "0.5rem" }}>custom</Label>}
                  </span>
                  <Button variant="plain" size="sm" onClick={() => removeModule(moduleId)} style={{ color: "#c9190b", padding: "0 0.25rem", minWidth: "auto" }} aria-label={`Remove ${moduleId}`}>
                    Remove
                  </Button>
                </div>
              );
            })}
          </div>
        ) : (
          <p style={{ color: "#6a6e73", fontStyle: "italic" }}>No modules restricted. Use the dropdown above to add modules.</p>
        )}
      </div>
    );
  };

  /* ── URL Restrictions: structured rule editor ── */
  const renderUrlRestrictionsEditor = () => {
    const updateRules = (newRules: UrlRestrictionRule[]) => {
      setUrlRestrictionRules(newRules);
      setContentRaw(buildUrlRestrictionContent(newRules, contentRaw));
    };

    const updateRule = (index: number, partial: Partial<UrlRestrictionRule>) => {
      const updated = urlRestrictionRules.map((r, i) => i === index ? { ...r, ...partial } : r);
      updateRules(updated);
    };

    const addRule = () => {
      updateRules([...urlRestrictionRules, { action: "open", referrerProtocol: "", referrerHost: "", referrerPath: "", protocol: "", host: "", path: "", enabled: true }]);
    };

    const removeRule = (index: number) => {
      const updated = urlRestrictionRules.filter((_, i) => i !== index);
      // Shift custom protocol indices to account for removed index
      const newCustom = new Set<number>();
      for (const ci of customProtocolIndices) {
        if (ci < index) newCustom.add(ci);
        else if (ci > index) newCustom.add(ci - 1);
      }
      setCustomProtocolIndices(newCustom);
      updateRules(updated);
    };

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>URL Restrictions</Title>
        <p style={{ color: "#6a6e73", fontSize: "0.85rem", marginBottom: "1rem" }}>
          File: <code>kdeglobals</code> &nbsp; Group: <code>[KDE URL Restrictions]</code>
        </p>
        <Button variant="secondary" size="sm" onClick={addRule} style={{ marginBottom: "1rem" }}>+ Add Rule</Button>
        {urlRestrictionRules.map((rule, idx) => (
          <Card key={idx} isCompact style={{ marginBottom: "0.75rem" }}>
            <CardTitle>
              <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }}>
                <FlexItem><strong>Rule {idx + 1}</strong></FlexItem>
                <FlexItem>
                  <Button variant="plain" size="sm" onClick={() => removeRule(idx)} aria-label={`Remove rule ${idx + 1}`} style={{ color: "#c9190b" }}>Remove</Button>
                </FlexItem>
              </Flex>
            </CardTitle>
            <CardBody>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem", marginBottom: "0.75rem" }}>
                <FormGroup label="Action" fieldId={`url-action-${idx}`}>
                  <FormSelect id={`url-action-${idx}`} value={rule.action} onChange={(_ev, val) => updateRule(idx, { action: val as UrlRestrictionRule["action"] })}>
                    <FormSelectOption value="open" label="open" />
                    <FormSelectOption value="list" label="list" />
                    <FormSelectOption value="redirect" label="redirect" />
                  </FormSelect>
                </FormGroup>
                <FormGroup label="Protocol" fieldId={`url-protocol-${idx}`} helperText="Without ! suffix = prefix-matches (e.g. http matches https)">
                  <FormSelect
                    id={`url-protocol-${idx}`}
                    value={customProtocolIndices.has(idx) ? "__custom__" : KIO_PROTOCOLS.includes(rule.protocol) ? rule.protocol : rule.protocol === "" ? "" : "__custom__"}
                    onChange={(_ev, val) => {
                      if (val === "__custom__") {
                        setCustomProtocolIndices(prev => new Set(prev).add(idx));
                        updateRule(idx, { protocol: "" });
                      } else {
                        setCustomProtocolIndices(prev => { const next = new Set(prev); next.delete(idx); return next; });
                        updateRule(idx, { protocol: val });
                      }
                    }}
                  >
                    <FormSelectOption value="" label="(any protocol)" />
                    {KIO_PROTOCOLS.map(p => <FormSelectOption key={p} value={p} label={p} />)}
                    <FormSelectOption value="__custom__" label="Custom..." />
                  </FormSelect>
                  {customProtocolIndices.has(idx) && (
                    <TextInput
                      id={`url-protocol-custom-${idx}`}
                      value={rule.protocol}
                      onChange={(_ev, val) => updateRule(idx, { protocol: val })}
                      placeholder="Enter custom protocol"
                      style={{ marginTop: "0.5rem" }}
                    />
                  )}
                </FormGroup>
                <FormGroup label="Access" fieldId={`url-enabled-${idx}`}>
                  <Switch id={`url-enabled-${idx}`} isChecked={rule.enabled} onChange={(_ev, checked) => updateRule(idx, { enabled: checked })} label="Allow" labelOff="Deny" />
                </FormGroup>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.75rem", marginBottom: "0.75rem" }}>
                <FormGroup label="Host" fieldId={`url-host-${idx}`} helperText="*.example.com, blank = all">
                  <TextInput id={`url-host-${idx}`} value={rule.host} onChange={(_ev, val) => updateRule(idx, { host: val })} placeholder="blank = all" />
                </FormGroup>
                <FormGroup label="Path" fieldId={`url-path-${idx}`} helperText="/path, blank = all, ! = exact only">
                  <TextInput id={`url-path-${idx}`} value={rule.path} onChange={(_ev, val) => updateRule(idx, { path: val })} placeholder="blank = all" />
                </FormGroup>
              </div>
              <details style={{ marginTop: "0.25rem" }}>
                <summary style={{ cursor: "pointer", color: "#6a6e73", fontSize: "0.85rem" }}>Referrer Matching (advanced)</summary>
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem", marginTop: "0.5rem" }}>
                  <FormGroup label="Referrer Protocol" fieldId={`url-ref-proto-${idx}`}>
                    <TextInput id={`url-ref-proto-${idx}`} value={rule.referrerProtocol} onChange={(_ev, val) => updateRule(idx, { referrerProtocol: val })} placeholder="blank = all" />
                  </FormGroup>
                  <FormGroup label="Referrer Host" fieldId={`url-ref-host-${idx}`}>
                    <TextInput id={`url-ref-host-${idx}`} value={rule.referrerHost} onChange={(_ev, val) => updateRule(idx, { referrerHost: val })} placeholder="blank = all" />
                  </FormGroup>
                  <FormGroup label="Referrer Path" fieldId={`url-ref-path-${idx}`}>
                    <TextInput id={`url-ref-path-${idx}`} value={rule.referrerPath} onChange={(_ev, val) => updateRule(idx, { referrerPath: val })} placeholder="blank = all" />
                  </FormGroup>
                </div>
              </details>
            </CardBody>
          </Card>
        ))}
        {urlRestrictionRules.length === 0 && (
          <p style={{ color: "#6a6e73", fontStyle: "italic" }}>No rules configured. Click "+ Add Rule" to begin.</p>
        )}
      </div>
    );
  };

  const renderKconfigPropertyEditor = () => {
    if (!kconfigSelectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", color: "#6a6e73" }}>
          <Title headingLevel="h3" size="lg">Select a KDE Kiosk policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its properties.</p>
        </div>
      );
    }

    if (kconfigSelectedKey === "urlRestrictions") {
      return renderUrlRestrictionsEditor();
    }

    if (kconfigSelectedKey === "kcmRestrictions") {
      return renderKcmRestrictionsEditor();
    }

    const policyDef = KCONFIG_ALL_POLICIES.find(p => p.key === kconfigSelectedKey);
    if (!policyDef) return null;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{policyDef.label}</Title>
        <p style={{ color: "#6a6e73", fontSize: "0.85rem", marginBottom: "1rem" }}>
          {policyDef.group}
        </p>
        <Form>
          {policyDef.type === "boolean" && (
            <FormGroup label="Value" fieldId="kc-prop-bool">
              <Switch
                id="kc-prop-bool"
                isChecked={kconfigValue === "true"}
                onChange={(_ev, checked) => updateKconfigValue(checked ? "true" : "false")}
                label="true"
                labelOff="false"
              />
            </FormGroup>
          )}
          {policyDef.type === "string" && (
            <FormGroup label="Value" fieldId="kc-prop-string">
              <TextInput
                id="kc-prop-string"
                value={kconfigValue}
                onChange={(_ev, val) => updateKconfigValue(val)}
              />
            </FormGroup>
          )}
          {policyDef.type === "int" && (
            <FormGroup label="Value" fieldId="kc-prop-int">
              <TextInput
                id="kc-prop-int"
                type="number"
                value={kconfigValue}
                onChange={(_ev, val) => updateKconfigValue(val)}
              />
            </FormGroup>
          )}
          {policyDef.type === "select" && (
            <FormGroup label="Value" fieldId="kc-prop-select">
              <FormSelect
                id="kc-prop-select"
                value={kconfigValue}
                onChange={(_ev, val) => updateKconfigValue(val)}
              >
                <FormSelectOption key="" value="" label="(not set)" />
                {(policyDef.selectOptions || []).map(v => (
                  <FormSelectOption key={v} value={v} label={FILL_MODE_LABELS[v] || v} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "color" && (
            <FormGroup label="Value" fieldId="kc-prop-color">
              <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
                <input
                  type="color"
                  id="kc-prop-color"
                  value={rgbToHex(kconfigValue || "0,0,0")}
                  onChange={(ev) => updateKconfigValue(hexToRgb(ev.target.value))}
                  style={{ width: "48px", height: "36px", padding: "2px", border: "1px solid #d2d2d2", borderRadius: "4px", cursor: "pointer" }}
                />
                <TextInput
                  id="kc-prop-color-text"
                  value={kconfigValue}
                  onChange={(_ev, val) => updateKconfigValue(val)}
                  placeholder="R,G,B"
                  style={{ maxWidth: "140px" }}
                />
              </div>
            </FormGroup>
          )}
          <FormGroup label="Enforced (Immutable)" fieldId="kc-prop-enforced">
            <Switch
              id="kc-prop-enforced"
              isChecked={kconfigEnforced}
              onChange={(_ev, checked) => updateKconfigEnforced(checked)}
              label="Enforced [$i]"
              labelOff="Not enforced"
            />
          </FormGroup>
        </Form>
      </div>
    );
  };

  /* ── KConfig: tree view + property editor layout ── */
  const renderKconfigForm = () => {
    const tree = buildKConfigTree();
    const configuredKeys = detectKConfigConfiguredKeys(contentRaw);

    return (
      <div style={{ display: "flex", minHeight: "400px" }}>
        {/* Left panel: tree view */}
        <div style={{
          width: "260px",
          minWidth: "260px",
          borderRight: "1px solid var(--pf-t--global--border--color--default)",
          overflowY: "auto",
          paddingRight: "0",
        }}>
          {Array.from(tree.entries()).map(([group, policies]) => (
            <div key={group} style={{ marginBottom: "2px" }}>
              {/* Group header */}
              <div
                role="button"
                tabIndex={0}
                onClick={() => toggleKconfigGroup(group)}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggleKconfigGroup(group); } }}
                style={{
                  padding: "0.4rem 0.75rem",
                  cursor: "pointer",
                  fontWeight: 600,
                  fontSize: "0.8rem",
                  textTransform: "uppercase",
                  letterSpacing: "0.03em",
                  color: "var(--pf-t--global--text--color--regular)",
                  backgroundColor: "var(--pf-t--global--background--color--secondary--default)",
                  borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                  userSelect: "none",
                  display: "flex",
                  alignItems: "center",
                  gap: "0.4rem",
                }}
              >
                <span style={{
                  display: "inline-block",
                  width: 0,
                  height: 0,
                  borderStyle: "solid",
                  ...(kconfigExpandedGroups.has(group)
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "var(--pf-t--global--text--color--regular) transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent var(--pf-t--global--text--color--regular)" }),
                }} />
                {group}
              </div>
              {/* Policy items */}
              {kconfigExpandedGroups.has(group) && policies.map(p => {
                const isSelected = kconfigSelectedKey === p.key;
                const isConfigured = configuredKeys.includes(p.key);
                return (
                  <div
                    key={p.key}
                    role="button"
                    tabIndex={0}
                    onClick={() => handleKconfigSelectPolicy(p)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleKconfigSelectPolicy(p); } }}
                    style={{
                      padding: "0.35rem 0.75rem 0.35rem 1.5rem",
                      cursor: "pointer",
                      fontSize: "0.85rem",
                      backgroundColor: isSelected ? "#2d6a4f" : isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent",
                      color: isSelected ? "#fff" : "var(--pf-t--global--text--color--regular)",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.22)" : "rgba(45, 106, 79, 0.1)"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent"; }}
                  >
                    <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                      {isConfigured && <span style={{ color: isSelected ? "#fff" : "var(--pf-t--global--color--brand--200)", fontSize: "0.7rem" }}>●</span>}
                      {p.label}
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleKconfigRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "var(--pf-t--global--text--color--subtle)",
                          padding: "0 0.25rem",
                          minWidth: "auto",
                        }}
                        aria-label={`Remove ${p.label}`}
                      >✕</Button>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {/* Right panel: property editor */}
        <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>
          {renderKconfigPropertyEditor()}
        </div>
      </div>
    );
  };

  /* ── Chrome: property editor for selected policy ── */
  const renderChromePropertyEditor = () => {
    if (!chromeSelectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", color: "#6a6e73" }}>
          <Title headingLevel="h3" size="lg">Select a Chrome policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its value.</p>
        </div>
      );
    }

    const policyDef = CHROME_ALL_POLICIES.find(p => p.key === chromeSelectedKey);
    if (!policyDef) return null;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{policyDef.label}</Title>
        <p style={{ color: "#6a6e73", fontSize: "0.85rem", marginBottom: "1rem" }}>{policyDef.description}</p>
        <Form>
          {policyDef.type === "boolean" && (
            <FormGroup label="Value" fieldId="cr-prop-bool">
              <Switch
                id="cr-prop-bool"
                isChecked={chromeValue === true}
                onChange={(_ev, checked) => updateChromeValue(checked)}
                label="Enabled"
                labelOff="Disabled"
              />
            </FormGroup>
          )}
          {policyDef.type === "string" && (
            <FormGroup label="Value" fieldId="cr-prop-string">
              <TextInput
                id="cr-prop-string"
                value={(chromeValue as string) || ""}
                onChange={(_ev, val) => updateChromeValue(val)}
              />
            </FormGroup>
          )}
          {policyDef.type === "integer" && (
            <FormGroup label="Value" fieldId="cr-prop-int">
              <TextInput
                id="cr-prop-int"
                type="number"
                value={String(chromeValue ?? 0)}
                onChange={(_ev, val) => updateChromeValue(parseInt(val, 10) || 0)}
              />
            </FormGroup>
          )}
          {policyDef.type === "integer-enum" && policyDef.intOptions && (
            <FormGroup label="Value" fieldId="cr-prop-int-enum">
              <FormSelect
                id="cr-prop-int-enum"
                value={String(chromeValue ?? policyDef.intOptions[0]?.value ?? 0)}
                onChange={(_ev, val) => updateChromeValue(parseInt(val, 10))}
              >
                {policyDef.intOptions.map(opt => (
                  <FormSelectOption key={String(opt.value)} value={String(opt.value)} label={opt.label} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "string-enum" && policyDef.stringOptions && (
            <FormGroup label="Value" fieldId="cr-prop-str-enum">
              <FormSelect
                id="cr-prop-str-enum"
                value={(chromeValue as string) || (policyDef.stringOptions[0] ?? "")}
                onChange={(_ev, val) => updateChromeValue(val)}
              >
                {policyDef.stringOptions.map(opt => (
                  <FormSelectOption key={opt} value={opt} label={opt} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "list" && (
            <FormGroup label="Value" fieldId="cr-prop-list" helperText="One item per line">
              <TextArea
                id="cr-prop-list"
                value={((chromeValue as string[]) || []).join("\n")}
                onChange={(_ev, val) => updateChromeValue(val.split("\n").filter(Boolean))}
                rows={5}
                placeholder="One item per line"
              />
            </FormGroup>
          )}
          {policyDef.type === "json" && (
            <FormGroup label="Value (JSON)" fieldId="cr-prop-json" helperText="Enter a valid JSON object or array">
              <TextArea
                id="cr-prop-json"
                validated={jsonFieldInvalid ? "error" : "default"}
                aria-invalid={jsonFieldInvalid || undefined}
                aria-describedby={jsonFieldInvalid ? "cr-prop-json-error" : undefined}
                value={typeof chromeValue === "string" ? chromeValue : JSON.stringify(chromeValue ?? {}, null, 2)}
                onChange={(_ev, val) => {
                  try {
                    updateChromeValue(JSON.parse(val));
                    setJsonFieldInvalid(false);
                  } catch {
                    // Keep the raw text so the user can fix it, and flag it invalid
                    // so save is blocked until it parses.
                    updateChromeValue(val);
                    setJsonFieldInvalid(true);
                  }
                }}
                rows={8}
                style={{ fontFamily: "monospace", fontSize: "0.82rem" }}
                placeholder="{}"
              />
              {jsonFieldInvalid && (
                <div id="cr-prop-json-error" aria-live="polite" style={{ color: "var(--pf-t--global--text--color--status--danger--default, #c9190b)", fontSize: "0.85rem", marginTop: 4 }}>
                  Invalid JSON — fix it before saving.
                </div>
              )}
            </FormGroup>
          )}
          {isEditable && (
            <FormGroup fieldId="cr-prop-actions">
              <Button
                variant="danger"
                size="sm"
                onClick={() => handleChromeRemovePolicy(policyDef.key)}
              >
                Remove Policy
              </Button>
            </FormGroup>
          )}
        </Form>
      </div>
    );
  };

  /* ── Chrome: tree view + property editor layout ── */
  const renderChromeForm = () => {
    const tree = buildChromeTree();
    const configuredKeys = detectChromeConfiguredKeys(contentRaw);

    return (
      <div style={{ display: "flex", minHeight: "400px" }}>
        {/* Left panel: tree view */}
        <div style={{
          width: "260px",
          minWidth: "260px",
          borderRight: "1px solid var(--pf-t--global--border--color--default)",
          overflowY: "auto",
          paddingRight: "0",
        }}>
          {Array.from(tree.entries()).map(([group, policies]) => (
            <div key={group} style={{ marginBottom: "2px" }}>
              {/* Group header */}
              <div
                role="button"
                tabIndex={0}
                onClick={() => toggleChromeGroup(group)}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggleChromeGroup(group); } }}
                style={{
                  padding: "0.4rem 0.75rem",
                  cursor: "pointer",
                  fontWeight: 600,
                  fontSize: "0.8rem",
                  textTransform: "uppercase",
                  letterSpacing: "0.03em",
                  color: "var(--pf-t--global--text--color--regular)",
                  backgroundColor: "var(--pf-t--global--background--color--secondary--default)",
                  borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                  userSelect: "none",
                  display: "flex",
                  alignItems: "center",
                  gap: "0.4rem",
                }}
              >
                <span style={{
                  display: "inline-block",
                  width: 0,
                  height: 0,
                  borderStyle: "solid",
                  ...(chromeExpandedGroups.has(group)
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "var(--pf-t--global--text--color--regular) transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent var(--pf-t--global--text--color--regular)" }),
                }} />
                {group}
              </div>
              {/* Policy items */}
              {chromeExpandedGroups.has(group) && policies.map(p => {
                const isSelected = chromeSelectedKey === p.key;
                const isConfigured = configuredKeys.includes(p.key);
                return (
                  <div
                    key={p.key}
                    role="button"
                    tabIndex={0}
                    onClick={() => handleChromeSelectPolicy(p)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleChromeSelectPolicy(p); } }}
                    style={{
                      padding: "0.35rem 0.75rem 0.35rem 1.5rem",
                      cursor: "pointer",
                      fontSize: "0.85rem",
                      backgroundColor: isSelected ? "#2d6a4f" : isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent",
                      color: isSelected ? "#fff" : "var(--pf-t--global--text--color--regular)",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.22)" : "rgba(45, 106, 79, 0.1)"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent"; }}
                  >
                    <span style={{ display: "flex", flexDirection: "column", gap: "0.1rem", overflow: "hidden" }}>
                      <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                        {isConfigured && <span style={{ color: isSelected ? "#fff" : "var(--pf-t--global--color--brand--200)", fontSize: "0.7rem" }}>●</span>}
                        {p.label}
                        {p.chromeOnly && (
                          <Label color="blue" isCompact style={{ marginLeft: 8 }}>
                            Chrome only
                          </Label>
                        )}
                      </span>
                      <span style={{ fontSize: "0.72rem", color: isSelected ? "rgba(255,255,255,0.75)" : "var(--pf-t--global--text--color--subtle)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                        {p.description}
                      </span>
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleChromeRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "#6a6e73",
                          padding: "0 0.25rem",
                          minWidth: "auto",
                          flexShrink: 0,
                        }}
                        aria-label={`Remove ${p.label}`}
                      >✕</Button>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {/* Right panel: property editor */}
        <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>
          {renderChromePropertyEditor()}
        </div>
      </div>
    );
  };

  /* ── Edge: property editor ── */
  const renderEdgePropertyEditor = () => {
    if (!edgeSelectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", color: "#6a6e73" }}>
          <Title headingLevel="h3" size="lg">Select a Microsoft Edge policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its value.</p>
        </div>
      );
    }

    const policyDef = EDGE_ALL_POLICIES.find(p => p.key === edgeSelectedKey);
    if (!policyDef) return null;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{policyDef.label}</Title>
        <p style={{ color: "#6a6e73", fontSize: "0.85rem", marginBottom: "1rem" }}>{policyDef.description}</p>
        <Form>
          {policyDef.type === "boolean" && (
            <FormGroup label="Value" fieldId="ed-prop-bool">
              <Switch
                id="ed-prop-bool"
                isChecked={edgeValue === true}
                onChange={(_ev, checked) => updateEdgeValue(checked)}
                label="Enabled"
                labelOff="Disabled"
              />
            </FormGroup>
          )}
          {policyDef.type === "string" && (
            <FormGroup label="Value" fieldId="ed-prop-string">
              <TextInput
                id="ed-prop-string"
                value={(edgeValue as string) || ""}
                onChange={(_ev, val) => updateEdgeValue(val)}
              />
            </FormGroup>
          )}
          {policyDef.type === "integer" && (
            <FormGroup label="Value" fieldId="ed-prop-int">
              <TextInput
                id="ed-prop-int"
                type="number"
                value={String(edgeValue ?? 0)}
                onChange={(_ev, val) => updateEdgeValue(parseInt(val, 10) || 0)}
              />
            </FormGroup>
          )}
          {policyDef.type === "integer-enum" && policyDef.intOptions && (
            <FormGroup label="Value" fieldId="ed-prop-int-enum">
              <FormSelect
                id="ed-prop-int-enum"
                value={String(edgeValue ?? policyDef.intOptions[0]?.value ?? 0)}
                onChange={(_ev, val) => updateEdgeValue(parseInt(val, 10))}
              >
                {policyDef.intOptions.map(opt => (
                  <FormSelectOption key={String(opt.value)} value={String(opt.value)} label={opt.label} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "string-enum" && policyDef.stringOptions && (
            <FormGroup label="Value" fieldId="ed-prop-str-enum">
              <FormSelect
                id="ed-prop-str-enum"
                value={(edgeValue as string) || (policyDef.stringOptions[0] ?? "")}
                onChange={(_ev, val) => updateEdgeValue(val)}
              >
                {policyDef.stringOptions.map(opt => (
                  <FormSelectOption key={opt} value={opt} label={opt} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "list" && (
            <FormGroup label="Value" fieldId="ed-prop-list" helperText="One item per line">
              <TextArea
                id="ed-prop-list"
                value={((edgeValue as string[]) || []).join("\n")}
                onChange={(_ev, val) => updateEdgeValue(val.split("\n").filter(Boolean))}
                rows={5}
                placeholder="One item per line"
              />
            </FormGroup>
          )}
          {policyDef.type === "json" && (
            <FormGroup label="Value (JSON)" fieldId="ed-prop-json" helperText="Enter a valid JSON object or array">
              <TextArea
                id="ed-prop-json"
                validated={jsonFieldInvalid ? "error" : "default"}
                aria-invalid={jsonFieldInvalid || undefined}
                aria-describedby={jsonFieldInvalid ? "ed-prop-json-error" : undefined}
                value={typeof edgeValue === "string" ? edgeValue : JSON.stringify(edgeValue ?? {}, null, 2)}
                onChange={(_ev, val) => {
                  try {
                    updateEdgeValue(JSON.parse(val));
                    setJsonFieldInvalid(false);
                  } catch {
                    // Keep the raw text so the user can fix it, and flag it invalid
                    // so save is blocked until it parses.
                    updateEdgeValue(val);
                    setJsonFieldInvalid(true);
                  }
                }}
                rows={8}
                style={{ fontFamily: "monospace", fontSize: "0.82rem" }}
                placeholder="{}"
              />
              {jsonFieldInvalid && (
                <div id="ed-prop-json-error" aria-live="polite" style={{ color: "var(--pf-t--global--text--color--status--danger--default, #c9190b)", fontSize: "0.85rem", marginTop: 4 }}>
                  Invalid JSON — fix it before saving.
                </div>
              )}
            </FormGroup>
          )}
          {isEditable && (
            <FormGroup fieldId="ed-prop-actions">
              <Button
                variant="danger"
                size="sm"
                onClick={() => handleEdgeRemovePolicy(policyDef.key)}
              >
                Remove Policy
              </Button>
            </FormGroup>
          )}
        </Form>
      </div>
    );
  };

  /* ── Edge: tree view + property editor layout ── */
  const renderEdgeForm = () => {
    const tree = buildEdgeTree();
    const configuredKeys = detectEdgeConfiguredKeys(contentRaw);

    return (
      <div style={{ display: "flex", minHeight: "400px" }}>
        <div style={{
          width: "260px",
          minWidth: "260px",
          borderRight: "1px solid var(--pf-t--global--border--color--default)",
          overflowY: "auto",
          paddingRight: "0",
        }}>
          {Array.from(tree.entries()).map(([group, policies]) => (
            <div key={group} style={{ marginBottom: "2px" }}>
              <div
                role="button"
                tabIndex={0}
                onClick={() => toggleEdgeGroup(group)}
                onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); toggleEdgeGroup(group); } }}
                style={{
                  padding: "0.4rem 0.75rem",
                  cursor: "pointer",
                  fontWeight: 600,
                  fontSize: "0.8rem",
                  textTransform: "uppercase",
                  letterSpacing: "0.03em",
                  color: "var(--pf-t--global--text--color--regular)",
                  backgroundColor: "var(--pf-t--global--background--color--secondary--default)",
                  borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                  userSelect: "none",
                  display: "flex",
                  alignItems: "center",
                  gap: "0.4rem",
                }}
              >
                <span style={{
                  display: "inline-block",
                  width: 0,
                  height: 0,
                  borderStyle: "solid",
                  ...(edgeExpandedGroups.has(group)
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "var(--pf-t--global--text--color--regular) transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent var(--pf-t--global--text--color--regular)" }),
                }} />
                {group}
              </div>
              {edgeExpandedGroups.has(group) && policies.map(p => {
                const isSelected = edgeSelectedKey === p.key;
                const isConfigured = configuredKeys.includes(p.key);
                return (
                  <div
                    key={p.key}
                    role="button"
                    tabIndex={0}
                    onClick={() => handleEdgeSelectPolicy(p)}
                    onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleEdgeSelectPolicy(p); } }}
                    style={{
                      padding: "0.35rem 0.75rem 0.35rem 1.5rem",
                      cursor: "pointer",
                      fontSize: "0.85rem",
                      backgroundColor: isSelected ? "#2d6a4f" : isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent",
                      color: isSelected ? "#fff" : "var(--pf-t--global--text--color--regular)",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.22)" : "rgba(45, 106, 79, 0.1)"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent"; }}
                  >
                    <span style={{ display: "flex", flexDirection: "column", gap: "0.1rem", overflow: "hidden" }}>
                      <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                        {isConfigured && <span style={{ color: isSelected ? "#fff" : "var(--pf-t--global--color--brand--200)", fontSize: "0.7rem" }}>●</span>}
                        {p.label}
                      </span>
                      <span style={{ fontSize: "0.72rem", color: isSelected ? "rgba(255,255,255,0.75)" : "var(--pf-t--global--text--color--subtle)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                        {p.description}
                      </span>
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleEdgeRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "#6a6e73",
                          padding: "0 0.25rem",
                          minWidth: "auto",
                          flexShrink: 0,
                        }}
                        aria-label={`Remove ${p.label}`}
                      >✕</Button>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>
          {renderEdgePropertyEditor()}
        </div>
      </div>
    );
  };

  /* ── Structured form for the selected policy type ── */
  const renderStructuredForm = () => {
    if (policyType === "Firefox") {
      return renderFirefoxForm();
    }
    if (policyType === "Kconfig") {
      return renderKconfigForm();
    }
    if (policyType === "Chrome") {
      return renderChromeForm();
    }
    if (policyType === "Edge") {
      return renderEdgeForm();
    }

    const config = TYPE_CONFIGS[policyType];
    if (!config) {
      return (
        <Alert variant="info" isInline title="No structured form available">
          No structured editor is available for this policy type yet. Use the Raw JSON editor tab.
        </Alert>
      );
    }

    return (
      <div>
        {structuredFieldsList.map((fields, idx) => (
          <Card key={idx} isPlain isCompact style={{ marginBottom: "1rem", border: structuredFieldsList.length > 1 ? "1px solid #d2d2d2" : "none", borderRadius: "4px" }}>
            {structuredFieldsList.length > 1 && (
              <CardTitle style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span>Setting {idx + 1}</span>
                <Button variant="plain" size="sm" onClick={() => handleRemoveSetting(idx)} aria-label={`Remove setting ${idx + 1}`}>
                  ✕
                </Button>
              </CardTitle>
            )}
            <CardBody>
              <Form>
                {config.fields.map((field) => (
                  <FormGroup key={field.key} label={field.label} fieldId={`field-${idx}-${field.key}`}>
                    {field.type === "textarea" ? (
                      <TextArea
                        id={`field-${idx}-${field.key}`}
                        value={fields[field.key] || ""}
                        onChange={(_ev, val) => handleStructuredFieldChange(idx, field.key, val)}
                        rows={4}
                      />
                    ) : field.type === "checkbox" ? (
                      <input
                        type="checkbox"
                        id={`field-${idx}-${field.key}`}
                        checked={fields[field.key] === "true"}
                        onChange={(e) =>
                          handleStructuredFieldChange(idx, field.key, e.target.checked ? "true" : "false")
                        }
                        style={{ marginTop: "0.5rem" }}
                      />
                    ) : field.type === "array" ? (
                      <TextArea
                        id={`field-${idx}-${field.key}`}
                        value={fields[field.key] || ""}
                        onChange={(_ev, val) => handleStructuredFieldChange(idx, field.key, val)}
                        rows={3}
                        placeholder="One item per line"
                      />
                    ) : (
                      <TextInput
                        id={`field-${idx}-${field.key}`}
                        value={fields[field.key] || ""}
                        onChange={(_ev, val) => handleStructuredFieldChange(idx, field.key, val)}
                      />
                    )}
                  </FormGroup>
                ))}
              </Form>
            </CardBody>
          </Card>
        ))}
        <Button variant="secondary" size="sm" onClick={handleAddSetting}>
          + Add Setting
        </Button>
      </div>
    );
  };

  /* ── Raw JSON editor ── */
  const renderRawEditor = () => (
    <div>
      <TextArea
        id="raw-json-editor"
        value={contentRaw}
        onChange={(_ev, val) => syncContentFromRaw(val)}
        rows={16}
        style={{ fontFamily: "monospace", fontSize: "0.85rem" }}
        aria-label="Policy content JSON editor"
      />
      <div aria-live="assertive" aria-atomic="true">
        {validationError && (
          <Alert variant="danger" isInline title="Validation Error" style={{ marginTop: "0.5rem" }}>
            {validationError}
          </Alert>
        )}
      </div>
    </div>
  );

  /* ── Validation preview ── */
  const renderValidationPreview = () => {
    let parsed: unknown = null;
    let isValid = false;
    try {
      parsed = JSON.parse(contentRaw);
      isValid = true;
    } catch {
      /* invalid */
    }

    return (
      <div style={{ marginTop: "1rem" }}>
        <Label color={isValid ? "green" : "red"} style={{ marginBottom: "0.5rem" }}>
          {isValid ? "Valid JSON" : "Invalid JSON"}
        </Label>
        {isValid && (
          <CodeBlock>
            <CodeBlockCode>{JSON.stringify(parsed, null, 2)}</CodeBlockCode>
          </CodeBlock>
        )}
      </div>
    );
  };

  /* ── Overview summary tab (read-only, edit mode only) ── */
  const renderOverviewSummaryTab = () => {
    const rows = buildSettingsRows(policyType, contentRaw);
    const hasLockedColumn = rows.some(r => r.locked !== null);

    return (
      <div style={{ padding: "1rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "1rem" }}>Policy Settings</Title>
        {rows.length > 0 ? (
          <Table aria-label="Policy settings summary" variant="compact">
            <Thead>
              <Tr>
                <Th>Setting</Th>
                <Th>Value</Th>
                {hasLockedColumn && <Th>Locked</Th>}
              </Tr>
            </Thead>
            <Tbody>
              {rows.map((row) => (
                <Tr key={row.setting}>
                  <Td dataLabel="Setting">{row.setting}</Td>
                  <Td dataLabel="Value">{row.value}</Td>
                  {hasLockedColumn && <Td dataLabel="Locked">
                    {row.locked !== null ? (
                      <Label color={row.locked === "Yes" ? "orange" : "grey"} isCompact>
                        {row.locked}
                      </Label>
                    ) : "—"}
                  </Td>}
                </Tr>
              ))}
            </Tbody>
          </Table>
        ) : (
          <Alert variant="info" isInline title="No settings configured">
            This policy does not have any settings configured yet.
          </Alert>
        )}

        {description && (
          <Card isPlain isCompact style={{ marginTop: "1.5rem" }}>
            <CardTitle>Description</CardTitle>
            <CardBody>{description}</CardBody>
          </Card>
        )}
      </div>
    );
  };

  /* ── Overview tab ── */
  const renderOverviewTab = () => (
    <div style={{ padding: "1rem 0" }}>
      <Form>
        <FormGroup label="Name" isRequired fieldId="policy-name">
          <TextInput
            id="policy-name"
            value={name}
            onChange={(_ev, val) => setName(val)}
            placeholder="Enter policy name"
            isRequired
            isDisabled={!isEditable}
          />
        </FormGroup>
        <FormGroup label="Description" fieldId="policy-description">
          <TextArea
            id="policy-description"
            value={description}
            onChange={(_ev, val) => setDescription(val)}
            rows={3}
            placeholder="Describe this policy"
            isDisabled={!isEditable}
          />
        </FormGroup>
        <FormGroup label="Type" isRequired fieldId="policy-type">
          <FormSelect
            id="policy-type"
            value={policyType}
            onChange={(_ev, val) => handleTypeChange(val)}
            isDisabled={!isEditable}
          >
            {POLICY_TYPES.map((t) => (
              <FormSelectOption key={t.value} value={t.value} label={t.label} isDisabled={t.isDisabled} />
            ))}
          </FormSelect>
        </FormGroup>
        <FormGroup label="State" fieldId="policy-status">
          {/* Lifecycle buttons live in the status bar above the tabs. */}
          <Label
            color={status === "released" ? "green" : status === "archived" ? "red" : "blue"}
          >
            {status.charAt(0).toUpperCase() + status.slice(1)}
          </Label>
        </FormGroup>

        {isEditMode && policy && (
          <DescriptionList isHorizontal style={{ marginTop: "1rem" }}>
            <DescriptionListGroup>
              <DescriptionListTerm>Version</DescriptionListTerm>
              <DescriptionListDescription>v{policy.version} (auto-incremented on save)</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Created by</DescriptionListTerm>
              <DescriptionListDescription>{policy.created_by || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Last modified</DescriptionListTerm>
              <DescriptionListDescription>
                {new Date(policy.updated_at).toLocaleString()}
              </DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Created at</DescriptionListTerm>
              <DescriptionListDescription>
                {new Date(policy.created_at).toLocaleString()}
              </DescriptionListDescription>
            </DescriptionListGroup>
          </DescriptionList>
        )}
      </Form>
    </div>
  );

  /* ── Configuration tab ── */
  const renderConfigurationTab = () => {
    const isFirefox = policyType === "Firefox";
    const isThunderbird = policyType === "Thunderbird";
    const isKconfig = policyType === "Kconfig";
    const isChrome = policyType === "Chrome";
    const isEdge = policyType === "Edge";

    // For Firefox / Thunderbird / KConfig / Chrome / Edge: render the tree view directly (they have their own split layout)
    if (isFirefox) {
      return (
        <div style={{ padding: "1rem 0" }}>
          {renderFirefoxForm()}
        </div>
      );
    }
    if (isThunderbird) {
      return (
        <div style={{ padding: "1rem 0" }}>
          {renderThunderbirdForm()}
        </div>
      );
    }
    if (isKconfig) {
      return (
        <div style={{ padding: "1rem 0" }}>
          {renderKconfigForm()}
        </div>
      );
    }
    if (isChrome) {
      return (
        <div style={{ padding: "1rem 0" }}>
          {renderChromeForm()}
        </div>
      );
    }
    if (isEdge) {
      return (
        <div style={{ padding: "1rem 0" }}>
          {renderEdgeForm()}
        </div>
      );
    }
    if (policyType === "Dconf") {
      return (
        <div style={{ padding: "1rem 0" }}>
          <DConfPolicyEditor
            contentRaw={contentRaw}
            onChange={(newRaw) => { setContentRaw(newRaw); }}
            isDisabled={!isEditable}
          />
        </div>
      );
    }
    if (policyType === "Polkit") {
      return (
        <div style={{ padding: "1rem 0" }}>
          <PolkitPolicyEditor
            contentRaw={contentRaw}
            onChange={(newRaw) => { setContentRaw(newRaw); }}
            isDisabled={!isEditable}
          />
        </div>
      );
    }
    if (policyType === "Package") {
      return (
        <div style={{ padding: "1rem 0" }}>
          <PackagePolicyEditor
            contentRaw={contentRaw}
            onChange={(newRaw) => { setContentRaw(newRaw); }}
            isDisabled={!isEditable}
          />
        </div>
      );
    }
    if (policyType === "Firewalld") {
      return (
        <div style={{ padding: "1rem 0" }}>
          <FirewalldPolicyEditor
            contentRaw={contentRaw}
            onChange={(newRaw) => { setContentRaw(newRaw); }}
            isDisabled={!isEditable}
          />
        </div>
      );
    }

    return (
    <div style={{ padding: "1rem 0" }}>
      <Split hasGutter>
        {/* Left panel: type tree / mode toggle */}
        <SplitItem style={{ minWidth: "200px" }}>
          <div style={{ marginBottom: "1rem" }}>
            <strong>Policy Type</strong>
            <div
              style={{
                marginTop: "0.5rem",
                border: "1px solid #d2d2d2",
                borderRadius: "4px",
                overflow: "hidden",
              }}
            >
              {POLICY_TYPES.map((t) => {
                const isActive = policyType === t.value;
                const isHovered = hoveredType === t.value;
                return (
                <div
                  key={t.value}
                  role="button"
                  tabIndex={t.isDisabled ? -1 : 0}
                  onClick={() => { if (!t.isDisabled) handleTypeChange(t.value); }}
                  onKeyDown={(e) => { if (!t.isDisabled && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); handleTypeChange(t.value); } }}
                  onMouseEnter={() => { if (!t.isDisabled) setHoveredType(t.value); }}
                  onMouseLeave={() => setHoveredType(null)}
                  style={{
                    padding: "0.5rem 0.75rem",
                    cursor: t.isDisabled ? "not-allowed" : "pointer",
                    backgroundColor: isActive ? "#2d6a4f" : isHovered ? "rgba(45, 106, 79, 0.1)" : "transparent",
                    color: isActive ? "#fff" : t.isDisabled ? "var(--pf-t--global--text--color--disabled)" : "var(--pf-t--global--text--color--regular)",
                    borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                    fontSize: "0.875rem",
                    fontWeight: isActive ? 600 : 400,
                    userSelect: "none",
                    transition: "background-color 0.15s ease",
                  }}
                >
                  {t.label}
                </div>
                );
              })}
            </div>
          </div>
          <div style={{ marginBottom: "1rem" }}>
            <strong>Editor Mode</strong>
            <div style={{ marginTop: "0.5rem" }}>
              <Button
                variant={configMode === "structured" ? "primary" : "secondary"}
                size="sm"
                onClick={() => setConfigMode("structured")}
                style={{ marginRight: "0.5rem" }}
              >
                Structured
              </Button>
              <Button
                variant={configMode === "raw" ? "primary" : "secondary"}
                size="sm"
                onClick={() => setConfigMode("raw")}
              >
                Raw JSON
              </Button>
            </div>
          </div>
        </SplitItem>

        {/* Right panel: editor */}
        <SplitItem isFilled>
          {configMode === "structured"
              ? renderStructuredForm()
              : renderRawEditor()}
          {renderValidationPreview()}
        </SplitItem>
      </Split>
    </div>
    );
  };

  return (
    <>
    <Modal
      variant={ModalVariant.large}
      isOpen={isOpen}
      onClose={handleClose}
    >
      <ModalHeader title={isEditMode ? `Edit Policy: ${policy?.name}` : "Create Policy"} />
      <ModalBody>
      <div aria-live="assertive" aria-atomic="true">
        {error && (
          <Alert variant="danger" isInline title="Error" style={{ marginBottom: "1rem" }}>
            {error}
          </Alert>
        )}
      </div>

      {/* Lifecycle status bar — visible on every tab */}
      {isEditMode && (
        <Flex
          alignItems={{ default: "alignItemsCenter" }}
          spaceItems={{ default: "spaceItemsSm" }}
          style={{ marginBottom: "1rem" }}
        >
          <FlexItem>
            <Label color={status === "released" ? "green" : status === "archived" ? "red" : "blue"}>
              {status.charAt(0).toUpperCase() + status.slice(1)}
            </Label>
          </FlexItem>
          {status === "draft" && (
            <FlexItem>
              <Button
                variant="primary"
                size="sm"
                onClick={() => handleStateTransition("released")}
                isLoading={saving}
                isDisabled={saving || !name.trim() || !policyType || !policyHasSettings(contentRaw) || hasUnsavedChanges}
              >
                Release
              </Button>
            </FlexItem>
          )}
          {status === "released" && (
            <>
              <FlexItem>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => handleStateTransition("draft")}
                  isLoading={saving}
                  isDisabled={saving || hasUnsavedChanges}
                >
                  Unpublish
                </Button>
              </FlexItem>
              <FlexItem>
                <Button
                  variant="warning"
                  size="sm"
                  onClick={() => handleStateTransition("archived")}
                  isLoading={saving}
                  isDisabled={saving || hasUnsavedChanges}
                >
                  Archive
                </Button>
              </FlexItem>
            </>
          )}
          {status === "archived" && (
            <FlexItem>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => handleStateTransition("draft")}
                isLoading={saving}
                isDisabled={saving || hasUnsavedChanges}
              >
                Restore to draft
              </Button>
            </FlexItem>
          )}
          {hasUnsavedChanges && (
            <FlexItem>
              <span style={{ fontSize: "0.85rem", color: "var(--pf-t--global--text--color--subtle, #6a6e73)" }}>
                Unsaved changes — save to enable lifecycle actions.
              </span>
            </FlexItem>
          )}
        </Flex>
      )}

      <Tabs
        activeKey={activeTab}
        onSelect={(_ev, key) => setActiveTab(key as number)}
        aria-label="Policy details tabs"
      >
        {isEditMode && (
          <Tab eventKey={0} title={<TabTitleText>Overview</TabTitleText>}>
            {renderOverviewSummaryTab()}
          </Tab>
        )}
        <Tab eventKey={isEditMode ? 1 : 0} title={<TabTitleText>Details</TabTitleText>}>
          {renderOverviewTab()}
        </Tab>
        <Tab eventKey={isEditMode ? 2 : 1} title={<TabTitleText>Configuration</TabTitleText>} isDisabled={isEditMode && !isEditable}>
          {renderConfigurationTab()}
        </Tab>
      </Tabs>
      </ModalBody>
      <ModalFooter>
        {isEditable && (
        <Button
          key="save"
          variant="primary"
          onClick={handleSave}
          isLoading={saving}
          isDisabled={saving || !name.trim() || jsonFieldInvalid}
        >
          {isEditMode ? "Save Changes" : "Create Policy"}
        </Button>
        )}
        {isEditMode && (
        <Button
          key="delete"
          variant="danger"
          onClick={() => setShowDeleteConfirm(true)}
          isDisabled={saving}
        >
          Delete
        </Button>
        )}
        <Button key="cancel" variant="link" onClick={handleClose}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>

    {/* Discard unsaved changes confirmation */}
    <Modal
      variant={ModalVariant.small}
      isOpen={showDiscardConfirm}
      onClose={() => setShowDiscardConfirm(false)}
    >
      <ModalHeader title="Discard unsaved changes?" titleIconVariant="warning" />
      <ModalBody>
        You have unsaved changes to this policy. If you close now, those changes
        will be lost.
      </ModalBody>
      <ModalFooter>
        <Button key="discard" variant="danger" onClick={finalizeClose}>
          Discard changes
        </Button>
        <Button key="keep" variant="link" onClick={() => setShowDiscardConfirm(false)}>
          Keep editing
        </Button>
      </ModalFooter>
    </Modal>

    {/* Confirm a policy-type switch that would clear configured content */}
    <Modal
      variant={ModalVariant.small}
      isOpen={pendingTypeChange !== null}
      onClose={() => setPendingTypeChange(null)}
    >
      <ModalHeader title="Change policy type?" titleIconVariant="warning" />
      <ModalBody>
        Switching to <strong>{pendingTypeChange}</strong> will clear the settings
        you&rsquo;ve configured for <strong>{policyType}</strong>. This can&rsquo;t
        be undone.
      </ModalBody>
      <ModalFooter>
        <Button
          key="change-type"
          variant="danger"
          onClick={() => {
            if (pendingTypeChange) applyTypeChange(pendingTypeChange);
            setPendingTypeChange(null);
          }}
        >
          Change type and clear settings
        </Button>
        <Button key="keep-type" variant="link" onClick={() => setPendingTypeChange(null)}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>

    {/* Delete confirmation dialog */}
    <Modal
      variant={ModalVariant.small}
      isOpen={showDeleteConfirm}
      onClose={() => setShowDeleteConfirm(false)}
    >
      <ModalHeader title="Delete Policy" />
      <ModalBody>
      Are you sure you want to delete the policy <strong>{name}</strong>?
      This action cannot be undone. All associated bindings will also be removed.
      </ModalBody>
      <ModalFooter>
        <Button
          key="confirm-delete"
          variant="danger"
          onClick={handleDelete}
          isLoading={saving}
          isDisabled={saving}
        >
          Delete
        </Button>
        <Button key="cancel-delete" variant="link" onClick={() => setShowDeleteConfirm(false)}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
    </>
  );
};

/* ── Helper: flatten object for structured form ── */
function flattenForForm(obj: unknown): Record<string, string> {
  const result: Record<string, string> = {};
  if (obj && typeof obj === "object" && !Array.isArray(obj)) {
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      result[k] = typeof v === "string" ? v : JSON.stringify(v);
    }
  }
  return result;
}
