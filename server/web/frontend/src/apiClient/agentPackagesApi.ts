// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import { authHeaders } from "./authApi";

async function apiRequest<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: "same-origin", ...init });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch {
      /* swallow */
    }
    throw new Error(detail);
  }
  return res.json();
}

/* ── Types (mirror server/internal/api/agent_repo.go) ── */

export interface AgentRepoFile {
  path: string;
  format: "deb" | "rpm" | "apk" | "arch";
  arch: string;
  size: number;
  sha256: string;
}

export interface AgentRepoManifest {
  version: string;
  apk_version: string;
  generated_at: string;
  signed: boolean;
  files: AgentRepoFile[];
}

export interface AgentPackagesResponse {
  repo_available: boolean;
  server_version: string;
  version_match: boolean;
  manifest?: AgentRepoManifest;
}

/* ── API ── */

// fetchAgentPackages returns the agent package repository manifest served by
// this Bor instance (requires node:view). repo_available=false means the
// server ships without the repository — the deploy UI hides itself.
export async function fetchAgentPackages(): Promise<AgentPackagesResponse> {
  return apiRequest<AgentPackagesResponse>("/api/v1/agent-packages", {
    headers: authHeaders(),
  });
}
