// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect } from "react";
import {
  Modal,
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
import type { FirefoxPolicy } from "../../generated/proto/firefox";

/* ── Known policy types and their config schemas ── */

const POLICY_TYPES = ["Kconfig", "Dconf", "Firefox", "Polkit", "Chrome", "custom"];

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

// Each Firefox policy definition: the tree will show these grouped by category.
// One defined policy contains exactly ONE Firefox policy setting.
interface FirefoxPolicyDef {
  key: keyof FirefoxPolicy;
  label: string;
  group: string;
  type: "boolean" | "string" | "select" | "object";
  selectOptions?: string[];
  objectFields?: { key: string; label: string; fieldType: "boolean" | "string" | "select" | "stringlist"; selectOptions?: string[] }[];
}

const FIREFOX_ALL_POLICIES: FirefoxPolicyDef[] = [
  // Updates
  { key: "DisableAppUpdate", label: "Disable App Update", group: "Updates", type: "boolean" },
  { key: "AppAutoUpdate", label: "App Auto Update", group: "Updates", type: "boolean" },
  { key: "AppUpdatePin", label: "App Update Pin Version", group: "Updates", type: "string" },
  // Privacy & Telemetry
  { key: "DisableFirefoxStudies", label: "Disable Firefox Studies", group: "Privacy & Telemetry", type: "boolean" },
  { key: "DisablePocket", label: "Disable Pocket", group: "Privacy & Telemetry", type: "boolean" },
  { key: "DisableTelemetry", label: "Disable Telemetry", group: "Privacy & Telemetry", type: "boolean" },
  { key: "DisableFirefoxAccounts", label: "Disable Firefox Accounts", group: "Privacy & Telemetry", type: "boolean" },
  { key: "DisableFormHistory", label: "Disable Form History", group: "Privacy & Telemetry", type: "boolean" },
  // Network
  { key: "CaptivePortal", label: "Enable Captive Portal Detection", group: "Network", type: "boolean" },
  { key: "NetworkPrediction", label: "Enable Network Prediction", group: "Network", type: "boolean" },
  { key: "DNSOverHTTPS", label: "DNS over HTTPS", group: "Network", type: "object", objectFields: [
    { key: "Enabled", label: "Enabled", fieldType: "boolean" },
    { key: "ProviderURL", label: "Provider URL", fieldType: "string" },
    { key: "Locked", label: "Locked", fieldType: "boolean" },
    { key: "Fallback", label: "Fallback", fieldType: "boolean" },
    { key: "ExcludedDomains", label: "Excluded Domains", fieldType: "stringlist" },
  ]},
  // Security
  { key: "DisablePasswordReveal", label: "Disable Password Reveal", group: "Security", type: "boolean" },
  { key: "DisableMasterPasswordCreation", label: "Disable Master Password Creation", group: "Security", type: "boolean" },
  { key: "PasswordManagerEnabled", label: "Enable Password Manager", group: "Security", type: "boolean" },
  { key: "OfferToSaveLogins", label: "Offer to Save Logins", group: "Security", type: "boolean" },
  { key: "SSLVersionMin", label: "SSL Version Minimum", group: "Security", type: "select", selectOptions: ["tls1", "tls1.1", "tls1.2", "tls1.3"] },
  { key: "SSLVersionMax", label: "SSL Version Maximum", group: "Security", type: "select", selectOptions: ["tls1", "tls1.1", "tls1.2", "tls1.3"] },
  // Restrictions
  { key: "BlockAboutAddons", label: "Block about:addons", group: "Restrictions", type: "boolean" },
  { key: "BlockAboutConfig", label: "Block about:config", group: "Restrictions", type: "boolean" },
  { key: "BlockAboutProfiles", label: "Block about:profiles", group: "Restrictions", type: "boolean" },
  { key: "BlockAboutSupport", label: "Block about:support", group: "Restrictions", type: "boolean" },
  // General
  { key: "DontCheckDefaultBrowser", label: "Don't Check Default Browser", group: "General", type: "boolean" },
  { key: "PromptForDownloadLocation", label: "Prompt for Download Location", group: "General", type: "boolean" },
  { key: "HardwareAcceleration", label: "Enable Hardware Acceleration", group: "General", type: "boolean" },
  { key: "NoDefaultBookmarks", label: "No Default Bookmarks", group: "General", type: "boolean" },
  { key: "SearchSuggestEnabled", label: "Enable Search Suggestions", group: "General", type: "boolean" },
  { key: "DefaultDownloadDirectory", label: "Default Download Directory", group: "General", type: "string" },
  // Appearance
  { key: "DisplayBookmarksToolbar", label: "Display Bookmarks Toolbar", group: "Appearance", type: "boolean" },
  { key: "DisplayMenuBar", label: "Display Menu Bar", group: "Appearance", type: "boolean" },
  // Homepage
  { key: "Homepage", label: "Homepage", group: "Homepage & New Tab", type: "object", objectFields: [
    { key: "URL", label: "URL", fieldType: "string" },
    { key: "Locked", label: "Locked", fieldType: "boolean" },
    { key: "StartPage", label: "Start Page", fieldType: "select", selectOptions: ["none", "homepage", "previous-session", "homepage-locked"] },
    { key: "Additional", label: "Additional Homepage URLs", fieldType: "stringlist" },
  ]},
  { key: "FirefoxHome", label: "Firefox Home (New Tab)", group: "Homepage & New Tab", type: "object", objectFields: [
    { key: "Search", label: "Show Search", fieldType: "boolean" },
    { key: "TopSites", label: "Show Top Sites", fieldType: "boolean" },
    { key: "SponsoredTopSites", label: "Show Sponsored Top Sites", fieldType: "boolean" },
    { key: "Highlights", label: "Show Highlights", fieldType: "boolean" },
    { key: "Pocket", label: "Show Pocket", fieldType: "boolean" },
    { key: "SponsoredPocket", label: "Show Sponsored Pocket", fieldType: "boolean" },
    { key: "Snippets", label: "Show Snippets", fieldType: "boolean" },
    { key: "Locked", label: "Lock Settings", fieldType: "boolean" },
  ]},
  // Tracking Protection
  { key: "EnableTrackingProtection", label: "Enhanced Tracking Protection", group: "Tracking Protection", type: "object", objectFields: [
    { key: "Value", label: "Enable", fieldType: "boolean" },
    { key: "Locked", label: "Locked", fieldType: "boolean" },
    { key: "Cryptomining", label: "Block Cryptomining", fieldType: "boolean" },
    { key: "Fingerprinting", label: "Block Fingerprinting", fieldType: "boolean" },
    { key: "EmailTracking", label: "Block Email Tracking", fieldType: "boolean" },
    { key: "Exceptions", label: "Exceptions", fieldType: "stringlist" },
  ]},
  // Extensions
  { key: "Extensions", label: "Extensions", group: "Extensions", type: "object", objectFields: [
    { key: "Install", label: "Install (URLs or extension IDs)", fieldType: "stringlist" },
    { key: "Uninstall", label: "Uninstall (extension IDs)", fieldType: "stringlist" },
    { key: "Locked", label: "Locked (extension IDs)", fieldType: "stringlist" },
  ]},
  // Cookies
  { key: "Cookies", label: "Cookies", group: "Cookies & Popups", type: "object", objectFields: [
    { key: "Behavior", label: "Behavior", fieldType: "select", selectOptions: ["accept", "reject-foreign", "reject-all", "limit-foreign", "reject-tracker", "reject-tracker-and-partition-foreign"] },
    { key: "BehaviorPrivateBrowsing", label: "Behavior (Private Browsing)", fieldType: "select", selectOptions: ["accept", "reject-foreign", "reject-all", "limit-foreign", "reject-tracker", "reject-tracker-and-partition-foreign"] },
    { key: "Allow", label: "Allow Origins", fieldType: "stringlist" },
    { key: "Block", label: "Block Origins", fieldType: "stringlist" },
    { key: "AllowSession", label: "Allow Session Origins", fieldType: "stringlist" },
    { key: "Locked", label: "Locked", fieldType: "boolean" },
  ]},
  // Popup Blocking
  { key: "PopupBlocking", label: "Popup Blocking", group: "Cookies & Popups", type: "object", objectFields: [
    { key: "Default", label: "Block Popups by Default", fieldType: "boolean" },
    { key: "Allow", label: "Allow Exceptions", fieldType: "stringlist" },
    { key: "Locked", label: "Locked", fieldType: "boolean" },
  ]},
];

// Build category groups for the tree view
function buildFirefoxTree(): Map<string, FirefoxPolicyDef[]> {
  const groups = new Map<string, FirefoxPolicyDef[]>();
  for (const p of FIREFOX_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

/* ── Chrome/Chromium policy model ── */

interface ChromePolicyDef {
  key: string;
  label: string;
  group: string;
  type: "boolean" | "string" | "integer" | "string-enum" | "integer-enum" | "list";
  description: string;
  enumOptions?: { value: number | string; label: string }[];
}

const CHROME_ALL_POLICIES: ChromePolicyDef[] = [
  // Homepage & Startup
  { key: "HomepageLocation", label: "Home Page URL", group: "Homepage & Startup", type: "string", description: "Set the home page URL" },
  { key: "HomepageIsNewTabPage", label: "Use New Tab as Home Page", group: "Homepage & Startup", type: "boolean", description: "Use New Tab page as home page" },
  { key: "RestoreOnStartup", label: "Action on Startup", group: "Homepage & Startup", type: "integer-enum", description: "Action on startup", enumOptions: [
    { value: 1, label: "1 – Open new tab" },
    { value: 4, label: "4 – Open URLs from list" },
    { value: 5, label: "5 – Open last session" },
  ]},
  { key: "RestoreOnStartupURLs", label: "Startup URLs", group: "Homepage & Startup", type: "list", description: "URLs to open on startup" },
  { key: "ShowHomeButton", label: "Show Home Button", group: "Homepage & Startup", type: "boolean", description: "Show Home button in toolbar" },
  { key: "NewTabPageLocation", label: "New Tab Page URL", group: "Homepage & Startup", type: "string", description: "Set the new tab page URL" },
  // Privacy & Security
  { key: "SafeBrowsingProtectionLevel", label: "Safe Browsing Level", group: "Privacy & Security", type: "integer-enum", description: "Safe Browsing protection level", enumOptions: [
    { value: 0, label: "0 – No protection" },
    { value: 1, label: "1 – Standard" },
    { value: 2, label: "2 – Enhanced" },
  ]},
  { key: "DefaultCookiesSetting", label: "Default Cookies Setting", group: "Privacy & Security", type: "integer-enum", description: "Default cookies setting", enumOptions: [
    { value: 1, label: "1 – Allow" },
    { value: 2, label: "2 – Block" },
    { value: 4, label: "4 – Session only" },
  ]},
  { key: "DefaultJavaScriptSetting", label: "Default JavaScript Setting", group: "Privacy & Security", type: "integer-enum", description: "Default JavaScript setting", enumOptions: [
    { value: 1, label: "1 – Allow" },
    { value: 2, label: "2 – Block" },
  ]},
  { key: "DefaultPopupsSetting", label: "Default Pop-ups Setting", group: "Privacy & Security", type: "integer-enum", description: "Default pop-ups setting", enumOptions: [
    { value: 1, label: "1 – Allow" },
    { value: 2, label: "2 – Block" },
  ]},
  { key: "DefaultGeolocationSetting", label: "Default Geolocation Setting", group: "Privacy & Security", type: "integer-enum", description: "Default geolocation setting", enumOptions: [
    { value: 1, label: "1 – Allow" },
    { value: 2, label: "2 – Block" },
    { value: 3, label: "3 – Ask" },
  ]},
  { key: "DefaultNotificationsSetting", label: "Default Notifications Setting", group: "Privacy & Security", type: "integer-enum", description: "Default notifications setting", enumOptions: [
    { value: 1, label: "1 – Allow" },
    { value: 2, label: "2 – Block" },
    { value: 3, label: "3 – Ask" },
  ]},
  { key: "IncognitoModeAvailability", label: "Incognito Mode", group: "Privacy & Security", type: "integer-enum", description: "Incognito mode availability", enumOptions: [
    { value: 0, label: "0 – Available" },
    { value: 1, label: "1 – Disabled" },
    { value: 2, label: "2 – Forced" },
  ]},
  { key: "BlockThirdPartyCookies", label: "Block Third-Party Cookies", group: "Privacy & Security", type: "boolean", description: "Block third-party cookies" },
  { key: "SitePerProcess", label: "Enable Site Isolation", group: "Privacy & Security", type: "boolean", description: "Enable site isolation" },
  { key: "DNSInterceptionChecksEnabled", label: "DNS Interception Checks", group: "Privacy & Security", type: "boolean", description: "Enable DNS interception checks" },
  // Extensions
  { key: "ExtensionInstallForcelist", label: "Force-Install Extensions", group: "Extensions", type: "list", description: "Force-install extensions (extension ID;update URL)" },
  { key: "ExtensionInstallAllowlist", label: "Allowed Extensions", group: "Extensions", type: "list", description: "Allowed extensions (IDs)" },
  { key: "ExtensionInstallBlocklist", label: "Blocked Extensions", group: "Extensions", type: "list", description: "Blocked extensions (use [\"*\"] to block all)" },
  { key: "ExtensionInstallSources", label: "Extension Install Sources", group: "Extensions", type: "list", description: "Allowed extension install sources" },
  { key: "ExtensionManifestV2Availability", label: "Manifest V2 Availability", group: "Extensions", type: "integer-enum", description: "Manifest V2 extension availability", enumOptions: [
    { value: 0, label: "0 – Default" },
    { value: 1, label: "1 – Disabled" },
    { value: 2, label: "2 – Enabled" },
    { value: 3, label: "3 – Enabled for forced" },
  ]},
  // Password Manager
  { key: "PasswordManagerEnabled", label: "Enable Password Manager", group: "Password Manager", type: "boolean", description: "Enable password manager" },
  { key: "PasswordLeakDetectionEnabled", label: "Password Leak Detection", group: "Password Manager", type: "boolean", description: "Enable password leak detection" },
  // Downloads
  { key: "DownloadDirectory", label: "Download Directory", group: "Downloads", type: "string", description: "Default download directory" },
  { key: "PromptForDownloadLocation", label: "Prompt for Download Location", group: "Downloads", type: "boolean", description: "Ask where to save each file before downloading" },
  { key: "DownloadRestrictions", label: "Download Restrictions", group: "Downloads", type: "integer-enum", description: "Download restrictions", enumOptions: [
    { value: 0, label: "0 – No restrictions" },
    { value: 1, label: "1 – Block dangerous" },
    { value: 2, label: "2 – Block dangerous and unwanted" },
    { value: 3, label: "3 – Block all" },
  ]},
  // Network & Proxy
  { key: "ProxyMode", label: "Proxy Mode", group: "Network & Proxy", type: "string-enum", description: "Proxy mode", enumOptions: [
    { value: "direct", label: "direct" },
    { value: "auto_detect", label: "auto_detect" },
    { value: "pac_script", label: "pac_script" },
    { value: "fixed_servers", label: "fixed_servers" },
    { value: "system", label: "system" },
  ]},
  { key: "ProxyServer", label: "Proxy Server", group: "Network & Proxy", type: "string", description: "Proxy server address (host:port)" },
  { key: "ProxyPacUrl", label: "Proxy PAC URL", group: "Network & Proxy", type: "string", description: "Proxy PAC file URL" },
  { key: "ProxyBypassList", label: "Proxy Bypass List", group: "Network & Proxy", type: "string", description: "Proxy bypass list (semicolon-separated)" },
  { key: "DnsOverHttpsMode", label: "DNS over HTTPS Mode", group: "Network & Proxy", type: "string-enum", description: "DNS over HTTPS mode", enumOptions: [
    { value: "off", label: "off" },
    { value: "automatic", label: "automatic" },
    { value: "secure", label: "secure" },
  ]},
  { key: "DnsOverHttpsTemplates", label: "DNS over HTTPS Templates", group: "Network & Proxy", type: "string", description: "DNS over HTTPS template URIs" },
  // User & Sync
  { key: "BrowserSignin", label: "Browser Sign-in", group: "User & Sync", type: "integer-enum", description: "Browser sign-in settings", enumOptions: [
    { value: 0, label: "0 – Disable" },
    { value: 1, label: "1 – Enable" },
    { value: 2, label: "2 – Force" },
  ]},
  { key: "SyncDisabled", label: "Disable Chrome Sync", group: "User & Sync", type: "boolean", description: "Disable Chrome Sync" },
  { key: "AutofillAddressEnabled", label: "Address Autofill", group: "User & Sync", type: "boolean", description: "Enable address autofill" },
  { key: "AutofillCreditCardEnabled", label: "Credit Card Autofill", group: "User & Sync", type: "boolean", description: "Enable credit card autofill" },
  { key: "UserFeedbackAllowed", label: "Allow User Feedback", group: "User & Sync", type: "boolean", description: "Allow user feedback" },
  // Search
  { key: "DefaultSearchProviderEnabled", label: "Enable Default Search Provider", group: "Search", type: "boolean", description: "Enable default search provider" },
  { key: "DefaultSearchProviderName", label: "Search Provider Name", group: "Search", type: "string", description: "Default search provider name" },
  { key: "DefaultSearchProviderSearchURL", label: "Search Provider URL", group: "Search", type: "string", description: "Default search provider URL (use {searchTerms})" },
  { key: "DefaultSearchProviderKeyword", label: "Search Provider Keyword", group: "Search", type: "string", description: "Default search provider keyword" },
  // Browser & Display
  { key: "BookmarkBarEnabled", label: "Enable Bookmark Bar", group: "Browser & Display", type: "boolean", description: "Enable bookmark bar" },
  { key: "ShowAppsShortcutInBookmarkBar", label: "Show Apps Shortcut", group: "Browser & Display", type: "boolean", description: "Show apps shortcut in bookmark bar" },
  { key: "DeveloperToolsAvailability", label: "Developer Tools", group: "Browser & Display", type: "integer-enum", description: "Developer tools availability", enumOptions: [
    { value: 0, label: "0 – Disallow" },
    { value: 1, label: "1 – Allow" },
    { value: 2, label: "2 – Allow for extensions" },
  ]},
  { key: "PrintingEnabled", label: "Enable Printing", group: "Browser & Display", type: "boolean", description: "Enable printing" },
  { key: "DefaultBrowserSettingEnabled", label: "Default Browser Check", group: "Browser & Display", type: "boolean", description: "Allow Chrome to check if it is the default browser" },
  { key: "FullscreenAllowed", label: "Allow Full Screen", group: "Browser & Display", type: "boolean", description: "Allow full screen mode" },
  { key: "MetricsReportingEnabled", label: "Metrics Reporting", group: "Browser & Display", type: "boolean", description: "Enable usage and crash-related data reporting" },
];

function buildChromeTree(): Map<string, ChromePolicyDef[]> {
  const groups = new Map<string, ChromePolicyDef[]>();
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

/* ── KDE Kiosk (KConfig) policy model ── */

interface KConfigPolicyDef {
  key: string;
  label: string;
  group: string;
  file: string;
  iniGroup: string;
  iniKey: string;
  type: "boolean" | "string" | "select" | "int";
  selectOptions?: string[];
  defaultValue?: string;
}

const KCONFIG_ALL_POLICIES: KConfigPolicyDef[] = [
  // Action Restrictions (kdeglobals)
  { key: "shell_access", label: "Shell Access", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "shell_access", type: "boolean" },
  { key: "run_command", label: "Run Command (KRunner)", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "run_command", type: "boolean" },
  { key: "action/logout", label: "Logout Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/logout", type: "boolean" },
  { key: "action/file_new", label: "File New Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/file_new", type: "boolean" },
  { key: "action/file_open", label: "File Open Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/file_open", type: "boolean" },
  { key: "action/file_save", label: "File Save Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/file_save", type: "boolean" },
  // Control Module Restrictions (kdeglobals)
  { key: "kcm_mouse", label: "Mouse Settings", group: "Control Module Restrictions", file: "kdeglobals", iniGroup: "KDE Control Module Restrictions", iniKey: "kcm_mouse", type: "boolean" },
  { key: "kcm_keyboard", label: "Keyboard Settings", group: "Control Module Restrictions", file: "kdeglobals", iniGroup: "KDE Control Module Restrictions", iniKey: "kcm_keyboard", type: "boolean" },
  { key: "kcm_kscreen", label: "Display Settings", group: "Control Module Restrictions", file: "kdeglobals", iniGroup: "KDE Control Module Restrictions", iniKey: "kcm_kscreen", type: "boolean" },
  // Resource Restrictions (kdeglobals)
  { key: "wallpaper", label: "Wallpaper Changes", group: "Resource Restrictions", file: "kdeglobals", iniGroup: "KDE Resource Restrictions", iniKey: "wallpaper", type: "boolean" },
  { key: "icons", label: "Icon Changes", group: "Resource Restrictions", file: "kdeglobals", iniGroup: "KDE Resource Restrictions", iniKey: "icons", type: "boolean" },
  { key: "autostart", label: "Autostart Changes", group: "Resource Restrictions", file: "kdeglobals", iniGroup: "KDE Resource Restrictions", iniKey: "autostart", type: "boolean" },
  { key: "colors", label: "Color Scheme Changes", group: "Resource Restrictions", file: "kdeglobals", iniGroup: "KDE Resource Restrictions", iniKey: "colors", type: "boolean" },
  { key: "cursors", label: "Cursor Theme Changes", group: "Resource Restrictions", file: "kdeglobals", iniGroup: "KDE Resource Restrictions", iniKey: "cursors", type: "boolean" },
  // Window Manager (kwinrc)
  { key: "BorderlessMaximizedWindows", label: "Borderless Maximized Windows", group: "Window Manager", file: "kwinrc", iniGroup: "Windows", iniKey: "BorderlessMaximizedWindows", type: "boolean" },
  // Desktop (plasmarc)
  { key: "plasmoidUnlockedDesktop", label: "Unlock Desktop Widgets", group: "Desktop", file: "plasmarc", iniGroup: "General", iniKey: "plasmoidUnlockedDesktop", type: "boolean" },
  { key: "allow_configure_when_locked", label: "Configure When Locked", group: "Desktop", file: "plasmarc", iniGroup: "General", iniKey: "allow_configure_when_locked", type: "boolean" },
  // Screen Lock (kscreenlockerrc)
  { key: "AutoLock", label: "Auto Lock", group: "Screen Lock", file: "kscreenlockerrc", iniGroup: "Daemon", iniKey: "AutoLock", type: "boolean" },
  { key: "LockOnResume", label: "Lock on Resume", group: "Screen Lock", file: "kscreenlockerrc", iniGroup: "Daemon", iniKey: "LockOnResume", type: "boolean" },
  { key: "Timeout", label: "Lock Timeout (seconds)", group: "Screen Lock", file: "kscreenlockerrc", iniGroup: "Daemon", iniKey: "Timeout", type: "int" },
];

function buildKConfigTree(): Map<string, KConfigPolicyDef[]> {
  const groups = new Map<string, KConfigPolicyDef[]>();
  for (const p of KCONFIG_ALL_POLICIES) {
    const arr = groups.get(p.group) || [];
    arr.push(p);
    groups.set(p.group, arr);
  }
  return groups;
}

// Detect configured KConfig policy def keys from content JSON.
// Returns the policyDef.key (not the iniKey) for each configured entry.
function detectKConfigConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    const entries: { key?: string }[] = parsed.entries || [];
    const result: string[] = [];
    for (const e of entries) {
      // Find the policy def whose iniKey matches the stored key
      const def = KCONFIG_ALL_POLICIES.find(p => p.iniKey === e.key);
      if (def) result.push(def.key);
    }
    return result;
  } catch { return []; }
}

// Extract the value + enforced state for a KConfig policy by def key
function extractKConfigEntry(content: string, defKey: string): { value: string; enforced: boolean } | undefined {
  try {
    const def = KCONFIG_ALL_POLICIES.find(p => p.key === defKey);
    if (!def) return undefined;
    const parsed = JSON.parse(content || "{}");
    const entries: { key?: string; value?: string; enforced?: boolean }[] = parsed.entries || [];
    const entry = entries.find(e => e.key === def.iniKey);
    if (!entry) return undefined;
    return { value: entry.value ?? "", enforced: entry.enforced ?? false };
  } catch { return undefined; }
}

// Build KConfig content JSON by adding/updating an entry
function buildKConfigContent(policyDef: KConfigPolicyDef, value: string, enforced: boolean, existingContent?: string): string {
  let parsed: { entries: { file: string; group: string; key: string; value: string; type: string; enforced: boolean }[] } = { entries: [] };
  try { parsed = JSON.parse(existingContent || '{"entries":[]}'); } catch { /* ignore */ }
  if (!parsed.entries) parsed.entries = [];

  const idx = parsed.entries.findIndex(e => e.key === policyDef.iniKey);
  const entry = {
    file: policyDef.file,
    group: policyDef.iniGroup,
    key: policyDef.iniKey,
    value,
    type: policyDef.type === "boolean" ? "bool" : policyDef.type === "int" ? "int" : "string",
    enforced,
  };

  if (idx >= 0) {
    parsed.entries[idx] = entry;
  } else {
    parsed.entries.push(entry);
  }
  return JSON.stringify(parsed, null, 2);
}

// Remove a KConfig entry from content JSON by policy def key
function removeKConfigContentKey(defKey: string, existingContent: string): string {
  let parsed: { entries: { key?: string }[] } = { entries: [] };
  try { parsed = JSON.parse(existingContent || '{"entries":[]}'); } catch { /* ignore */ }
  if (!parsed.entries) parsed.entries = [];

  const policyDef = KCONFIG_ALL_POLICIES.find(p => p.key === defKey);
  if (policyDef) {
    parsed.entries = parsed.entries.filter(e => e.key !== policyDef.iniKey);
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
        for (const field of policyDef.objectFields || []) {
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
      } else if (policyDef.type === "integer-enum" && policyDef.enumOptions) {
        const opt = policyDef.enumOptions.find(o => o.value === val);
        displayVal = opt ? opt.label : formatDisplayValue(val);
      } else if (policyDef.type === "string-enum" && policyDef.enumOptions) {
        const opt = policyDef.enumOptions.find(o => o.value === val);
        displayVal = opt ? opt.label : formatDisplayValue(val);
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
    const parsed = raw as { entries?: { key?: string; value?: string; enforced?: boolean; file?: string; group?: string }[] };
    const entries = parsed.entries || [];
    for (const entry of entries) {
      const def = KCONFIG_ALL_POLICIES.find(p => p.iniKey === entry.key);
      rows.push({
        setting: def ? `${def.group} › ${def.label}` : (entry.key || "Unknown"),
        value: formatDisplayValue(entry.value),
        locked: entry.enforced !== undefined ? (entry.enforced ? "Yes" : "No") : null,
      });
    }
    return rows;
  }

  // Normalize to array for multi-setting support (backward compat with single-object)
  const items: Record<string, unknown>[] = Array.isArray(raw)
    ? raw
    : [raw as Record<string, unknown>];

  if (policyType === "Dconf") {
    const config = TYPE_CONFIGS["Dconf"];
    for (const [idx, parsed] of items.entries()) {
      const prefix = items.length > 1 ? `Setting ${idx + 1} › ` : "";
      const lockedVal = parsed["lock"] !== undefined
        ? (parsed["lock"] === "true" || parsed["lock"] === true ? "Yes" : "No")
        : null;
      for (const field of config.fields) {
        if (field.key === "lock") continue;
        rows.push({
          setting: prefix + field.label,
          value: formatDisplayValue(parsed[field.key]),
          locked: lockedVal,
        });
      }
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

  // Derived: whether policy fields are editable (create mode or DRAFT state)
  const isEditable = !isEditMode || status === "draft";

  // Firefox-specific state: selected policy key + its value
  const [firefoxSelectedKey, setFirefoxSelectedKey] = useState<string | null>(null);
  const [firefoxValue, setFirefoxValue] = useState<unknown>(undefined);
  const [firefoxExpandedGroups, setFirefoxExpandedGroups] = useState<Set<string>>(new Set());

  // KConfig-specific state: selected policy key, value + enforced
  const [kconfigSelectedKey, setKconfigSelectedKey] = useState<string | null>(null);
  const [kconfigValue, setKconfigValue] = useState<string>("");
  const [kconfigEnforced, setKconfigEnforced] = useState<boolean>(false);
  const [kconfigExpandedGroups, setKconfigExpandedGroups] = useState<Set<string>>(new Set());

  // Chrome-specific state: selected policy key + its value
  const [chromeSelectedKey, setChromeSelectedKey] = useState<string | null>(null);
  const [chromeValue, setChromeValue] = useState<unknown>(undefined);
  const [chromeExpandedGroups, setChromeExpandedGroups] = useState<Set<string>>(new Set());

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
        } else {
          setKconfigSelectedKey(null);
          setKconfigValue("");
          setKconfigEnforced(false);
          setKconfigExpandedGroups(new Set());
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
      setKconfigSelectedKey(null);
      setKconfigValue("");
      setKconfigEnforced(false);
      setKconfigExpandedGroups(new Set());
      setChromeSelectedKey(null);
      setChromeValue(undefined);
      setChromeExpandedGroups(new Set());
    }
    setActiveTab(0);
    setConfigMode("structured");
    setError(null);
    setValidationError(null);
    setShowDeleteConfirm(false);
    setDirty(false);
  }, [isOpen, policy]);

  // When policy type changes, reset content for the target type
  const handleTypeChange = (newType: string) => {
    setPolicyType(newType);
    if (newType === "Firefox") {
      setFirefoxSelectedKey(null);
      setFirefoxValue(undefined);
      setContentRaw("{}");
      setFirefoxExpandedGroups(new Set());
    } else if (newType === "Kconfig") {
      setKconfigSelectedKey(null);
      setKconfigValue("");
      setKconfigEnforced(false);
      setContentRaw('{"entries":[]}');
      setKconfigExpandedGroups(new Set());
    } else if (newType === "Chrome") {
      setChromeSelectedKey(null);
      setChromeValue(undefined);
      setContentRaw("{}");
      setChromeExpandedGroups(new Set());
    } else {
      setStructuredFieldsList([{}]);
      setContentRaw(JSON.stringify([{}], null, 2));
    }
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
      if (policyType !== "Firefox") {
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
  const handleFirefoxSelectPolicy = (policyDef: FirefoxPolicyDef) => {
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
      } else if (policyDef.type === "select") {
        defaultValue = policyDef.selectOptions?.[0] || "";
      } else if (policyDef.type === "object") {
        const obj: Record<string, unknown> = {};
        for (const f of policyDef.objectFields || []) {
          if (f.fieldType === "boolean") obj[f.key] = false;
          else if (f.fieldType === "string" || f.fieldType === "select") obj[f.key] = "";
          else if (f.fieldType === "stringlist") obj[f.key] = [];
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

  // KConfig: select a policy from the tree
  const handleKconfigSelectPolicy = (policyDef: KConfigPolicyDef) => {
    setKconfigSelectedKey(policyDef.key);
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
  const handleChromeSelectPolicy = (policyDef: ChromePolicyDef) => {
    setChromeSelectedKey(policyDef.key);
    const existing = extractChromeValue(contentRaw, policyDef.key);
    if (existing !== undefined) {
      setChromeValue(existing);
    } else {
      let defaultValue: unknown;
      if (policyDef.type === "boolean") {
        defaultValue = true;
      } else if (policyDef.type === "string" || policyDef.type === "string-enum") {
        defaultValue = policyDef.enumOptions ? policyDef.enumOptions[0]?.value ?? "" : "";
      } else if (policyDef.type === "integer" || policyDef.type === "integer-enum") {
        defaultValue = policyDef.enumOptions ? policyDef.enumOptions[0]?.value ?? 0 : 0;
      } else if (policyDef.type === "list") {
        defaultValue = [];
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

  const handleSave = async () => {
    setError(null);
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
      } else if (policyType === "Kconfig") {
        // Save the current selection into content before validating
        if (kconfigSelectedKey) {
          const def = KCONFIG_ALL_POLICIES.find(p => p.key === kconfigSelectedKey);
          if (def) {
            finalContent = buildKConfigContent(def, kconfigValue, kconfigEnforced, contentRaw);
          }
        }
        try {
          const parsed = JSON.parse(finalContent);
          if (!parsed.entries || parsed.entries.length === 0) {
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

  /* ── Close handler: refresh parent list if state was changed ── */
  const handleClose = () => {
    if (dirty) onSaved();
    onClose();
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
          {policyDef.type === "select" && (
            <FormGroup label="Value" fieldId="ff-prop-select">
              <FormSelect
                id="ff-prop-select"
                value={(firefoxValue as string) || ""}
                onChange={(_ev, val) => updateFirefoxValue(val)}
              >
                <FormSelectOption key="" value="" label="(not set)" />
                {(policyDef.selectOptions || []).map(v => (
                  <FormSelectOption key={v} value={v} label={v} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "object" && policyDef.objectFields && (
            <>
              {policyDef.objectFields.map(field => {
                const objVal = (firefoxValue as Record<string, unknown>) || {};
                return (
                  <FormGroup key={field.key} label={field.label} fieldId={`ff-prop-${field.key}`}>
                    {field.fieldType === "boolean" && (
                      <Switch
                        id={`ff-prop-${field.key}`}
                        isChecked={objVal[field.key] === true}
                        onChange={(_ev, checked) => updateFirefoxValue({ ...objVal, [field.key]: checked })}
                        label="Yes"
                        labelOff="No"
                      />
                    )}
                    {field.fieldType === "string" && (
                      <TextInput
                        id={`ff-prop-${field.key}`}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateFirefoxValue({ ...objVal, [field.key]: val })}
                      />
                    )}
                    {field.fieldType === "select" && (
                      <FormSelect
                        id={`ff-prop-${field.key}`}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateFirefoxValue({ ...objVal, [field.key]: val })}
                      >
                        <FormSelectOption key="" value="" label="(not set)" />
                        {(field.selectOptions || []).map(v => (
                          <FormSelectOption key={v} value={v} label={v} />
                        ))}
                      </FormSelect>
                    )}
                    {field.fieldType === "stringlist" && (
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
          borderRight: "1px solid #d2d2d2",
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
                  color: "#151515",
                  backgroundColor: "#f0f0f0",
                  borderBottom: "1px solid #d2d2d2",
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
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "#151515 transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent #151515" }),
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
                      backgroundColor: isSelected ? "#0066cc" : isConfigured ? "#e7f1fa" : "transparent",
                      color: isSelected ? "#fff" : "#151515",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid #e8e8e8",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "#d2e4f5" : "#e7f1fa"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "#e7f1fa" : "transparent"; }}
                  >
                    <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                      {isConfigured && <span style={{ color: isSelected ? "#fff" : "#0066cc", fontSize: "0.7rem" }}>●</span>}
                      {p.label}
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleFirefoxRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "#6a6e73",
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

  /* ── KConfig: property editor for selected policy ── */
  const renderKconfigPropertyEditor = () => {
    if (!kconfigSelectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", color: "#6a6e73" }}>
          <Title headingLevel="h3" size="lg">Select a KDE Kiosk policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its properties.</p>
        </div>
      );
    }

    const policyDef = KCONFIG_ALL_POLICIES.find(p => p.key === kconfigSelectedKey);
    if (!policyDef) return null;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{policyDef.label}</Title>
        <p style={{ color: "#6a6e73", fontSize: "0.85rem", marginBottom: "1rem" }}>
          File: <code>{policyDef.file}</code> &nbsp; Group: <code>[{policyDef.iniGroup}]</code>
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
                  <FormSelectOption key={v} value={v} label={v} />
                ))}
              </FormSelect>
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
          borderRight: "1px solid #d2d2d2",
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
                  color: "#151515",
                  backgroundColor: "#f0f0f0",
                  borderBottom: "1px solid #d2d2d2",
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
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "#151515 transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent #151515" }),
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
                      backgroundColor: isSelected ? "#0066cc" : isConfigured ? "#e7f1fa" : "transparent",
                      color: isSelected ? "#fff" : "#151515",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid #e8e8e8",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "#d2e4f5" : "#e7f1fa"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "#e7f1fa" : "transparent"; }}
                  >
                    <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                      {isConfigured && <span style={{ color: isSelected ? "#fff" : "#0066cc", fontSize: "0.7rem" }}>●</span>}
                      {p.label}
                    </span>
                    {isConfigured && (
                      <Button
                        variant="plain"
                        size="sm"
                        onClick={(e) => { e.stopPropagation(); handleKconfigRemovePolicy(p.key); }}
                        style={{
                          fontSize: "0.75rem",
                          color: isSelected ? "#fff" : "#6a6e73",
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
          {policyDef.type === "integer-enum" && policyDef.enumOptions && (
            <FormGroup label="Value" fieldId="cr-prop-int-enum">
              <FormSelect
                id="cr-prop-int-enum"
                value={String(chromeValue ?? policyDef.enumOptions[0]?.value ?? 0)}
                onChange={(_ev, val) => updateChromeValue(parseInt(val, 10))}
              >
                {policyDef.enumOptions.map(opt => (
                  <FormSelectOption key={String(opt.value)} value={String(opt.value)} label={opt.label} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {policyDef.type === "string-enum" && policyDef.enumOptions && (
            <FormGroup label="Value" fieldId="cr-prop-str-enum">
              <FormSelect
                id="cr-prop-str-enum"
                value={(chromeValue as string) || String(policyDef.enumOptions[0]?.value ?? "")}
                onChange={(_ev, val) => updateChromeValue(val)}
              >
                {policyDef.enumOptions.map(opt => (
                  <FormSelectOption key={String(opt.value)} value={String(opt.value)} label={opt.label} />
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
          borderRight: "1px solid #d2d2d2",
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
                  color: "#151515",
                  backgroundColor: "#f0f0f0",
                  borderBottom: "1px solid #d2d2d2",
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
                    ? { borderWidth: "5px 4px 0 4px", borderColor: "#151515 transparent transparent transparent" }
                    : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent #151515" }),
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
                      backgroundColor: isSelected ? "#0066cc" : isConfigured ? "#e7f1fa" : "transparent",
                      color: isSelected ? "#fff" : "#151515",
                      fontWeight: isSelected || isConfigured ? 600 : 400,
                      borderBottom: "1px solid #e8e8e8",
                      userSelect: "none",
                      transition: "background-color 0.1s ease",
                      display: "flex",
                      alignItems: "center",
                      justifyContent: "space-between",
                    }}
                    onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "#d2e4f5" : "#e7f1fa"; }}
                    onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "#e7f1fa" : "transparent"; }}
                  >
                    <span style={{ display: "flex", flexDirection: "column", gap: "0.1rem", overflow: "hidden" }}>
                      <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                        {isConfigured && <span style={{ color: isSelected ? "#fff" : "#0066cc", fontSize: "0.7rem" }}>●</span>}
                        {p.label}
                      </span>
                      <span style={{ fontSize: "0.72rem", color: isSelected ? "rgba(255,255,255,0.75)" : "#6a6e73", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
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

    const config = TYPE_CONFIGS[policyType];
    if (!config) {
      return (
        <Alert variant="info" isInline title="No structured form available">
          Use the Raw JSON editor for custom policy types.
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
      {validationError && (
        <Alert variant="danger" isInline title="Validation Error" style={{ marginTop: "0.5rem" }}>
          {validationError}
        </Alert>
      )}
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
              <FormSelectOption key={t} value={t} label={t} />
            ))}
          </FormSelect>
        </FormGroup>
        <FormGroup label="State" fieldId="policy-status">
          <Flex alignItems={{ default: "alignItemsCenter" }} spaceItems={{ default: "spaceItemsSm" }}>
            <FlexItem>
              <Label
                color={status === "released" ? "green" : status === "archived" ? "red" : "blue"}
              >
                {status.charAt(0).toUpperCase() + status.slice(1)}
              </Label>
            </FlexItem>
            {isEditMode && status === "draft" && (
              <FlexItem>
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => handleStateTransition("released")}
                  isLoading={saving}
                  isDisabled={saving || !name.trim() || !policyType || !contentRaw.trim()}
                >
                  Release
                </Button>
              </FlexItem>
            )}
            {isEditMode && status === "released" && (
              <>
                <FlexItem>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => handleStateTransition("draft")}
                    isLoading={saving}
                    isDisabled={saving}
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
                    isDisabled={saving}
                  >
                    Archive
                  </Button>
                </FlexItem>
              </>
            )}
          </Flex>
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
    const isKconfig = policyType === "Kconfig";
    const isChrome = policyType === "Chrome";

    // For Firefox / KConfig / Chrome: render the tree view directly (they have their own split layout)
    if (isFirefox) {
      return (
        <div style={{ padding: "1rem 0" }}>
          {renderFirefoxForm()}
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
                const isActive = policyType === t;
                const isHovered = hoveredType === t;
                return (
                <div
                  key={t}
                  role="button"
                  tabIndex={0}
                  onClick={() => handleTypeChange(t)}
                  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); handleTypeChange(t); } }}
                  onMouseEnter={() => setHoveredType(t)}
                  onMouseLeave={() => setHoveredType(null)}
                  style={{
                    padding: "0.5rem 0.75rem",
                    cursor: "pointer",
                    backgroundColor: isActive ? "#0066cc" : isHovered ? "#e7f1fa" : "transparent",
                    color: isActive ? "#fff" : "inherit",
                    borderBottom: "1px solid #d2d2d2",
                    fontSize: "0.875rem",
                    fontWeight: isActive ? 600 : 400,
                    userSelect: "none",
                    transition: "background-color 0.15s ease",
                  }}
                >
                  {t}
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
      title={isEditMode ? `Edit Policy: ${policy?.name}` : "Create Policy"}
      isOpen={isOpen}
      onClose={handleClose}
      actions={[
        ...(isEditable ? [
        <Button
          key="save"
          variant="primary"
          onClick={handleSave}
          isLoading={saving}
          isDisabled={saving || !name.trim()}
        >
          {isEditMode ? "Save Changes" : "Create Policy"}
        </Button>,
        ] : []),
        ...(isEditMode ? [
        <Button
          key="delete"
          variant="danger"
          onClick={() => setShowDeleteConfirm(true)}
          isDisabled={saving}
        >
          Delete
        </Button>,
        ] : []),
        <Button key="cancel" variant="link" onClick={handleClose}>
          Cancel
        </Button>,
      ]}
    >
      {error && (
        <Alert variant="danger" isInline title="Error" style={{ marginBottom: "1rem" }}>
          {error}
        </Alert>
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
    </Modal>

    {/* Delete confirmation dialog */}
    <Modal
      variant={ModalVariant.small}
      title="Delete Policy"
      isOpen={showDeleteConfirm}
      onClose={() => setShowDeleteConfirm(false)}
      actions={[
        <Button
          key="confirm-delete"
          variant="danger"
          onClick={handleDelete}
          isLoading={saving}
          isDisabled={saving}
        >
          Delete
        </Button>,
        <Button key="cancel-delete" variant="link" onClick={() => setShowDeleteConfirm(false)}>
          Cancel
        </Button>,
      ]}
    >
      Are you sure you want to delete the policy <strong>{name}</strong>?
      This action cannot be undone. All associated bindings will also be removed.
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
