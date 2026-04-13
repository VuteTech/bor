// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Page,
  Masthead,
  MastheadMain,
  MastheadBrand,
  MastheadLogo,
  MastheadContent,
  MastheadToggle,
  PageSidebar,
  PageSidebarBody,
  PageSection,
  Nav,
  NavList,
  NavItem,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
  MenuToggle,
  MenuToggleElement,
  Dropdown,
  DropdownItem,
  DropdownList,
  PageToggleButton,
  Button,
  Tooltip,
} from "@patternfly/react-core";
import UserIcon from "@patternfly/react-icons/dist/esm/icons/user-icon";
import BarsIcon from "@patternfly/react-icons/dist/esm/icons/bars-icon";
import SunIcon from "@patternfly/react-icons/dist/esm/icons/sun-icon";
import MoonIcon from "@patternfly/react-icons/dist/esm/icons/moon-icon";
import DesktopIcon from "@patternfly/react-icons/dist/esm/icons/desktop-icon";
import AdjustIcon from "@patternfly/react-icons/dist/esm/icons/adjust-icon";

import { checkSession, logout, getMFAStatus, getPublicConfig, UserInfo } from "./apiClient/authApi";
import { setPermissions, clearPermissions, hasPermission } from "./apiClient/permissions";
import { LoginPage } from "./views/LoginPage";
import { AccountModal } from "./views/Settings/AccountModal";
import { MFARequiredGate } from "./views/MFARequiredGate";
import { DashboardPage } from "./views/Dashboard";
import { PoliciesPage } from "./views/Policies";
import { NodesPage } from "./views/Nodes";
import { NodeGroupsPage } from "./views/NodeGroups";
import { PolicyBindingsPage } from "./views/PolicyBindings";
import { SettingsPage } from "./views/Settings";
import { AuditLogsPage } from "./views/AuditLogs";
import { CompliancePage } from "./views/Compliance";
import logoWhite from "./assets/logo-white.svg";

type ScreenKey = "dashboard" | "policies" | "nodes" | "node-groups" | "policy-bindings" | "compliance" | "audit-logs" | "settings";
type ThemeMode = "light" | "dark" | "system";

const PAGE_NAMES: Record<ScreenKey, string> = {
  dashboard:         "Dashboard",
  policies:          "Policies",
  nodes:             "Nodes",
  "node-groups":     "Node Groups",
  "policy-bindings": "Policy Bindings",
  compliance:        "Compliance",
  "audit-logs":      "Audit Logs",
  settings:          "Settings",
};

export const Shell: React.FC = () => {
  /* ── Theme state ── */
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => {
    return (localStorage.getItem("bor-theme") as ThemeMode) || "system";
  });

  useEffect(() => {
    const root = document.documentElement;
    const applyDark = (dark: boolean) => root.classList.toggle("pf-v6-theme-dark", dark);

    if (themeMode === "dark") {
      applyDark(true);
      return;
    }
    if (themeMode === "light") {
      applyDark(false);
      return;
    }
    // system: follow prefers-color-scheme
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    applyDark(mq.matches);
    const handler = (e: MediaQueryListEvent) => applyDark(e.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [themeMode]);

  const cycleTheme = () => {
    const next: ThemeMode =
      themeMode === "light" ? "dark" : themeMode === "dark" ? "system" : "light";
    localStorage.setItem("bor-theme", next);
    setThemeMode(next);
  };

  /* ── High contrast state ── */
  const [isHighContrast, setIsHighContrast] = useState(
    () => localStorage.getItem("bor-hc") === "true"
  );

  useEffect(() => {
    document.documentElement.classList.toggle("bor-theme-hc", isHighContrast);
    localStorage.setItem("bor-hc", String(isHighContrast));
  }, [isHighContrast]);

  /* ── Public server config ── */
  const [privacyPolicyURL, setPrivacyPolicyURL] = useState<string>("");

  useEffect(() => {
    getPublicConfig().then(cfg => setPrivacyPolicyURL(cfg.privacy_policy_url)).catch(() => {});
  }, []);

  /* ── Auth state ── */
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [currentUser, setCurrentUser] = useState<string>("");
  const [authChecked, setAuthChecked] = useState(false);

  const [activeScreen, setActiveScreen] = useState<ScreenKey>("dashboard");

  /* ── Update document title on screen change (WCAG 2.4.2) ── */
  useEffect(() => {
    document.title = `${PAGE_NAMES[activeScreen]} | Bor`;
  }, [activeScreen]);

  const [isUserMenuOpen, setIsUserMenuOpen] = useState(false);
  const [isAccountModalOpen, setIsAccountModalOpen] = useState(false);
  // Tracks the element that opened AccountModal so focus returns on close (WCAG 2.1.1)
  const accountModalTriggerRef = React.useRef<HTMLElement | null>(null);
  const [mfaGateActive, setMfaGateActive] = useState(false);

  /* ── After session is established, check if MFA setup is required ── */
  const applySession = useCallback(async (user: UserInfo) => {
    setPermissions(user.permissions || []);
    setCurrentUser(user.full_name || user.username);
    setIsLoggedIn(true);
    // Check whether MFA is enforced but not yet set up for this user.
    // A failure here is non-fatal — we simply don't show the gate.
    try {
      const mfa = await getMFAStatus();
      setMfaGateActive(mfa.mfa_required && !mfa.enabled);
    } catch {
      setMfaGateActive(false);
    }
  }, []);

  /* ── Validate existing session on mount ── */
  useEffect(() => {
    checkSession()
      .then((user: UserInfo) => applySession(user))
      .catch(() => {
        clearPermissions();
      })
      .finally(() => setAuthChecked(true));
  }, [applySession]);

  const handleLoggedIn = useCallback(
    (_token: string, user: { username: string; full_name: string }) => {
      checkSession()
        .then((me: UserInfo) => applySession(me))
        .catch(() => {
          // If /me fails, still allow login without the MFA gate check
          setPermissions([]);
          setIsLoggedIn(true);
          setCurrentUser(user.full_name || user.username);
        });
    },
    [applySession]
  );

  const performLogout = useCallback(() => {
    clearPermissions();
    logout().catch(() => { /* best-effort server notification */ });
    setIsLoggedIn(false);
    setCurrentUser("");
    setActiveScreen("dashboard");
    setMfaGateActive(false);
  }, []);

  /* ── Show login page if not signed in ── */
  if (!authChecked) {
    return (
      <PageSection>
        <div style={{ textAlign: "center", marginTop: 120 }}>Loading...</div>
      </PageSection>
    );
  }

  if (!isLoggedIn) {
    return <LoginPage onLoggedIn={handleLoggedIn} />;
  }

  if (mfaGateActive) {
    return (
      <MFARequiredGate
        onMFAConfigured={() => setMfaGateActive(false)}
        onLogout={performLogout}
      />
    );
  }

  /* ── User dropdown items ── */
  const userDropdownItems = (
    <>
      <DropdownItem
        key="security"
        onClick={() => {
          accountModalTriggerRef.current = document.activeElement as HTMLElement;
          setIsUserMenuOpen(false);
          setIsAccountModalOpen(true);
        }}
      >
        Account security
      </DropdownItem>
      <DropdownItem
        key="logout"
        onClick={() => {
          setIsUserMenuOpen(false);
          performLogout();
        }}
      >
        Log out
      </DropdownItem>
    </>
  );

  /* ── Header / Masthead ── */
  const mastheadBlock = (
    <Masthead>
      <MastheadMain>
        <MastheadToggle>
          <PageToggleButton variant="plain" aria-label="Global navigation">
            <BarsIcon />
          </PageToggleButton>
        </MastheadToggle>
        <MastheadBrand>
          <MastheadLogo>
            <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
              <img src={logoWhite} alt="Bor" style={{ height: "36px" }} />
              <span
                style={{
                  fontFamily: "RedHatDisplay, Overpass, Arial, sans-serif",
                  fontSize: "1.125rem",
                  fontWeight: 600,
                  color: "#fff",
                  letterSpacing: "0.02em",
                }}
              >
                Bor
              </span>
            </div>
          </MastheadLogo>
        </MastheadBrand>
      </MastheadMain>
      <MastheadContent>
        <Toolbar id="masthead-toolbar" isFullHeight isStatic>
          <ToolbarContent>
            {/* Separator + current page name */}
            <ToolbarItem style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
              <div style={{ width: 1, height: 20, background: "rgba(255,255,255,0.3)" }} />
              <span style={{ color: "#fff", fontSize: "1rem", fontWeight: 400, opacity: 0.9 }}>
                {PAGE_NAMES[activeScreen]}
              </span>
            </ToolbarItem>
            <ToolbarItem align={{ default: "alignEnd" }} style={{ display: "flex", alignItems: "center", gap: "0.25rem" }}>
              <Tooltip
                content={isHighContrast ? "High contrast on (click to disable)" : "High contrast off (click to enable)"}
                position="bottom"
              >
                <Button
                  variant="plain"
                  aria-label={isHighContrast ? "Disable high contrast" : "Enable high contrast"}
                  aria-pressed={isHighContrast}
                  onClick={() => setIsHighContrast(v => !v)}
                  style={{
                    color: "#fff",
                    padding: "0.375rem",
                    ...(isHighContrast && {
                      backgroundColor: "rgba(255,255,255,0.2)",
                      borderRadius: "3px",
                      outline: "2px solid #fff",
                    }),
                  }}
                >
                  <AdjustIcon />
                </Button>
              </Tooltip>
              <Tooltip
                content={
                  themeMode === "light"
                    ? "Light theme (click for dark)"
                    : themeMode === "dark"
                    ? "Dark theme (click for system)"
                    : "System theme (click for light)"
                }
                position="bottom"
              >
                <Button
                  variant="plain"
                  aria-label={
                    themeMode === "light"
                      ? "Switch to dark theme"
                      : themeMode === "dark"
                      ? "Switch to system theme"
                      : "Switch to light theme"
                  }
                  onClick={cycleTheme}
                  style={{ color: "#fff", padding: "0.375rem" }}
                >
                  {themeMode === "light" ? (
                    <SunIcon />
                  ) : themeMode === "dark" ? (
                    <MoonIcon />
                  ) : (
                    <DesktopIcon />
                  )}
                </Button>
              </Tooltip>
              <Dropdown
                isOpen={isUserMenuOpen}
                onSelect={() => setIsUserMenuOpen(false)}
                onOpenChange={(isOpen: boolean) => setIsUserMenuOpen(isOpen)}
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={() => setIsUserMenuOpen(!isUserMenuOpen)}
                    isExpanded={isUserMenuOpen}
                    icon={<UserIcon />}
                  >
                    {currentUser || "User"}
                  </MenuToggle>
                )}
              >
                <DropdownList>{userDropdownItems}</DropdownList>
              </Dropdown>
            </ToolbarItem>
          </ToolbarContent>
        </Toolbar>
      </MastheadContent>
    </Masthead>
  );

  /* ── Sidebar ── */
  const sideNavBlock = (
    <PageSidebar>
      <PageSidebarBody>
        <Nav
          onSelect={(_ev, result) => {
            const target = result.itemId as ScreenKey;
            setActiveScreen(target);
          }}
        >
          <NavList>
            <NavItem itemId="dashboard" isActive={activeScreen === "dashboard"}>
              Dashboard
            </NavItem>
            <NavItem itemId="policies" isActive={activeScreen === "policies"}>
              Policies
            </NavItem>
            <NavItem itemId="nodes" isActive={activeScreen === "nodes"}>
              Nodes
            </NavItem>
            <NavItem itemId="node-groups" isActive={activeScreen === "node-groups"}>
              Node Groups
            </NavItem>
            <NavItem itemId="policy-bindings" isActive={activeScreen === "policy-bindings"}>
              Policy Bindings
            </NavItem>
            <NavItem itemId="compliance" isActive={activeScreen === "compliance"}>
              Compliance
            </NavItem>
            {hasPermission("audit_log:view") && (
              <NavItem itemId="audit-logs" isActive={activeScreen === "audit-logs"}>
                Audit Logs
              </NavItem>
            )}
            {(hasPermission("user:manage") || hasPermission("role:manage") || hasPermission("user_group:view")) && (
              <NavItem itemId="settings" isActive={activeScreen === "settings"}>
                Settings
              </NavItem>
            )}
          </NavList>
        </Nav>
        <div
          style={{
            marginTop: "auto",
            padding: "1rem",
            borderTop: "1px solid #3c3f42",
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            gap: "0.75rem",
          }}
        >
          <div
            style={{
              fontSize: "0.75rem",
              color: "#999",
              textAlign: "center",
              lineHeight: "1.4",
            }}
          >
            &copy; {new Date().getFullYear()} Bor. All rights reserved.{" "}
            <a
              href="https://getbor.dev"
              target="_blank"
              rel="noopener noreferrer"
              style={{ color: "#2E7D32", textDecoration: "none" }}
            >
              getbor.dev
            </a>
            {privacyPolicyURL && (
              <>
                {" · "}
                <a
                  href={privacyPolicyURL}
                  target="_blank"
                  rel="noopener noreferrer"
                  style={{ color: "#999", textDecoration: "none" }}
                >
                  Privacy Policy
                </a>
              </>
            )}
          </div>
        </div>
      </PageSidebarBody>
    </PageSidebar>
  );

  const PAGE_SUBTITLES: Record<ScreenKey, string> = {
    dashboard:          "Overview of fleet health and policy compliance.",
    policies:           "Manage desktop policies for your Linux fleet. Each update creates a new version.",
    nodes:              "Manage and monitor connected desktop agents.",
    "node-groups":      "Manage node groups and generate enrollment tokens for agent registration.",
    "policy-bindings":  "Bind policies to node groups. Nodes inherit policies through group membership.",
    compliance:         "Track policy enforcement status across your fleet.",
    "audit-logs":       "Track system changes and security events.",
    settings:           "Manage users, roles, and system configuration.",
  };

  const subtitleStrip = PAGE_SUBTITLES[activeScreen] ? (
    <PageSection
      variant="light"
      padding={{ default: "paddingSm" }}
      style={{ borderBottom: "1px solid var(--pf-t--global--border--color--default)" }}
    >
      <span style={{ color: "#6a6e73", fontSize: "0.875rem" }}>
        {PAGE_SUBTITLES[activeScreen]}
      </span>
    </PageSection>
  ) : null;

  /* ── Active screen content ── */
  const renderActiveScreen = () => {
    switch (activeScreen) {
      case "dashboard":
        return <DashboardPage />;
      case "policies":
        return <PoliciesPage />;
      case "nodes":
        return <NodesPage />;
      case "node-groups":
        return <NodeGroupsPage />;
      case "policy-bindings":
        return <PolicyBindingsPage />;
      case "compliance":
        return <CompliancePage />;
      case "audit-logs":
        return <AuditLogsPage />;
      case "settings":
        return <SettingsPage />;
      default:
        return null;
    }
  };

  return (
    <>
      {/* Skip navigation — first focusable element, visible on focus (WCAG 2.4.1) */}
      <a href="#bor-main-content" className="bor-skip-nav">
        Skip to main content
      </a>
      <Page
        masthead={mastheadBlock}
        sidebar={sideNavBlock}
        isManagedSidebar
        defaultManagedSidebarIsOpen={true}
        mainContainerId="bor-main-content"
      >
        {subtitleStrip}
        {renderActiveScreen()}
      </Page>
      <AccountModal
        isOpen={isAccountModalOpen}
        onClose={() => {
          setIsAccountModalOpen(false);
          setTimeout(() => accountModalTriggerRef.current?.focus(), 0);
        }}
      />
    </>
  );
};
