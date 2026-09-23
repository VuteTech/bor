// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * DiskEncryptionPage - the fleet recovery-key directory: summary
 * StatCards, a search
 * tuned for help-desk input (hostname, LUKS UUID or escrow-id prefix with or
 * without dashes, mapping name), per-volume protectors and key age, and the
 * reveal / rotate actions.
 */

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Gallery,
  Label,
  MenuToggle,
  PageSection,
  Pagination,
  Spinner,
  Tooltip,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorToolbar } from "../../components/BorToolbar";
import { BorEmptyState } from "../../components/BorEmptyState";
import { ConfirmModal } from "../../components/ConfirmModal";
import { StatCard } from "../../components/StatCard";
import { useToast } from "../../components/ToastHost";
import { hasPermission } from "../../apiClient/permissions";
import {
  DiskEncryptionSummary,
  fetchDiskEncryptionSummary,
  fetchLuksVolumes,
  LuksVolume,
  rotateRecoveryKey,
} from "../../apiClient/diskEncryptionApi";
import { RevealKeyModal } from "./RevealKeyModal";

function keyAgeDays(v: LuksVolume): number | null {
  const at = v.active_key?.confirmed_at;
  if (!at) return null;
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) return null;
  return Math.floor((Date.now() - d.getTime()) / (24 * 3600 * 1000));
}

/** Protector labels from the volume's last reported keyslots. */
function protectorLabels(v: LuksVolume): React.ReactNode {
  const kinds = new Set((v.state?.keyslots ?? []).map((k) => k.kind ?? ""));
  const labels: React.ReactNode[] = [];
  if (kinds.has("LUKS_KEYSLOT_KIND_TPM2")) {
    labels.push(
      <Label key="tpm2" isCompact color="green" icon={<CheckCircleIcon />}>
        TPM2
      </Label>,
    );
  }
  if (kinds.has("LUKS_KEYSLOT_KIND_CLEVIS")) {
    labels.push(
      <Label key="tang" isCompact color="green" icon={<CheckCircleIcon />}>
        Tang
      </Label>,
    );
  }
  if (v.active_key) {
    labels.push(
      <Label key="recovery" isCompact color="green" icon={<CheckCircleIcon />}>
        Recovery key
      </Label>,
    );
  } else if (kinds.has("LUKS_KEYSLOT_KIND_BOR_RECOVERY")) {
    labels.push(
      <Label key="recovery-drift" isCompact color="orange" icon={<ExclamationTriangleIcon />}>
        Recovery key (unconfirmed)
      </Label>,
    );
  }
  if (v.state?.externallyManagedBy) {
    labels.push(
      <Label key="ext" isCompact>
        managed by {v.state.externallyManagedBy}
      </Label>,
    );
  }
  if (labels.length === 0) {
    return <span className="bor-text-secondary">no managed protectors</span>;
  }
  return <span style={{ display: "inline-flex", gap: 4, flexWrap: "wrap" }}>{labels}</span>;
}

export const DiskEncryptionPage: React.FC = () => {
  const canReveal = hasPermission("disk_encryption:reveal");
  const canRotate = hasPermission("disk_encryption:rotate");
  const { addToast } = useToast();

  const [summary, setSummary] = useState<DiskEncryptionSummary | null>(null);
  const [volumes, setVolumes] = useState<LuksVolume[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [revealTarget, setRevealTarget] = useState<LuksVolume | null>(null);
  const [rotateTarget, setRotateTarget] = useState<LuksVolume | null>(null);
  const [rotateBusy, setRotateBusy] = useState(false);
  const [rotateError, setRotateError] = useState<string | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    Promise.all([
      fetchDiskEncryptionSummary(),
      fetchLuksVolumes({ search: search.trim(), page, perPage }),
    ])
      .then(([sum, pageRes]) => {
        setSummary(sum);
        setVolumes(pageRes.items);
        setTotal(pageRes.total);
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed to load volumes"))
      .finally(() => setLoading(false));
  }, [search, page, perPage]);

  useEffect(() => {
    const t = window.setTimeout(reload, 250); // debounce typing in the search box
    return () => window.clearTimeout(t);
  }, [reload]);

  const doRotate = async () => {
    if (!rotateTarget) return;
    setRotateBusy(true);
    setRotateError(null);
    try {
      await rotateRecoveryKey(rotateTarget.id);
      addToast({
        variant: "success",
        title: "Rotation requested",
        detail: `${rotateTarget.node_name} · ${rotateTarget.mapping_name || rotateTarget.luks_uuid}`,
      });
      setRotateTarget(null);
      reload();
    } catch (e: unknown) {
      setRotateError(e instanceof Error ? e.message : "Rotation request failed");
    } finally {
      setRotateBusy(false);
    }
  };

  const rowActions = (v: LuksVolume): IAction[] => {
    const items: IAction[] = [];
    if (canReveal) {
      const blocked = !v.active_key ? "No confirmed recovery key is escrowed for this volume." : null;
      items.push({
        title: "Show recovery key…",
        onClick: () => setRevealTarget(v),
        isAriaDisabled: !!blocked,
        tooltipProps: blocked ? { content: blocked } : undefined,
      });
    }
    if (canRotate) {
      const pending = v.rotation_requested_at !== null;
      items.push({
        title: "Rotate now",
        onClick: () => {
          setRotateError(null);
          setRotateTarget(v);
        },
        isAriaDisabled: pending,
        tooltipProps: pending ? { content: "A rotation is already pending for this volume." } : undefined,
      });
    }
    return items;
  };

  const summaryCards = useMemo(() => {
    if (!summary) return null;
    return (
      <Gallery hasGutter minWidths={{ default: "160px" }} style={{ marginBottom: 16 }}>
        <StatCard title="Volumes" value={summary.volumes} color="blue" />
        <StatCard title="Keys escrowed" value={summary.escrowed_active} color="green" />
        <StatCard title="Rotations pending" value={summary.rotations_overdue} color={summary.rotations_overdue > 0 ? "orange" : "green"} />
        <StatCard title="Unencrypted mounts" value={summary.unencrypted_mounts} color={summary.unencrypted_mounts > 0 ? "red" : "green"} />
        <StatCard title="Externally managed" value={summary.externally_managed} color="grey" />
        <StatCard title="Clone suspects" value={summary.clone_suspects} color={summary.clone_suspects > 0 ? "red" : "green"} />
      </Gallery>
    );
  }, [summary]);

  return (
    <PageSection>
      {summary && !summary.escrow_configured && (
        <LiveAlert
          id="de-page-no-kek"
          variant="warning"
          isInline
          message="Recovery-key escrow is not configured on this server (BOR_ESCROW_KEK_FILE). Volumes are inventoried, but agents cannot escrow keys."
          style={{ marginBottom: 12 }}
        />
      )}
      {summaryCards}
      {error && <LiveAlert variant="danger" isInline message={error} style={{ marginBottom: 12 }} />}

      <BorToolbar
        searchValue={search}
        onSearchChange={(v) => {
          setSearch(v);
          setPage(1);
        }}
        searchAriaLabel="Search volumes"
        searchPlaceholder="Hostname, LUKS UUID, key ID or mapping name"
        onClearAll={() => {
          setSearch("");
          setPage(1);
        }}
      />

      {loading ? (
        <Spinner aria-label="Loading" />
      ) : volumes.length === 0 ? (
        <BorEmptyState
          isEmptyData={total === 0 && search.trim() === ""}
          itemsLabel="LUKS volumes"
          emptyTitle="No LUKS volumes reported yet"
          emptyBody="Volumes appear here once agents report their disk encryption inventory (a DiskEncryption policy is not required for the inventory)."
          onClearFilters={() => setSearch("")}
        />
      ) : (
        <>
          <Table aria-label="LUKS volumes" variant="compact">
            <Thead>
              <Tr>
                <Th>Node</Th>
                <Th>Mount</Th>
                <Th>
                  <abbr title="Linux Unified Key Setup">LUKS</abbr> UUID
                </Th>
                <Th>Protectors</Th>
                <Th>Recovery key</Th>
                <Th screenReaderText="Actions" />
              </Tr>
            </Thead>
            <Tbody>
              {volumes.map((v) => {
                const age = keyAgeDays(v);
                return (
                  <Tr key={v.id}>
                    <Td dataLabel="Node">
                      <strong>{v.node_name}</strong>
                      {v.orphaned_at && (
                        <Tooltip content="The node was deleted; the keys stay revealable until the retention elapses.">
                          <Label isCompact style={{ marginLeft: 6 }}>
                            orphaned
                          </Label>
                        </Tooltip>
                      )}
                      {v.clone_suspected && (
                        <Tooltip content="Several nodes report this LUKS UUID - cloned images share one volume key.">
                          <Label isCompact color="red" style={{ marginLeft: 6 }}>
                            clone?
                          </Label>
                        </Tooltip>
                      )}
                    </Td>
                    <Td dataLabel="Mount">{v.mountpoints.join(", ") || v.mapping_name || "-"}</Td>
                    <Td dataLabel="LUKS UUID">
                      <code>{v.luks_uuid}</code>
                    </Td>
                    <Td dataLabel="Protectors">{protectorLabels(v)}</Td>
                    <Td dataLabel="Recovery key">
                      {v.active_key ? (
                        <>
                          <code>{v.active_key.id.slice(0, 8)}</code>
                          {v.active_key.keyslot !== null && <> · slot {v.active_key.keyslot}</>}
                          {age !== null && <> · {age} day{age === 1 ? "" : "s"} old</>}
                          {v.active_key.reveal_count > 0 && <> · revealed {v.active_key.reveal_count}×</>}
                          {v.rotation_requested_at && (
                            <Label isCompact color="orange" style={{ marginLeft: 6 }}>
                              rotation pending
                            </Label>
                          )}
                        </>
                      ) : (
                        <span className="bor-text-secondary">not escrowed</span>
                      )}
                    </Td>
                    <Td dataLabel="Actions" isActionCell>
                      {rowActions(v).length > 0 && (
                        <ActionsColumn
                          items={rowActions(v)}
                          actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                            <MenuToggle
                              ref={toggleRef}
                              aria-label={`Actions for volume ${v.mapping_name || v.luks_uuid} on ${v.node_name}`}
                              variant="plain"
                              onClick={onToggle}
                              isExpanded={isOpen}
                              isDisabled={isDisabled}
                              icon={<EllipsisVIcon />}
                            />
                          )}
                        />
                      )}
                    </Td>
                  </Tr>
                );
              })}
            </Tbody>
          </Table>
          <Pagination
            itemCount={total}
            page={page}
            perPage={perPage}
            onSetPage={(_e, p) => setPage(p)}
            onPerPageSelect={(_e, pp) => {
              setPerPage(pp);
              setPage(1);
            }}
            variant="bottom"
          />
        </>
      )}

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
          The agent on <strong>{rotateTarget?.node_name}</strong> escrows a new key for{" "}
          <code>{rotateTarget?.mapping_name || rotateTarget?.luks_uuid}</code> and wipes the old
          slot once the server confirms it. The device stays bootable throughout.
        </p>
      </ConfirmModal>
    </PageSection>
  );
};
