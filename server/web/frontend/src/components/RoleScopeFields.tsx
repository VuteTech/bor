// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// RoleScopeFields: the scope part of a role-assignment form. A role is
// assigned either globally or scoped to one node group (delegated
// administration).

import React, { useEffect, useState } from "react";
import {
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
} from "@patternfly/react-core";
import { fetchNodeGroups, NodeGroup } from "../apiClient/nodeGroupsApi";

export type RoleScopeType = "global" | "node_group";

/** The scope fields' value, as sent to the role-binding APIs. */
export interface RoleScopeValue {
  scopeType: RoleScopeType;
  nodeGroupId: string;
}

export const GLOBAL_SCOPE: RoleScopeValue = { scopeType: "global", nodeGroupId: "" };

/** Whether the value is complete enough to submit. */
export function isRoleScopeComplete(v: RoleScopeValue): boolean {
  return v.scopeType === "global" || v.nodeGroupId !== "";
}

/** Request fields for a role-binding create call. */
export function roleScopeRequest(v: RoleScopeValue): { scope_type: RoleScopeType; scope_id?: string } {
  return v.scopeType === "global"
    ? { scope_type: "global" }
    : { scope_type: "node_group", scope_id: v.nodeGroupId };
}

/**
 * Human-readable scope of an existing binding. When node-group scopes are
 * not enabled (scopesEnabled false), a scoped binding grants nothing, and the
 * label says so.
 */
export function roleScopeLabel(
  scopeType: string | undefined,
  scopeId: string | undefined,
  nodeGroups: NodeGroup[],
  scopesEnabled = true
): string {
  if (!scopeType || scopeType === "global") return "Global";
  const group = nodeGroups.find((g) => g.id === scopeId);
  const label = `Node group: ${group ? group.name : scopeId ?? "unknown"}`;
  return scopesEnabled ? label : `${label} (inactive in this edition)`;
}

/**
 * Whether the scope picker and scope column should be shown: when the
 * edition enables node-group scopes, or when scoped bindings already exist
 * (so they stay visible and removable after the feature is switched off).
 */
export function showRoleScopes(
  scopesEnabled: boolean,
  bindings: { scope_type?: string }[]
): boolean {
  return scopesEnabled || bindings.some((b) => b.scope_type && b.scope_type !== "global");
}

/**
 * Loads the node groups the current user can see, for the scope picker and
 * scope labels. Failures leave the list empty: the picker then only offers
 * global scope, which the server validates anyway.
 */
export function useScopeNodeGroups(): NodeGroup[] {
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  useEffect(() => {
    let active = true;
    fetchNodeGroups()
      .then((g) => {
        if (active) setGroups(g);
      })
      .catch(() => {
        if (active) setGroups([]);
      });
    return () => {
      active = false;
    };
  }, []);
  return groups;
}

interface RoleScopeFieldsProps {
  /** Prefix for element ids, unique per form. */
  idPrefix: string;
  value: RoleScopeValue;
  onChange: (v: RoleScopeValue) => void;
  nodeGroups: NodeGroup[];
}

export const RoleScopeFields: React.FC<RoleScopeFieldsProps> = ({
  idPrefix,
  value,
  onChange,
  nodeGroups,
}) => {
  const scopeId = `${idPrefix}-scope`;
  const groupId = `${idPrefix}-scope-group`;
  const helpId = `${idPrefix}-scope-help`;

  return (
    <>
      <FormGroup label="Scope" isRequired fieldId={scopeId}>
        <select
          id={scopeId}
          className="pf-v6-c-form-control"
          value={value.scopeType}
          aria-describedby={helpId}
          onChange={(e) =>
            onChange({ scopeType: e.target.value as RoleScopeType, nodeGroupId: "" })
          }
        >
          <option value="global">Global (everywhere)</option>
          <option value="node_group" disabled={nodeGroups.length === 0}>
            One node group
          </option>
        </select>
        <FormHelperText>
          <HelperText id={helpId}>
            <HelperTextItem>
              A node-group scope grants the role&apos;s permissions on nodes, node groups,
              policies, policy bindings and compliance for that group only. Permissions on
              users, roles, settings, audit logs and disk encryption always need a global
              assignment.
            </HelperTextItem>
          </HelperText>
        </FormHelperText>
      </FormGroup>
      {value.scopeType === "node_group" && (
        <FormGroup label="Node group" isRequired fieldId={groupId}>
          <select
            id={groupId}
            className="pf-v6-c-form-control"
            value={value.nodeGroupId}
            onChange={(e) => onChange({ scopeType: "node_group", nodeGroupId: e.target.value })}
          >
            <option value="">Select a node group...</option>
            {nodeGroups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </FormGroup>
      )}
    </>
  );
};
