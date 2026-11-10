// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Group-side membership dialogs of the Node Groups page:
 *
 *   AddNodesModal       - pick several nodes that are not members yet and add
 *                         them to the group in one call.
 *   ManageMembersModal  - list the member nodes and remove one or several
 *                         (type-to-confirm).
 */

import React, { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import {
  Button,
  Flex,
  FlexItem,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  Pagination,
  Spinner,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";

import { BorEmptyState } from "../../components/BorEmptyState";
import { BorToolbar } from "../../components/BorToolbar";
import { ConfirmModal } from "../../components/ConfirmModal";
import { LiveAlert } from "../../components/LiveAlert";
import { fetchNodesPaged, Node, NodeStatus } from "../../apiClient/nodesApi";
import {
  addNodesToGroup,
  removeNodeFromNodeGroup,
  NodeGroup,
} from "../../apiClient/nodeGroupsApi";

/** Rows fetched per request; also the server's page-size cap. */
const CANDIDATE_LIMIT = 100;
const MEMBERS_PER_PAGE = 20;
const SEARCH_DEBOUNCE_MS = 300;

const statusColor = (status: NodeStatus): "green" | "red" | "grey" => {
  switch (status) {
    case "online":  return "green";
    case "offline": return "red";
    default:        return "grey";
  }
};

const plural = (n: number, noun: string): string => `${n} ${noun}${n === 1 ? "" : "s"}`;

const errorText = (err: unknown, fallback: string): string =>
  err instanceof Error ? err.message : fallback;

/** Returns `value` after it has been stable for SEARCH_DEBOUNCE_MS. */
function useDebounced(value: string): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [value]);
  return debounced;
}

const centeredSpinner = (
  <Flex justifyContent={{ default: "justifyContentCenter" }}>
    <FlexItem><Spinner size="lg" aria-label="Loading" /></FlexItem>
  </Flex>
);

/* ── Add nodes ── */

interface AddNodesModalProps {
  /** The group to add to; null keeps the dialog closed. */
  group: NodeGroup | null;
  onClose: () => void;
  /** Called after nodes were added, with the number of new members. */
  onAdded: (added: number) => void;
}

export const AddNodesModal: React.FC<AddNodesModalProps> = ({ group, onClose, onAdded }) => {
  const [search, setSearch] = useState("");
  const appliedSearch = useDebounced(search);
  const [candidates, setCandidates] = useState<Node[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // id -> name, so the selection survives a changed search.
  const [selected, setSelected] = useState<Map<string, string>>(new Map());
  const [saving, setSaving] = useState(false);

  const groupId = group?.id;

  useEffect(() => {
    setSearch("");
    setSelected(new Map());
    setError(null);
    setSaving(false);
  }, [groupId]);

  useEffect(() => {
    if (!groupId) return undefined;
    let cancelled = false;
    setLoading(true);
    fetchNodesPaged({
      exclude_group: groupId,
      search: appliedSearch,
      per_page: CANDIDATE_LIMIT,
      sort_field: "name",
      sort_order: "asc",
    })
      .then((res) => {
        if (cancelled) return;
        setCandidates(res.items);
        setTotal(res.total);
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        setCandidates([]);
        setTotal(0);
        setError(errorText(err, "Failed to load nodes"));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [groupId, appliedSearch]);

  const toggle = (node: Node) => {
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(node.id)) { next.delete(node.id); } else { next.set(node.id, node.name); }
      return next;
    });
  };

  const allVisibleSelected = candidates.length > 0 && candidates.every((n) => selected.has(n.id));

  const toggleAllVisible = () => {
    setSelected((prev) => {
      const next = new Map(prev);
      candidates.forEach((n) => {
        if (allVisibleSelected) { next.delete(n.id); } else { next.set(n.id, n.name); }
      });
      return next;
    });
  };

  const handleAdd = async () => {
    if (!group || selected.size === 0) return;
    setSaving(true);
    setError(null);
    try {
      const added = await addNodesToGroup(group.id, Array.from(selected.keys()));
      onAdded(added);
    } catch (err) {
      setError(errorText(err, "Failed to add nodes"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      variant={ModalVariant.large}
      isOpen={group !== null}
      onClose={onClose}
      aria-labelledby="add-nodes-title"
    >
      <ModalHeader
        title={`Add nodes to ${group?.name ?? ""}`}
        labelId="add-nodes-title"
        description="Nodes keep their other groups; a node receives the policies of every group it is in."
      />
      <ModalBody>
        <LiveAlert message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
        <BorToolbar
          searchValue={search}
          onSearchChange={setSearch}
          searchAriaLabel="Search nodes to add"
          searchPlaceholder="Search by name, FQDN, IP..."
          onClearAll={() => setSearch("")}
        >
          <ToolbarItem>
            <span aria-live="polite" className="bor-text-secondary">
              {plural(selected.size, "node")} selected
            </span>
          </ToolbarItem>
        </BorToolbar>

        {loading && candidates.length === 0 ? (
          centeredSpinner
        ) : candidates.length === 0 ? (
          <BorEmptyState
            isEmptyData={appliedSearch.trim() === ""}
            itemsLabel="nodes"
            emptyTitle="No nodes to add"
            emptyBody="Every node you can manage is already a member of this group."
            onClearFilters={() => setSearch("")}
          />
        ) : (
          <>
            <Table aria-label={`Nodes that are not in ${group?.name ?? "the group"}`} variant="compact">
              <Thead>
                <Tr>
                  <Th
                    select={{ onSelect: toggleAllVisible, isSelected: allVisibleSelected }}
                    aria-label="Select all listed nodes"
                  />
                  <Th>Name</Th>
                  <Th>Status</Th>
                  <Th>Current groups</Th>
                </Tr>
              </Thead>
              <Tbody>
                {candidates.map((node, rowIndex) => (
                  <Tr key={node.id}>
                    <Td
                      select={{
                        rowIndex,
                        onSelect: () => toggle(node),
                        isSelected: selected.has(node.id),
                      }}
                    />
                    <Td dataLabel="Name">{node.name}</Td>
                    <Td dataLabel="Status">
                      <Label color={statusColor(node.status)}>{node.status}</Label>
                    </Td>
                    <Td dataLabel="Current groups">
                      {node.node_group_names?.length ? node.node_group_names.join(", ") : "Unassigned"}
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
            {total > candidates.length && (
              <p className="bor-text-secondary" style={{ marginTop: "0.5rem" }}>
                Showing the first {candidates.length} of {total} nodes. Search to find the others;
                your selection is kept.
              </p>
            )}
          </>
        )}
      </ModalBody>
      <ModalFooter>
        <Button
          variant="primary"
          onClick={handleAdd}
          isLoading={saving}
          isDisabled={saving || selected.size === 0}
        >
          {selected.size > 0 ? `Add ${plural(selected.size, "node")}` : "Add"}
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={saving}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};

/* ── Manage members ── */

interface ManageMembersModalProps {
  /** The group whose members are shown; null keeps the dialog closed. */
  group: NodeGroup | null;
  /** Whether the user may remove members (node:delete). */
  canRemove: boolean;
  onClose: () => void;
  /** Called after members were removed, so the group list can refresh. */
  onChanged: () => void;
}

interface RemovalTarget {
  id: string;
  name: string;
}

export const ManageMembersModal: React.FC<ManageMembersModalProps> = ({
  group,
  canRemove,
  onClose,
  onChanged,
}) => {
  const [search, setSearch] = useState("");
  const appliedSearch = useDebounced(search);
  const [page, setPage] = useState(1);
  const [members, setMembers] = useState<Node[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [status, setStatus] = useState<string | null>(null);
  // id -> name, so a selection can span pages.
  const [selected, setSelected] = useState<Map<string, string>>(new Map());
  const [removal, setRemoval] = useState<RemovalTarget[] | null>(null);
  const [removing, setRemoving] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const groupId = group?.id;

  useEffect(() => {
    setSearch("");
    setPage(1);
    setSelected(new Map());
    setError(null);
    setStatus(null);
    setRemoval(null);
  }, [groupId]);

  useEffect(() => {
    setPage(1);
  }, [appliedSearch]);

  const load = useCallback(async () => {
    if (!groupId) return;
    setLoading(true);
    try {
      const res = await fetchNodesPaged({
        group: groupId,
        search: appliedSearch,
        page,
        per_page: MEMBERS_PER_PAGE,
        sort_field: "name",
        sort_order: "asc",
      });
      // Removing the last rows of the last page leaves it empty: step back.
      if (res.items.length === 0 && res.total > 0 && page > 1) {
        setPage(Math.max(1, res.total_pages));
        return;
      }
      setMembers(res.items);
      setTotal(res.total);
      setError(null);
    } catch (err) {
      setMembers([]);
      setTotal(0);
      setError(errorText(err, "Failed to load member nodes"));
    } finally {
      setLoading(false);
    }
  }, [groupId, appliedSearch, page]);

  useEffect(() => {
    load();
  }, [load]);

  const toggle = (node: Node) => {
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(node.id)) { next.delete(node.id); } else { next.set(node.id, node.name); }
      return next;
    });
  };

  const allVisibleSelected = members.length > 0 && members.every((n) => selected.has(n.id));

  const toggleAllVisible = () => {
    setSelected((prev) => {
      const next = new Map(prev);
      members.forEach((n) => {
        if (allVisibleSelected) { next.delete(n.id); } else { next.set(n.id, n.name); }
      });
      return next;
    });
  };

  const openRemoval = (targets: RemovalTarget[]) => {
    setRemoveError(null);
    setStatus(null);
    setRemoval(targets);
  };

  const handleRemove = async () => {
    if (!group || !removal) return;
    setRemoving(true);
    setRemoveError(null);
    const results = await Promise.allSettled(
      removal.map((t) => removeNodeFromNodeGroup(group.id, t.id)),
    );
    const removedIds = removal.filter((_t, i) => results[i].status === "fulfilled").map((t) => t.id);
    const failed = results.find((r): r is PromiseRejectedResult => r.status === "rejected");
    setRemoving(false);

    if (removedIds.length > 0) {
      setSelected((prev) => {
        const next = new Map(prev);
        removedIds.forEach((id) => next.delete(id));
        return next;
      });
      onChanged();
      load();
    }
    if (failed) {
      const failures = removal.length - removedIds.length;
      setRemoveError(
        `${plural(failures, "node")} could not be removed: ${errorText(failed.reason, "request failed")}`,
      );
      return;
    }
    setStatus(`Removed ${plural(removedIds.length, "node")} from ${group.name}`);
    setRemoval(null);
  };

  const single = removal?.length === 1 ? removal[0] : null;
  const confirmPhrase = single ? single.name : "Yes";

  return (
    <>
      <Modal
        variant={ModalVariant.large}
        isOpen={group !== null}
        onClose={onClose}
        aria-labelledby="manage-members-title"
      >
        <ModalHeader title={`Members of ${group?.name ?? ""}`} labelId="manage-members-title" />
        <ModalBody>
          <LiveAlert message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
          <LiveAlert message={status} variant="success" isInline style={{ marginBottom: "1rem" }} />
          <BorToolbar
            searchValue={search}
            onSearchChange={setSearch}
            searchAriaLabel="Search member nodes"
            searchPlaceholder="Search by name, FQDN, IP..."
            onClearAll={() => setSearch("")}
          >
            {canRemove && (
              <ToolbarItem>
                <Button
                  variant="secondary"
                  isDanger
                  isDisabled={selected.size === 0}
                  onClick={() =>
                    openRemoval(Array.from(selected, ([id, name]) => ({ id, name })))
                  }
                >
                  Remove selected ({selected.size})
                </Button>
              </ToolbarItem>
            )}
            {total > MEMBERS_PER_PAGE && (
              <ToolbarItem variant="pagination">
                <Pagination
                  itemCount={total}
                  page={page}
                  perPage={MEMBERS_PER_PAGE}
                  onSetPage={(_ev, p) => setPage(p)}
                  isCompact
                  perPageOptions={[]}
                  aria-label="Member nodes pagination"
                />
              </ToolbarItem>
            )}
          </BorToolbar>

          {loading && members.length === 0 ? (
            centeredSpinner
          ) : members.length === 0 ? (
            <BorEmptyState
              isEmptyData={appliedSearch.trim() === ""}
              itemsLabel="member nodes"
              emptyTitle="No member nodes"
              emptyBody='This group has no nodes yet. Use "Add nodes..." in the group menu, or enroll an agent with a token of this group.'
              onClearFilters={() => setSearch("")}
            />
          ) : (
            <Table aria-label={`Member nodes of ${group?.name ?? "the group"}`} variant="compact">
              <Thead>
                <Tr>
                  {canRemove && (
                    <Th
                      select={{ onSelect: toggleAllVisible, isSelected: allVisibleSelected }}
                      aria-label="Select all listed nodes"
                    />
                  )}
                  <Th>Name</Th>
                  <Th>Status</Th>
                  <Th>Other groups</Th>
                  {canRemove && <Th screenReaderText="Actions" />}
                </Tr>
              </Thead>
              <Tbody>
                {members.map((node, rowIndex) => {
                  const others = (node.node_group_names ?? []).filter(
                    (_name, i) => node.node_group_ids?.[i] !== groupId,
                  );
                  return (
                    <Tr key={node.id}>
                      {canRemove && (
                        <Td
                          select={{
                            rowIndex,
                            onSelect: () => toggle(node),
                            isSelected: selected.has(node.id),
                          }}
                        />
                      )}
                      <Td dataLabel="Name">{node.name}</Td>
                      <Td dataLabel="Status">
                        <Label color={statusColor(node.status)}>{node.status}</Label>
                      </Td>
                      <Td dataLabel="Other groups">{others.length ? others.join(", ") : "-"}</Td>
                      {canRemove && (
                        <Td dataLabel="Actions" isActionCell>
                          <Button
                            variant="link"
                            isDanger
                            aria-label={`Remove ${node.name} from ${group?.name ?? "the group"}`}
                            onClick={() => openRemoval([{ id: node.id, name: node.name }])}
                          >
                            Remove
                          </Button>
                        </Td>
                      )}
                    </Tr>
                  );
                })}
              </Tbody>
            </Table>
          )}
        </ModalBody>
        <ModalFooter>
          <Button variant="primary" onClick={onClose}>
            Close
          </Button>
          {group && (
            <Button
              variant="link"
              component={(props: React.ComponentProps<"a">) => (
                <Link {...props} to={`/nodes?group=${group.id}`} />
              )}
            >
              Open in Nodes
            </Button>
          )}
        </ModalFooter>
      </Modal>

      <ConfirmModal
        isOpen={removal !== null}
        title={
          single
            ? `Remove ${single.name} from ${group?.name ?? "the group"}?`
            : `Remove ${removal?.length ?? 0} nodes from ${group?.name ?? "the group"}?`
        }
        isDanger
        confirmLabel="Remove"
        confirmPhrase={confirmPhrase}
        confirmPhraseLabel={
          single
            ? `Type the node name "${single.name}" to confirm`
            : `Type "Yes" to confirm removing ${removal?.length ?? 0} nodes`
        }
        isBusy={removing}
        error={removeError}
        onConfirm={handleRemove}
        onCancel={() => setRemoval(null)}
      >
        {single ? "The node stops" : "The nodes stop"} receiving the policies bound to this group.
        Other group memberships are kept; a node left in no group receives no policies.
      </ConfirmModal>
    </>
  );
};
