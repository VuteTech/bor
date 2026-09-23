// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared content model for the DiskEncryption policy editor: the protojson
 * (camelCase, enum names as strings) shape stored in `Policy.content`, a
 * defensive parser and a compact serializer.
 * Mirrors proto/policy/disk_encryption.proto.
 */

import { isValidTangThumbprint, isValidTangUrl } from "../../apiClient/diskEncryptionApi";

export type LuksVolumeScope =
  | "LUKS_VOLUME_SCOPE_SYSTEM"
  | "LUKS_VOLUME_SCOPE_MOUNTPOINTS"
  | "LUKS_VOLUME_SCOPE_ALL_FIXED";

export type TpmPcrProfile =
  | "TPM_PCR_PROFILE_SECURE_BOOT"
  | "TPM_PCR_PROFILE_SECURE_BOOT_SHIM"
  | "TPM_PCR_PROFILE_CUSTOM";

export type InitramfsMode = "INITRAMFS_MODE_VERIFY_ONLY" | "INITRAMFS_MODE_MANAGE";

export interface DiskEncTangServer {
  url: string;
  /** Preferred signing key (RFC 7638 S256, base64url, 43 chars). */
  thumbprint: string;
  /** Older keys still trusted; bindings made with them get re-bound. */
  acceptedThumbprints: string[];
  /** Registry row it was copied from (UI cache; ignored by the agent). */
  registryId: string;
}

export interface DiskEncContent {
  requireEncryption: boolean;
  volumeScope: LuksVolumeScope;
  mountpoints: string[];
  minVolumeKeyBits: number;
  allowedCiphers: string[];
  requireTpm2: boolean;
  requireSecureBoot: boolean;
  tpm2: {
    enabled: boolean;
    pcrProfile: TpmPcrProfile;
    pcrs: number[];
    autoReseal: boolean;
    allowFirmwarePcrs: boolean;
  };
  tang: {
    enabled: boolean;
    servers: DiskEncTangServer[];
    threshold: number;
  };
  recovery: {
    escrow: boolean;
    rotationIntervalDays: number;
    rotateAfterReveal: boolean;
    serverAssistedRotation: boolean;
  };
  initramfsMode: InitramfsMode;
  adoptExisting: boolean;
}

export const DEFAULT_ROTATION_DAYS = 180;

export const VOLUME_SCOPE_OPTIONS: { value: LuksVolumeScope; label: string; description: string }[] = [
  {
    value: "LUKS_VOLUME_SCOPE_SYSTEM",
    label: "System volumes",
    description: "Volumes backing /, /usr, /var, /home and active swap",
  },
  {
    value: "LUKS_VOLUME_SCOPE_MOUNTPOINTS",
    label: "Specific mountpoints",
    description: "Only the volumes backing the listed mountpoints",
  },
  {
    value: "LUKS_VOLUME_SCOPE_ALL_FIXED",
    label: "All fixed disks",
    description: "Every active LUKS mapping on a non-removable disk",
  },
];

export const PCR_PROFILE_OPTIONS: { value: TpmPcrProfile; label: string; description: string }[] = [
  {
    value: "TPM_PCR_PROFILE_SECURE_BOOT",
    label: "Secure Boot (PCR 7)",
    description: "Unseals while the Secure Boot state is unchanged - the recommended default",
  },
  {
    value: "TPM_PCR_PROFILE_SECURE_BOOT_SHIM",
    label: "Secure Boot + shim (PCR 7 + 14)",
    description: "Additionally binds to the shim/MOK certificates",
  },
  {
    value: "TPM_PCR_PROFILE_CUSTOM",
    label: "Custom PCR list",
    description: "Advanced: choose the PCRs yourself (0 and 2 change on firmware updates)",
  },
];

export function defaultDiskEncContent(): DiskEncContent {
  return {
    requireEncryption: true,
    volumeScope: "LUKS_VOLUME_SCOPE_SYSTEM",
    mountpoints: [],
    minVolumeKeyBits: 0,
    allowedCiphers: [],
    requireTpm2: false,
    requireSecureBoot: false,
    tpm2: {
      enabled: true,
      pcrProfile: "TPM_PCR_PROFILE_SECURE_BOOT",
      pcrs: [],
      autoReseal: true,
      allowFirmwarePcrs: false,
    },
    tang: { enabled: false, servers: [], threshold: 0 },
    recovery: {
      escrow: true,
      rotationIntervalDays: DEFAULT_ROTATION_DAYS,
      rotateAfterReveal: true,
      serverAssistedRotation: true,
    },
    initramfsMode: "INITRAMFS_MODE_VERIFY_ONLY",
    adoptExisting: true,
  };
}

/* eslint-disable @typescript-eslint/no-explicit-any */

/** Defensive parse of `Policy.content` (protojson written by this editor or the API). */
export function parseDiskEncContent(raw: string): DiskEncContent {
  const out = defaultDiskEncContent();
  let parsed: any;
  try {
    parsed = JSON.parse(raw || "{}");
  } catch {
    return out;
  }
  if (!parsed || typeof parsed !== "object") return out;

  out.requireEncryption = parsed.requireEncryption === true;
  if (typeof parsed.volumeScope === "string" && parsed.volumeScope.startsWith("LUKS_VOLUME_SCOPE_")) {
    if (parsed.volumeScope !== "LUKS_VOLUME_SCOPE_UNSPECIFIED") out.volumeScope = parsed.volumeScope;
  }
  out.mountpoints = Array.isArray(parsed.mountpoints)
    ? parsed.mountpoints.filter((m: unknown) => typeof m === "string")
    : [];
  out.minVolumeKeyBits = typeof parsed.minVolumeKeyBits === "number" ? parsed.minVolumeKeyBits : 0;
  out.allowedCiphers = Array.isArray(parsed.allowedCiphers)
    ? parsed.allowedCiphers.filter((c: unknown) => typeof c === "string")
    : [];
  out.requireTpm2 = parsed.requireTpm2 === true;
  out.requireSecureBoot = parsed.requireSecureBoot === true;

  const tpm2 = parsed.tpm2;
  if (tpm2 && typeof tpm2 === "object") {
    out.tpm2.enabled = tpm2.enabled === true;
    if (typeof tpm2.pcrProfile === "string" && tpm2.pcrProfile.startsWith("TPM_PCR_PROFILE_")) {
      if (tpm2.pcrProfile !== "TPM_PCR_PROFILE_UNSPECIFIED") out.tpm2.pcrProfile = tpm2.pcrProfile;
    }
    out.tpm2.pcrs = Array.isArray(tpm2.pcrs)
      ? tpm2.pcrs.filter((p: unknown) => typeof p === "number")
      : [];
    out.tpm2.autoReseal = tpm2.autoReseal !== false;
    out.tpm2.allowFirmwarePcrs = tpm2.allowFirmwarePcrs === true;
  } else {
    out.tpm2.enabled = false;
  }

  const tang = parsed.tang;
  if (tang && typeof tang === "object") {
    out.tang.enabled = tang.enabled === true;
    out.tang.threshold = typeof tang.threshold === "number" ? tang.threshold : 0;
    out.tang.servers = Array.isArray(tang.servers)
      ? tang.servers
          .filter((s: any) => s && typeof s === "object")
          .map((s: any) => ({
            url: typeof s.url === "string" ? s.url : "",
            thumbprint: typeof s.thumbprint === "string" ? s.thumbprint : "",
            acceptedThumbprints: Array.isArray(s.acceptedThumbprints)
              ? s.acceptedThumbprints.filter((t: unknown) => typeof t === "string")
              : [],
            registryId: typeof s.registryId === "string" ? s.registryId : "",
          }))
      : [];
  }

  const recovery = parsed.recovery;
  if (recovery && typeof recovery === "object") {
    out.recovery.escrow = recovery.escrow === true;
    out.recovery.rotationIntervalDays =
      typeof recovery.rotationIntervalDays === "number"
        ? recovery.rotationIntervalDays
        : DEFAULT_ROTATION_DAYS;
    out.recovery.rotateAfterReveal = recovery.rotateAfterReveal !== false;
    out.recovery.serverAssistedRotation = recovery.serverAssistedRotation !== false;
  } else {
    out.recovery.escrow = false;
  }

  if (parsed.initramfsMode === "INITRAMFS_MODE_MANAGE") out.initramfsMode = "INITRAMFS_MODE_MANAGE";
  out.adoptExisting = parsed.adoptExisting !== false;
  return out;
}

/* eslint-enable @typescript-eslint/no-explicit-any */

/** Serializes the editor state to the protojson content string. */
export function serializeDiskEncContent(c: DiskEncContent): string {
  const doc: Record<string, unknown> = {
    requireEncryption: c.requireEncryption,
    volumeScope: c.volumeScope,
  };
  if (c.volumeScope === "LUKS_VOLUME_SCOPE_MOUNTPOINTS" && c.mountpoints.length > 0) {
    doc.mountpoints = c.mountpoints;
  }
  if (c.minVolumeKeyBits > 0) doc.minVolumeKeyBits = c.minVolumeKeyBits;
  if (c.allowedCiphers.length > 0) doc.allowedCiphers = c.allowedCiphers;
  if (c.requireTpm2) doc.requireTpm2 = true;
  if (c.requireSecureBoot) doc.requireSecureBoot = true;
  if (c.tpm2.enabled) {
    doc.tpm2 = {
      enabled: true,
      pcrProfile: c.tpm2.pcrProfile,
      ...(c.tpm2.pcrProfile === "TPM_PCR_PROFILE_CUSTOM" ? { pcrs: c.tpm2.pcrs } : {}),
      autoReseal: c.tpm2.autoReseal,
      ...(c.tpm2.allowFirmwarePcrs ? { allowFirmwarePcrs: true } : {}),
    };
  }
  if (c.tang.enabled) {
    doc.tang = {
      enabled: true,
      servers: c.tang.servers.map((s) => ({
        url: s.url,
        thumbprint: s.thumbprint,
        ...(s.acceptedThumbprints.length > 0 ? { acceptedThumbprints: s.acceptedThumbprints } : {}),
        ...(s.registryId ? { registryId: s.registryId } : {}),
      })),
      ...(c.tang.servers.length > 1 ? { threshold: c.tang.threshold || 1 } : {}),
    };
  }
  if (c.recovery.escrow) {
    doc.recovery = {
      escrow: true,
      rotationIntervalDays: c.recovery.rotationIntervalDays,
      rotateAfterReveal: c.recovery.rotateAfterReveal,
      serverAssistedRotation: c.recovery.serverAssistedRotation,
    };
  }
  doc.initramfsMode = c.initramfsMode;
  if (!c.adoptExisting) doc.adoptExisting = false;
  return JSON.stringify(doc, null, 2);
}

/**
 * Mirrors the server's ValidateDiskEncryptionContent: returns the first
 * problem, or null when the content can be saved.
 */
export function validateDiskEncContent(c: DiskEncContent): string | null {
  if (c.volumeScope === "LUKS_VOLUME_SCOPE_MOUNTPOINTS") {
    if (c.mountpoints.length === 0) return "Add at least one mountpoint, or change the volume scope";
    for (const m of c.mountpoints) {
      if (!m.startsWith("/") || m.includes("..") || /\s/.test(m)) {
        return `Invalid mountpoint "${m}": must be an absolute path`;
      }
    }
  }
  if (c.tpm2.enabled && c.tpm2.pcrProfile === "TPM_PCR_PROFILE_CUSTOM") {
    if (c.tpm2.pcrs.length === 0) return "The custom PCR profile needs at least one PCR";
    for (const p of c.tpm2.pcrs) {
      if (p < 0 || p > 23) return `PCR ${p} is out of range (0-23)`;
      if ((p === 0 || p === 2) && !c.tpm2.allowFirmwarePcrs) {
        return `PCR ${p} changes on firmware updates; enable "Allow firmware PCRs" to use it`;
      }
    }
  }
  if (c.tang.enabled) {
    if (c.tang.servers.length === 0) return "The Tang protector needs at least one server";
    if (c.tang.servers.length > 4) return "At most 4 Tang servers are allowed per policy";
    for (const s of c.tang.servers) {
      if (!isValidTangUrl(s.url)) return `Invalid Tang URL "${s.url}"`;
      if (!isValidTangThumbprint(s.thumbprint)) {
        return "Every Tang server needs its confirmed signing thumbprint (43 base64url characters)";
      }
    }
    if (c.tang.servers.length > 1 && c.tang.threshold > c.tang.servers.length) {
      return "The Tang threshold cannot exceed the number of servers";
    }
  }
  if (
    c.recovery.escrow &&
    c.recovery.rotationIntervalDays !== 0 &&
    (c.recovery.rotationIntervalDays < 30 || c.recovery.rotationIntervalDays > 730)
  ) {
    return "The rotation interval must be 0 (never) or between 30 and 730 days";
  }
  if (
    !c.requireEncryption &&
    !c.requireTpm2 &&
    !c.requireSecureBoot &&
    !c.tpm2.enabled &&
    !c.tang.enabled &&
    !c.recovery.escrow
  ) {
    return "Require encryption or enable at least one protector before saving";
  }
  return null;
}
