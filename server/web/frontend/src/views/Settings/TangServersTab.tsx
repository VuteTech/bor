// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * TangServersTab - Settings -> Tang servers: the registry of external Tang
 * servers that DiskEncryption policies copy their entries from. The per-key
 * bound-volume counts are the rotation fleet view stock Tang lacks: they say
 * when an old key's hidden files can be deleted on the Tang host
 *.
 */

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Button, Label, MenuToggle, Spinner, Tooltip } from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorToolbar } from "../../components/BorToolbar";
import { BorEmptyState } from "../../components/BorEmptyState";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import { hasPermission } from "../../apiClient/permissions";
import {
  checkTangServer,
  deleteTangServer,
  fetchTangServers,
  TangCheckStatus,
  TangServer,
} from "../../apiClient/diskEncryptionApi";
import { TangServerModal } from "./TangServerModal";

type LabelColor = "grey" | "green" | "red" | "orange";

const STATUS: Record<TangCheckStatus, { text: string; color: LabelColor }> = {
  never: { text: "Never checked", color: "grey" },
  ok: { text: "OK", color: "green" },
  new_key: { text: "New key advertised", color: "orange" },
  error: { text: "Error", color: "red" },
};

function formatWhen(ts: string | null): string {
  if (!ts) return "-";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString();
}

export const TangServersTab: React.FC = () => {
  const canCreate = hasPermission("tang_server:create");
  const canEdit = hasPermission("tang_server:edit");
  const canDelete = hasPermission("tang_server:delete");
  const { addToast } = useToast();

  const [servers, setServers] = useState<TangServer[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [modal, setModal] = useState<{ open: boolean; server: TangServer | null }>({ open: false, server: null });
  const [deleteTarget, setDeleteTarget] = useState<TangServer | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchTangServers()
      .then(setServers)
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed to load Tang servers"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return servers;
    return servers.filter((s) => [s.name, s.url].some((v) => v.toLowerCase().includes(q)));
  }, [servers, search]);

  const handleCheck = async (server: TangServer) => {
    try {
      await checkTangServer(server.id);
      addToast({ variant: "success", title: "Advertisement checked", detail: server.name });
      reload();
    } catch (e: unknown) {
      addToast({ variant: "danger", title: "Check failed", detail: e instanceof Error ? e.message : undefined });
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await deleteTangServer(deleteTarget.id);
      addToast({ variant: "success", title: "Tang server removed", detail: deleteTarget.name });
      setDeleteTarget(null);
      reload();
    } catch (e: unknown) {
      setDeleteError(e instanceof Error ? e.message : "Delete failed");
    } finally {
      setDeleteBusy(false);
    }
  };

  const rowActions = (server: TangServer): IAction[] => {
    const items: IAction[] = [];
    if (canEdit) {
      items.push({ title: "Check advertisement", onClick: () => void handleCheck(server) });
      items.push({
        title: server.last_check_status === "new_key" ? "Confirm new key…" : "Edit",
        onClick: () => setModal({ open: true, server }),
      });
    }
    if (canDelete) {
      items.push({
        title: "Delete",
        isDanger: true,
        onClick: () => {
          setDeleteError(null);
          setDeleteTarget(server);
        },
      });
    }
    return items;
  };

  const keyRow = (server: TangServer, thp: string, preferred: boolean) => {
    const bound = server.bound_volumes?.[thp] ?? 0;
    return (
      <div key={thp} style={{ whiteSpace: "nowrap" }}>
        <code>{thp.slice(0, 12)}…</code>{" "}
        {preferred ? <Label isCompact color="green">preferred</Label> : <Label isCompact>accepted</Label>}{" "}
        <span className="bor-text-secondary">
          {bound === 0 ? "no volumes bound" : `${bound} volume${bound === 1 ? "" : "s"} bound`}
        </span>
      </div>
    );
  };

  return (
    <div>
      <p style={{ marginBottom: 12 }}>
        Tang servers that <strong>Disk encryption</strong> policies can bind volumes to for
        network-bound unlocking. The confirmed signing keys are copied into each policy; after a
        key rotation, delete the old key on the Tang host only when its bound-volume count here
        reaches zero.
      </p>
      {error && <LiveAlert variant="danger" isInline message={error} style={{ marginBottom: 12 }} />}

      <BorToolbar
        searchValue={search}
        onSearchChange={setSearch}
        searchAriaLabel="Search Tang servers"
        searchPlaceholder="Search by name or URL"
        onClearAll={() => setSearch("")}
      >
        {canCreate && (
          <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => setModal({ open: true, server: null })}>
            Add Tang server
          </Button>
        )}
      </BorToolbar>

      {loading ? (
        <Spinner aria-label="Loading" />
      ) : filtered.length === 0 ? (
        <BorEmptyState
          isEmptyData={servers.length === 0}
          itemsLabel="Tang servers"
          emptyTitle="No Tang servers"
          emptyBody="Add a Tang server and confirm its signing key to enable network-bound unlocking in disk encryption policies."
          action={
            canCreate ? (
              <Button variant="primary" onClick={() => setModal({ open: true, server: null })}>
                Add Tang server
              </Button>
            ) : undefined
          }
          onClearFilters={() => setSearch("")}
        />
      ) : (
        <Table aria-label="Tang servers" variant="compact">
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>URL</Th>
              <Th>Trusted keys</Th>
              <Th>Status</Th>
              <Th>Last check</Th>
              <Th screenReaderText="Actions" />
            </Tr>
          </Thead>
          <Tbody>
            {filtered.map((server) => {
              const st = STATUS[server.last_check_status] ?? STATUS.never;
              return (
                <Tr key={server.id}>
                  <Td dataLabel="Name">
                    <strong>{server.name}</strong>
                  </Td>
                  <Td dataLabel="URL" style={{ wordBreak: "break-all" }}>
                    {server.url}
                  </Td>
                  <Td dataLabel="Trusted keys">
                    {server.trusted_thumbprints.map((thp, i) => keyRow(server, thp, i === 0))}
                  </Td>
                  <Td dataLabel="Status">
                    {server.last_check_status === "error" && server.last_check_error ? (
                      <Tooltip content={server.last_check_error}>
                        <Label color={st.color}>{st.text}</Label>
                      </Tooltip>
                    ) : server.last_check_status === "new_key" ? (
                      <Tooltip content="The server advertises a signing key you have not confirmed yet. Open the server and confirm the new thumbprint against tang-show-keys.">
                        <Label color={st.color}>{st.text}</Label>
                      </Tooltip>
                    ) : (
                      <Label color={st.color}>{st.text}</Label>
                    )}
                  </Td>
                  <Td dataLabel="Last check">{formatWhen(server.last_check_at)}</Td>
                  <Td dataLabel="Actions" isActionCell>
                    {rowActions(server).length > 0 && (
                      <ActionsColumn
                        items={rowActions(server)}
                        actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                          <MenuToggle
                            ref={toggleRef}
                            aria-label={`Actions for Tang server ${server.name}`}
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
      )}

      <TangServerModal
        isOpen={modal.open}
        initial={modal.server}
        onSaved={(saved) => {
          setModal({ open: false, server: null });
          addToast({
            variant: "success",
            title: modal.server ? "Tang server updated" : "Tang server added",
            detail: saved.name,
          });
          reload();
        }}
        onClose={() => setModal({ open: false, server: null })}
      />
      <ConfirmModal
        isOpen={!!deleteTarget}
        title="Delete Tang server"
        confirmLabel="Delete"
        isDanger
        confirmPhrase={deleteTarget?.name}
        isBusy={deleteBusy}
        error={deleteError}
        onConfirm={() => void handleDelete()}
        onCancel={() => setDeleteTarget(null)}
      >
        <p>
          This removes <strong>{deleteTarget?.name}</strong> from the registry. Servers referenced
          by a disk encryption policy cannot be deleted; volumes already bound keep unlocking
          until they are re-bound.
        </p>
      </ConfirmModal>
    </div>
  );
};
