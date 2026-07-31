// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback, useRef } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import {
  PageSection,
  Title,
  Button,
  Alert,
  AlertActionCloseButton,
  Spinner,
  Flex,
  FlexItem,
  Label,
  ToolbarItem,
  ToolbarFilter,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectOption,
  SelectList,
  Dropdown,
  DropdownItem,
  DropdownList,
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Form,
  FormGroup,
  TextInput,
  ActionGroup,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction, ThProps } from "@patternfly/react-table";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";

import { fetchAllPolicies, deletePolicy, setPolicyState, Policy } from "../../apiClient/policiesApi";
import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { BorToolbar } from "../../components/BorToolbar";

/* ── Filter options ── */

const TYPE_OPTIONS = ["Kconfig", "Dconf", "Firefox", "Thunderbird", "Polkit", "Chrome", "Edge", "Package", "Firewalld"];
const STATUS_OPTIONS = ["draft", "released", "archived"];

const statusLabelColor = (status: string): "green" | "red" | "blue" | "orange" | "grey" => {
  switch (status) {
    case "released": return "green";
    case "archived": return "red";
    case "draft":    return "blue";
    default:         return "grey";
  }
};

/* ── Policy lifecycle actions ── */

type LifecycleAction = "release" | "unpublish" | "archive" | "restore";

const LIFECYCLE_TARGET: Record<LifecycleAction, string> = {
  release: "released",
  unpublish: "draft",
  archive: "archived",
  restore: "draft",
};

const LIFECYCLE_DONE: Record<LifecycleAction, string> = {
  release: "released",
  unpublish: "unpublished",
  archive: "archived",
  restore: "restored to draft",
};

// Which states a lifecycle action can start from, and whether enabled
// bindings block it (mirrors the server-side transition rules).
const LIFECYCLE_FROM: Record<LifecycleAction, Policy["state"]> = {
  release: "draft",
  unpublish: "released",
  archive: "released",
  restore: "archived",
};

const LIFECYCLE_BLOCKED_BY_BINDINGS: Record<LifecycleAction, boolean> = {
  release: false,
  unpublish: true,
  archive: true,
  restore: true,
};

const isEligible = (p: Policy, action: LifecycleAction): boolean =>
  p.state === LIFECYCLE_FROM[action] &&
  (!LIFECYCLE_BLOCKED_BY_BINDINGS[action] || (p.enabled_bindings_count ?? 0) === 0);

interface ActionFeedback {
  variant: "success" | "danger" | "warning";
  title: string;
  details?: string[];
}

export const PoliciesPage: React.FC = () => {
  const navigate = useNavigate();
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Filters
  const [searchText, setSearchText] = useState("");
  const [sortIndex, setSortIndex] = useState<number | undefined>(undefined);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");
  const [typeFilter, setTypeFilter] = useState<string[]>([]);
  // Status can be seeded from the URL (?state=) for dashboard drill-down.
  const [searchParams] = useSearchParams();
  const [statusFilter, setStatusFilter] = useState<string[]>(() => {
    const s = searchParams.get("state");
    return s && STATUS_OPTIONS.includes(s) ? [s] : [];
  });
  const [bindingsFilter, setBindingsFilter] = useState<string | null>(null);
  const [recentlyModified, setRecentlyModified] = useState(false);

  // Filter dropdown states
  const [isTypeOpen, setIsTypeOpen] = useState(false);
  const [isStatusOpen, setIsStatusOpen] = useState(false);
  const [isBindingsOpen, setIsBindingsOpen] = useState(false);

  // Selection
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [bulkOpen, setBulkOpen] = useState(false);

  // Delete modal (type-to-confirm)
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleteTargetIds, setDeleteTargetIds] = useState<string[]>([]);
  const [deleteConfirmText, setDeleteConfirmText] = useState("");
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Lifecycle actions (release/unpublish/archive/restore)
  const [confirmLifecycle, setConfirmLifecycle] = useState<{
    action: LifecycleAction;
    policies: Policy[];
    skipped: number;
  } | null>(null);
  const [lifecycleLoading, setLifecycleLoading] = useState(false);
  const [feedback, setFeedback] = useState<ActionFeedback | null>(null);
  // Element that triggered the confirm modal, to restore focus on close (WCAG)
  const confirmTriggerRef = useRef<HTMLElement | null>(null);

  // showSpinner only on initial load; refreshes after actions keep the table
  // (and the aria-live feedback region) mounted.
  const loadPolicies = useCallback(async (showSpinner = false) => {
    try {
      if (showSpinner) setLoading(true);
      setError(null);
      const data = await fetchAllPolicies();
      setPolicies(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load policies");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadPolicies(true);
  }, [loadPolicies]);

  /* ── Selection ── */
  const filteredPolicies = policies.filter((p) => {
    if (searchText && !p.name.toLowerCase().includes(searchText.toLowerCase())) return false;
    if (typeFilter.length > 0 && !typeFilter.includes(p.type)) return false;
    if (statusFilter.length > 0 && !statusFilter.includes(p.state)) return false;
    if (bindingsFilter === "has" && (p.bindings_count ?? 0) === 0) return false;
    if (bindingsFilter === "none" && (p.bindings_count ?? 0) > 0) return false;
    if (recentlyModified) {
      const sevenDaysAgo = new Date();
      sevenDaysAgo.setDate(sevenDaysAgo.getDate() - 7);
      if (new Date(p.updated_at) < sevenDaysAgo) return false;
    }
    return true;
  });

  /* ── Client-side sort ──
     Column indices match header order (0 = select cell): Name=1, Type=2,
     Version=3, Status=4, Bindings=5, Last Updated=6. */
  const sortedPolicies = (() => {
    if (sortIndex === undefined) return filteredPolicies;
    const cmp = (a: Policy, b: Policy): number => {
      switch (sortIndex) {
        case 1: return a.name.localeCompare(b.name);
        case 2: return a.type.localeCompare(b.type);
        case 3: return a.version - b.version;
        case 4: return a.state.localeCompare(b.state);
        case 5: return (a.bindings_count ?? 0) - (b.bindings_count ?? 0);
        case 6: return new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime();
        default: return 0;
      }
    };
    return [...filteredPolicies].sort((a, b) => (sortDir === "asc" ? cmp(a, b) : -cmp(a, b)));
  })();

  const getSort = (columnIndex: number): ThProps["sort"] => ({
    sortBy: { index: sortIndex, direction: sortDir },
    onSort: (_e, index, direction) => {
      setSortIndex(index);
      setSortDir(direction);
    },
    columnIndex,
  });

  const isAllSelected =
    sortedPolicies.length > 0 && selectedIds.size === sortedPolicies.length;

  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(sortedPolicies.map((p) => p.id)));
    }
  };

  const toggleSelectPolicy = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) { next.delete(id); } else { next.add(id); }
      return next;
    });
  };

  /* ── Create / Edit ──
     Editing is now a full-page route (/policies/new, /policies/:id/edit)
     rather than an overlay modal. */
  const handleCreate = () => {
    navigate("/policies/new");
  };

  const handleEdit = (policy: Policy) => {
    navigate(`/policies/${policy.id}/edit`);
  };

  /* ── Lifecycle actions (release / unpublish / archive / restore) ── */

  // Release and archive are outward-facing → confirm first. Unpublish and
  // restore run immediately; the server rejects them when bindings exist.
  const requestLifecycle = (action: LifecycleAction, targets: Policy[], skipped = 0) => {
    if (targets.length === 0) return;
    if (action === "release" || action === "archive") {
      confirmTriggerRef.current = document.activeElement as HTMLElement | null;
      setConfirmLifecycle({ action, policies: targets, skipped });
    } else {
      runLifecycle(action, targets, skipped);
    }
  };

  const closeConfirmLifecycle = () => {
    setConfirmLifecycle(null);
    confirmTriggerRef.current?.focus();
    confirmTriggerRef.current = null;
  };

  const runLifecycle = async (action: LifecycleAction, targets: Policy[], skipped = 0) => {
    setLifecycleLoading(true);
    setFeedback(null);
    const results = await Promise.allSettled(
      targets.map((p) => setPolicyState(p.id, { state: LIFECYCLE_TARGET[action] })),
    );
    setLifecycleLoading(false);
    setConfirmLifecycle(null);
    confirmTriggerRef.current = null;

    const failures = results
      .map((r, i) => ({ r, p: targets[i] }))
      .filter((x): x is { r: PromiseRejectedResult; p: Policy } => x.r.status === "rejected");
    const okCount = targets.length - failures.length;

    const noun = (n: number) => `${n} ${n === 1 ? "policy" : "policies"}`;
    const skippedSuffix = skipped > 0 ? ` (${skipped} not eligible, skipped)` : "";
    if (failures.length === 0) {
      const what = targets.length === 1 ? `"${targets[0].name}"` : noun(okCount);
      setFeedback({
        variant: "success",
        title: `${what.charAt(0).toUpperCase() + what.slice(1)} ${LIFECYCLE_DONE[action]}${skippedSuffix}`,
      });
    } else {
      setFeedback({
        variant: okCount > 0 ? "warning" : "danger",
        title:
          okCount > 0
            ? `${noun(okCount)} ${LIFECYCLE_DONE[action]}, ${failures.length} failed${skippedSuffix}`
            : `Failed to ${action} ${noun(failures.length)}${skippedSuffix}`,
        details: failures.map(
          ({ r, p }) => `${p.name}: ${r.reason instanceof Error ? r.reason.message : String(r.reason)}`,
        ),
      });
    }

    setSelectedIds(new Set());
    loadPolicies();
  };

  /* ── Delete (type-to-confirm) ── */
  const openDeleteModal = (ids: string[]) => {
    setDeleteTargetIds(ids);
    setDeleteConfirmText("");
    setDeleteError(null);
    setDeleteLoading(false);
    setDeleteModalOpen(true);
  };

  // Single: type policy name. Multiple: type "Yes".
  const deletePrompt = deleteTargetIds.length === 1
    ? policies.find((p) => p.id === deleteTargetIds[0])?.name ?? ""
    : "Yes";
  const deleteConfirmLabel = deleteTargetIds.length === 1
    ? `Type the policy name "${deletePrompt}" to confirm`
    : `Type "Yes" to confirm deleting ${deleteTargetIds.length} policies`;
  const deleteValid = deleteConfirmText === deletePrompt;

  const handleBulkDelete = async () => {
    if (!deleteValid) return;
    setDeleteLoading(true);
    setDeleteError(null);
    try {
      await Promise.all(deleteTargetIds.map((id) => deletePolicy(id)));
      setSelectedIds(new Set());
      setDeleteModalOpen(false);
      loadPolicies();
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Failed to delete");
    } finally {
      setDeleteLoading(false);
    }
  };

  /* ── Row actions ── */
  const rowActions = (policy: Policy): IAction[] => {
    const enabled = policy.enabled_bindings_count ?? 0;
    const bindingsBlock = (verb: string) =>
      enabled > 0
        ? {
            isAriaDisabled: true,
            tooltipProps: {
              content: `Cannot ${verb}: ${enabled} enabled binding${enabled === 1 ? "" : "s"} exist. Disable the bindings first.`,
            },
          }
        : {};

    const items: IAction[] = [];
    if (policy.state === "draft") {
      items.push({ title: "Release", onClick: () => requestLifecycle("release", [policy]) });
    }
    if (policy.state === "released") {
      items.push({
        title: "Unpublish",
        onClick: () => requestLifecycle("unpublish", [policy]),
        ...bindingsBlock("unpublish"),
      });
      items.push({
        title: "Archive",
        onClick: () => requestLifecycle("archive", [policy]),
        ...bindingsBlock("archive"),
      });
    }
    if (policy.state === "archived") {
      items.push({ title: "Restore to draft", onClick: () => requestLifecycle("restore", [policy]) });
    }
    items.push({
      title: policy.state === "draft" ? "Edit" : "View details",
      onClick: () => handleEdit(policy),
    });
    items.push({ isSeparator: true });
    items.push({ title: "Delete", isDanger: true, onClick: () => openDeleteModal([policy.id]) });
    return items;
  };

  /* ── Filter helpers ── */
  const onTypeSelect = (_ev: React.MouseEvent | undefined, value: string | number | undefined) => {
    const val = String(value);
    setTypeFilter((prev) => prev.includes(val) ? prev.filter((f) => f !== val) : [...prev, val]);
  };

  const onStatusSelect = (_ev: React.MouseEvent | undefined, value: string | number | undefined) => {
    const val = String(value);
    setStatusFilter((prev) => prev.includes(val) ? prev.filter((f) => f !== val) : [...prev, val]);
  };

  const onBindingsSelect = (_ev: React.MouseEvent | undefined, value: string | number | undefined) => {
    const val = String(value);
    setBindingsFilter((prev) => (prev === val ? null : val));
    setIsBindingsOpen(false);
  };

  const selectedPolicies = filteredPolicies.filter((p) => selectedIds.has(p.id));

  if (loading) {
    return (
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentCenter" }}>
          <FlexItem><Spinner size="xl" aria-label="Loading" /></FlexItem>
        </Flex>
      </PageSection>
    );
  }

  return (
    <>
      <PageSection>
        <Flex
          justifyContent={{ default: "justifyContentSpaceBetween" }}
          alignItems={{ default: "alignItemsCenter" }}
        >
          <FlexItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={handleCreate}>
              Create a policy
            </Button>
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <div aria-live="assertive" aria-atomic="true">
          {error && (
            <Alert variant="danger" title="Error loading policies" style={{ marginBottom: "1rem" }}>
              {error}
            </Alert>
          )}
        </div>

        {/* Lifecycle action feedback */}
        <LiveAlert
          message={feedback?.title ?? null}
          variant={feedback?.variant ?? "success"}
          isInline
          actionClose={<AlertActionCloseButton onClose={() => setFeedback(null)} />}
          style={{ marginBottom: "1rem" }}
        >
          {feedback?.details && (
            <ul>
              {feedback.details.map((d) => (
                <li key={d}>{d}</li>
              ))}
            </ul>
          )}
        </LiveAlert>

        {/* Toolbar with filters + bulk Actions */}
        <BorToolbar
          searchValue={searchText}
          onSearchChange={setSearchText}
          searchAriaLabel="Search policies by name"
          searchPlaceholder="Search by name..."
          onClearAll={() => {
            setTypeFilter([]);
            setStatusFilter([]);
            setBindingsFilter(null);
            setRecentlyModified(false);
            setSearchText("");
          }}
        >
            <ToolbarFilter
              chips={typeFilter}
              deleteChip={(_cat, chip) =>
                setTypeFilter((prev) => prev.filter((f) => f !== chip))
              }
              deleteChipGroup={() => setTypeFilter([])}
              categoryName="Type"
            >
              <Select
                aria-label="Type filter"
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={() => setIsTypeOpen(!isTypeOpen)}
                    isExpanded={isTypeOpen}
                  >
                    Type{typeFilter.length > 0 ? ` (${typeFilter.length})` : ""}
                  </MenuToggle>
                )}
                onSelect={onTypeSelect}
                selected={typeFilter}
                isOpen={isTypeOpen}
                onOpenChange={(open) => setIsTypeOpen(open)}
              >
                <SelectList>
                  {TYPE_OPTIONS.map((t) => (
                    <SelectOption key={t} value={t} hasCheckbox isSelected={typeFilter.includes(t)}>
                      {t}
                    </SelectOption>
                  ))}
                </SelectList>
              </Select>
            </ToolbarFilter>

            <ToolbarFilter
              chips={statusFilter}
              deleteChip={(_cat, chip) =>
                setStatusFilter((prev) => prev.filter((f) => f !== chip))
              }
              deleteChipGroup={() => setStatusFilter([])}
              categoryName="Status"
            >
              <Select
                aria-label="Status filter"
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={() => setIsStatusOpen(!isStatusOpen)}
                    isExpanded={isStatusOpen}
                  >
                    Status{statusFilter.length > 0 ? ` (${statusFilter.length})` : ""}
                  </MenuToggle>
                )}
                onSelect={onStatusSelect}
                selected={statusFilter}
                isOpen={isStatusOpen}
                onOpenChange={(open) => setIsStatusOpen(open)}
              >
                <SelectList>
                  {STATUS_OPTIONS.map((s) => (
                    <SelectOption key={s} value={s} hasCheckbox isSelected={statusFilter.includes(s)}>
                      {s.charAt(0).toUpperCase() + s.slice(1)}
                    </SelectOption>
                  ))}
                </SelectList>
              </Select>
            </ToolbarFilter>

            <ToolbarFilter
              chips={bindingsFilter ? [bindingsFilter === "has" ? "Has bindings" : "No bindings"] : []}
              deleteChip={() => setBindingsFilter(null)}
              categoryName="Bindings"
            >
              <Select
                aria-label="Bindings filter"
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    onClick={() => setIsBindingsOpen(!isBindingsOpen)}
                    isExpanded={isBindingsOpen}
                  >
                    Bindings
                  </MenuToggle>
                )}
                onSelect={onBindingsSelect}
                selected={bindingsFilter || undefined}
                isOpen={isBindingsOpen}
                onOpenChange={(open) => setIsBindingsOpen(open)}
              >
                <SelectList>
                  <SelectOption value="has" isSelected={bindingsFilter === "has"}>
                    Has bindings
                  </SelectOption>
                  <SelectOption value="none" isSelected={bindingsFilter === "none"}>
                    No bindings
                  </SelectOption>
                </SelectList>
              </Select>
            </ToolbarFilter>

            <ToolbarItem>
              <Button
                variant={recentlyModified ? "primary" : "secondary"}
                size="sm"
                onClick={() => setRecentlyModified(!recentlyModified)}
              >
                Modified recently
              </Button>
            </ToolbarItem>

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
                    {(["release", "unpublish", "archive"] as const).map((action) => {
                      const eligible = selectedPolicies.filter((p) => isEligible(p, action));
                      const label = action.charAt(0).toUpperCase() + action.slice(1);
                      return (
                        <DropdownItem
                          key={action}
                          isDisabled={eligible.length === 0}
                          description={
                            eligible.length < selectedPolicies.length
                              ? `${eligible.length} of ${selectedPolicies.length} selected eligible`
                              : undefined
                          }
                          onClick={() =>
                            requestLifecycle(action, eligible, selectedPolicies.length - eligible.length)
                          }
                        >
                          {label}
                        </DropdownItem>
                      );
                    })}
                    <DropdownItem
                      key="edit"
                      isDisabled={selectedIds.size !== 1}
                      onClick={() => {
                        const p = selectedPolicies[0];
                        if (p) handleEdit(p);
                      }}
                    >
                      Edit
                    </DropdownItem>
                    <DropdownItem
                      key="delete"
                      isDanger
                      onClick={() => openDeleteModal(Array.from(selectedIds))}
                    >
                      Delete
                    </DropdownItem>
                  </DropdownList>
                </Dropdown>
              </ToolbarItem>
            )}
        </BorToolbar>

        {/* Policies table */}
        {sortedPolicies.length === 0 ? (
          <div style={{ marginTop: "1rem" }}>
            <BorEmptyState
              isEmptyData={policies.length === 0}
              itemsLabel="policies"
              emptyTitle="No policies configured"
              emptyBody='Click "Create a policy" to get started.'
              action={
                <Button variant="primary" icon={<PlusCircleIcon />} onClick={handleCreate}>
                  Create a policy
                </Button>
              }
              onClearFilters={() => {
                setTypeFilter([]);
                setStatusFilter([]);
                setBindingsFilter(null);
                setRecentlyModified(false);
                setSearchText("");
              }}
            />
          </div>
        ) : (
          <Table aria-label="Policies table" variant="compact" style={{ marginTop: "1rem" }}>
            <Thead>
              <Tr>
                <Th
                  select={{
                    onSelect: toggleSelectAll,
                    isSelected: isAllSelected,
                  }}
                />
                <Th sort={getSort(1)}>Name</Th>
                <Th sort={getSort(2)}>Type</Th>
                <Th sort={getSort(3)}>Version</Th>
                <Th sort={getSort(4)}>Status</Th>
                <Th sort={getSort(5)}>Bindings</Th>
                <Th sort={getSort(6)}>Last Updated</Th>
                <Th screenReaderText="Actions" />
              </Tr>
            </Thead>
            <Tbody>
              {sortedPolicies.map((policy, rowIndex) => (
                <Tr key={policy.id}>
                  <Td
                    select={{
                      rowIndex,
                      onSelect: () => toggleSelectPolicy(policy.id),
                      isSelected: selectedIds.has(policy.id),
                    }}
                  />
                  <Td dataLabel="Name">
                    <strong>{policy.name}</strong>
                    {policy.description && (
                      <div style={{ fontSize: "0.8rem", color: "var(--pf-t--global--text--color--subtle)" }}>
                        {policy.description}
                      </div>
                    )}
                  </Td>
                  <Td dataLabel="Type">
                    <Label color="blue" isCompact>{policy.type}</Label>
                  </Td>
                  <Td dataLabel="Version">v{policy.version}</Td>
                  <Td dataLabel="Status">
                    <Label color={statusLabelColor(policy.state)} isCompact>
                      {policy.state.charAt(0).toUpperCase() + policy.state.slice(1)}
                    </Label>
                    {policy.deprecated_at && (
                      <Label color="orange" isCompact style={{ marginLeft: "0.5rem" }}>
                        Deprecated
                      </Label>
                    )}
                  </Td>
                  <Td dataLabel="Bindings">
                    {policy.bindings_count ?? 0}
                    {(policy.enabled_bindings_count ?? 0) > 0 && (
                      <span style={{ fontSize: "0.8rem", color: "var(--pf-t--global--text--color--subtle)" }}>
                        {" "}({policy.enabled_bindings_count} enabled)
                      </span>
                    )}
                  </Td>
                  <Td dataLabel="Last Updated">
                    {new Date(policy.updated_at).toLocaleString()}
                  </Td>
                  <Td dataLabel="Actions" isActionCell>
                    <ActionsColumn
                      items={rowActions(policy)}
                      actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                        <MenuToggle
                          ref={toggleRef}
                          aria-label={`Actions for policy ${policy.name}`}
                          variant="plain"
                          onClick={onToggle}
                          isExpanded={isOpen}
                          isDisabled={isDisabled}
                          icon={<EllipsisVIcon />}
                        />
                      )}
                    />
                  </Td>
                </Tr>
              ))}
            </Tbody>
          </Table>
        )}
      </PageSection>

      {/* ── Delete Confirmation Modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={deleteModalOpen}
        onClose={() => setDeleteModalOpen(false)}
      >
        <ModalHeader
          title={`Delete Polic${deleteTargetIds.length !== 1 ? "ies" : "y"}`}
          titleIconVariant="warning"
        />
        <ModalBody>
        <Form>
          <p>
            {deleteTargetIds.length === 1 ? (
              <>This will permanently delete the policy and all its bindings.</>
            ) : (
              <>
                This will permanently delete{" "}
                <strong>{deleteTargetIds.length} policies</strong> and all their bindings.
              </>
            )}
          </p>
          <div aria-live="assertive" aria-atomic="true">
            {deleteError && (
              <Alert variant="danger" title="Error" isInline>{deleteError}</Alert>
            )}
          </div>
          <FormGroup label={deleteConfirmLabel} isRequired fieldId="policy-delete-confirm">
            <TextInput
              id="policy-delete-confirm"
              value={deleteConfirmText}
              onChange={(_ev, val) => setDeleteConfirmText(val)}
              placeholder={deletePrompt}
              validated={deleteConfirmText === "" ? "default" : deleteValid ? "success" : "error"}
            />
          </FormGroup>
          <ActionGroup>
            <Button
              variant="danger"
              isDisabled={!deleteValid || deleteLoading}
              isLoading={deleteLoading}
              onClick={handleBulkDelete}
            >
              Delete
            </Button>
            <Button variant="link" onClick={() => setDeleteModalOpen(false)}>
              Cancel
            </Button>
          </ActionGroup>
        </Form>
        </ModalBody>
      </Modal>

      {/* ── Release / Archive Confirmation Modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={confirmLifecycle !== null}
        onClose={closeConfirmLifecycle}
      >
        <ModalHeader
          title={
            confirmLifecycle
              ? `${confirmLifecycle.action === "release" ? "Release" : "Archive"} ${
                  confirmLifecycle.policies.length === 1
                    ? `"${confirmLifecycle.policies[0].name}"`
                    : `${confirmLifecycle.policies.length} policies`
                }?`
              : ""
          }
          titleIconVariant={confirmLifecycle?.action === "archive" ? "warning" : "info"}
        />
        <ModalBody>
          {confirmLifecycle && (
            <>
              <p>
                {confirmLifecycle.action === "release"
                  ? "Released policies become available for binding and are delivered to agents on all bound node groups."
                  : "Archived policies are hidden from new bindings. You can restore an archived policy to draft later."}
              </p>
              {confirmLifecycle.policies.length > 1 && (
                <ul>
                  {confirmLifecycle.policies.map((p) => (
                    <li key={p.id}>{p.name}</li>
                  ))}
                </ul>
              )}
              {confirmLifecycle.skipped > 0 && (
                <p style={{ color: "var(--pf-t--global--text--color--subtle)", fontSize: "0.9rem" }}>
                  {confirmLifecycle.skipped} selected {confirmLifecycle.skipped === 1 ? "policy is" : "policies are"}{" "}
                  not eligible and will be skipped.
                </p>
              )}
            </>
          )}
        </ModalBody>
        <ModalFooter>
          <Button
            variant={confirmLifecycle?.action === "archive" ? "warning" : "primary"}
            isLoading={lifecycleLoading}
            isDisabled={lifecycleLoading || !confirmLifecycle}
            onClick={() =>
              confirmLifecycle &&
              runLifecycle(confirmLifecycle.action, confirmLifecycle.policies, confirmLifecycle.skipped)
            }
          >
            {confirmLifecycle?.action === "release" ? "Release" : "Archive"}
          </Button>
          <Button variant="link" onClick={closeConfirmLifecycle} isDisabled={lifecycleLoading}>
            Cancel
          </Button>
        </ModalFooter>
      </Modal>
    </>
  );
};
