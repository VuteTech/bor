// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Disk encryption API client - the fleet recovery-key directory, per-node
 * detail, reveal (step-up protected) and rotate, plus the Tang server
 * registry (Settings -> Tang servers) and the generic step-up endpoint.
 * See docs/disk-encryption.md
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

/* ── Volumes and recovery keys ── */

export type RecoveryKeyStatus =
  | "pending"
  | "active"
  | "superseded"
  | "retired"
  | "expired"
  | "destroyed";

export interface RecoveryKeyMeta {
  id: string;
  volume_id: string;
  status: RecoveryKeyStatus;
  reason: string;
  keyslot: number | null;
  keyslot_fingerprint?: string;
  created_at: string;
  confirmed_at: string | null;
  retired_at?: string | null;
  destroy_after?: string | null;
  reveal_count: number;
  last_revealed_at?: string | null;
  last_revealed_by?: string;
  released_at?: string | null;
}

/** One keyslot of the volume's last reported state (protojson camelCase). */
export interface LuksKeyslotState {
  index?: number;
  kind?: string;
  kdf?: string;
  fingerprint?: string;
  tpm2Pcrs?: number[];
  clevisPin?: string;
  clevisThreshold?: number;
  tangUrls?: string[];
  tangSigningThumbprints?: string[];
  borEscrowId?: string;
}

export interface LuksVolumeState {
  luksUuid?: string;
  mappingName?: string;
  mountpoints?: string[];
  isSystem?: boolean;
  luksVersion?: number;
  cipher?: string;
  volumeKeyBits?: number;
  keyslots?: LuksKeyslotState[];
  externallyManagedBy?: string;
  initramfsStatus?: string;
}

export interface LuksVolume {
  id: string;
  node_id: string | null;
  node_name: string;
  machine_id?: string;
  luks_uuid: string;
  mapping_name: string;
  mountpoints: string[];
  is_system: boolean;
  luks_version: number;
  cipher: string;
  volume_key_bits: number;
  state?: LuksVolumeState;
  rotation_requested_at: string | null;
  rotation_reason?: string;
  last_reported_at: string;
  orphaned_at?: string | null;
  created_at: string;
  active_key?: RecoveryKeyMeta;
  clone_suspected?: boolean;
}

export interface DiskEncryptionSummary {
  volumes: number;
  encrypted_nodes: number;
  unencrypted_mounts: number;
  escrowed_active: number;
  rotations_overdue: number;
  externally_managed: number;
  clone_suspects: number;
  escrow_configured: boolean;
}

export interface LuksVolumePage {
  items: LuksVolume[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface LuksVolumeDetail {
  volume: LuksVolume;
  keys: RecoveryKeyMeta[];
}

export interface NodeDiskEncryption {
  node_id: string;
  tpm2_present: boolean | null;
  secure_boot: string;
  initramfs_generator: string;
  systemd_version: string;
  cryptsetup_version: string;
  clevis_version: string;
  unencrypted_system_mounts: { mountpoint: string; source: string; fstype: string }[];
  reported_at: string;
}

export interface NodeDiskEncryptionDetail {
  platform: NodeDiskEncryption | null;
  volumes: LuksVolume[];
}

export async function fetchDiskEncryptionSummary(): Promise<DiskEncryptionSummary> {
  return apiRequest<DiskEncryptionSummary>("/api/v1/disk-encryption/summary", {
    headers: authHeaders(),
  });
}

export async function fetchLuksVolumes(params: {
  search?: string;
  nodeId?: string;
  page?: number;
  perPage?: number;
}): Promise<LuksVolumePage> {
  const q = new URLSearchParams();
  if (params.search) q.set("search", params.search);
  if (params.nodeId) q.set("node_id", params.nodeId);
  if (params.page) q.set("page", String(params.page));
  if (params.perPage) q.set("per_page", String(params.perPage));
  const res = await apiRequest<Partial<LuksVolumePage>>(
    `/api/v1/disk-encryption/volumes?${q.toString()}`,
    { headers: authHeaders() },
  );
  return {
    items: res.items ?? [],
    total: res.total ?? 0,
    page: res.page ?? 1,
    per_page: res.per_page ?? (params.perPage ?? 25),
    total_pages: res.total_pages ?? 0,
  };
}

export async function fetchLuksVolume(id: string): Promise<LuksVolumeDetail> {
  return apiRequest<LuksVolumeDetail>(
    `/api/v1/disk-encryption/volumes/${encodeURIComponent(id)}`,
    { headers: authHeaders() },
  );
}

export async function fetchNodeDiskEncryption(nodeId: string): Promise<NodeDiskEncryptionDetail> {
  const res = await apiRequest<Partial<NodeDiskEncryptionDetail>>(
    `/api/v1/disk-encryption/nodes/${encodeURIComponent(nodeId)}`,
    { headers: authHeaders() },
  );
  return { platform: res.platform ?? null, volumes: res.volumes ?? [] };
}

export interface RevealRecoveryKeyResponse {
  recovery_key: string;
  escrow_id: string;
  keyslot: number | null;
  created_at: string;
  rotation_pending: boolean;
}

/**
 * Reveals an escrowed recovery key. Requires a fresh single-use step-up
 * token (issueStepUp) on top of the disk_encryption:reveal permission; the
 * server audits every attempt.
 */
export async function revealRecoveryKey(
  volumeId: string,
  reason: string,
  stepUpToken: string,
  escrowId?: string,
): Promise<RevealRecoveryKeyResponse> {
  return apiRequest<RevealRecoveryKeyResponse>(
    `/api/v1/disk-encryption/volumes/${encodeURIComponent(volumeId)}/reveal`,
    {
      method: "POST",
      headers: { ...authHeaders(), "X-Bor-Step-Up": stepUpToken },
      body: JSON.stringify({ reason, escrow_id: escrowId || undefined }),
    },
  );
}

export async function rotateRecoveryKey(volumeId: string): Promise<void> {
  await apiRequest<{ status: string }>(
    `/api/v1/disk-encryption/volumes/${encodeURIComponent(volumeId)}/rotate`,
    { method: "POST", headers: authHeaders(), body: "{}" },
  );
}

/* ── Step-up re-authentication ── */

export const STEP_UP_PURPOSE_REVEAL = "reveal_recovery_key";

/**
 * Re-authenticates the current user for one privileged action and returns a
 * single-use token (5 minutes). A TOTP code is required when the user has
 * one enrolled, or when the server requires MFA for the purpose.
 */
export async function issueStepUp(
  purpose: string,
  password: string,
  totpCode?: string,
): Promise<{ token: string; expires_in: number }> {
  return apiRequest<{ token: string; expires_in: number }>("/api/v1/auth/step-up", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ purpose, password, totp_code: totpCode || undefined }),
  });
}

/* ── Tang servers (Settings) ── */

export type TangCheckStatus = "never" | "ok" | "new_key" | "error";

export interface TangServer {
  id: string;
  name: string;
  url: string;
  /** Admin-confirmed thumbprints; [0] is the preferred signing key. */
  trusted_thumbprints: string[];
  /** What the server advertises now (may include unconfirmed keys). */
  advertised_thumbprints: string[];
  last_check_at: string | null;
  last_check_status: TangCheckStatus;
  last_check_error?: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
  /** Bound volumes per signing thumbprint, from agent inventory reports. */
  bound_volumes?: Record<string, number>;
}

export interface TangServerRequest {
  name: string;
  url: string;
  trusted_thumbprints: string[];
}

export async function fetchTangServers(): Promise<TangServer[]> {
  const res = await apiRequest<{ items?: TangServer[] }>("/api/v1/tang-servers", {
    headers: authHeaders(),
  });
  return res.items ?? [];
}

export async function createTangServer(req: TangServerRequest): Promise<TangServer> {
  return apiRequest<TangServer>("/api/v1/tang-servers", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function updateTangServer(id: string, req: TangServerRequest): Promise<TangServer> {
  return apiRequest<TangServer>(`/api/v1/tang-servers/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(req),
  });
}

export async function deleteTangServer(id: string): Promise<void> {
  await apiRequest<void>(`/api/v1/tang-servers/${encodeURIComponent(id)}`, {
    method: "DELETE",
    headers: authHeaders(),
  });
}

export interface TangProbeResponse {
  url: string;
  signing_thumbprints: string[];
  exchange_thumbprints: string[];
}

/**
 * Fetches a Tang server's advertisement so the admin can compare the signing
 * thumbprints with `tang-show-keys` output on the Tang host before trusting
 * them (explicit, human-verified trust on first use).
 */
export async function probeTangServer(url: string): Promise<TangProbeResponse> {
  return apiRequest<TangProbeResponse>("/api/v1/tang-servers/probe", {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify({ url }),
  });
}

export async function checkTangServer(id: string): Promise<TangServer> {
  return apiRequest<TangServer>(`/api/v1/tang-servers/${encodeURIComponent(id)}/check`, {
    method: "POST",
    headers: authHeaders(),
    body: "{}",
  });
}

/* ── Shared validation (mirrors server/internal/services/disk_encryption.go) ── */

/** RFC 7638 S256 thumbprint: 43 base64url characters. */
export const TANG_THUMBPRINT_RE = /^[A-Za-z0-9_-]{43}$/;

export function isValidTangThumbprint(thp: string): boolean {
  return TANG_THUMBPRINT_RE.test(thp);
}

export function isValidTangUrl(url: string): boolean {
  const lower = url.toLowerCase();
  return (
    (lower.startsWith("http://") || lower.startsWith("https://")) &&
    !/[\s"']/.test(url) &&
    url.length <= 2048
  );
}
