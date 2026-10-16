// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import { authHeaders } from "./authApi";
import type { KConfigCatalogueEntry } from "../views/Policies/editors/kconfigCatalogue";

async function apiRequest<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { credentials: "same-origin", ...init });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch { /* swallow */ }
    throw new Error(detail);
  }
  return res.json();
}

/** A System Settings module reported by a node. */
export interface KcmModuleInfo {
  id: string;
  name: string;
}

/**
 * The KDE settings catalogue the server holds: the built-in entries plus
 * the KConfigXT schema entries nodes reported. With a node id, only the
 * entries that node reported.
 */
export async function fetchKConfigCatalogue(nodeId?: string): Promise<KConfigCatalogueEntry[]> {
  const qs = nodeId ? `?node_id=${encodeURIComponent(nodeId)}` : "";
  return apiRequest<KConfigCatalogueEntry[]>(`/api/v1/kconfig/catalogue${qs}`, {
    headers: authHeaders(),
  });
}

/** System Settings modules reported by one node, or by any node. */
export async function fetchKConfigKcms(nodeId?: string): Promise<KcmModuleInfo[]> {
  const qs = nodeId ? `?node_id=${encodeURIComponent(nodeId)}` : "";
  return apiRequest<KcmModuleInfo[]>(`/api/v1/kconfig/kcms${qs}`, {
    headers: authHeaders(),
  });
}
