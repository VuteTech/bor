// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState } from "react";
import {
  PageSection,
  Title,
  Tabs,
  Tab,
  TabTitleText,
} from "@patternfly/react-core";
import { hasPermission } from "../../apiClient/permissions";
import { UsersTab } from "./UsersTab";
import { RolesTab } from "./RolesTab";
import { UserGroupsTab } from "./UserGroupsTab";
import { AuditLogsTab } from "./AuditLogsTab";
import { AgentNotificationsTab } from "./AgentNotificationsTab";

export const SettingsPage: React.FC = () => {
  const canUsers = hasPermission("user:manage");
  const canRoles = hasPermission("role:manage");
  const canUserGroups = hasPermission("user_group:view");
  const canAuditLogs = hasPermission("audit_log:view");
  const canSettings = hasPermission("settings:manage");

  const [activeTab, setActiveTab] = useState<string>(
    canUsers ? "users" : canRoles ? "roles" : canUserGroups ? "user-groups" : canAuditLogs ? "audit-logs" : canSettings ? "agent-notifications" : ""
  );

  if (!canUsers && !canRoles && !canUserGroups && !canAuditLogs && !canSettings) {
    return (
      <PageSection>
        <Title headingLevel="h1">Access Denied</Title>
        <p>You do not have permission to access Settings.</p>
      </PageSection>
    );
  }

  return (
    <PageSection>
      <Title headingLevel="h1" style={{ marginBottom: 16 }}>
        Settings
      </Title>
      <Tabs
        activeKey={activeTab}
        onSelect={(_ev, key) => setActiveTab(key as string)}
      >
        {canUsers && (
          <Tab eventKey="users" title={<TabTitleText>Users</TabTitleText>}>
            <div style={{ paddingTop: 16 }}>
              <UsersTab />
            </div>
          </Tab>
        )}
        {canRoles && (
          <Tab eventKey="roles" title={<TabTitleText>Roles</TabTitleText>}>
            <div style={{ paddingTop: 16 }}>
              <RolesTab />
            </div>
          </Tab>
        )}
        {canUserGroups && (
          <Tab eventKey="user-groups" title={<TabTitleText>User Groups</TabTitleText>}>
            <div style={{ paddingTop: 16 }}>
              <UserGroupsTab />
            </div>
          </Tab>
        )}
        {canAuditLogs && (
          <Tab eventKey="audit-logs" title={<TabTitleText>Audit Logs</TabTitleText>}>
            <div style={{ paddingTop: 16 }}>
              <AuditLogsTab />
            </div>
          </Tab>
        )}
        {canSettings && (
          <Tab eventKey="agent-notifications" title={<TabTitleText>Agent Notifications</TabTitleText>}>
            <div style={{ paddingTop: 16 }}>
              <AgentNotificationsTab />
            </div>
          </Tab>
        )}
      </Tabs>
    </PageSection>
  );
};
