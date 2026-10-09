// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// permissions.ts — frontend permission utility
// Stores the current user's permission set and provides a lookup function.
// Permissions are precomputed by the backend and sent as "resource:action" strings.
// A permission may be held globally or only for some node groups (delegated
// administration); the backend enforces the scope, the UI uses it to hide
// actions that would be refused.

/** Where one permission is held. */
export interface PermissionScope {
  global: boolean;
  node_group_ids: string[];
}

/** Optional server features, as reported by /api/v1/auth/me. */
export const FEATURE_NODE_GROUP_SCOPED_RBAC = "node_group_scoped_rbac";

let currentPermissions: Set<string> = new Set();
let currentScopes: Record<string, PermissionScope> = {};
let currentFeatures: Set<string> = new Set();

/** Replace the stored permission set (called after login / session check). */
export function setPermissions(
  permissions: string[],
  scopes?: Record<string, PermissionScope>,
  features?: string[]
): void {
  currentPermissions = new Set(permissions);
  currentScopes = scopes ?? {};
  currentFeatures = new Set(features ?? []);
}

/** Clear stored permissions (called on logout). */
export function clearPermissions(): void {
  currentPermissions = new Set();
  currentScopes = {};
  currentFeatures = new Set();
}

/** Whether the server's edition currently enables an optional feature. */
export function hasFeature(feature: string): boolean {
  return currentFeatures.has(feature);
}

/**
 * Check whether the current user has the given permission anywhere
 * (globally or for at least one node group).
 * @param permission  A "resource:action" string, e.g. "policy:edit".
 */
export function hasPermission(permission: string): boolean {
  return currentPermissions.has(permission);
}

/**
 * Check whether the current user holds the permission globally. Use it for
 * actions that cannot be confined to a node group, such as creating a new
 * node group. A server that predates scoped permissions sends no scope
 * information; every permission was global there.
 */
export function hasGlobalPermission(permission: string): boolean {
  if (!currentPermissions.has(permission)) return false;
  const scope = currentScopes[permission];
  return scope === undefined || scope.global;
}
