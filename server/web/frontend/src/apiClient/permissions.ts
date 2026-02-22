// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// permissions.ts — frontend permission utility
// Stores the current user's permission set and provides a lookup function.
// Permissions are precomputed by the backend and sent as "resource:action" strings.

let currentPermissions: Set<string> = new Set();

/** Replace the stored permission set (called after login / session check). */
export function setPermissions(permissions: string[]): void {
  currentPermissions = new Set(permissions);
}

/** Clear stored permissions (called on logout). */
export function clearPermissions(): void {
  currentPermissions = new Set();
}

/**
 * Check whether the current user has the given permission.
 * @param permission  A "resource:action" string, e.g. "policy:edit".
 */
export function hasPermission(permission: string): boolean {
  return currentPermissions.has(permission);
}
