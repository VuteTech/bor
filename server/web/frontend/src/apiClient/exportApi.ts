// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Policy export/import in the bor.dev/v1 YAML envelope
// (docs/policy-export-import-plan.md).

import { authHeaders } from "./authApi";

export interface ImportDocResult {
  doc: number;
  kind: string;
  name: string;
  status: "created" | "updated" | "skipped" | "error";
  message?: string;
}

export interface ImportReport {
  ok: boolean;
  dry_run: boolean;
  created: number;
  updated: number;
  skipped: number;
  errors: number;
  results: ImportDocResult[];
}

export type OnConflict = "error" | "skip" | "new-version";

/** Downloads selected policies (all when ids is empty) as a YAML bundle. */
export async function downloadPolicyExport(ids: string[], includeBindings: boolean): Promise<void> {
  const params = new URLSearchParams();
  if (ids.length > 0) params.set("ids", ids.join(","));
  if (includeBindings) params.set("include_bindings", "true");
  const res = await fetch(`/api/v1/policies/export?${params.toString()}`, {
    credentials: "same-origin",
    headers: authHeaders(),
  });
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
  const disposition = res.headers.get("Content-Disposition") || "";
  const match = /filename="([^"]+)"/.exec(disposition);
  const filename = match ? match[1] : "bor-policies.yaml";
  const blob = await res.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

/** Submits a bundle for import; dryRun previews without writing. */
export async function importPolicies(
  bundle: string,
  opts: { dryRun: boolean; onConflict: OnConflict }
): Promise<ImportReport> {
  const params = new URLSearchParams();
  if (opts.dryRun) params.set("dry_run", "true");
  params.set("on_conflict", opts.onConflict);
  const res = await fetch(`/api/v1/policies/import?${params.toString()}`, {
    method: "POST",
    credentials: "same-origin",
    headers: { ...authHeaders(), "Content-Type": "application/yaml" },
    body: bundle,
  });
  const body = await res.json().catch(() => null);
  // 422 carries a full report (validation failures); other errors carry {error}.
  if (!res.ok && (!body || !("results" in body || "ok" in body))) {
    throw new Error(body?.error || res.statusText);
  }
  return body as ImportReport;
}
