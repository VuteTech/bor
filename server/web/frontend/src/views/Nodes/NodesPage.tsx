// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useMemo, useCallback } from "react";
import {
  PageSection,
  Title,
  Alert,
  Spinner,
  Flex,
  FlexItem,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
  ToolbarFilter,
  SearchInput,
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
  EmptyStateHeader,
  EmptyStateIcon,
  Modal,
  ModalVariant,
  TextInput,
  Form,
  FormGroup,
  ActionGroup,
  Checkbox,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ThProps } from "@patternfly/react-table";
import SearchIcon from "@patternfly/react-icons/dist/esm/icons/search-icon";
import CubesIcon from "@patternfly/react-icons/dist/esm/icons/cubes-icon";

import {
  fetchNodes,
  refreshNodeMetadata,
  addNodeToGroup,
  removeNodeFromGroup,
  deleteNode,
  Node,
  NodeStatus,
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
  const [nodes, setNodes] = useState<Node[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Search
  const [searchValue, setSearchValue] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");

  // Filters
  const [statusFilter, setStatusFilter] = useState<string>("All");
  const [statusOpen, setStatusOpen] = useState(false);
  const [osFilter, setOsFilter] = useState<string>("All");
  const [osOpen, setOsOpen] = useState(false);
  const [desktopFilter, setDesktopFilter] = useState<string>("All");
  const [desktopOpen, setDesktopOpen] = useState(false);
  const [agentVersionFilter, setAgentVersionFilter] = useState<string>("All");
  const [agentVersionOpen, setAgentVersionOpen] = useState(false);

  // Sort
  const [sortField, setSortField] = useState<SortField>("last_seen");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("desc");

  // Drawer
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [drawerExpanded, setDrawerExpanded] = useState(false);

  // Selection (for bulk actions)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());

  // Bulk actions dropdown
  const [bulkOpen, setBulkOpen] = useState(false);

  // Metadata refresh (in drawer)
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState<string | null>(null);

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

  /* ── Load data ── */
  const loadNodes = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await fetchNodes(
        appliedSearch ? { search: appliedSearch } : undefined
      );
      setNodes(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load nodes");
    } finally {
      setLoading(false);
    }
  }, [appliedSearch]);

  useEffect(() => {
    loadNodes();
  }, [loadNodes]);

  /* ── Derive filter options from data ── */
  const osOptions = useMemo(() => {
    const set = new Set<string>();
    nodes.forEach((n) => { const os = osDisplay(n); if (os) set.add(os); });
    return ["All", ...Array.from(set).sort()];
  }, [nodes]);

  const desktopOptions = useMemo(() => {
    const set = new Set<string>();
    nodes.forEach((n) => { if (n.desktop_env) set.add(n.desktop_env); });
    return ["All", ...Array.from(set).sort()];
  }, [nodes]);

  const agentVersionOptions = useMemo(() => {
    const set = new Set<string>();
    nodes.forEach((n) => { if (n.agent_version) set.add(n.agent_version); });
    return ["All", ...Array.from(set).sort()];
  }, [nodes]);

  /* ── Filtering ── */
  const filteredNodes = useMemo(() => {
    return nodes.filter((n) => {
      if (statusFilter !== "All" && n.status !== statusFilter) return false;
      if (osFilter !== "All" && osDisplay(n) !== osFilter) return false;
      if (desktopFilter !== "All" && n.desktop_env !== desktopFilter) return false;
      if (agentVersionFilter !== "All" && n.agent_version !== agentVersionFilter) return false;
      return true;
    });
  }, [nodes, statusFilter, osFilter, desktopFilter, agentVersionFilter]);

  /* ── Sorting ── */
  const sortedNodes = useMemo(() => {
    const sorted = [...filteredNodes];
    sorted.sort((a, b) => {
      let cmp = 0;
      switch (sortField) {
        case "name":
          cmp = a.name.localeCompare(b.name);
          break;
        case "last_seen":
        default: {
          const aTime = a.last_seen ? new Date(a.last_seen).getTime() : 0;
          const bTime = b.last_seen ? new Date(b.last_seen).getTime() : 0;
          cmp = aTime - bTime;
          break;
        }
      }
      return sortDirection === "asc" ? cmp : -cmp;
    });
    return sorted;
  }, [filteredNodes, sortField, sortDirection]);

  /* ── Sort handler ── */
  const getSortParams = (field: SortField): ThProps["sort"] => ({
    sortBy: {
      index: field === "name" ? 0 : 4,
      direction: sortField === field ? sortDirection : "asc",
    },
    onSort: (_ev, _idx, dir) => {
      setSortField(field);
      setSortDirection(dir);
    },
    columnIndex: field === "name" ? 0 : 4,
  });

  /* ── Selection ── */
  const isAllSelected = sortedNodes.length > 0 && selectedIds.size === sortedNodes.length;
  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(sortedNodes.map((n) => n.id)));
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
  const exportCSV = () => {
    const selected = sortedNodes.filter((n) => selectedIds.has(n.id));
    const rows = selected.length > 0 ? selected : sortedNodes;
    const header = "Name,Status,Node Groups,Last Seen,Agent Version,OS,Notes";
    const csvRows = rows.map(
      (n) =>
        `"${n.name}","${n.status}","${n.node_group_names?.join("; ") || ""}","${n.last_seen || ""}","${n.agent_version || ""}","${osDisplay(n)}","${n.notes || ""}"`
    );
    const blob = new Blob([header + "\n" + csvRows.join("\n")], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "nodes-export.csv";
    a.click();
    URL.revokeObjectURL(url);
  };

  /* ── Metadata refresh ── */
  const handleRefreshMetadata = async () => {
    if (!selectedNode) return;
    setRefreshing(true);
    setRefreshError(null);
    try {
      await refreshNodeMetadata(selectedNode.id);
    } catch (err) {
      setRefreshError(err instanceof Error ? err.message : "Failed to request metadata refresh");
    } finally {
      setRefreshing(false);
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
            <DescriptionListGroup>
              <DescriptionListTerm>Notes</DescriptionListTerm>
              <DescriptionListDescription>{selectedNode.notes || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
          </DescriptionList>

          <Title headingLevel="h3" size="md" style={{ marginTop: "1.5rem", marginBottom: "0.5rem" }}>
            Actions
          </Title>

          <Flex direction={{ default: "column" }} spaceItems={{ default: "spaceItemsSm" }}>
            <FlexItem>
              {refreshError && (
                <Alert variant="danger" title="Metadata refresh failed" isInline style={{ marginBottom: "0.5rem" }}>
                  {refreshError}
                </Alert>
              )}
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
          <FlexItem><Spinner size="xl" /></FlexItem>
        </Flex>
      </PageSection>
    );
  }

  if (error) {
    return (
      <PageSection>
        <Alert variant="danger" title="Error loading nodes">{error}</Alert>
      </PageSection>
    );
  }

  const activeFilters: string[] = [];
  if (statusFilter !== "All") activeFilters.push(`Status: ${statusFilter}`);
  if (osFilter !== "All") activeFilters.push(`OS: ${osFilter}`);
  if (desktopFilter !== "All") activeFilters.push(`Desktop: ${desktopFilter}`);
  if (agentVersionFilter !== "All") activeFilters.push(`Agent: ${agentVersionFilter}`);

  const selectedNodes = sortedNodes.filter((n) => selectedIds.has(n.id));

  return (
    <>
      <PageSection variant="light">
        <Title headingLevel="h1" size="2xl">Nodes</Title>
        <p style={{ color: "#6a6e73", marginTop: "0.25rem" }}>
          Manage and monitor connected desktop agents.
        </p>
      </PageSection>
      <PageSection>
        {actionError && (
          <Alert
            variant="danger"
            title="Action failed"
            isInline
            actionClose={<Button variant="plain" onClick={() => setActionError(null)}>×</Button>}
            style={{ marginBottom: "1rem" }}
          >
            {actionError}
          </Alert>
        )}

        <Drawer isExpanded={drawerExpanded} isInline>
          <DrawerContent panelContent={drawerPanel}>
            <DrawerContentBody>
              {/* ── Toolbar ── */}
              <Toolbar clearAllFilters={() => {
                setStatusFilter("All");
                setOsFilter("All");
                setDesktopFilter("All");
                setAgentVersionFilter("All");
              }}>
                <ToolbarContent>
                  <ToolbarItem>
                    <SearchInput
                      placeholder="Search by name, FQDN, IP, group..."
                      value={searchValue}
                      onChange={(_ev, val) => setSearchValue(val)}
                      onSearch={() => setAppliedSearch(searchValue)}
                      onClear={() => { setSearchValue(""); setAppliedSearch(""); }}
                    />
                  </ToolbarItem>

                  <ToolbarFilter
                    chips={statusFilter !== "All" ? [statusFilter] : []}
                    deleteChip={() => setStatusFilter("All")}
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
                    chips={osFilter !== "All" ? [osFilter] : []}
                    deleteChip={() => setOsFilter("All")}
                    categoryName="OS"
                  >
                    <Select
                      isOpen={osOpen}
                      selected={osFilter}
                      onSelect={(_ev, val) => { setOsFilter(val as string); setOsOpen(false); }}
                      onOpenChange={setOsOpen}
                      toggle={(ref: React.Ref<MenuToggleElement>) => (
                        <MenuToggle ref={ref} onClick={() => setOsOpen(!osOpen)} isExpanded={osOpen}>
                          OS: {osFilter}
                        </MenuToggle>
                      )}
                    >
                      <SelectList>
                        {osOptions.map((o) => (
                          <SelectOption key={o} value={o}>{o}</SelectOption>
                        ))}
                      </SelectList>
                    </Select>
                  </ToolbarFilter>

                  <ToolbarFilter
                    chips={desktopFilter !== "All" ? [desktopFilter] : []}
                    deleteChip={() => setDesktopFilter("All")}
                    categoryName="Desktop"
                  >
                    <Select
                      isOpen={desktopOpen}
                      selected={desktopFilter}
                      onSelect={(_ev, val) => { setDesktopFilter(val as string); setDesktopOpen(false); }}
                      onOpenChange={setDesktopOpen}
                      toggle={(ref: React.Ref<MenuToggleElement>) => (
                        <MenuToggle ref={ref} onClick={() => setDesktopOpen(!desktopOpen)} isExpanded={desktopOpen}>
                          Desktop: {desktopFilter}
                        </MenuToggle>
                      )}
                    >
                      <SelectList>
                        {desktopOptions.map((d) => (
                          <SelectOption key={d} value={d}>{d}</SelectOption>
                        ))}
                      </SelectList>
                    </Select>
                  </ToolbarFilter>

                  <ToolbarFilter
                    chips={agentVersionFilter !== "All" ? [agentVersionFilter] : []}
                    deleteChip={() => setAgentVersionFilter("All")}
                    categoryName="Agent Version"
                  >
                    <Select
                      isOpen={agentVersionOpen}
                      selected={agentVersionFilter}
                      onSelect={(_ev, val) => { setAgentVersionFilter(val as string); setAgentVersionOpen(false); }}
                      onOpenChange={setAgentVersionOpen}
                      toggle={(ref: React.Ref<MenuToggleElement>) => (
                        <MenuToggle ref={ref} onClick={() => setAgentVersionOpen(!agentVersionOpen)} isExpanded={agentVersionOpen}>
                          Agent: {agentVersionFilter}
                        </MenuToggle>
                      )}
                    >
                      <SelectList>
                        {agentVersionOptions.map((v) => (
                          <SelectOption key={v} value={v}>{v}</SelectOption>
                        ))}
                      </SelectList>
                    </Select>
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

                  <ToolbarItem align={{ default: "alignRight" }}>
                    <Button variant="link" onClick={exportCSV}>
                      Export CSV
                    </Button>
                  </ToolbarItem>
                </ToolbarContent>
              </Toolbar>

              {/* ── Table ── */}
              {sortedNodes.length === 0 ? (
                <EmptyState>
                  <EmptyStateHeader
                    titleText="No nodes found"
                    headingLevel="h2"
                    icon={<EmptyStateIcon icon={CubesIcon} />}
                  />
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
                    {sortedNodes.map((node, rowIndex) => (
                      <Tr
                        key={node.id}
                        isClickable
                        isRowSelected={selectedNode?.id === node.id}
                        onRowClick={() => {
                          setSelectedNode(node);
                          setDrawerExpanded(true);
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
            </DrawerContentBody>
          </DrawerContent>
        </Drawer>
      </PageSection>

      {/* ── Add to group modal ── */}
      <Modal
        variant={ModalVariant.small}
        title={`Add to group (${groupModalTargetIds.length} node${groupModalTargetIds.length !== 1 ? "s" : ""})`}
        isOpen={groupModalOpen}
        onClose={() => setGroupModalOpen(false)}
      >
        <Form>
          {groupActionError && (
            <Alert variant="danger" title="Error" isInline>{groupActionError}</Alert>
          )}
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
      </Modal>

      {/* ── Remove from group modal ── */}
      <Modal
        variant={ModalVariant.small}
        title="Remove from group"
        isOpen={removeGroupModalOpen}
        onClose={() => setRemoveGroupModalOpen(false)}
      >
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
          {removeGroupError && (
            <Alert variant="danger" title="Error" isInline>{removeGroupError}</Alert>
          )}
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
      </Modal>

      {/* ── Decommission confirmation modal ── */}
      <Modal
        variant={ModalVariant.small}
        title="Decommission node"
        titleIconVariant="warning"
        isOpen={decommModalOpen}
        onClose={() => setDecommModalOpen(false)}
      >
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
          {decommError && (
            <Alert variant="danger" title="Error" isInline>{decommError}</Alert>
          )}
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
      </Modal>
    </>
  );
};
