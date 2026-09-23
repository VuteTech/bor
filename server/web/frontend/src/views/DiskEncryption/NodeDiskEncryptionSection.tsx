// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * NodeDiskEncryptionSection - the "Disk encryption" block of the node
 * drawer: platform facts, one
 * entry per LUKS volume with its protectors and recovery-key metadata, and
 * the reveal / rotate actions. Self-contained: fetches its own data and
 * renders nothing while the node has never reported an inventory.
 */

import React, { useEffect, useState } from "react";
import { Button, Label, Spinner, Title } from "@patternfly/react-core";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import { hasPermission } from "../../apiClient/permissions";
import {
  fetchNodeDiskEncryption,
  LuksVolume,
  NodeDiskEncryptionDetail,
  rotateRecoveryKey,
} from "../../apiClient/diskEncryptionApi";
import { RevealKeyModal } from "./RevealKeyModal";

function secureBootLabel(v: string): string {
  switch (v) {
    case "enabled":
      return "Secure Boot enabled";
    case "disabled":
      return "Secure Boot disabled";
    case "setup_mode":
      return "Secure Boot in setup mode";
    case "legacy_bios":
      return "legacy BIOS";
    default:
      return "Secure Boot unknown";
  }
}

function keyAge(confirmedAt: string | null | undefined): string | null {
  if (!confirmedAt) return null;
  const d = new Date(confirmedAt);
  if (Number.isNaN(d.getTime())) return null;
  const days = Math.floor((Date.now() - d.getTime()) / (24 * 3600 * 1000));
  return `${days} day${days === 1 ? "" : "s"} old`;
}

interface Props {
  nodeId: string;
}

export const NodeDiskEncryptionSection: React.FC<Props> = ({ nodeId }) => {
  const canReveal = hasPermission("disk_encryption:reveal");
  const canRotate = hasPermission("disk_encryption:rotate");
  const { addToast } = useToast();

  const [detail, setDetail] = useState<NodeDiskEncryptionDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [revealTarget, setRevealTarget] = useState<LuksVolume | null>(null);
  const [rotateTarget, setRotateTarget] = useState<LuksVolume | null>(null);
  const [rotateBusy, setRotateBusy] = useState(false);
  const [rotateError, setRotateError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setDetail(null);
    fetchNodeDiskEncryption(nodeId)
      .then((d) => {
        if (!cancelled) setDetail(d);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : "Failed to load");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [nodeId]);

  const doRotate = async () => {
    if (!rotateTarget) return;
    setRotateBusy(true);
    setRotateError(null);
    try {
      await rotateRecoveryKey(rotateTarget.id);
      addToast({ variant: "success", title: "Rotation requested", detail: rotateTarget.mapping_name || rotateTarget.luks_uuid });
      setRotateTarget(null);
      const d = await fetchNodeDiskEncryption(nodeId);
      setDetail(d);
    } catch (e: unknown) {
      setRotateError(e instanceof Error ? e.message : "Rotation request failed");
    } finally {
      setRotateBusy(false);
    }
  };

  // Nothing reported yet (and no error): stay out of the drawer entirely.
  if (!loading && !error && detail && detail.platform === null && detail.volumes.length === 0) {
    return null;
  }

  return (
    <>
      <Title headingLevel="h3" size="md" style={{ marginTop: "1.5rem", marginBottom: "0.5rem" }}>
        Disk encryption
      </Title>
      {loading && <Spinner size="md" aria-label="Loading" />}
      <LiveAlert id="node-de-error" variant="danger" isInline message={error} />
      {detail?.platform && (
        <p className="bor-text-secondary" style={{ marginBottom: "0.5rem" }}>
          <abbr title="Trusted Platform Module">TPM</abbr> 2.0{" "}
          {detail.platform.tpm2_present ? "present" : "absent"} · {secureBootLabel(detail.platform.secure_boot)} ·{" "}
          {detail.platform.initramfs_generator}
        </p>
      )}
      {(detail?.platform?.unencrypted_system_mounts?.length ?? 0) > 0 && (
        <LiveAlert
          id="node-de-unencrypted"
          variant="warning"
          isInline
          message={`Unencrypted system mounts: ${detail!.platform!.unencrypted_system_mounts
            .map((m) => m.mountpoint)
            .join(", ")}`}
          style={{ marginBottom: "0.5rem" }}
        />
      )}
      {detail?.volumes.map((v) => {
        const kinds = new Set((v.state?.keyslots ?? []).map((k) => k.kind ?? ""));
        const age = keyAge(v.active_key?.confirmed_at);
        const unmanaged = (v.state?.keyslots ?? []).filter(
          (k) => k.kind === "LUKS_KEYSLOT_KIND_PASSWORD" || k.kind === "LUKS_KEYSLOT_KIND_OTHER",
        ).length;
        return (
          <div key={v.id} style={{ marginBottom: "1rem" }}>
            <div>
              <strong>{v.mountpoints.join(", ") || v.mapping_name}</strong>{" "}
              <code style={{ fontSize: "0.8rem" }}>{v.luks_uuid.slice(0, 13)}…</code>
            </div>
            <div className="bor-text-secondary">
              LUKS{v.luks_version} · {v.cipher || "unknown cipher"}
              {v.volume_key_bits > 0 && <> · {v.volume_key_bits}-bit key</>}
            </div>
            <div style={{ display: "flex", gap: 4, flexWrap: "wrap", margin: "0.25rem 0" }}>
              {kinds.has("LUKS_KEYSLOT_KIND_TPM2") && (
                <Label isCompact color="green" icon={<CheckCircleIcon />}>TPM2</Label>
              )}
              {kinds.has("LUKS_KEYSLOT_KIND_CLEVIS") && (
                <Label isCompact color="green" icon={<CheckCircleIcon />}>Tang</Label>
              )}
              {v.active_key ? (
                <Label isCompact color="green" icon={<CheckCircleIcon />}>Recovery key</Label>
              ) : (
                <Label isCompact color="orange" icon={<ExclamationTriangleIcon />}>No escrowed key</Label>
              )}
              {v.state?.externallyManagedBy && (
                <Label isCompact>managed by {v.state.externallyManagedBy}</Label>
              )}
            </div>
            {v.active_key && (
              <div className="bor-text-secondary">
                Key <code>{v.active_key.id.slice(0, 8)}</code>
                {v.active_key.keyslot !== null && <> · slot {v.active_key.keyslot}</>}
                {age && <> · {age}</>} · revealed {v.active_key.reveal_count}×
                {v.rotation_requested_at && (
                  <Label isCompact color="orange" style={{ marginLeft: 6 }}>rotation pending</Label>
                )}
              </div>
            )}
            {unmanaged > 0 && (
              <div className="bor-text-secondary">
                {unmanaged} unmanaged passphrase slot{unmanaged === 1 ? "" : "s"}
              </div>
            )}
            <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.25rem" }}>
              {canReveal && v.active_key && (
                <Button variant="secondary" size="sm" onClick={() => setRevealTarget(v)}>
                  Show recovery key…
                </Button>
              )}
              {canRotate && v.active_key && !v.rotation_requested_at && (
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    setRotateError(null);
                    setRotateTarget(v);
                  }}
                >
                  Rotate now
                </Button>
              )}
            </div>
          </div>
        );
      })}

      <RevealKeyModal volume={revealTarget} onClose={() => setRevealTarget(null)} />
      <ConfirmModal
        isOpen={!!rotateTarget}
        title="Rotate the recovery key?"
        confirmLabel="Rotate"
        isBusy={rotateBusy}
        error={rotateError}
        onConfirm={() => void doRotate()}
        onCancel={() => setRotateTarget(null)}
      >
        <p>
          The agent escrows a new key for{" "}
          <code>{rotateTarget?.mapping_name || rotateTarget?.luks_uuid}</code> and wipes the old
          slot once the server confirms it.
        </p>
      </ConfirmModal>
    </>
  );
};
