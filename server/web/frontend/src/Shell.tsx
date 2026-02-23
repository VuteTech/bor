// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Page,
  Masthead,
  MastheadMain,
  MastheadBrand,
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
} from "@patternfly/react-core";
import UserIcon from "@patternfly/react-icons/dist/esm/icons/user-icon";
import BarsIcon from "@patternfly/react-icons/dist/esm/icons/bars-icon";

import { checkSession, logout, getStoredToken, UserInfo } from "./apiClient/authApi";
import { setPermissions, clearPermissions, hasPermission } from "./apiClient/permissions";
import { LoginPage } from "./views/LoginPage";
import { DashboardPage } from "./views/Dashboard";
import { PoliciesPage } from "./views/Policies";
import { NodesPage } from "./views/Nodes";
import { NodeGroupsPage } from "./views/NodeGroups";
import { PolicyBindingsPage } from "./views/PolicyBindings";
import { SettingsPage } from "./views/Settings";
import logoWhite from "./assets/logo-white.svg";

type ScreenKey = "dashboard" | "policies" | "nodes" | "node-groups" | "policy-bindings" | "compliance" | "settings";

export const Shell: React.FC = () => {
  /* ── Auth state ── */
  const [isLoggedIn, setIsLoggedIn] = useState(false);
  const [currentUser, setCurrentUser] = useState<string>("");
  const [authChecked, setAuthChecked] = useState(false);

  const [activeScreen, setActiveScreen] = useState<ScreenKey>("dashboard");
  const [isUserMenuOpen, setIsUserMenuOpen] = useState(false);

  /* ── Validate existing session on mount ── */
  useEffect(() => {
    const existingToken = getStoredToken();
    if (!existingToken) {
      setAuthChecked(true);
      return;
    }
    checkSession()
      .then((user: UserInfo) => {
        setPermissions(user.permissions || []);
        setIsLoggedIn(true);
        setCurrentUser(user.full_name || user.username);
      })
      .catch(() => {
        clearPermissions();
        logout();
      })
      .finally(() => setAuthChecked(true));
  }, []);

  const handleLoggedIn = useCallback(
    (_token: string, user: { username: string; full_name: string }) => {
      // After login, fetch the full /me response to get permissions
      // before updating login state so the UI has permissions ready.
      checkSession()
        .then((me: UserInfo) => {
          setPermissions(me.permissions || []);
          setIsLoggedIn(true);
          setCurrentUser(me.full_name || me.username);
        })
        .catch(() => {
          // If permission fetch fails, still allow login but with empty permissions
          setIsLoggedIn(true);
          setCurrentUser(user.full_name || user.username);
        });
    },
    []
  );

  const performLogout = useCallback(() => {
    clearPermissions();
    logout();
    setIsLoggedIn(false);
    setCurrentUser("");
    setActiveScreen("dashboard");
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

  /* ── User dropdown items ── */
  const userDropdownItems = (
    <>
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
      <MastheadToggle>
        <PageToggleButton variant="plain" aria-label="Global navigation">
          <BarsIcon />
        </PageToggleButton>
      </MastheadToggle>
      <MastheadMain>
        <MastheadBrand>
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
        </MastheadBrand>
      </MastheadMain>
      <MastheadContent>
        <Toolbar id="masthead-toolbar" isFullHeight isStatic>
          <ToolbarContent>
            <ToolbarItem align={{ default: "alignRight" }}>
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
            {(hasPermission("user:manage") || hasPermission("role:manage") || hasPermission("user_group:view") || hasPermission("audit_log:view")) && (
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
          </div>
        </div>
      </PageSidebarBody>
    </PageSidebar>
  );

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
        return (
          <PageSection>
            <div className="pf-v5-c-content">
              <h1>Compliance</h1>
              <p>View compliance reports and status.</p>
            </div>
          </PageSection>
        );
      case "settings":
        return <SettingsPage />;
      default:
        return null;
    }
  };

  return (
    <Page
      header={mastheadBlock}
      sidebar={sideNavBlock}
      isManagedSidebar
      defaultManagedSidebarIsOpen={true}
    >
      {renderActiveScreen()}
    </Page>
  );
};
