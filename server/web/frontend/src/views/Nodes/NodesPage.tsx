// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useMemo, useCallback } from "react";
import { useSearchParams } from "react-router";
import { LiveAlert } from "../../components/LiveAlert";
import { useToast } from "../../components/ToastHost";
import { BorToolbar } from "../../components/BorToolbar";
import { SearchableSelect } from "../../components/SearchableSelect";
import {
  PageSection,
  Title,
  Alert,
  Spinner,
  Flex,
  FlexItem,
  ToolbarItem,
  ToolbarFilter,
  Pagination,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectOption,
  SelectList,
  Label,
  Button,
  Dropdown,
  DropdownItem,
  DropdownList,
  Tooltip,
  DrawerPanelContent,
  DrawerHead,
  DrawerActions,
  DrawerCloseButton,
  DrawerPanelBody,
  Drawer,
  DrawerContent,
  DrawerContentBody,
  DescriptionList,
  DescriptionListGroup,
  DescriptionListTerm,
  DescriptionListDescription,
  EmptyState,
  EmptyStateBody,
  Modal,
  ModalHeader,
  ModalBody,
  ModalVariant,
  TextInput,
  TextArea,
  Form,
  FormGroup,
  ActionGroup,
  Checkbox,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ThProps } from "@patternfly/react-table";
import SearchIcon from "@patternfly/react-icons/dist/esm/icons/search-icon";
import CubesIcon from "@patternfly/react-icons/dist/esm/icons/cubes-icon";
import PencilAltIcon from "@patternfly/react-icons/dist/esm/icons/pencil-alt-icon";

import {
  fetchNodesPaged,
  fetchNodeFilterOptions,
  refreshNodeMetadata,
  addNodeToGroup,
  removeNodeFromGroup,
  deleteNode,
  revokeNodeCertificate,
  updateNode,
  Node,
  NodeStatus,
  NodeFilterOptions,
} from "../../apiClient/nodesApi";
import { fetchNodeGroups, NodeGroup } from "../../apiClient/nodeGroupsApi";

/* ── Helpers ── */

const STATUS_OPTIONS: NodeStatus[] = ["online", "offline", "unknown"];
const MAX_NOTES_DISPLAY_LENGTH = 30;

const statusColor = (status: NodeStatus): "green" | "red" | "grey" => {
  switch (status) {
    case "online":  return "green";
    case "offline": return "red";
    default:        return "grey";
  }
};

const statusTooltip = (status: NodeStatus, reason?: string): string => {
  if (reason) return reason;
  switch (status) {
    case "online":  return "Agent stream connected";
    case "offline": return "Agent stream disconnected";
    case "unknown": return "Never connected or enrollment pending";
    default:        return "";
  }
};

const osDisplay = (node: Node): string =>
  [node.os_name, node.os_version].filter(Boolean).join(" ") || "";

const timeAgo = (dateStr?: string): string => {
  if (!dateStr) return "Never";
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
};

type SortField = "last_seen" | "name";

/* ── Component ── */

export const NodesPage: React.FC = () => {
  const { addToast } = useToast();
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Search
  const [searchValue, setSearchValue] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");

  // Filters — status can be seeded from the URL (?status=) for dashboard drill-down.
  const [searchParams] = useSearchParams();
  const [statusFilter, setStatusFilter] = useState<string>(() => {
    const s = searchParams.get("status");
    return s && (STATUS_OPTIONS as string[]).includes(s) ? s : "All";
  });
  const [statusOpen, setStatusOpen] = useState(false);
  // Group filter: "All", "none" (unassigned), or a group id. Seeded from ?group=.
  const [groupFilter, setGroupFilter] = useState<string>(() => searchParams.get("group") ?? "All");
  const [osFilter, setOsFilter] = useState<string>("All");
  const [desktopFilter, setDesktopFilter] = useState<string>("All");
  const [agentVersionFilter, setAgentVersionFilter] = useState<string>("All");

  // Sort
  const [sortField, setSortField] = useState<SortField>("last_seen");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("desc");

  // Server-side pagination
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(20);
  const [total, setTotal] = useState(0);
  const [filterOptions, setFilterOptions] = useState<NodeFilterOptions>({
    os: [],
    desktops: [],
    agent_versions: [],
  });

  // Drawer
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [drawerExpanded, setDrawerExpanded] = useState(false);

  // Inline note editing (in the detail drawer)
  const [editingNotes, setEditingNotes] = useState(false);
  const [notesDraft, setNotesDraft] = useState("");
  const [savingNotes, setSavingNotes] = useState(false);
  const [notesError, setNotesError] = useState<string | null>(null);

  // Reset the note editor whenever the selected node changes (or the drawer closes).
  useEffect(() => {
    setEditingNotes(false);
    setNotesError(null);
  }, [selectedNode?.id]);

  // Selection (for bulk actions)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  // Bulk actions dropdown
  const [bulkOpen, setBulkOpen] = useState(false);

  // Metadata refresh (in drawer)
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState<string | null>(null);

  // Certificate revocation (in drawer)
  const [revoking, setRevoking] = useState(false);
  const [revokeError, setRevokeError] = useState<string | null>(null);
  const [revokeSuccess, setRevokeSuccess] = useState(false);

  // Action error banner
  const [actionError, setActionError] = useState<string | null>(null);

  // "Add to group" modal state
  const [groupModalOpen, setGroupModalOpen] = useState(false);
  const [groupModalTargetIds, setGroupModalTargetIds] = useState<string[]>([]);
  const [nodeGroups, setNodeGroups] = useState<NodeGroup[]>([]);
  const [groupPickerOpen, setGroupPickerOpen] = useState(false);
  const [selectedGroupId, setSelectedGroupId] = useState<string>("");
  const [groupActionLoading, setGroupActionLoading] = useState(false);
  const [groupActionError, setGroupActionError] = useState<string | null>(null);

  // "Decommission" modal state
  const [decommModalOpen, setDecommModalOpen] = useState(false);
  const [decommTargetIds, setDecommTargetIds] = useState<string[]>([]);
  const [decommConfirmText, setDecommConfirmText] = useState("");
  const [decommLoading, setDecommLoading] = useState(false);
  const [decommError, setDecommError] = useState<string | null>(null);

  // "Remove from group" modal state
  const [removeGroupModalOpen, setRemoveGroupModalOpen] = useState(false);
  const [removeGroupTargetIds, setRemoveGroupTargetIds] = useState<string[]>([]);
  const [removeGroupOptions, setRemoveGroupOptions] = useState<{ id: string; name: string }[]>([]);
  const [removeGroupSelectedIds, setRemoveGroupSelectedIds] = useState<Set<string>>(new Set());
  const [removeGroupLoading, setRemoveGroupLoading] = useState(false);
  const [removeGroupError, setRemoveGroupError] = useState<string | null>(null);

  /* ── Build the current query from filters/sort/page ── */
  const nodeQuery = useCallback(
    () => ({
      search: appliedSearch || undefined,
      status: statusFilter !== "All" ? statusFilter : undefined,
      os: osFilter !== "All" ? osFilter : undefined,
      desktop: desktopFilter !== "All" ? desktopFilter : undefined,
      agent_version: agentVersionFilter !== "All" ? agentVersionFilter : undefined,
      group: groupFilter !== "All" ? groupFilter : undefined,
      sort_field: sortField,
      sort_order: sortDirection,
    }),
    [appliedSearch, statusFilter, osFilter, desktopFilter, agentVersionFilter, groupFilter, sortField, sortDirection],
  );

  /* ── Load a page of data (all filtering/sorting is server-side) ── */
  const loadNodes = useCallback(
    async (showSpinner = true) => {
      try {
        if (showSpinner) setLoading(true);
        setError(null);
        const result = await fetchNodesPaged({ page, per_page: perPage, ...nodeQuery() });
        setNodes(result.items);
        setTotal(result.total);
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load nodes");
      } finally {
        setLoading(false);
      }
    },
    [page, perPage, nodeQuery],
  );

  useEffect(() => {
    loadNodes();
  }, [loadNodes]);

  // Reset to the first page whenever the filters/search/sort change, so the
  // user never lands on an out-of-range page.
  useEffect(() => {
    setPage(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [appliedSearch, statusFilter, osFilter, desktopFilter, agentVersionFilter, groupFilter, sortField, sortDirection]);

  /* ── Filter dropdown options (distinct values, fetched once) ── */
  useEffect(() => {
    fetchNodeFilterOptions().then(setFilterOptions).catch(() => {});
  }, []);

  const osOptions = useMemo(() => ["All", ...filterOptions.os], [filterOptions.os]);
  const desktopOptions = useMemo(() => ["All", ...filterOptions.desktops], [filterOptions.desktops]);
  const agentVersionOptions = useMemo(
    () => ["All", ...filterOptions.agent_versions],
    [filterOptions.agent_versions],
  );

  /* ── Sort handler ── */
  // Column indices must match the real header order, where index 0 is the
  // row-select cell: Node = 1, Last Seen = 5. sortBy.index is the *active*
  // column so only that header shows a sort arrow.
  const columnIndexFor = (field: SortField): number => (field === "name" ? 1 : 5);
  const getSortParams = (field: SortField): ThProps["sort"] => ({
    sortBy: {
      index: columnIndexFor(sortField),
      direction: sortDirection,
      defaultDirection: "asc",
    },
    onSort: (_ev, _idx, dir) => {
      setSortField(field);
      setSortDirection(dir);
    },
    columnIndex: columnIndexFor(field),
  });

  /* ── Selection ── */
  const isAllSelected = nodes.length > 0 && selectedIds.size === nodes.length;
  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(nodes.map((n) => n.id)));
    }
  };
  const toggleSelectNode = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) { next.delete(id); } else { next.add(id); }
      return next;
    });
  };

  /* ── Export CSV ── */
  // Quote every field, double embedded quotes (RFC 4180), and neutralise
  // spreadsheet formula injection by prefixing a value that starts with
  // = + - @ (or a tab/CR) with a single quote.
  const csvCell = (value: string): string => {
    let v = value ?? "";
    if (/^[=+\-@\t\r]/.test(v)) v = `'${v}`;
    return `"${v.replace(/"/g, '""')}"`;
  };

  const exportCSV = async () => {
    // Export all rows matching the current filters (or just the selected ones),
    // not only the visible page — fetch every matching page first.
    let rows: Node[];
    if (selectedIds.size > 0) {
      rows = nodes.filter((n) => selectedIds.has(n.id));
    } else {
      const all: Node[] = [];
      const query = nodeQuery();
      for (let p = 1; ; p++) {
        const res = await fetchNodesPaged({ page: p, per_page: 100, ...query });
        all.push(...res.items);
        if (res.items.length === 0 || p >= res.total_pages) break;
      }
      rows = all;
    }
    const header = "Name,Status,Node Groups,Last Seen,Agent Version,OS,Notes";
    const csvRows = rows.map((n) =>
      [
        n.name,
        n.status,
        n.node_group_names?.join("; ") || "",
        n.last_seen || "",
        n.agent_version || "",
        osDisplay(n),
        n.notes || "",
      ]
        .map((f) => csvCell(String(f)))
        .join(","),
    );
    const blob = new Blob([header + "\n" + csvRows.join("\n")], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "nodes-export.csv";
    a.click();
    URL.revokeObjectURL(url);
    addToast({
      variant: "success",
      title: `Exported ${rows.length} node${rows.length === 1 ? "" : "s"} to CSV`,
    });
  };

  /* ── Metadata refresh ── */
  const handleRefreshMetadata = async () => {
    if (!selectedNode) return;
    setRefreshing(true);
    setRefreshError(null);
    try {
      await refreshNodeMetadata(selectedNode.id);
      addToast({
        variant: "success",
        title: "Metadata refresh requested",
        detail: `${selectedNode.name} will report updated details shortly.`,
      });
    } catch (err) {
      setRefreshError(err instanceof Error ? err.message : "Failed to request metadata refresh");
    } finally {
      setRefreshing(false);
    }
  };

  /* ── Note editing ── */
  const startEditNotes = () => {
    setNotesDraft(selectedNode?.notes ?? "");
    setNotesError(null);
    setEditingNotes(true);
  };

  const saveNotes = async () => {
    if (!selectedNode) return;
    const nodeId = selectedNode.id;
    const value = notesDraft;
    setSavingNotes(true);
    setNotesError(null);
    try {
      await updateNode(nodeId, { notes: value });
      // Patch only the note locally so computed fields (groups, etc.) are kept.
      setSelectedNode((prev) => (prev && prev.id === nodeId ? { ...prev, notes: value } : prev));
      setNodes((prev) => prev.map((n) => (n.id === nodeId ? { ...n, notes: value } : n)));
      setEditingNotes(false);
      addToast({ variant: "success", title: "Note saved" });
    } catch (err) {
      setNotesError(err instanceof Error ? err.message : "Failed to save note");
    } finally {
      setSavingNotes(false);
    }
  };

  /* ── Certificate revocation ── */
  const handleRevokeCertificate = async () => {
    if (!selectedNode) return;
    setRevoking(true);
    setRevokeError(null);
    setRevokeSuccess(false);
    try {
      await revokeNodeCertificate(selectedNode.id);
      setRevokeSuccess(true);
    } catch (err) {
      setRevokeError(err instanceof Error ? err.message : "Failed to revoke certificate");
    } finally {
      setRevoking(false);
    }
  };

  /* ── Add to group ── */
  const openGroupModal = async (ids: string[]) => {
    setGroupModalTargetIds(ids);
    setGroupPickerOpen(false);
    setSelectedGroupId("");
    setGroupActionError(null);
    setGroupActionLoading(false);
    // Lazily load groups
    try {
      const groups = await fetchNodeGroups();
      setNodeGroups(groups);
    } catch {
      setNodeGroups([]);
    }
    setGroupModalOpen(true);
  };

  const handleAddToGroup = async () => {
    if (!selectedGroupId) return;
    setGroupActionLoading(true);
    setGroupActionError(null);
    try {
      await Promise.all(groupModalTargetIds.map((id) => addNodeToGroup(id, selectedGroupId)));
      await loadNodes();
      setGroupModalOpen(false);
      setSelectedIds(new Set());
    } catch (err) {
      setGroupActionError(err instanceof Error ? err.message : "Failed to assign group");
    } finally {
      setGroupActionLoading(false);
    }
  };

  /* ── Remove from group ── */
  const openRemoveGroupModal = (ids: string[]) => {
    // Collect all distinct groups across the target nodes from current data
    const groupMap = new Map<string, string>(); // id → name
    ids.forEach((nodeId) => {
      const node = nodes.find((n) => n.id === nodeId);
      if (node?.node_group_ids) {
        node.node_group_ids.forEach((gid, idx) => {
          const name = node.node_group_names?.[idx] ?? gid;
          groupMap.set(gid, name);
        });
      }
    });
    const options = Array.from(groupMap.entries()).map(([id, name]) => ({ id, name }));
    setRemoveGroupOptions(options);
    // Pre-select all when there's only one option
    setRemoveGroupSelectedIds(options.length === 1 ? new Set([options[0].id]) : new Set());
    setRemoveGroupTargetIds(ids);
    setRemoveGroupError(null);
    setRemoveGroupLoading(false);
    setRemoveGroupModalOpen(true);
  };

  const handleRemoveFromGroups = async () => {
    setRemoveGroupLoading(true);
    setRemoveGroupError(null);
    try {
      const ops: Promise<void>[] = [];
      removeGroupTargetIds.forEach((nodeId) => {
        const node = nodes.find((n) => n.id === nodeId);
        if (!node?.node_group_ids) return;
        node.node_group_ids.forEach((gid) => {
          if (removeGroupSelectedIds.has(gid)) {
            ops.push(removeNodeFromGroup(nodeId, gid));
          }
        });
      });
      await Promise.all(ops);
      await loadNodes();
      setSelectedIds(new Set());
      setRemoveGroupModalOpen(false);
    } catch (err) {
      setRemoveGroupError(err instanceof Error ? err.message : "Failed to remove from group");
    } finally {
      setRemoveGroupLoading(false);
    }
  };

  /* ── Decommission ── */
  const openDecommModal = (ids: string[]) => {
    setDecommTargetIds(ids);
    setDecommConfirmText("");
    setDecommError(null);
    setDecommLoading(false);
    setDecommModalOpen(true);
  };

  // For single node: must type the node name. For multiple: must type "Yes".
  const decommPrompt = decommTargetIds.length === 1
    ? nodes.find((n) => n.id === decommTargetIds[0])?.name ?? ""
    : "Yes";

  const decommConfirmLabel = decommTargetIds.length === 1
    ? `Type the node name "${decommPrompt}" to confirm`
    : `Type "Yes" to confirm decommissioning ${decommTargetIds.length} nodes`;

  const decommValid = decommConfirmText === decommPrompt;

  const handleDecommission = async () => {
    if (!decommValid) return;
    setDecommLoading(true);
    setDecommError(null);
    try {
      await Promise.all(decommTargetIds.map((id) => deleteNode(id)));
      await loadNodes();
      // Close drawer if the selected node was deleted
      if (selectedNode && decommTargetIds.includes(selectedNode.id)) {
        setSelectedNode(null);
        setDrawerExpanded(false);
      }
      setSelectedIds(new Set());
      setDecommModalOpen(false);
    } catch (err) {
      setDecommError(err instanceof Error ? err.message : "Failed to decommission node(s)");
    } finally {
      setDecommLoading(false);
    }
  };

  /* ── Drawer panel ── */
  const drawerPanel = (
    <DrawerPanelContent widths={{ default: "width_33" }}>
      <DrawerHead>
        <Title headingLevel="h2" size="lg">
          {selectedNode?.name || "Node Details"}
        </Title>
        <DrawerActions>
          <DrawerCloseButton
            onClick={() => {
              setDrawerExpanded(false);
              setSelectedNode(null);
            }}
          />
        </DrawerActions>
      </DrawerHead>
      {selectedNode && (
        <DrawerPanelBody>
          <Title headingLevel="h3" size="md" style={{ marginBottom: "1rem" }}>
            Summary
          </Title>
          <DescriptionList isHorizontal isCompact>
            <DescriptionListGroup>
              <DescriptionListTerm>Status</DescriptionListTerm>
              <DescriptionListDescription>
                <Tooltip content={statusTooltip(selectedNode.status, selectedNode.status_reason)}>
                  <Label color={statusColor(selectedNode.status)}>{selectedNode.status}</Label>
                </Tooltip>
              </DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>FQDN</DescriptionListTerm>
              <DescriptionListDescription>{selectedNode.fqdn || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Machine ID</DescriptionListTerm>
              <DescriptionListDescription>{selectedNode.machine_id || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>IP Address</DescriptionListTerm>
              <DescriptionListDescription>{selectedNode.ip_address || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>OS</DescriptionListTerm>
              <DescriptionListDescription>{osDisplay(selectedNode) || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Desktop</DescriptionListTerm>
              <DescriptionListDescription>{selectedNode.desktop_env || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Agent Version</DescriptionListTerm>
              <DescriptionListDescription>{selectedNode.agent_version || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Node Groups</DescriptionListTerm>
              <DescriptionListDescription>
                {selectedNode.node_group_names?.length ? selectedNode.node_group_names.join(", ") : "—"}
              </DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Last Seen</DescriptionListTerm>
              <DescriptionListDescription>
                {selectedNode.last_seen
                  ? `${new Date(selectedNode.last_seen).toLocaleString()} (${timeAgo(selectedNode.last_seen)})`
                  : "Never"}
              </DescriptionListDescription>
            </DescriptionListGroup>
          </DescriptionList>

          {/* ── Notes (editable) ── */}
          <Flex
            justifyContent={{ default: "justifyContentSpaceBetween" }}
            alignItems={{ default: "alignItemsCenter" }}
            style={{ marginTop: "1.5rem", marginBottom: "0.5rem" }}
          >
            <FlexItem>
              <Title headingLevel="h3" size="md">Notes</Title>
            </FlexItem>
            {!editingNotes && (
              <FlexItem>
                <Button
                  variant="link"
                  isInline
                  icon={<PencilAltIcon />}
                  onClick={startEditNotes}
                  aria-label="Edit note"
                >
                  Edit
                </Button>
              </FlexItem>
            )}
          </Flex>
          {editingNotes ? (
            <div>
              <TextArea
                aria-label={`Notes for ${selectedNode.name}`}
                value={notesDraft}
                onChange={(_ev, v) => setNotesDraft(v)}
                rows={4}
                placeholder="Add a note about this node…"
                aria-invalid={notesError ? true : undefined}
                aria-describedby={notesError ? "node-notes-error" : undefined}
              />
              <LiveAlert
                id="node-notes-error"
                message={notesError}
                variant="danger"
                isInline
                style={{ marginTop: "0.5rem" }}
              />
              <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.5rem" }}>
                <Button variant="primary" onClick={saveNotes} isLoading={savingNotes} isDisabled={savingNotes}>
                  Save
                </Button>
                <Button
                  variant="link"
                  onClick={() => { setEditingNotes(false); setNotesError(null); }}
                  isDisabled={savingNotes}
                >
                  Cancel
                </Button>
              </div>
            </div>
          ) : (
            <p style={{ whiteSpace: "pre-wrap", color: selectedNode.notes ? undefined : "var(--pf-t--global--text--color--subtle)" }}>
              {selectedNode.notes || "No note yet."}
            </p>
          )}

          {selectedNode.cert_serial && (() => {
            const notAfter = selectedNode.cert_not_after ? new Date(selectedNode.cert_not_after) : null;
            const msPerDay = 86_400_000;
            const daysLeft = notAfter ? Math.ceil((notAfter.getTime() - Date.now()) / msPerDay) : null;
            const certColor = daysLeft === null ? "var(--pf-t--global--text--color--subtle)"
              : daysLeft <= 0 ? "var(--pf-t--global--text--color--status--danger--default)"
              : daysLeft <= 30 ? "var(--pf-t--global--text--color--status--warning--default)"
              : "var(--pf-t--global--text--color--status--success--default)";
            const certLabel = daysLeft === null ? "Unknown"
              : daysLeft <= 0 ? `Expired ${Math.abs(daysLeft)}d ago`
              : `Expires in ${daysLeft}d`;
            return (
              <>
                <Title headingLevel="h3" size="md" style={{ marginTop: "1.5rem", marginBottom: "1rem" }}>
                  Certificate
                </Title>
                <DescriptionList isHorizontal isCompact>
                  <DescriptionListGroup>
                    <DescriptionListTerm>Serial</DescriptionListTerm>
                    <DescriptionListDescription>
                      <span style={{ fontFamily: "monospace", fontSize: "0.8rem" }}>
                        {selectedNode.cert_serial.length > 16
                          ? `…${selectedNode.cert_serial.slice(-16)}`
                          : selectedNode.cert_serial}
                      </span>
                    </DescriptionListDescription>
                  </DescriptionListGroup>
                  {notAfter && (
                    <DescriptionListGroup>
                      <DescriptionListTerm>Expires</DescriptionListTerm>
                      <DescriptionListDescription>
                        <span style={{ color: certColor, fontWeight: 600 }}>
                          {notAfter.toLocaleDateString()}
                        </span>
                        {" "}
                        <span style={{ color: certColor, fontSize: "0.8rem" }}>({certLabel})</span>
                      </DescriptionListDescription>
                    </DescriptionListGroup>
                  )}
                </DescriptionList>
              </>
            );
          })()}

          <Title headingLevel="h3" size="md" style={{ marginTop: "1.5rem", marginBottom: "0.5rem" }}>
            Actions
          </Title>

          <Flex direction={{ default: "column" }} spaceItems={{ default: "spaceItemsSm" }}>
            <FlexItem>
              <div aria-live="assertive" aria-atomic="true">
                {refreshError && (
                  <Alert variant="danger" title="Metadata refresh failed" isInline style={{ marginBottom: "0.5rem" }}>
                    {refreshError}
                  </Alert>
                )}
              </div>
              <Button
                variant="secondary"
                isLoading={refreshing}
                isDisabled={refreshing || selectedNode.status !== "online"}
                onClick={handleRefreshMetadata}
              >
                Update metadata
              </Button>
            </FlexItem>
            <FlexItem>
              <Button
                variant="secondary"
                onClick={() => openGroupModal([selectedNode.id])}
              >
                Add to group
              </Button>
            </FlexItem>
            {(selectedNode.node_group_ids?.length ?? 0) > 0 && (
              <FlexItem>
                <Button
                  variant="secondary"
                  onClick={() => openRemoveGroupModal([selectedNode.id])}
                >
                  Remove from group
                </Button>
              </FlexItem>
            )}
            <FlexItem>
              <Button
                variant="danger"
                onClick={() => openDecommModal([selectedNode.id])}
              >
                Decommission
              </Button>
            </FlexItem>
            {selectedNode.cert_serial && (
              <FlexItem>
                <div aria-live="assertive" aria-atomic="true">
                  {revokeError && (
                    <Alert variant="danger" title="Revocation failed" isInline style={{ marginBottom: "0.5rem" }}>
                      {revokeError}
                    </Alert>
                  )}
                </div>
                <div aria-live="polite" aria-atomic="true">
                  {revokeSuccess && (
                    <Alert variant="success" title="Certificate revoked" isInline style={{ marginBottom: "0.5rem" }}>
                      The agent will be denied on its next reconnect attempt.
                    </Alert>
                  )}
                </div>
                <Button
                  variant="secondary"
                  isDanger
                  isLoading={revoking}
                  isDisabled={revoking || revokeSuccess}
                  onClick={handleRevokeCertificate}
                >
                  Revoke Certificate
                </Button>
              </FlexItem>
            )}
          </Flex>
        </DrawerPanelBody>
      )}
    </DrawerPanelContent>
  );

  /* ── Render ── */
  if (loading) {
    return (
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentCenter" }}>
          <FlexItem><Spinner size="xl" aria-label="Loading" /></FlexItem>
        </Flex>
      </PageSection>
    );
  }

  if (error) {
    return (
      <PageSection>
        <div aria-live="assertive" aria-atomic="true">
          <Alert variant="danger" title="Error loading nodes">{error}</Alert>
        </div>
      </PageSection>
    );
  }

  const groupLabel = (v: string) =>
    v === "none" ? "Unassigned" : (nodeGroups.find((g) => g.id === v)?.name ?? "Unknown group");

  const activeFilters: string[] = [];
  if (statusFilter !== "All") activeFilters.push(`Status: ${statusFilter}`);
  if (osFilter !== "All") activeFilters.push(`OS: ${osFilter}`);
  if (desktopFilter !== "All") activeFilters.push(`Desktop: ${desktopFilter}`);
  if (agentVersionFilter !== "All") activeFilters.push(`Agent: ${agentVersionFilter}`);
  if (groupFilter !== "All") activeFilters.push(`Group: ${groupLabel(groupFilter)}`);

  const selectedNodes = nodes.filter((n) => selectedIds.has(n.id));

  return (
    <>
      <PageSection>
        <div aria-live="assertive" aria-atomic="true">
          {actionError && (
            <Alert
              variant="danger"
              title="Action failed"
              isInline
              actionClose={<Button variant="plain" aria-label="Close" onClick={() => setActionError(null)}>×</Button>}
              style={{ marginBottom: "1rem" }}
            >
              {actionError}
            </Alert>
          )}
        </div>

        <Drawer isExpanded={drawerExpanded} isInline>
          <DrawerContent panelContent={drawerPanel}>
            <DrawerContentBody>
              {/* ── Toolbar ── */}
              <BorToolbar
                searchValue={searchValue}
                onSearchChange={setSearchValue}
                searchAriaLabel="Search nodes"
                searchPlaceholder="Search by name, FQDN, IP, group..."
                onSearch={() => setAppliedSearch(searchValue)}
                onSearchClear={() => { setSearchValue(""); setAppliedSearch(""); }}
                onClearAll={() => {
                  setStatusFilter("All");
                  setOsFilter("All");
                  setDesktopFilter("All");
                  setAgentVersionFilter("All");
                  setGroupFilter("All");
                }}
              >
                  <ToolbarFilter
                    labels={statusFilter !== "All" ? [statusFilter] : []}
                    deleteLabel={() => setStatusFilter("All")}
                    categoryName="Status"
                  >
                    <Select
                      isOpen={statusOpen}
                      selected={statusFilter}
                      onSelect={(_ev, val) => { setStatusFilter(val as string); setStatusOpen(false); }}
                      onOpenChange={setStatusOpen}
                      toggle={(ref: React.Ref<MenuToggleElement>) => (
                        <MenuToggle ref={ref} onClick={() => setStatusOpen(!statusOpen)} isExpanded={statusOpen}>
                          Status: {statusFilter}
                        </MenuToggle>
                      )}
                    >
                      <SelectList>
                        <SelectOption value="All">All</SelectOption>
                        {STATUS_OPTIONS.map((s) => (
                          <SelectOption key={s} value={s}>{s}</SelectOption>
                        ))}
                      </SelectList>
                    </Select>
                  </ToolbarFilter>

                  <ToolbarFilter
                    labels={groupFilter !== "All" ? [groupLabel(groupFilter)] : []}
                    deleteLabel={() => setGroupFilter("All")}
                    categoryName="Group"
                  >
                    <SearchableSelect
                      ariaLabel="Filter by group"
                      placeholder="Filter by group"
                      emptyValue="All"
                      selected={groupFilter}
                      onSelect={setGroupFilter}
                      options={[
                        { value: "All", label: "All Groups" },
                        { value: "none", label: "Unassigned" },
                        ...nodeGroups.map((g) => ({ value: g.id, label: g.name })),
                      ]}
                    />
                  </ToolbarFilter>

                  <ToolbarFilter
                    labels={osFilter !== "All" ? [osFilter] : []}
                    deleteLabel={() => setOsFilter("All")}
                    categoryName="OS"
                  >
                    <SearchableSelect
                      ariaLabel="Filter by OS"
                      placeholder="Filter by OS"
                      emptyValue="All"
                      selected={osFilter}
                      onSelect={setOsFilter}
                      options={osOptions.map((o) => ({ value: o, label: o }))}
                    />
                  </ToolbarFilter>

                  <ToolbarFilter
                    labels={desktopFilter !== "All" ? [desktopFilter] : []}
                    deleteLabel={() => setDesktopFilter("All")}
                    categoryName="Desktop"
                  >
                    <SearchableSelect
                      ariaLabel="Filter by desktop"
                      placeholder="Filter by desktop"
                      emptyValue="All"
                      selected={desktopFilter}
                      onSelect={setDesktopFilter}
                      options={desktopOptions.map((d) => ({ value: d, label: d }))}
                    />
                  </ToolbarFilter>

                  <ToolbarFilter
                    labels={agentVersionFilter !== "All" ? [agentVersionFilter] : []}
                    deleteLabel={() => setAgentVersionFilter("All")}
                    categoryName="Agent Version"
                  >
                    <SearchableSelect
                      ariaLabel="Filter by agent version"
                      placeholder="Filter by agent version"
                      emptyValue="All"
                      selected={agentVersionFilter}
                      onSelect={setAgentVersionFilter}
                      options={agentVersionOptions.map((v) => ({ value: v, label: v }))}
                    />
                  </ToolbarFilter>

                  {selectedIds.size > 0 && (
                    <ToolbarItem>
                      <Dropdown
                        isOpen={bulkOpen}
                        onSelect={() => setBulkOpen(false)}
                        onOpenChange={setBulkOpen}
                        toggle={(ref: React.Ref<MenuToggleElement>) => (
                          <MenuToggle
                            ref={ref}
                            onClick={() => setBulkOpen(!bulkOpen)}
                            isExpanded={bulkOpen}
                            variant="primary"
                          >
                            Actions ({selectedIds.size})
                          </MenuToggle>
                        )}
                      >
                        <DropdownList>
                          <DropdownItem key="export" onClick={exportCSV}>
                            Export selected (CSV)
                          </DropdownItem>
                          <DropdownItem
                            key="add-group"
                            onClick={() => {
                              setBulkOpen(false);
                              openGroupModal(Array.from(selectedIds));
                            }}
                          >
                            Add to group
                          </DropdownItem>
                          <DropdownItem
                            key="remove-group"
                            onClick={() => {
                              setBulkOpen(false);
                              openRemoveGroupModal(Array.from(selectedIds));
                            }}
                          >
                            Remove from group
                          </DropdownItem>
                          <DropdownItem
                            key="decommission"
                            isDanger
                            onClick={() => {
                              setBulkOpen(false);
                              openDecommModal(Array.from(selectedIds));
                            }}
                          >
                            Decommission
                          </DropdownItem>
                        </DropdownList>
                      </Dropdown>
                    </ToolbarItem>
                  )}

                  <ToolbarItem align={{ default: "alignEnd" }}>
                    <Button variant="link" onClick={exportCSV}>
                      Export CSV
                    </Button>
                  </ToolbarItem>
              </BorToolbar>

              {total > 0 && (
                <Pagination
                  itemCount={total}
                  page={page}
                  perPage={perPage}
                  onSetPage={(_ev, p) => setPage(p)}
                  onPerPageSelect={(_ev, pp) => {
                    setPerPage(pp);
                    setPage(1);
                  }}
                  isCompact
                  aria-label="Nodes pagination top"
                />
              )}

              {/* ── Table ── */}
              {nodes.length === 0 ? (
                <EmptyState titleText="No nodes found" headingLevel="h2" icon={CubesIcon}>
                  <EmptyStateBody>
                    {appliedSearch || activeFilters.length > 0
                      ? "No nodes match the current filters. Try adjusting your search or filters."
                      : "No nodes registered yet. Nodes will appear here once agents connect."}
                  </EmptyStateBody>
                </EmptyState>
              ) : (
                <Table aria-label="Nodes table" variant="compact">
                  <Thead>
                    <Tr>
                      <Th
                        select={{
                          onSelect: toggleSelectAll,
                          isSelected: isAllSelected,
                        }}
                      />
                      <Th sort={getSortParams("name")}>Node</Th>
                      <Th>Status</Th>
                      <Th>Node Group</Th>
                      <Th>OS</Th>
                      <Th sort={getSortParams("last_seen")}>Last Seen</Th>
                      <Th>Agent</Th>
                      <Th>Notes</Th>
                    </Tr>
                  </Thead>
                  <Tbody>
                    {nodes.map((node, rowIndex) => (
                      <Tr
                        key={node.id}
                        isClickable
                        isRowSelected={selectedNode?.id === node.id}
                        onRowClick={() => {
                          setSelectedNode(node);
                          setDrawerExpanded(true);
                          setRevokeError(null);
                          setRevokeSuccess(false);
                        }}
                      >
                        <Td
                          select={{
                            rowIndex,
                            onSelect: () => toggleSelectNode(node.id),
                            isSelected: selectedIds.has(node.id),
                          }}
                        />
                        <Td dataLabel="Node">{node.name}</Td>
                        <Td dataLabel="Status">
                          <Tooltip content={statusTooltip(node.status, node.status_reason)}>
                            <Label color={statusColor(node.status)}>{node.status}</Label>
                          </Tooltip>
                        </Td>
                        <Td dataLabel="Groups">{node.node_group_names?.join(", ") || "—"}</Td>
                        <Td dataLabel="OS">{osDisplay(node) || "—"}</Td>
                        <Td dataLabel="Last Seen">{timeAgo(node.last_seen)}</Td>
                        <Td dataLabel="Agent">{node.agent_version || "—"}</Td>
                        <Td dataLabel="Notes">
                          {node.notes
                            ? node.notes.length > MAX_NOTES_DISPLAY_LENGTH
                              ? node.notes.substring(0, MAX_NOTES_DISPLAY_LENGTH) + "..."
                              : node.notes
                            : "—"}
                        </Td>
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              )}

              {total > 0 && (
                <Pagination
                  itemCount={total}
                  page={page}
                  perPage={perPage}
                  onSetPage={(_ev, p) => setPage(p)}
                  onPerPageSelect={(_ev, pp) => {
                    setPerPage(pp);
                    setPage(1);
                  }}
                  variant="bottom"
                  aria-label="Nodes pagination bottom"
                />
              )}
            </DrawerContentBody>
          </DrawerContent>
        </Drawer>
      </PageSection>

      {/* ── Add to group modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={groupModalOpen}
        onClose={() => setGroupModalOpen(false)}
      >
        <ModalHeader title={`Add to group (${groupModalTargetIds.length} node${groupModalTargetIds.length !== 1 ? "s" : ""})`} />
        <ModalBody>
        <Form>
          <div aria-live="assertive" aria-atomic="true">
            {groupActionError && (
              <Alert variant="danger" title="Error" isInline>{groupActionError}</Alert>
            )}
          </div>
          <FormGroup label="Node group" isRequired fieldId="group-picker">
            <Select
              isOpen={groupPickerOpen}
              selected={selectedGroupId}
              onSelect={(_ev, val) => {
                setSelectedGroupId(val as string);
                setGroupPickerOpen(false);
              }}
              onOpenChange={setGroupPickerOpen}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle
                  ref={ref}
                  onClick={() => setGroupPickerOpen(!groupPickerOpen)}
                  isExpanded={groupPickerOpen}
                  style={{ width: "100%" }}
                >
                  {nodeGroups.find((g) => g.id === selectedGroupId)?.name || "Select a group"}
                </MenuToggle>
              )}
            >
              <SelectList>
                {nodeGroups.map((g) => (
                  <SelectOption key={g.id} value={g.id}>
                    {g.name}
                  </SelectOption>
                ))}
              </SelectList>
            </Select>
          </FormGroup>
          <ActionGroup>
            <Button
              variant="primary"
              isDisabled={!selectedGroupId || groupActionLoading}
              isLoading={groupActionLoading}
              onClick={handleAddToGroup}
            >
              Assign
            </Button>
            <Button variant="link" onClick={() => setGroupModalOpen(false)}>
              Cancel
            </Button>
          </ActionGroup>
        </Form>
        </ModalBody>
      </Modal>

      {/* ── Remove from group modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={removeGroupModalOpen}
        onClose={() => setRemoveGroupModalOpen(false)}
      >
        <ModalHeader title="Remove from group" />
        <ModalBody>
        <Form>
          {removeGroupOptions.length === 0 ? (
            <p>The selected node(s) are not in any groups.</p>
          ) : (
            <>
              <p>Select the group(s) to remove the selected node(s) from:</p>
              {removeGroupOptions.map((opt) => (
                <Checkbox
                  key={opt.id}
                  id={`remove-group-${opt.id}`}
                  label={opt.name}
                  isChecked={removeGroupSelectedIds.has(opt.id)}
                  onChange={(_ev, checked) => {
                    setRemoveGroupSelectedIds((prev) => {
                      const next = new Set(prev);
                      if (checked) { next.add(opt.id); } else { next.delete(opt.id); }
                      return next;
                    });
                  }}
                />
              ))}
            </>
          )}
          <div aria-live="assertive" aria-atomic="true">
            {removeGroupError && (
              <Alert variant="danger" title="Error" isInline>{removeGroupError}</Alert>
            )}
          </div>
          <ActionGroup>
            <Button
              variant="primary"
              isDisabled={removeGroupSelectedIds.size === 0 || removeGroupLoading || removeGroupOptions.length === 0}
              isLoading={removeGroupLoading}
              onClick={handleRemoveFromGroups}
            >
              Remove
            </Button>
            <Button variant="link" onClick={() => setRemoveGroupModalOpen(false)}>
              Cancel
            </Button>
          </ActionGroup>
        </Form>
        </ModalBody>
      </Modal>

      {/* ── Decommission confirmation modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={decommModalOpen}
        onClose={() => setDecommModalOpen(false)}
      >
        <ModalHeader title="Decommission node" titleIconVariant="warning" />
        <ModalBody>
        <Form>
          <p>
            {decommTargetIds.length === 1 ? (
              <>
                This will permanently delete the node record and revoke its access to the
                management server. The agent will need to re-enroll to reconnect.
              </>
            ) : (
              <>
                This will permanently delete <strong>{decommTargetIds.length} nodes</strong> and
                revoke their access. The agents will need to re-enroll to reconnect.
              </>
            )}
          </p>
          <div aria-live="assertive" aria-atomic="true">
            {decommError && (
              <Alert variant="danger" title="Error" isInline>{decommError}</Alert>
            )}
          </div>
          <FormGroup label={decommConfirmLabel} isRequired fieldId="decommission-confirm">
            <TextInput
              id="decommission-confirm"
              value={decommConfirmText}
              onChange={(_ev, val) => setDecommConfirmText(val)}
              placeholder={decommPrompt}
              validated={decommConfirmText === "" ? "default" : decommValid ? "success" : "error"}
            />
          </FormGroup>
          <ActionGroup>
            <Button
              variant="danger"
              isDisabled={!decommValid || decommLoading}
              isLoading={decommLoading}
              onClick={handleDecommission}
            >
              Decommission
            </Button>
            <Button variant="link" onClick={() => setDecommModalOpen(false)}>
              Cancel
            </Button>
          </ActionGroup>
        </Form>
        </ModalBody>
      </Modal>
    </>
  );
};
