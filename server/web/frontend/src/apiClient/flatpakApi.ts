// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Flatpak API client — server-side repository settings, the indexed
 * application catalog used by the policy editor, and the `.flatpakrepo`
 * probe. See docs/flatpak-policy-plan.md §6.3 for the contract.
 */

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
  if (res.status === 204) return undefined as T;
  return res.json();
}

/* ── Repositories (Settings) ── */

export type FlatpakRefreshStatus = "never" | "ok" | "error" | "running" | "disabled";

export interface FlatpakRepository {
  id: string;
  name: string;
  title: string;
  url: string;
  flatpakrepo_url: string;
  homepage: string;
  comment: string;
  description: string;
  icon_url: string;
  has_gpg_key: boolean;
  gpg_key_id: string;
  collection_id: string;
  default_branch: string;
  subset: string;
  /** "" = derived from the repository URL. */
  appstream_url: string;
  arches: string[];
  catalog_enabled: boolean;
  refresh_interval_s: number;
  builtin: boolean;
  last_refresh_at: string | null;
  last_refresh_status: FlatpakRefreshStatus;
  last_refresh_error: string;
  app_count: number;
  created_at: string;
  updated_at: string;
}

export interface FlatpakRepositoryRequest {
  name: string;
  title: string;
  url: string;
  flatpakrepo_url: string;
  homepage: string;
  comment: string;
  description: string;
  icon_url: string;
  /** base64; on PUT an empty/omitted value keeps the stored key. */
  gpg_key_data?: string;
  /** PUT only: remove the stored key. */
  clear_gpg_key?: boolean;
  gpg_key_id: string;
  collection_id: string;
  default_branch: string;
  subset: string;
  appstream_url: string;
  arches: string[];
  catalog_enabled: boolean;
  refresh_interval_s: number;
}

export interface FlatpakRepositoryList {
  items: FlatpakRepository[];
  /** False when the server runs with BOR_FLATPAK_CATALOG_REFRESH=false. */
  refresh_enabled: boolean;
}

export async function fetchFlatpakRepositories(): Promise<FlatpakRepositoryList> {
  const res = await apiRequest<Partial<FlatpakRepositoryList>>("/api/v1/flatpak-repos", {
    headers: authHeaders(),
  });
  return { items: res.items ?? [], refresh_enabled: res.refresh_enabled !== false };
}

export async function fetchFlatpakRepository(id: string): Promise<FlatpakRepository> {
  return apiRequest<FlatpakRepository>(`/api/v1/flatpak-repos/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  });
}

export async function createFlatpakRepository(req: FlatpakRepositoryRequest): Promise<FlatpakRepository> {
  return apiRequest<FlatpakRepository>("/api/v1/flatpak-repos", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateFlatpakRepository(
  id: string,
  req: FlatpakRepositoryRequest,
): Promise<FlatpakRepository> {
  return apiRequest<FlatpakRepository>(`/api/v1/flatpak-repos/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteFlatpakRepository(id: string): Promise<void> {
  await apiRequest<void>(`/api/v1/flatpak-repos/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

export async function refreshFlatpakRepository(id: string): Promise<{ status: string }> {
  return apiRequest<{ status: string }>(`/api/v1/flatpak-repos/${encodeURIComponent(id)}/refresh`, {
    method: "POST",
    headers: authHeaders(),
  });
}

/** Air-gapped path: upload an AppStream catalog (appstream.xml.gz) by hand. */
export async function uploadFlatpakCatalog(
  id: string,
  file: File,
  arch: string,
): Promise<{ app_count: number }> {
  const formData = new FormData();
  formData.append("file", file);
  formData.append("arch", arch);
  // Drop Content-Type from authHeaders — the browser sets it automatically
  // with the correct multipart boundary when body is FormData.
  const { "Content-Type": _ct, ...csrfHeader } = authHeaders();
  void _ct;
  return apiRequest<{ app_count: number }>(
    `/api/v1/flatpak-repos/${encodeURIComponent(id)}/catalog-upload`,
    { method: "POST", headers: csrfHeader, body: formData },
  );
}

/* ── .flatpakrepo probe ── */

export interface FlatpakRemoteInfo {
  name: string;
  title: string;
  url: string;
  homepage: string;
  comment: string;
  description: string;
  icon_url: string;
  /** base64 keyring, "" when the file carries none. */
  gpg_key_data: string;
  collection_id: string;
  default_branch: string;
  subset: string;
  warning: string;
}

export async function probeFlatpakRemote(flatpakrepoUrl: string): Promise<FlatpakRemoteInfo> {
  return apiRequest<FlatpakRemoteInfo>("/api/v1/flatpak-remote-info", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ flatpakrepo_url: flatpakrepoUrl }),
  });
}

/* ── Catalog (policy editor) ── */

export interface FlatpakCatalogRepo {
  id: string;
  name: string;
  title: string;
  url: string;
  homepage: string;
  comment: string;
  subset: string;
  collection_id: string;
  default_branch: string;
  /** base64 keyring, "" when none. */
  gpg_key_data: string;
  app_count: number;
  catalog_enabled: boolean;
  last_refresh_status: FlatpakRefreshStatus;
}

export async function fetchFlatpakCatalogRepos(): Promise<FlatpakCatalogRepo[]> {
  const res = await apiRequest<{ items?: FlatpakCatalogRepo[] }>("/api/v1/flatpak-catalog/repos", {
    headers: authHeaders(),
  });
  return res.items ?? [];
}

export interface FlatpakCatalogApp {
  repo_id: string;
  repo_name: string;
  app_id: string;
  arch: string;
  branch: string;
  ref: string;
  kind: string;
  name: string;
  summary: string;
  developer: string;
  project_license: string;
  homepage: string;
  categories: string[];
  latest_version: string;
  latest_release_at: string | null;
  verified: boolean;
  runtime: string;
  has_icon: boolean;
}

export interface FlatpakCatalogAppDetail extends FlatpakCatalogApp {
  description: string;
  keywords: string[];
  content_rating: string;
  branches: string[];
}

export interface FlatpakCatalogSearch {
  search?: string;
  repo?: string;
  /** Comma-separated component kinds; server default is desktop + console apps. */
  kind?: string;
  verified?: boolean;
  arch?: string;
  page?: number;
  per_page?: number;
}

export interface FlatpakCatalogAppPage {
  items: FlatpakCatalogApp[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export async function searchFlatpakCatalog(q: FlatpakCatalogSearch): Promise<FlatpakCatalogAppPage> {
  const params = new URLSearchParams();
  if (q.search) params.set("search", q.search);
  if (q.repo) params.set("repo", q.repo);
  if (q.kind) params.set("kind", q.kind);
  if (q.verified !== undefined) params.set("verified", q.verified ? "true" : "false");
  if (q.arch) params.set("arch", q.arch);
  if (q.page) params.set("page", String(q.page));
  if (q.per_page) params.set("per_page", String(q.per_page));
  const res = await apiRequest<Partial<FlatpakCatalogAppPage>>(
    `/api/v1/flatpak-catalog/apps?${params.toString()}`,
    { headers: authHeaders() },
  );
  return {
    items: res.items ?? [],
    total: res.total ?? 0,
    page: res.page ?? 1,
    per_page: res.per_page ?? (q.per_page ?? 20),
    total_pages: res.total_pages ?? 0,
  };
}

export async function fetchFlatpakCatalogApp(repoName: string, appId: string): Promise<FlatpakCatalogAppDetail> {
  return apiRequest<FlatpakCatalogAppDetail>(
    `/api/v1/flatpak-catalog/apps/${encodeURIComponent(repoName)}/${encodeURIComponent(appId)}`,
    { headers: authHeaders() },
  );
}

/** URL of the proxied/cached catalog icon, usable directly as an <img src>. */
export function flatpakCatalogIconUrl(repoName: string, appId: string): string {
  return `/api/v1/flatpak-catalog/icon/${encodeURIComponent(repoName)}/${encodeURIComponent(appId)}`;
}

/* ── Shared identifier rules (mirror server/internal/services/flatpak.go) ── */

export const FLATPAK_REMOTE_NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;
export const FLATPAK_APP_ID_RE = /^[A-Za-z_][A-Za-z0-9_.-]{5,254}$/;
export const FLATPAK_BRANCH_RE = /^[A-Za-z0-9._-]{1,64}$/;
export const FLATPAK_FILTER_REF_RE = /^[A-Za-z0-9._*/-]{1,256}$/;

export function isValidFlatpakRemoteName(name: string): boolean {
  return FLATPAK_REMOTE_NAME_RE.test(name) && !name.includes("..");
}

export function isValidFlatpakAppId(id: string): boolean {
  return FLATPAK_APP_ID_RE.test(id) && id.includes(".") && !id.includes("..");
}

export function isValidFlatpakRemoteUrl(url: string): boolean {
  const lower = url.toLowerCase();
  return (lower.startsWith("https://") || lower.startsWith("oci+https://")) && !/[\s"']/.test(url);
}

export function isValidFlatpakBranch(branch: string): boolean {
  return branch === "" || FLATPAK_BRANCH_RE.test(branch);
}

export function isValidFlatpakFilterRef(ref: string): boolean {
  return FLATPAK_FILTER_REF_RE.test(ref) && !ref.includes("..");
}

/** Reads a File as base64 (no data: prefix), for GPG keyring uploads. */
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result;
      if (typeof result !== "string") {
        reject(new Error("Could not read file"));
        return;
      }
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.onerror = () => reject(reader.error ?? new Error("Could not read file"));
    reader.readAsDataURL(file);
  });
}
