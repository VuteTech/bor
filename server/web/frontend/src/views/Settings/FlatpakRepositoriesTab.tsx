// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * FlatpakRepositoriesTab — Settings → Flatpak repositories. The remotes the
 * server indexes for the policy editor's application catalog (Flathub is
 * built in). Server state only: nodes get remotes through Flatpak policies.
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
  FlatpakRepository,
  FlatpakRefreshStatus,
  deleteFlatpakRepository,
  fetchFlatpakRepositories,
  refreshFlatpakRepository,
} from "../../apiClient/flatpakApi";
import { FlatpakRepositoryModal } from "./FlatpakRepositoryModal";
import { FlatpakCatalogUploadModal } from "./FlatpakCatalogUploadModal";

type LabelColor = "grey" | "green" | "red" | "blue" | "orange";

const STATUS: Record<FlatpakRefreshStatus, { text: string; color: LabelColor }> = {
  never: { text: "Never refreshed", color: "grey" },
  ok: { text: "OK", color: "green" },
  error: { text: "Error", color: "red" },
  running: { text: "Running", color: "blue" },
  disabled: { text: "Disabled", color: "grey" },
};

function formatWhen(ts: string | null): string {
  if (!ts) return "—";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString();
}

export const FlatpakRepositoriesTab: React.FC = () => {
  const canCreate = hasPermission("flatpak_repo:create");
  const canEdit = hasPermission("flatpak_repo:edit");
  const canDelete = hasPermission("flatpak_repo:delete");
  const canRefresh = hasPermission("flatpak_repo:refresh");
  const { addToast } = useToast();

  const [repos, setRepos] = useState<FlatpakRepository[]>([]);
  const [refreshEnabled, setRefreshEnabled] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [modal, setModal] = useState<{ open: boolean; repo: FlatpakRepository | null }>({ open: false, repo: null });
  const [uploadTarget, setUploadTarget] = useState<FlatpakRepository | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<FlatpakRepository | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchFlatpakRepositories()
      .then((res) => {
        setRepos(res.items);
        setRefreshEnabled(res.refresh_enabled);
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed to load repositories"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  // While a refresh is running, poll until it settles.
  useEffect(() => {
    if (!repos.some((r) => r.last_refresh_status === "running")) return;
    const t = window.setTimeout(reload, 5000);
    return () => window.clearTimeout(t);
  }, [repos, reload]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return repos;
    return repos.filter((r) => [r.name, r.title, r.url].some((v) => v.toLowerCase().includes(q)));
  }, [repos, search]);

  const handleRefresh = async (repo: FlatpakRepository) => {
    try {
      await refreshFlatpakRepository(repo.id);
      addToast({ variant: "success", title: "Catalog refresh queued", detail: repo.title || repo.name });
      window.setTimeout(reload, 1500);
    } catch (e: unknown) {
      addToast({ variant: "danger", title: "Refresh failed", detail: e instanceof Error ? e.message : undefined });
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await deleteFlatpakRepository(deleteTarget.id);
      addToast({ variant: "success", title: "Repository deleted", detail: deleteTarget.name });
      setDeleteTarget(null);
      reload();
    } catch (e: unknown) {
      setDeleteError(e instanceof Error ? e.message : "Delete failed");
    } finally {
      setDeleteBusy(false);
    }
  };

  const rowActions = (repo: FlatpakRepository): IAction[] => {
    const items: IAction[] = [];
    if (canRefresh) {
      const blocked = !refreshEnabled
        ? "Outbound catalog refresh is disabled on this server (BOR_FLATPAK_CATALOG_REFRESH=false). Upload a catalog instead."
        : !repo.catalog_enabled
        ? "Enable the catalog for this repository first."
        : null;
      items.push({
        title: "Refresh now",
        onClick: () => void handleRefresh(repo),
        isAriaDisabled: !!blocked,
        tooltipProps: blocked ? { content: blocked } : undefined,
      });
      items.push({ title: "Upload catalog…", onClick: () => setUploadTarget(repo) });
    }
    if (canEdit) items.push({ title: "Edit", onClick: () => setModal({ open: true, repo }) });
    if (canDelete) {
      items.push({
        title: "Delete",
        onClick: () => { setDeleteError(null); setDeleteTarget(repo); },
        isAriaDisabled: repo.builtin,
        tooltipProps: repo.builtin ? { content: "Built-in repositories cannot be deleted. Disable the catalog instead." } : undefined,
      });
    }
    return items;
  };

  return (
    <div>
      <p style={{ marginBottom: 12 }}>
        Repositories indexed by this server for the policy editor&apos;s application catalog.
        Nodes receive remotes through <strong>Flatpak</strong> policies; adding a repository
        here only makes its applications searchable and its definition available to copy.
      </p>
      {error && <LiveAlert variant="danger" isInline message={error} style={{ marginBottom: 12 }} />}
      {!refreshEnabled && (
        <LiveAlert
          variant="info"
          isInline
          message="Outbound catalog refresh is disabled on this server (BOR_FLATPAK_CATALOG_REFRESH=false). Use “Upload catalog…” to import an appstream.xml.gz downloaded elsewhere."
          style={{ marginBottom: 12 }}
        />
      )}

      <BorToolbar
        searchValue={search}
        onSearchChange={setSearch}
        searchAriaLabel="Search Flatpak repositories"
        searchPlaceholder="Search by name, title or URL"
        onClearAll={() => setSearch("")}
      >
        {canCreate && (
          <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => setModal({ open: true, repo: null })}>
            Add repository
          </Button>
        )}
      </BorToolbar>

      {loading ? (
        <Spinner aria-label="Loading" />
      ) : filtered.length === 0 ? (
        <BorEmptyState
          isEmptyData={repos.length === 0}
          itemsLabel="Flatpak repositories"
          emptyTitle="No Flatpak repositories"
          emptyBody="Add a repository to index its applications for the policy editor."
          action={canCreate ? <Button variant="primary" onClick={() => setModal({ open: true, repo: null })}>Add repository</Button> : undefined}
          onClearFilters={() => setSearch("")}
        />
      ) : (
        <Table aria-label="Flatpak repositories" variant="compact">
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>URL</Th>
              <Th>Subset</Th>
              <Th>Catalog</Th>
              <Th>Last refresh</Th>
              <Th screenReaderText="Actions" />
            </Tr>
          </Thead>
          <Tbody>
            {filtered.map((repo) => {
              const st = STATUS[repo.last_refresh_status] ?? STATUS.never;
              return (
                <Tr key={repo.id}>
                  <Td dataLabel="Name">
                    <strong>{repo.title || repo.name}</strong>
                    <div><code>{repo.name}</code>{repo.builtin && <Label isCompact style={{ marginLeft: 8 }}>built-in</Label>}</div>
                  </Td>
                  <Td dataLabel="URL" style={{ wordBreak: "break-all" }}>{repo.url}</Td>
                  <Td dataLabel="Subset">{repo.subset || "all"}</Td>
                  <Td dataLabel="Catalog">
                    {repo.catalog_enabled ? (
                      <>
                        {repo.last_refresh_status === "error" && repo.last_refresh_error ? (
                          <Tooltip content={repo.last_refresh_error}><Label color={st.color}>{st.text}</Label></Tooltip>
                        ) : (
                          <Label color={st.color}>{st.text}</Label>
                        )}
                        <span style={{ marginLeft: 8 }}>{repo.app_count} apps</span>
                      </>
                    ) : (
                      <Label color="grey">Catalog off</Label>
                    )}
                  </Td>
                  <Td dataLabel="Last refresh">{formatWhen(repo.last_refresh_at)}</Td>
                  <Td dataLabel="Actions" isActionCell>
                    {rowActions(repo).length > 0 && (
                      <ActionsColumn
                        items={rowActions(repo)}
                        actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                          <MenuToggle
                            ref={toggleRef}
                            aria-label={`Actions for repository ${repo.name}`}
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

      <FlatpakRepositoryModal
        isOpen={modal.open}
        initial={modal.repo}
        onSaved={(saved) => {
          setModal({ open: false, repo: null });
          addToast({ variant: "success", title: modal.repo ? "Repository updated" : "Repository added", detail: saved.name });
          reload();
        }}
        onClose={() => setModal({ open: false, repo: null })}
      />
      <FlatpakCatalogUploadModal
        repo={uploadTarget}
        onDone={(n) => {
          addToast({ variant: "success", title: "Catalog imported", detail: `${n} applications indexed for ${uploadTarget?.name ?? ""}` });
          setUploadTarget(null);
          reload();
        }}
        onClose={() => setUploadTarget(null)}
      />
      <ConfirmModal
        isOpen={!!deleteTarget}
        title="Delete Flatpak repository"
        confirmLabel="Delete"
        isDanger
        confirmPhrase={deleteTarget?.name}
        isBusy={deleteBusy}
        error={deleteError}
        onConfirm={() => void handleDelete()}
        onCancel={() => setDeleteTarget(null)}
      >
        <p>
          This removes <strong>{deleteTarget?.title || deleteTarget?.name}</strong> and its indexed
          catalog from the server. Policies that already copied this remote keep working.
        </p>
      </ConfirmModal>
    </div>
  );
};
