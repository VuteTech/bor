// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

export interface ServerVersion {
  version: string;
}

// getServerVersion fetches the running server version from the public
// /api/v1/version endpoint. No authentication is required.
export async function getServerVersion(): Promise<ServerVersion> {
  const res = await fetch("/api/v1/version", { credentials: "same-origin" });
  if (!res.ok) {
    throw new Error(`Failed to fetch server version: ${res.statusText}`);
  }
  return res.json() as Promise<ServerVersion>;
}
