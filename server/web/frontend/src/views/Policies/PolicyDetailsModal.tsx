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

const POLICY_TYPES: { value: string; label: string; isDisabled?: boolean }[] = [
  { value: "Kconfig", label: "Kconfig" },
  { value: "Dconf", label: "Dconf (not yet implemented)", isDisabled: true },
  { value: "Firefox", label: "Firefox" },
  { value: "Polkit", label: "Polkit (not yet implemented)", isDisabled: true },
  { value: "Chrome", label: "Chrome" },
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
  type: "boolean" | "string" | "integer" | "string-enum" | "integer-enum" | "list" | "json";
  description: string;
  enumOptions?: { value: number | string; label: string }[];
}

const CHROME_ALL_POLICIES: ChromePolicyDef[] = [
  // ── Homepage & Startup ──────────────────────────────────────────────────────
  { key: "HomepageLocation", label: "Home Page URL", group: "Homepage & Startup", type: "string", description: "Set the URL that is used as the browser home page." },
  { key: "HomepageIsNewTabPage", label: "Use New Tab as Home Page", group: "Homepage & Startup", type: "boolean", description: "Use the New Tab page as the home page instead of a custom URL." },
  { key: "RestoreOnStartup", label: "Action on Startup", group: "Homepage & Startup", type: "integer-enum", description: "Specifies the action to take on startup.", enumOptions: [
    { value: 1, label: "1 – Open the New Tab page" },
    { value: 4, label: "4 – Open a list of URLs" },
    { value: 5, label: "5 – Open the last session" },
  ]},
  { key: "RestoreOnStartupURLs", label: "Startup URLs", group: "Homepage & Startup", type: "list", description: "List of URLs to open on startup when RestoreOnStartup is 4." },
  { key: "ShowHomeButton", label: "Show Home Button", group: "Homepage & Startup", type: "boolean", description: "Show the Home button on the toolbar." },
  { key: "NewTabPageLocation", label: "New Tab Page URL", group: "Homepage & Startup", type: "string", description: "Configure the URL for the New Tab page." },
  { key: "NewTabPageManagedNewTabUrl", label: "Managed New Tab URL", group: "Homepage & Startup", type: "string", description: "Open a specific URL in a new tab created via managed shortcut." },

  // ── Extensions ──────────────────────────────────────────────────────────────
  { key: "ExtensionInstallForcelist", label: "Force-Install Extensions", group: "Extensions", type: "list", description: "Force-install extensions and apps. Each entry: extensionID;updateURL." },
  { key: "ExtensionInstallAllowlist", label: "Allowed Extensions", group: "Extensions", type: "list", description: "Allow specific extension IDs to be installed." },
  { key: "ExtensionInstallBlocklist", label: "Blocked Extensions", group: "Extensions", type: "list", description: "Block specific extension IDs (use [\"*\"] to block all)." },
  { key: "ExtensionInstallSources", label: "Extension Install Sources", group: "Extensions", type: "list", description: "URL patterns from which extensions can be installed." },
  { key: "ExtensionManifestV2Availability", label: "Manifest V2 Availability", group: "Extensions", type: "integer-enum", description: "Control availability of Manifest V2 extensions.", enumOptions: [
    { value: 0, label: "0 – Default browser behavior" },
    { value: 1, label: "1 – Disable Manifest V2 extensions" },
    { value: 2, label: "2 – Enable Manifest V2 extensions" },
    { value: 3, label: "3 – Enable Manifest V2 only for force-installed" },
  ]},
  { key: "ExtensionSettings", label: "Extension Settings", group: "Extensions", type: "json", description: "Comprehensive extension management settings (JSON object keyed by extension ID or wildcard)." },
  { key: "ExtensionAllowedTypes", label: "Allowed Extension Types", group: "Extensions", type: "list", description: "Restrict which extension types can be installed (e.g. extension, theme, app, hosted_app)." },
  { key: "ExtensionUnpublishedAvailability", label: "Unpublished Extension Availability", group: "Extensions", type: "integer-enum", description: "Control availability of extensions removed from the Chrome Web Store.", enumOptions: [
    { value: 0, label: "0 – Allow unpublished extensions" },
    { value: 1, label: "1 – Disable unpublished extensions" },
  ]},
  { key: "BlockExternalExtensions", label: "Block External Extensions", group: "Extensions", type: "boolean", description: "Block external extensions from being installed." },

  // ── Privacy & Security ───────────────────────────────────────────────────────
  { key: "IncognitoModeAvailability", label: "Incognito Mode Availability", group: "Privacy & Security", type: "integer-enum", description: "Control whether the user can open pages in incognito mode.", enumOptions: [
    { value: 0, label: "0 – Incognito available" },
    { value: 1, label: "1 – Incognito disabled" },
    { value: 2, label: "2 – Incognito forced" },
  ]},
  { key: "SitePerProcess", label: "Enable Site Isolation", group: "Privacy & Security", type: "boolean", description: "Require site isolation (strict origin) for every site." },
  { key: "DNSInterceptionChecksEnabled", label: "DNS Interception Checks", group: "Privacy & Security", type: "boolean", description: "Enable DNS interception checks to detect captive portals." },
  { key: "BuiltInDnsClientEnabled", label: "Built-in DNS Client", group: "Privacy & Security", type: "boolean", description: "Use Chrome's built-in DNS client instead of the OS resolver." },
  { key: "BlockThirdPartyCookies", label: "Block Third-Party Cookies", group: "Privacy & Security", type: "boolean", description: "Block third-party cookies and site data from being set." },
  { key: "SSLVersionMin", label: "Minimum SSL Version", group: "Privacy & Security", type: "string-enum", description: "Minimum TLS/SSL version accepted.", enumOptions: [
    { value: "tls1", label: "TLS 1.0" },
    { value: "tls1.1", label: "TLS 1.1" },
    { value: "tls1.2", label: "TLS 1.2" },
    { value: "tls1.3", label: "TLS 1.3" },
  ]},
  { key: "CipherSuiteBlacklist", label: "Disabled TLS Cipher Suites", group: "Privacy & Security", type: "list", description: "List of hex-encoded TLS cipher suites to disable." },
  { key: "NTLMv2Enabled", label: "NTLMv2 Authentication", group: "Privacy & Security", type: "boolean", description: "Enable NTLMv2 authentication." },
  { key: "AllowDinosaurEasterEgg", label: "Allow Dinosaur Easter Egg", group: "Privacy & Security", type: "boolean", description: "Allow users to play the dinosaur game when offline." },
  { key: "MetricsReportingEnabled", label: "Metrics Reporting", group: "Privacy & Security", type: "boolean", description: "Enable sending usage and crash-related data to Google." },
  { key: "ChromeVariations", label: "Chrome Variations", group: "Privacy & Security", type: "integer-enum", description: "Control which Chrome variations (field trials) are applied.", enumOptions: [
    { value: 0, label: "0 – Enable all variations" },
    { value: 1, label: "1 – Enable critical fix variations only" },
    { value: 2, label: "2 – Disable all variations" },
  ]},
  { key: "SuppressUnsupportedOSWarning", label: "Suppress Unsupported OS Warning", group: "Privacy & Security", type: "boolean", description: "Suppress the warning when Chrome is run on an unsupported OS." },
  { key: "BrowserNetworkTimeQueriesEnabled", label: "Network Time Queries", group: "Privacy & Security", type: "boolean", description: "Allow Chrome to make requests to a time service to detect clock skew." },
  { key: "PrivacySandboxPromptEnabled", label: "Privacy Sandbox Prompt", group: "Privacy & Security", type: "boolean", description: "Allow Chrome to show the Privacy Sandbox prompt to users." },
  { key: "PrivacySandboxAdTopicsEnabled", label: "Privacy Sandbox Ad Topics", group: "Privacy & Security", type: "boolean", description: "Enable the Privacy Sandbox Ad Topics API." },
  { key: "PrivacySandboxSiteEnabledAdsEnabled", label: "Privacy Sandbox Site-Enabled Ads", group: "Privacy & Security", type: "boolean", description: "Enable the Privacy Sandbox Site-Enabled Ads API." },
  { key: "PrivacySandboxAdMeasurementEnabled", label: "Privacy Sandbox Ad Measurement", group: "Privacy & Security", type: "boolean", description: "Enable the Privacy Sandbox Ad Measurement API." },
  { key: "HttpsOnlyMode", label: "HTTPS-Only Mode", group: "Privacy & Security", type: "string-enum", description: "Control HTTPS-Only Mode for navigation.", enumOptions: [
    { value: "allowed", label: "allowed – User can enable HTTPS-Only Mode" },
    { value: "disallowed", label: "disallowed – User cannot enable HTTPS-Only Mode" },
    { value: "force_enabled", label: "force_enabled – HTTPS-Only Mode is always enabled" },
  ]},
  { key: "HttpsUpgradesEnabled", label: "HTTPS Upgrades", group: "Privacy & Security", type: "boolean", description: "Automatically upgrade navigations to HTTPS where possible." },

  // ── Network & Proxy ──────────────────────────────────────────────────────────
  { key: "ProxyMode", label: "Proxy Mode", group: "Network & Proxy", type: "string-enum", description: "Choose how Chrome determines the proxy.", enumOptions: [
    { value: "direct", label: "direct – Never use a proxy" },
    { value: "auto_detect", label: "auto_detect – Auto-detect proxy settings" },
    { value: "pac_script", label: "pac_script – Use a .pac script URL" },
    { value: "fixed_servers", label: "fixed_servers – Use fixed proxy server" },
    { value: "system", label: "system – Use system proxy settings" },
  ]},
  { key: "ProxyServer", label: "Proxy Server", group: "Network & Proxy", type: "string", description: "Address or URL of the proxy server (host:port)." },
  { key: "ProxyPacUrl", label: "Proxy PAC URL", group: "Network & Proxy", type: "string", description: "URL of the proxy auto-config (.pac) file." },
  { key: "ProxyBypassList", label: "Proxy Bypass List", group: "Network & Proxy", type: "string", description: "Semicolon-separated list of hosts that bypass the proxy." },
  { key: "DnsOverHttpsMode", label: "DNS-over-HTTPS Mode", group: "Network & Proxy", type: "string-enum", description: "Control DNS-over-HTTPS mode.", enumOptions: [
    { value: "off", label: "off – Disable DNS-over-HTTPS" },
    { value: "automatic", label: "automatic – Use DoH with insecure fallback" },
    { value: "secure", label: "secure – Use DoH only, no fallback" },
  ]},
  { key: "DnsOverHttpsTemplates", label: "DNS-over-HTTPS Templates", group: "Network & Proxy", type: "string", description: "URI template(s) for the DNS-over-HTTPS resolver." },
  { key: "DnsOverHttpsSalt", label: "DNS-over-HTTPS Salt", group: "Network & Proxy", type: "string", description: "Salt value to use in privacy-preserving DNS-over-HTTPS identifiers." },
  { key: "NetworkPredictionOptions", label: "Network Prediction", group: "Network & Proxy", type: "integer-enum", description: "Enable or disable network prediction (preloading).", enumOptions: [
    { value: 0, label: "0 – Predict on all networks" },
    { value: 2, label: "2 – Predict on Wi-Fi and Ethernet only" },
    { value: 3, label: "3 – Never predict" },
  ]},
  { key: "MaxConnectionsPerProxy", label: "Max Connections Per Proxy", group: "Network & Proxy", type: "integer", description: "Maximum number of simultaneous connections to the proxy server." },
  { key: "URLBlocklist", label: "URL Blocklist", group: "Network & Proxy", type: "list", description: "Block access to a list of URL patterns." },
  { key: "URLAllowlist", label: "URL Allowlist", group: "Network & Proxy", type: "list", description: "Allow access to a list of URL patterns, overriding the blocklist." },
  { key: "BrowsingDataLifetime", label: "Browsing Data Lifetime", group: "Network & Proxy", type: "json", description: "Configure scheduled deletion of browsing data by type and TTL (JSON array)." },
  { key: "DataLeakPreventionRulesList", label: "Data Leak Prevention Rules", group: "Network & Proxy", type: "json", description: "List of data leak prevention rules (JSON array)." },

  // ── Content Settings ─────────────────────────────────────────────────────────
  { key: "DefaultCookiesSetting", label: "Default Cookies Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for cookies.", enumOptions: [
    { value: 1, label: "1 – Allow all cookies" },
    { value: 2, label: "2 – Block all cookies" },
    { value: 4, label: "4 – Session only cookies" },
  ]},
  { key: "CookiesAllowedForUrls", label: "Cookies Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where cookies are always allowed." },
  { key: "CookiesBlockedForUrls", label: "Cookies Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where cookies are always blocked." },
  { key: "CookiesSessionOnlyForUrls", label: "Cookies Session-Only For URLs", group: "Content Settings", type: "list", description: "URL patterns where cookies expire when the session ends." },
  { key: "DefaultJavaScriptSetting", label: "Default JavaScript Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for JavaScript.", enumOptions: [
    { value: 1, label: "1 – Allow JavaScript" },
    { value: 2, label: "2 – Block JavaScript" },
  ]},
  { key: "JavaScriptAllowedForUrls", label: "JavaScript Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where JavaScript is always allowed." },
  { key: "JavaScriptBlockedForUrls", label: "JavaScript Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where JavaScript is always blocked." },
  { key: "DefaultPopupsSetting", label: "Default Pop-ups Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for pop-up windows.", enumOptions: [
    { value: 1, label: "1 – Allow popups" },
    { value: 2, label: "2 – Block popups" },
  ]},
  { key: "PopupsAllowedForUrls", label: "Popups Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where pop-ups are always allowed." },
  { key: "PopupsBlockedForUrls", label: "Popups Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where pop-ups are always blocked." },
  { key: "DefaultGeolocationSetting", label: "Default Geolocation Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for geolocation access.", enumOptions: [
    { value: 1, label: "1 – Allow geolocation" },
    { value: 2, label: "2 – Block geolocation" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "DefaultNotificationsSetting", label: "Default Notifications Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for desktop notifications.", enumOptions: [
    { value: 1, label: "1 – Allow notifications" },
    { value: 2, label: "2 – Block notifications" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "NotificationsAllowedForUrls", label: "Notifications Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where notifications are always allowed." },
  { key: "NotificationsBlockedForUrls", label: "Notifications Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where notifications are always blocked." },
  { key: "DefaultImagesSetting", label: "Default Images Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for image loading.", enumOptions: [
    { value: 1, label: "1 – Allow all images" },
    { value: 2, label: "2 – Block all images" },
  ]},
  { key: "ImagesAllowedForUrls", label: "Images Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where images are always shown." },
  { key: "ImagesBlockedForUrls", label: "Images Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where images are always blocked." },
  { key: "DefaultWebBluetoothGuardSetting", label: "Default Web Bluetooth Setting", group: "Content Settings", type: "integer-enum", description: "Control Web Bluetooth API access.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "DefaultWebUsbGuardSetting", label: "Default WebUSB Setting", group: "Content Settings", type: "integer-enum", description: "Control WebUSB API access.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "WebUsbAllowDevicesForUrls", label: "WebUSB Allowed Devices For URLs", group: "Content Settings", type: "json", description: "Automatically grant permission to access specified USB devices for URLs (JSON array)." },
  { key: "DefaultSensorsSetting", label: "Default Sensors Setting", group: "Content Settings", type: "integer-enum", description: "Control access to sensors like accelerometer and gyroscope.", enumOptions: [
    { value: 1, label: "1 – Allow access" },
    { value: 2, label: "2 – Block access" },
  ]},
  { key: "SensorsAllowedForUrls", label: "Sensors Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where sensor access is always allowed." },
  { key: "SensorsBlockedForUrls", label: "Sensors Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where sensor access is always blocked." },
  { key: "DefaultSerialGuardSetting", label: "Default Serial API Setting", group: "Content Settings", type: "integer-enum", description: "Control Web Serial API access.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "DefaultFileSystemReadGuardSetting", label: "Default File System Read Setting", group: "Content Settings", type: "integer-enum", description: "Control read access to the local file system via the File System API.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "DefaultFileSystemWriteGuardSetting", label: "Default File System Write Setting", group: "Content Settings", type: "integer-enum", description: "Control write access to the local file system via the File System API.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "DefaultInsecureContentSetting", label: "Default Insecure Content Setting", group: "Content Settings", type: "integer-enum", description: "Control display of insecure mixed content.", enumOptions: [
    { value: 2, label: "2 – Block mixed content" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "InsecureContentAllowedForUrls", label: "Insecure Content Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns where insecure mixed content is always allowed." },
  { key: "InsecureContentBlockedForUrls", label: "Insecure Content Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns where insecure mixed content is always blocked." },
  { key: "DefaultMediaStreamSetting", label: "Default Camera/Mic Setting", group: "Content Settings", type: "integer-enum", description: "Default setting for webcam/microphone access.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "VideoCaptureAllowedUrls", label: "Camera Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns granted camera access without a prompt." },
  { key: "VideoCaptureAllowed", label: "Video Capture Allowed", group: "Content Settings", type: "boolean", description: "Allow sites to access the camera." },
  { key: "AudioCaptureAllowed", label: "Audio Capture Allowed", group: "Content Settings", type: "boolean", description: "Allow sites to access the microphone." },
  { key: "AudioCaptureAllowedUrls", label: "Microphone Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns granted microphone access without a prompt." },
  { key: "DefaultClipboardSetting", label: "Default Clipboard Setting", group: "Content Settings", type: "integer-enum", description: "Control clipboard read/write access.", enumOptions: [
    { value: 2, label: "2 – Block clipboard access" },
    { value: 3, label: "3 – Ask each time" },
  ]},
  { key: "ClipboardAllowedForUrls", label: "Clipboard Allowed For URLs", group: "Content Settings", type: "list", description: "URL patterns allowed to use the clipboard API." },
  { key: "ClipboardBlockedForUrls", label: "Clipboard Blocked For URLs", group: "Content Settings", type: "list", description: "URL patterns blocked from using the clipboard API." },
  { key: "DefaultLocalFontsSetting", label: "Default Local Fonts Setting", group: "Content Settings", type: "integer-enum", description: "Control access to local fonts via the Local Font Access API.", enumOptions: [
    { value: 2, label: "2 – Block access" },
    { value: 3, label: "3 – Ask each time" },
  ]},

  // ── Safe Browsing ────────────────────────────────────────────────────────────
  { key: "SafeBrowsingEnabled", label: "Enable Safe Browsing", group: "Safe Browsing", type: "boolean", description: "Enable Google Safe Browsing protection." },
  { key: "SafeBrowsingProtectionLevel", label: "Safe Browsing Protection Level", group: "Safe Browsing", type: "integer-enum", description: "Set the Safe Browsing protection level.", enumOptions: [
    { value: 0, label: "0 – Safe Browsing off" },
    { value: 1, label: "1 – Standard protection" },
    { value: 2, label: "2 – Enhanced protection" },
  ]},
  { key: "SafeBrowsingExtendedReportingEnabled", label: "Safe Browsing Extended Reporting", group: "Safe Browsing", type: "boolean", description: "Enable Safe Browsing extended reporting (sends data to Google)." },
  { key: "DisableSafeBrowsingProceedAnyway", label: "Disable Safe Browsing Proceed", group: "Safe Browsing", type: "boolean", description: "Prevent users from proceeding past Safe Browsing warning pages." },
  { key: "SafeBrowsingAllowlistDomains", label: "Safe Browsing Allowlist Domains", group: "Safe Browsing", type: "list", description: "Domains where Safe Browsing checks are not performed." },
  { key: "SafeSitesFilterBehavior", label: "Safe Sites Filter", group: "Safe Browsing", type: "integer-enum", description: "Control Safe Sites adult content filtering.", enumOptions: [
    { value: 0, label: "0 – Do not filter" },
    { value: 1, label: "1 – Filter top-level sites" },
  ]},
  { key: "PasswordProtectionWarningTrigger", label: "Password Protection Warning Trigger", group: "Safe Browsing", type: "integer-enum", description: "When to show password protection warnings.", enumOptions: [
    { value: 0, label: "0 – Password protection off" },
    { value: 1, label: "1 – Warn on password reuse on phishing page" },
    { value: 2, label: "2 – Warn on password reuse on any site" },
  ]},
  { key: "PasswordProtectionLoginURLs", label: "Password Protection Login URLs", group: "Safe Browsing", type: "list", description: "List of enterprise login URLs where password protection checks are performed." },
  { key: "PasswordProtectionChangePasswordURL", label: "Password Change URL", group: "Safe Browsing", type: "string", description: "URL where users can change their enterprise password." },
  { key: "SafeBrowsingDeepScanningEnabled", label: "Safe Browsing Deep Scanning", group: "Safe Browsing", type: "boolean", description: "Enable deep scanning of suspicious downloads in Safe Browsing." },

  // ── Passwords & Autofill ─────────────────────────────────────────────────────
  { key: "PasswordManagerEnabled", label: "Enable Password Manager", group: "Passwords & Autofill", type: "boolean", description: "Enable Chrome's built-in password manager." },
  { key: "PasswordLeakDetectionEnabled", label: "Password Leak Detection", group: "Passwords & Autofill", type: "boolean", description: "Enable password leak detection against Google's database of breached credentials." },
  { key: "PasswordSharingEnabled", label: "Password Sharing", group: "Passwords & Autofill", type: "boolean", description: "Allow sharing passwords with contacts via Family Link." },
  { key: "AutofillAddressEnabled", label: "Address Autofill", group: "Passwords & Autofill", type: "boolean", description: "Enable autofill for addresses." },
  { key: "AutofillCreditCardEnabled", label: "Credit Card Autofill", group: "Passwords & Autofill", type: "boolean", description: "Enable autofill for credit card information." },
  { key: "AutofillPredictionImprovementsEnabled", label: "Autofill Prediction Improvements", group: "Passwords & Autofill", type: "boolean", description: "Enable AI-powered autofill prediction improvements." },

  // ── Downloads ────────────────────────────────────────────────────────────────
  { key: "DownloadDirectory", label: "Download Directory", group: "Downloads", type: "string", description: "Default directory for file downloads." },
  { key: "PromptForDownloadLocation", label: "Prompt for Download Location", group: "Downloads", type: "boolean", description: "Ask where to save each file before downloading." },
  { key: "DownloadRestrictions", label: "Download Restrictions", group: "Downloads", type: "integer-enum", description: "Restrict which downloads are allowed.", enumOptions: [
    { value: 0, label: "0 – No special restrictions" },
    { value: 1, label: "1 – Block dangerous downloads" },
    { value: 2, label: "2 – Block dangerous and unwanted downloads" },
    { value: 3, label: "3 – Block all downloads" },
    { value: 4, label: "4 – Block malicious downloads" },
  ]},
  { key: "AllowedDownloadTypes", label: "Allowed Download Types", group: "Downloads", type: "list", description: "Allowlist of MIME types or file extensions that can be downloaded." },

  // ── Printing ─────────────────────────────────────────────────────────────────
  { key: "PrintingEnabled", label: "Enable Printing", group: "Printing", type: "boolean", description: "Enable printing in Chrome." },
  { key: "PrintPreviewUseSystemDefaultPrinter", label: "Use System Default Printer", group: "Printing", type: "boolean", description: "Use the system default printer as the default in Print Preview." },
  { key: "DefaultPrinterSelection", label: "Default Printer Selection", group: "Printing", type: "string", description: "Rules for selecting the default printer (JSON string)." },
  { key: "PrinterTypeDenyList", label: "Printer Type Deny List", group: "Printing", type: "list", description: "Disable printer types (e.g. cloud, local, extension)." },
  { key: "PrintRasterizationMode", label: "Print Rasterization Mode", group: "Printing", type: "integer-enum", description: "Control print rasterization.", enumOptions: [
    { value: 0, label: "0 – Full rasterization" },
    { value: 1, label: "1 – Fast rasterization" },
  ]},
  { key: "BackgroundGraphicsModeEnabled", label: "Background Graphics in Print", group: "Printing", type: "boolean", description: "Enable background graphics in print output by default." },
  { key: "PrintingAllowedBackgroundGraphicsModes", label: "Allowed Background Graphics Modes", group: "Printing", type: "string-enum", description: "Restrict the background graphics printing mode.", enumOptions: [
    { value: "any", label: "any – Allow any mode" },
    { value: "enabled", label: "enabled – Force background graphics on" },
    { value: "disabled", label: "disabled – Force background graphics off" },
  ]},

  // ── Browser UI ───────────────────────────────────────────────────────────────
  { key: "BookmarkBarEnabled", label: "Bookmark Bar Enabled", group: "Browser UI", type: "boolean", description: "Enable the bookmarks bar." },
  { key: "ShowAppsShortcutInBookmarkBar", label: "Show Apps Shortcut in Bookmarks Bar", group: "Browser UI", type: "boolean", description: "Show the Apps shortcut in the bookmarks bar." },
  { key: "ManagedBookmarks", label: "Managed Bookmarks", group: "Browser UI", type: "json", description: "A list of managed bookmarks provided to the user (JSON array)." },
  { key: "ManagedBookmarksSupervisedUser", label: "Managed Bookmarks (Supervised)", group: "Browser UI", type: "json", description: "Managed bookmarks for supervised users (JSON array)." },
  { key: "DeveloperToolsAvailability", label: "Developer Tools Availability", group: "Browser UI", type: "integer-enum", description: "Control when developer tools can be used.", enumOptions: [
    { value: 0, label: "0 – Disallow usage" },
    { value: 1, label: "1 – Always allow" },
    { value: 2, label: "2 – Allow except in force-installed extensions" },
  ]},
  { key: "DefaultBrowserSettingEnabled", label: "Default Browser Setting", group: "Browser UI", type: "boolean", description: "Allow Chrome to prompt the user to set it as the default browser." },
  { key: "FullscreenAllowed", label: "Fullscreen Allowed", group: "Browser UI", type: "boolean", description: "Allow the browser window to enter fullscreen mode." },
  { key: "TaskManagerEndProcessEnabled", label: "Task Manager End Process", group: "Browser UI", type: "boolean", description: "Allow users to terminate processes in the Task Manager." },
  { key: "AllowDeletingBrowserHistory", label: "Allow Deleting Browser History", group: "Browser UI", type: "boolean", description: "Allow users to delete browser and download history." },
  { key: "HideWebStoreIcon", label: "Hide Web Store Icon", group: "Browser UI", type: "boolean", description: "Hide the Chrome Web Store icon from the New Tab page and app launcher." },
  { key: "ShowFullUrlsInAddressBar", label: "Show Full URLs in Address Bar", group: "Browser UI", type: "boolean", description: "Display the full URL including scheme and subdomain in the address bar." },
  { key: "AllowedLanguages", label: "Allowed Browser Languages", group: "Browser UI", type: "list", description: "Restrict the languages users can set as the browser language." },
  { key: "ApplicationLocaleValue", label: "Application Locale", group: "Browser UI", type: "string", description: "Set the browser application locale (language code, e.g. en-US)." },
  { key: "SpellCheckServiceEnabled", label: "Spell Check Service", group: "Browser UI", type: "boolean", description: "Enable the web-based spell check service." },
  { key: "SpellcheckEnabled", label: "Spellcheck Enabled", group: "Browser UI", type: "boolean", description: "Enable spellcheck in Chrome." },
  { key: "SpellcheckLanguage", label: "Spellcheck Language", group: "Browser UI", type: "list", description: "Force-enable specific spellcheck languages." },
  { key: "SpellcheckLanguageBlocklist", label: "Spellcheck Language Blocklist", group: "Browser UI", type: "list", description: "Force-disable specific spellcheck languages." },
  { key: "TranslateEnabled", label: "Translation Enabled", group: "Browser UI", type: "boolean", description: "Enable the integrated Google Translate feature." },
  { key: "AutoplayAllowed", label: "Autoplay Allowed", group: "Browser UI", type: "boolean", description: "Allow websites to autoplay media." },
  { key: "AutoplayAllowlist", label: "Autoplay Allowlist", group: "Browser UI", type: "list", description: "URL patterns where autoplay is always allowed." },
  { key: "AccessibilityImageLabelsEnabled", label: "Accessibility Image Labels", group: "Browser UI", type: "boolean", description: "Allow Chrome to provide automatic image descriptions using Google servers." },
  { key: "ScreenCaptureAllowed", label: "Screen Capture Allowed", group: "Browser UI", type: "boolean", description: "Allow websites to prompt the user for screen capture." },
  { key: "ScreenCaptureAllowedByOrigins", label: "Screen Capture Allowed By Origins", group: "Browser UI", type: "list", description: "Origins where screen capture is allowed without restriction." },
  { key: "ScrollToTextFragmentEnabled", label: "Scroll-to-Text Fragment", group: "Browser UI", type: "boolean", description: "Allow navigations to scroll directly to text on a page via URL fragments." },
  { key: "BrowserAddPersonEnabled", label: "Add Person in Profile Manager", group: "Browser UI", type: "boolean", description: "Allow users to add a new profile using the profile manager." },
  { key: "BrowserGuestModeEnabled", label: "Guest Mode", group: "Browser UI", type: "boolean", description: "Allow users to use Chrome in guest mode." },
  { key: "BrowserGuestModeEnforced", label: "Force Guest Mode", group: "Browser UI", type: "boolean", description: "Force Chrome to use guest mode (all other sessions are blocked)." },
  { key: "ProfilePickerOnStartupAvailability", label: "Profile Picker on Startup", group: "Browser UI", type: "integer-enum", description: "Control profile picker display on startup.", enumOptions: [
    { value: 0, label: "0 – Default behavior" },
    { value: 1, label: "1 – Always show" },
    { value: 2, label: "2 – Never show" },
  ]},

  // ── User & Sync ──────────────────────────────────────────────────────────────
  { key: "BrowserSignin", label: "Browser Sign-in Policy", group: "User & Sync", type: "integer-enum", description: "Configure browser sign-in behavior.", enumOptions: [
    { value: 0, label: "0 – Disable browser sign-in" },
    { value: 1, label: "1 – Enable browser sign-in" },
    { value: 2, label: "2 – Force browser sign-in" },
  ]},
  { key: "SyncDisabled", label: "Disable Chrome Sync", group: "User & Sync", type: "boolean", description: "Disable Chrome Sync and prevent the user from enabling it." },
  { key: "SyncTypesListDisabled", label: "Disabled Sync Types", group: "User & Sync", type: "list", description: "Disable specific sync data types (e.g. bookmarks, passwords, extensions)." },
  { key: "UserFeedbackAllowed", label: "User Feedback Allowed", group: "User & Sync", type: "boolean", description: "Allow users to submit feedback to Google." },
  { key: "AllowedDomainsForApps", label: "Allowed Domains for Google Apps", group: "User & Sync", type: "string", description: "Define domains allowed for Google Workspace apps (comma-separated)." },
  { key: "SigninInterceptionEnabled", label: "Sign-in Interception", group: "User & Sync", type: "boolean", description: "Enable sign-in interception when a user signs in to a Google account in Chrome." },
  { key: "ForceSyncTypes", label: "Force Sync Types", group: "User & Sync", type: "list", description: "Force specific sync data types to always be synced." },

  // ── Search ───────────────────────────────────────────────────────────────────
  { key: "DefaultSearchProviderEnabled", label: "Default Search Provider Enabled", group: "Search", type: "boolean", description: "Enable the default search provider feature." },
  { key: "DefaultSearchProviderName", label: "Search Provider Name", group: "Search", type: "string", description: "The name of the default search provider." },
  { key: "DefaultSearchProviderSearchURL", label: "Search Provider Search URL", group: "Search", type: "string", description: "Search URL for the default search provider (use {searchTerms})." },
  { key: "DefaultSearchProviderSuggestURL", label: "Search Provider Suggest URL", group: "Search", type: "string", description: "URL for search suggestions from the default search provider." },
  { key: "DefaultSearchProviderIconURL", label: "Search Provider Icon URL", group: "Search", type: "string", description: "Favicon URL for the default search provider." },
  { key: "DefaultSearchProviderKeyword", label: "Search Provider Keyword", group: "Search", type: "string", description: "Keyword to trigger the default search provider in the address bar." },
  { key: "DefaultSearchProviderEncodings", label: "Search Provider Encodings", group: "Search", type: "list", description: "Character encodings supported by the default search provider." },
  { key: "DefaultSearchProviderNewTabURL", label: "Search Provider New Tab URL", group: "Search", type: "string", description: "New tab page URL provided by the default search provider." },
  { key: "DefaultSearchProviderImageURL", label: "Search Provider Image URL", group: "Search", type: "string", description: "URL for image search on the default search provider." },
  { key: "SearchSuggestEnabled", label: "Search Suggestions", group: "Search", type: "boolean", description: "Enable search suggestions in the address bar." },
  { key: "ContextualSearchEnabled", label: "Contextual Search", group: "Search", type: "boolean", description: "Enable contextual search via tap/select on desktop." },

  // ── Updates & Management ─────────────────────────────────────────────────────
  { key: "CloudManagementEnrollmentToken", label: "Cloud Management Enrollment Token", group: "Updates & Management", type: "string", description: "Enrollment token for Chrome Browser Cloud Management." },
  { key: "CloudManagementEnrollmentMandatory", label: "Mandatory Cloud Management", group: "Updates & Management", type: "boolean", description: "Make Chrome Browser Cloud Management enrollment mandatory." },
  { key: "CloudPolicyOverridesPlatformPolicy", label: "Cloud Policy Overrides Platform Policy", group: "Updates & Management", type: "boolean", description: "Give cloud policy higher priority than platform (machine-level) policy." },
  { key: "CloudUserPolicyOverridesCloudMachinePolicy", label: "Cloud User Policy Overrides Machine Policy", group: "Updates & Management", type: "boolean", description: "Give user-level cloud policy higher priority than machine-level cloud policy." },
  { key: "CommandLineFlagSecurityWarningsEnabled", label: "Command-Line Security Warnings", group: "Updates & Management", type: "boolean", description: "Show security warnings when Chrome is started with dangerous command-line flags." },
  { key: "RelaunchNotification", label: "Relaunch Notification", group: "Updates & Management", type: "integer-enum", description: "Notify or force users to relaunch Chrome for pending updates.", enumOptions: [
    { value: 1, label: "1 – Show relaunch recommendation" },
    { value: 2, label: "2 – Force relaunch" },
  ]},
  { key: "RelaunchNotificationPeriod", label: "Relaunch Notification Period (ms)", group: "Updates & Management", type: "integer", description: "Time in milliseconds before showing the relaunch notification." },
  { key: "RelaunchWindow", label: "Relaunch Window", group: "Updates & Management", type: "json", description: "Set a preferred time window for browser relaunches (JSON object with entries)." },
  { key: "EnterpriseRealTimeUrlCheckMode", label: "Real-Time URL Check Mode", group: "Updates & Management", type: "integer-enum", description: "Enable real-time enterprise URL checking.", enumOptions: [
    { value: 0, label: "0 – Disabled" },
    { value: 1, label: "1 – Enable for main frame URLs" },
  ]},
  { key: "PolicyDictionaryMultipleSourceMergeList", label: "Policy Dictionary Merge List", group: "Updates & Management", type: "list", description: "List of policies for which dictionary values are merged from multiple sources." },
  { key: "PolicyListMultipleSourceMergeList", label: "Policy List Merge List", group: "Updates & Management", type: "list", description: "List of policies for which list values are merged from multiple sources." },

  // ── HTTP Authentication ──────────────────────────────────────────────────────
  { key: "AuthSchemes", label: "Allowed Auth Schemes", group: "HTTP Authentication", type: "string", description: "Comma-separated list of supported HTTP authentication schemes (e.g. basic,digest,ntlm,negotiate)." },
  { key: "DisableAuthNegotiateCnameLookup", label: "Disable Auth Negotiate CNAME Lookup", group: "HTTP Authentication", type: "boolean", description: "Disable CNAME lookup when generating the Kerberos SPN." },
  { key: "EnableAuthNegotiatePort", label: "Enable Auth Negotiate Port", group: "HTTP Authentication", type: "boolean", description: "Include a non-standard port in the Kerberos SPN." },
  { key: "AuthServerAllowlist", label: "Auth Server Allowlist", group: "HTTP Authentication", type: "string", description: "Allowlist of servers for integrated Windows authentication (comma-separated patterns)." },
  { key: "AuthNegotiateDelegateAllowlist", label: "Auth Negotiate Delegate Allowlist", group: "HTTP Authentication", type: "string", description: "Servers that Chrome may delegate credentials to (comma-separated)." },
  { key: "AuthNegotiateDelegateByKdcPolicy", label: "Delegate Auth by KDC Policy", group: "HTTP Authentication", type: "boolean", description: "Trust the KDC policy to determine whether delegation is allowed." },
  { key: "NtlmV2Enabled", label: "NTLMv2 Enabled", group: "HTTP Authentication", type: "boolean", description: "Enable NTLMv2 when negotiating with NTLM challenge." },
  { key: "AllowCrossOriginAuthPrompt", label: "Allow Cross-Origin Auth Prompts", group: "HTTP Authentication", type: "boolean", description: "Allow cross-origin images to show HTTP Basic Auth prompts." },
  { key: "BasicAuthOverHttpEnabled", label: "HTTP Basic Auth over HTTP", group: "HTTP Authentication", type: "boolean", description: "Allow HTTP Basic Auth challenges over non-HTTPS connections." },

  // ── Kerberos ─────────────────────────────────────────────────────────────────
  { key: "KerberosEnabled", label: "Kerberos Enabled", group: "Kerberos", type: "boolean", description: "Enable Kerberos authentication support in Chrome." },
  { key: "KerberosAccounts", label: "Kerberos Accounts", group: "Kerberos", type: "json", description: "Pre-configured Kerberos accounts (JSON array of account objects)." },
  { key: "KerberosUseCustomPrevalidatedConfig", label: "Kerberos Use Custom Config", group: "Kerberos", type: "boolean", description: "Use a custom krb5 configuration for Kerberos authentication." },
  { key: "KerberosCustomPrevalidatedConfig", label: "Kerberos Custom Config Content", group: "Kerberos", type: "string", description: "Content of the custom krb5 configuration file." },
  { key: "KerberosAddAccountsAllowed", label: "Kerberos Add Accounts Allowed", group: "Kerberos", type: "boolean", description: "Allow users to add Kerberos accounts manually." },

  // ── Miscellaneous ────────────────────────────────────────────────────────────
  { key: "UserDataDir", label: "User Data Directory", group: "Miscellaneous", type: "string", description: "Set the directory that Chrome uses to store user data." },
  { key: "DiskCacheDir", label: "Disk Cache Directory", group: "Miscellaneous", type: "string", description: "Set the directory used to store the disk cache." },
  { key: "DiskCacheSize", label: "Disk Cache Size (bytes)", group: "Miscellaneous", type: "integer", description: "Set the disk cache size in bytes (0 = use default)." },
  { key: "MediaCacheSize", label: "Media Cache Size (bytes)", group: "Miscellaneous", type: "integer", description: "Set the media disk cache size in bytes (0 = use default)." },
  { key: "ProxySettings", label: "Proxy Settings", group: "Miscellaneous", type: "json", description: "Complete proxy settings as a JSON object (mode, server, pacUrl, bypassList)." },
  { key: "CertificateTransparencyEnforcementDisabledForUrls", label: "CT Enforcement Disabled For URLs", group: "Miscellaneous", type: "list", description: "URL patterns where Certificate Transparency enforcement is disabled." },
  { key: "CertificateTransparencyEnforcementDisabledForCas", label: "CT Enforcement Disabled For CAs", group: "Miscellaneous", type: "list", description: "CA certificates for which Certificate Transparency enforcement is disabled." },
  { key: "AdditionalDnsQueryTypesEnabled", label: "Additional DNS Query Types", group: "Miscellaneous", type: "boolean", description: "Allow additional DNS query types such as HTTPS when performing DNS lookups." },
  { key: "HardwareAccelerationModeEnabled", label: "Hardware Acceleration", group: "Miscellaneous", type: "boolean", description: "Enable hardware acceleration (GPU compositing)." },
  { key: "ThrottleNonVisibleCrossOriginIframesAllowed", label: "Throttle Non-Visible Iframes", group: "Miscellaneous", type: "boolean", description: "Allow throttling of non-visible cross-origin iframes." },
  { key: "OriginAgentClusterDefaultEnabled", label: "Origin-keyed Agent Cluster Default", group: "Miscellaneous", type: "boolean", description: "Enable origin-keyed agent clusters by default." },
  { key: "InsecurePrivateNetworkRequestsAllowed", label: "Insecure Private Network Requests", group: "Miscellaneous", type: "boolean", description: "Allow insecure requests from public sites to private network resources." },
  { key: "InsecurePrivateNetworkRequestsAllowedForUrls", label: "Insecure Private Network Requests Allowed For URLs", group: "Miscellaneous", type: "list", description: "URL patterns allowed to make insecure requests to private network resources." },
  { key: "PrivateNetworkAccessRestrictionsEnabled", label: "Private Network Access Restrictions", group: "Miscellaneous", type: "boolean", description: "Enable restrictions on requests from public sites to private network endpoints." },
  { key: "LegacySameSiteCookieBehaviorEnabled", label: "Legacy SameSite Cookie Behavior", group: "Miscellaneous", type: "integer-enum", description: "Revert to legacy SameSite=None cookie behavior for compatibility.", enumOptions: [
    { value: 0, label: "0 – Use default behavior for all cookies" },
    { value: 1, label: "1 – Revert to legacy behavior for all cookies" },
    { value: 2, label: "2 – Use default behavior for cookies on all sites" },
  ]},
  { key: "LegacySameSiteCookieBehaviorEnabledForDomainList", label: "Legacy SameSite Cookie Domains", group: "Miscellaneous", type: "list", description: "Domains where legacy SameSite=None cookie behavior is reverted." },
  { key: "TabFreezingEnabled", label: "Tab Freezing", group: "Miscellaneous", type: "boolean", description: "Allow freezing of background tabs to save memory and CPU." },
  { key: "BackForwardCacheEnabled", label: "Back/Forward Cache", group: "Miscellaneous", type: "boolean", description: "Enable the back/forward cache for instant back and forward navigation." },
  { key: "WebRtcAllowLegacyTLSProtocols", label: "WebRTC Legacy TLS", group: "Miscellaneous", type: "boolean", description: "Allow WebRTC connections to servers using deprecated TLS versions." },
  { key: "WebRtcEventLogCollectionAllowed", label: "WebRTC Event Log Collection", group: "Miscellaneous", type: "boolean", description: "Allow Google to collect WebRTC event logs from connected users." },
  { key: "WebRtcIPHandling", label: "WebRTC IP Handling", group: "Miscellaneous", type: "string-enum", description: "Control which network interfaces WebRTC uses to find the best path.", enumOptions: [
    { value: "default", label: "default – Use all network interfaces" },
    { value: "default_public_and_private_interfaces", label: "default_public_and_private_interfaces" },
    { value: "default_public_interface_only", label: "default_public_interface_only" },
    { value: "disable_non_proxied_udp", label: "disable_non_proxied_udp – Only use TCP or proxied UDP" },
  ]},
  { key: "WebRtcLocalIpsAllowedUrls", label: "WebRTC Local IPs Allowed For URLs", group: "Miscellaneous", type: "list", description: "Origins that can expose local IP addresses in WebRTC ICE candidates." },
  { key: "ForceEphemeralProfiles", label: "Force Ephemeral Profiles", group: "Miscellaneous", type: "boolean", description: "Use ephemeral (temporary) profiles that are erased when the browser closes." },
  { key: "BrowserThemeColor", label: "Browser Theme Color", group: "Miscellaneous", type: "string", description: "Set the browser theme color (hex color string, e.g. #FF0000)." },
  { key: "NativeMessagingAllowlist", label: "Native Messaging Allowlist", group: "Miscellaneous", type: "list", description: "Native messaging hosts that are not subject to the deny list." },
  { key: "NativeMessagingBlocklist", label: "Native Messaging Blocklist", group: "Miscellaneous", type: "list", description: "Native messaging hosts that are blocked (use [\"*\"] to block all)." },
  { key: "NativeMessagingUserLevelHosts", label: "Native Messaging User-Level Hosts", group: "Miscellaneous", type: "boolean", description: "Allow user-level native messaging hosts not installed at the system level." },
  { key: "TotalMemoryLimitMb", label: "Total Memory Limit (MB)", group: "Miscellaneous", type: "integer", description: "Set a soft memory limit in MB; Chrome will discard tabs when exceeded." },
  { key: "SharedArrayBufferUnrestrictedAccessAllowed", label: "SharedArrayBuffer Unrestricted", group: "Miscellaneous", type: "boolean", description: "Allow SharedArrayBuffer to be used without cross-origin isolation." },
  { key: "SharedArrayBufferAccessAllowed", label: "SharedArrayBuffer Access", group: "Miscellaneous", type: "boolean", description: "Allow access to SharedArrayBuffer API." },
  { key: "UserAgentClientHintsEnabled", label: "User-Agent Client Hints", group: "Miscellaneous", type: "boolean", description: "Enable User-Agent Client Hints feature." },
  { key: "UserAgentClientHintsGREASEUpdateEnabled", label: "User-Agent Client Hints GREASE", group: "Miscellaneous", type: "boolean", description: "Enable the GREASE update for User-Agent Client Hints." },
  { key: "IsolateOrigins", label: "Isolated Origins", group: "Miscellaneous", type: "string", description: "Comma-separated list of origins that require dedicated processes (site isolation)." },
  { key: "SiteIsolationEnabled", label: "Site Isolation Enabled", group: "Miscellaneous", type: "boolean", description: "Enable site isolation; each site runs in a separate process." },
  { key: "RendererCodeIntegrityEnabled", label: "Renderer Code Integrity", group: "Miscellaneous", type: "boolean", description: "Enable Renderer Code Integrity protection (blocks unauthorized code from being injected)." },
  { key: "LookalikeWarningAllowlistDomains", label: "Lookalike Warning Allowlist Domains", group: "Miscellaneous", type: "list", description: "Domains that are exempt from lookalike URL safety warnings." },
  { key: "ManagedConfigurationPerOrigin", label: "Managed Configuration Per Origin", group: "Miscellaneous", type: "json", description: "Key-value configuration delivered to specific origins via the Managed Configuration API (JSON array)." },
  { key: "DeviceLoginScreenSecondFactorAuthentication", label: "Second Factor Authentication", group: "Miscellaneous", type: "integer-enum", description: "Configure second factor authentication on the login screen.", enumOptions: [
    { value: 0, label: "0 – Disabled" },
    { value: 1, label: "1 – U2F" },
  ]},
  { key: "RestrictSigninToPattern", label: "Restrict Sign-In to Pattern", group: "Miscellaneous", type: "string", description: "Restrict Chrome sign-in to accounts matching a username pattern (regex)." },
  { key: "SSLErrorOverrideAllowed", label: "SSL Error Override Allowed", group: "Miscellaneous", type: "boolean", description: "Allow users to click through SSL warning pages." },
  { key: "SSLErrorOverrideAllowedForOrigins", label: "SSL Error Override Allowed For Origins", group: "Miscellaneous", type: "list", description: "Origins where users may click through SSL error pages." },
  { key: "OverrideSecurityRestrictionsOnInsecureOrigin", label: "Override Security on Insecure Origins", group: "Miscellaneous", type: "list", description: "Origins or hostname patterns treated as secure for development purposes." },
  { key: "EnterpriseAuthenticationAppLinkPolicy", label: "Enterprise Auth App Link Policy", group: "Miscellaneous", type: "json", description: "Redirect authentication URLs to an enterprise SSO app (JSON array)." },
  { key: "GaiaLockScreenOfflineSigninTimeLimitDays", label: "Offline Sign-in Time Limit (days)", group: "Miscellaneous", type: "integer", description: "Number of days a user can sign in without connecting to Google (-1 = no limit)." },
  { key: "ChromeOsLockOnIdleSuspend", label: "Lock on Idle/Suspend", group: "Miscellaneous", type: "boolean", description: "Lock the screen when the device is idle or suspended." },
  { key: "ExternalProtocolDialogShowAlwaysOpenCheckbox", label: "External Protocol Dialog Checkbox", group: "Miscellaneous", type: "boolean", description: "Show the 'Always open' checkbox in external protocol dialog." },
  { key: "ExternalProtocolDialogShowDefaultBrowserCheckbox", label: "External Protocol Dialog Browser Checkbox", group: "Miscellaneous", type: "boolean", description: "Show the default browser checkbox in external protocol dialog." },
  { key: "FetchKeepAliveDurationSecondsOnShutdown", label: "Fetch Keep-Alive Duration on Shutdown (s)", group: "Miscellaneous", type: "integer", description: "Seconds to keep fetch keepalive requests active on browser shutdown." },
  { key: "TabDiscardingExceptions", label: "Tab Discarding Exceptions", group: "Miscellaneous", type: "list", description: "URL patterns exempt from tab discarding (never discarded)." },
  { key: "AbusiveExperienceInterventionEnforce", label: "Abusive Experience Intervention", group: "Miscellaneous", type: "boolean", description: "Enforce abusive experience interventions for sites with abusive experiences." },
  { key: "AdsSettingForIntrusiveAdsSites", label: "Ads Setting for Intrusive Ad Sites", group: "Miscellaneous", type: "integer-enum", description: "Control ad behavior on sites known for intrusive ads.", enumOptions: [
    { value: 1, label: "1 – Allow ads on all sites" },
    { value: 2, label: "2 – Block ads on intrusive sites" },
  ]},
  { key: "AllowFileSelectionDialogs", label: "Allow File Selection Dialogs", group: "Miscellaneous", type: "boolean", description: "Allow Chrome to open file selection dialogs." },
  { key: "AlwaysOpenPdfExternally", label: "Always Open PDF Externally", group: "Miscellaneous", type: "boolean", description: "Always download and open PDF files in an external application." },
  { key: "PrintHeaderFooter", label: "Print Header and Footer", group: "Miscellaneous", type: "boolean", description: "Include headers and footers in printed pages." },
  { key: "ShowCastIconInToolbar", label: "Show Cast Icon in Toolbar", group: "Miscellaneous", type: "boolean", description: "Show the cast icon in the toolbar." },
  { key: "EnableMediaRouter", label: "Enable Media Router (Cast)", group: "Miscellaneous", type: "boolean", description: "Enable the Media Router (Google Cast) feature." },
  { key: "MediaRouterCastAllowAllIPs", label: "Cast Allow All IPs", group: "Miscellaneous", type: "boolean", description: "Allow casting to all IP addresses, not just private ones." },
  { key: "ForcedLanguages", label: "Forced Languages", group: "Miscellaneous", type: "list", description: "List of locale codes that will always appear in the language settings." },
  { key: "ShowManagedUiEnabled", label: "Show Managed UI", group: "Miscellaneous", type: "boolean", description: "Display 'Managed by your organization' UI elements." },
  { key: "StartupBrowserWindowLaunchSuppressed", label: "Suppress Startup Browser Window", group: "Miscellaneous", type: "boolean", description: "Suppress the browser window from appearing on startup." },
  { key: "ImportAutofillFormData", label: "Import Autofill Form Data", group: "Miscellaneous", type: "boolean", description: "Import autofill form data when Chrome is run for the first time." },
  { key: "ImportBookmarks", label: "Import Bookmarks", group: "Miscellaneous", type: "boolean", description: "Import bookmarks when Chrome is run for the first time." },
  { key: "ImportHistory", label: "Import History", group: "Miscellaneous", type: "boolean", description: "Import browsing history when Chrome is run for the first time." },
  { key: "ImportHomepage", label: "Import Homepage", group: "Miscellaneous", type: "boolean", description: "Import the homepage setting when Chrome is run for the first time." },
  { key: "ImportSavedPasswords", label: "Import Saved Passwords", group: "Miscellaneous", type: "boolean", description: "Import saved passwords when Chrome is run for the first time." },
  { key: "ImportSearchEngine", label: "Import Search Engine", group: "Miscellaneous", type: "boolean", description: "Import the search engine setting when Chrome is run for the first time." },
  { key: "WPADQuickCheckEnabled", label: "WPAD Quick Check", group: "Miscellaneous", type: "boolean", description: "Enable WPAD optimization (reduces startup delay for auto-proxy detection)." },
  { key: "ThirdPartyBlockingEnabled", label: "Third-Party Blocking", group: "Miscellaneous", type: "boolean", description: "Block third-party software from injecting executable code into Chrome." },
  { key: "ComponentUpdatesEnabled", label: "Component Updates", group: "Miscellaneous", type: "boolean", description: "Enable automatic component updates in Chrome." },
  { key: "RemoteDebuggingAllowed", label: "Remote Debugging Allowed", group: "Miscellaneous", type: "boolean", description: "Allow remote debugging of the Chrome browser via DevTools Protocol." },
  { key: "TripleDESEnabled", label: "Triple-DES Cipher Suites", group: "Miscellaneous", type: "boolean", description: "Enable Triple-DES cipher suites in TLS (legacy compatibility)." },
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
  // Action Restrictions (kdeglobals)
  { key: "shell_access", label: "Shell Access", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "shell_access", type: "boolean" },
  { key: "run_command", label: "Run Command (KRunner)", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "run_command", type: "boolean" },
  { key: "action/logout", label: "Logout Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/logout", type: "boolean" },
  { key: "action/file_new", label: "File New Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/file_new", type: "boolean" },
  { key: "action/file_open", label: "File Open Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/file_open", type: "boolean" },
  { key: "action/file_save", label: "File Save Action", group: "Action Restrictions", file: "kdeglobals", iniGroup: "KDE Action Restrictions", iniKey: "action/file_save", type: "boolean" },
  // System Settings Restrictions (kdeglobals)
  { key: "kcm_restrictions", label: "System Settings Modules", group: "System Settings Restrictions", file: "kde5rc", iniGroup: "KDE Control Module Restrictions", iniKey: "__kcm_restrictions__", type: "kcm-restrictions" },
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
  // Appearance
  { key: "icon_theme", label: "Icon Theme", group: "Appearance", file: "kdeglobals", iniGroup: "Icons", iniKey: "Theme", type: "string" },
  { key: "wallpaperplugin", label: "Wallpaper Plugin", group: "Appearance", file: "plasma-org.kde.plasma.desktop-appletsrc", iniGroup: "Containments][1", iniKey: "wallpaperplugin", type: "string", defaultValue: "org.kde.image" },
  { key: "wp_Image", label: "Wallpaper Image Path", group: "Appearance", file: "plasma-org.kde.plasma.desktop-appletsrc", iniGroup: "Containments][1][Wallpaper][org.kde.image][General", iniKey: "Image", type: "string" },
  { key: "wp_FillMode", label: "Wallpaper Fill Mode", group: "Appearance", file: "plasma-org.kde.plasma.desktop-appletsrc", iniGroup: "Containments][1][Wallpaper][org.kde.image][General", iniKey: "FillMode", type: "select", selectOptions: ["0", "1", "2", "3", "6"], defaultValue: "2" },
  { key: "wp_Color", label: "Wallpaper Background Color", group: "Appearance", file: "plasma-org.kde.plasma.desktop-appletsrc", iniGroup: "Containments][1][Wallpaper][org.kde.image][General", iniKey: "Color", type: "color" },
  // Security
  { key: "url_restrictions", label: "URL Restrictions", group: "Security", file: "kdeglobals", iniGroup: "KDE URL Restrictions", iniKey: "__url_restrictions__", type: "url-restrictions" },
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

// Detect configured KConfig policy def keys from content JSON.
// Returns the policyDef.key (not the iniKey) for each configured entry.
function detectKConfigConfiguredKeys(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    const entries: { key?: string; group?: string }[] = parsed.entries || [];
    const result: string[] = [];
    let hasUrlRestrictions = false;
    let hasKcmRestrictions = false;
    for (const e of entries) {
      if (e.group === "KDE URL Restrictions") {
        hasUrlRestrictions = true;
        continue;
      }
      if (e.group === "KDE Control Module Restrictions") {
        hasKcmRestrictions = true;
        continue;
      }
      // Find the policy def whose iniKey matches the stored key
      const def = KCONFIG_ALL_POLICIES.find(p => p.iniKey === e.key);
      if (def) result.push(def.key);
    }
    if (hasUrlRestrictions) result.push("url_restrictions");
    if (hasKcmRestrictions) result.push("kcm_restrictions");
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
    type: policyDef.type === "boolean" ? "bool" : policyDef.type === "int" ? "int" : "string", // color and select also map to "string"
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
  let parsed: { entries: { key?: string; group?: string }[] } = { entries: [] };
  try { parsed = JSON.parse(existingContent || '{"entries":[]}'); } catch { /* ignore */ }
  if (!parsed.entries) parsed.entries = [];

  if (defKey === "url_restrictions") {
    parsed.entries = parsed.entries.filter(e => e.group !== "KDE URL Restrictions");
  } else if (defKey === "kcm_restrictions") {
    parsed.entries = parsed.entries.filter(e => e.group !== "KDE Control Module Restrictions");
  } else {
    const policyDef = KCONFIG_ALL_POLICIES.find(p => p.key === defKey);
    if (policyDef) {
      parsed.entries = parsed.entries.filter(e => e.key !== policyDef.iniKey);
    }
  }
  return JSON.stringify(parsed, null, 2);
}

// Parse URL restriction rules from KConfig content JSON.
function parseUrlRestrictionRules(content: string): UrlRestrictionRule[] {
  try {
    const parsed = JSON.parse(content || "{}");
    const entries: { group?: string; key?: string; value?: string }[] = parsed.entries || [];
    const rules: UrlRestrictionRule[] = [];
    for (const e of entries) {
      if (e.group !== "KDE URL Restrictions") continue;
      if (!e.key || !e.key.match(/^rule_\d+$/)) continue;
      const fields = (e.value || "").split(",");
      if (fields.length !== 8) continue;
      rules.push({
        action: (fields[0] as UrlRestrictionRule["action"]) || "open",
        referrerProtocol: fields[1],
        referrerHost: fields[2],
        referrerPath: fields[3],
        protocol: fields[4],
        host: fields[5],
        path: fields[6],
        enabled: fields[7] === "true",
      });
    }
    return rules;
  } catch { return []; }
}

// Build URL restriction entries into KConfig content JSON.
// Removes all existing KDE URL Restrictions entries and adds fresh rule_count + rule_N entries.
function buildUrlRestrictionContent(rules: UrlRestrictionRule[], existingContent: string): string {
  let parsed: { entries: { file: string; group: string; key: string; value: string; type: string; enforced: boolean }[] } = { entries: [] };
  try { parsed = JSON.parse(existingContent || '{"entries":[]}'); } catch { /* ignore */ }
  if (!parsed.entries) parsed.entries = [];

  // Remove all existing KDE URL Restrictions entries.
  parsed.entries = parsed.entries.filter(e => e.group !== "KDE URL Restrictions");

  // Add fresh entries.
  if (rules.length > 0) {
    parsed.entries.push({
      file: "kdeglobals",
      group: "KDE URL Restrictions",
      key: "rule_count",
      value: String(rules.length),
      type: "string",
      enforced: true,
    });
    for (let i = 0; i < rules.length; i++) {
      const r = rules[i];
      const value = [r.action, r.referrerProtocol, r.referrerHost, r.referrerPath, r.protocol, r.host, r.path, r.enabled ? "true" : "false"].join(",");
      parsed.entries.push({
        file: "kdeglobals",
        group: "KDE URL Restrictions",
        key: `rule_${i + 1}`,
        value,
        type: "string",
        enforced: true,
      });
    }
  }

  return JSON.stringify(parsed, null, 2);
}

// Parse KCM restriction modules from KConfig content JSON.
// Returns an array of module IDs that are restricted (value=false).
function parseKcmRestrictions(content: string): string[] {
  try {
    const parsed = JSON.parse(content || "{}");
    const entries: { group?: string; key?: string; value?: string }[] = parsed.entries || [];
    const modules: string[] = [];
    for (const e of entries) {
      if (e.group === "KDE Control Module Restrictions" && e.key) {
        modules.push(e.key);
      }
    }
    return modules;
  } catch { return []; }
}

// Build KCM restriction entries into KConfig content JSON.
// Removes all existing KDE Control Module Restrictions entries and adds fresh ones.
function buildKcmRestrictionContent(modules: string[], existingContent: string): string {
  let parsed: { entries: { file: string; group: string; key: string; value: string; type: string; enforced: boolean }[] } = { entries: [] };
  try { parsed = JSON.parse(existingContent || '{"entries":[]}'); } catch { /* ignore */ }
  if (!parsed.entries) parsed.entries = [];

  // Remove all existing KDE Control Module Restrictions entries.
  parsed.entries = parsed.entries.filter(e => e.group !== "KDE Control Module Restrictions");

  // Add fresh entries — each module set to false (restricted) and enforced.
  for (const mod of modules) {
    parsed.entries.push({
      file: "kde5rc",
      group: "KDE Control Module Restrictions",
      key: mod,
      value: "false",
      type: "bool",
      enforced: true,
    });
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
    const parsed = raw as { entries?: { key?: string; value?: string; enforced?: boolean; file?: string; group?: string }[] };
    const entries = parsed.entries || [];
    for (const entry of entries) {
      // Show URL restriction rules with human-readable summary
      if (entry.group === "KDE URL Restrictions" && entry.key && entry.key.match(/^rule_\d+$/)) {
        const fields = (entry.value || "").split(",");
        if (fields.length === 8) {
          const summary = `${fields[0]} ${fields[4] || "*"}://${fields[5] || "*"}${fields[6] ? "/" + fields[6] : ""} → ${fields[7] === "true" ? "allow" : "deny"}`;
          rows.push({
            setting: `Security › URL Restrictions › ${entry.key}`,
            value: summary,
            locked: entry.enforced !== undefined ? (entry.enforced ? "Yes" : "No") : null,
          });
        }
        continue;
      }
      // Skip rule_count in overview display
      if (entry.group === "KDE URL Restrictions" && entry.key === "rule_count") continue;

      // Show KCM restrictions with human-readable labels
      if (entry.group === "KDE Control Module Restrictions" && entry.key) {
        const mod = KCM_MODULES.find(m => m.id === entry.key);
        rows.push({
          setting: `System Settings Restrictions › ${mod ? mod.label : entry.key}`,
          value: "Restricted",
          locked: entry.enforced !== undefined ? (entry.enforced ? "Yes" : "No") : null,
        });
        continue;
      }

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
  const [urlRestrictionRules, setUrlRestrictionRules] = useState<UrlRestrictionRule[]>([]);
  const [customProtocolIndices, setCustomProtocolIndices] = useState<Set<number>>(new Set());
  const [kcmRestrictedModules, setKcmRestrictedModules] = useState<string[]>([]);
  const [kcmCustomInput, setKcmCustomInput] = useState("");

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
          // If url_restrictions is configured, load the rules
          if (configuredKeys.includes("url_restrictions")) {
            const rules = parseUrlRestrictionRules(policy.content);
            setUrlRestrictionRules(rules);
            const customIdxs = new Set<number>();
            rules.forEach((r, i) => { if (r.protocol !== "" && !KIO_PROTOCOLS.includes(r.protocol)) customIdxs.add(i); });
            setCustomProtocolIndices(customIdxs);
          }
          if (configuredKeys.includes("kcm_restrictions")) {
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
      setUrlRestrictionRules([]);
      setCustomProtocolIndices(new Set());
      setKcmRestrictedModules([]);
      setKcmCustomInput("");
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
      setUrlRestrictionRules([]);
      setCustomProtocolIndices(new Set());
      setKcmRestrictedModules([]);
      setKcmCustomInput("");
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
        if (kconfigSelectedKey === "url_restrictions") {
          finalContent = buildUrlRestrictionContent(urlRestrictionRules, contentRaw);
        } else if (kconfigSelectedKey === "kcm_restrictions") {
          finalContent = buildKcmRestrictionContent(kcmRestrictedModules, contentRaw);
        } else if (kconfigSelectedKey) {
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

    if (kconfigSelectedKey === "url_restrictions") {
      return renderUrlRestrictionsEditor();
    }

    if (kconfigSelectedKey === "kcm_restrictions") {
      return renderKcmRestrictionsEditor();
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
          {policyDef.type === "json" && (
            <FormGroup label="Value (JSON)" fieldId="cr-prop-json" helperText="Enter a valid JSON object or array">
              <TextArea
                id="cr-prop-json"
                value={typeof chromeValue === "string" ? chromeValue : JSON.stringify(chromeValue ?? {}, null, 2)}
                onChange={(_ev, val) => {
                  try {
                    updateChromeValue(JSON.parse(val));
                  } catch {
                    // store as raw string while editing, will be re-parsed on save
                    updateChromeValue(val);
                  }
                }}
                rows={8}
                style={{ fontFamily: "monospace", fontSize: "0.82rem" }}
                placeholder="{}"
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
              <FormSelectOption key={t.value} value={t.value} label={t.label} isDisabled={t.isDisabled} />
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
                    backgroundColor: isActive ? "#0066cc" : isHovered ? "#e7f1fa" : "transparent",
                    color: isActive ? "#fff" : t.isDisabled ? "#aaa" : "inherit",
                    borderBottom: "1px solid #d2d2d2",
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
