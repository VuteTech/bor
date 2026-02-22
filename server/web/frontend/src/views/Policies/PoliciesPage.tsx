// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  PageSection,
  Title,
  Button,
  Alert,
  Spinner,
  Flex,
  FlexItem,
  Label,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
  ToolbarFilter,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectOption,
  SelectList,
  SearchInput,
  Dropdown,
  DropdownItem,
  DropdownList,
  Modal,
  ModalVariant,
  Form,
  FormGroup,
  TextInput,
  ActionGroup,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import PencilAltIcon from "@patternfly/react-icons/dist/esm/icons/pencil-alt-icon";

import { fetchAllPolicies, deletePolicy, Policy } from "../../apiClient/policiesApi";
import { PolicyDetailsModal } from "./PolicyDetailsModal";

/* ── Filter options ── */

const TYPE_OPTIONS = ["Kconfig", "Dconf", "Firefox", "Polkit", "Chrome", "custom"];
const STATUS_OPTIONS = ["draft", "released", "archived"];

const statusLabelColor = (status: string): "green" | "red" | "blue" | "orange" | "grey" => {
  switch (status) {
    case "released": return "green";
    case "archived": return "red";
    case "draft":    return "blue";
    default:         return "grey";
  }
};

export const PoliciesPage: React.FC = () => {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Filters
  const [searchText, setSearchText] = useState("");
  const [typeFilter, setTypeFilter] = useState<string[]>([]);
  const [statusFilter, setStatusFilter] = useState<string[]>([]);
  const [bindingsFilter, setBindingsFilter] = useState<string | null>(null);
  const [recentlyModified, setRecentlyModified] = useState(false);

  // Filter dropdown states
  const [isTypeOpen, setIsTypeOpen] = useState(false);
  const [isStatusOpen, setIsStatusOpen] = useState(false);
  const [isBindingsOpen, setIsBindingsOpen] = useState(false);

  // Selection
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [bulkOpen, setBulkOpen] = useState(false);

  // Create/Edit modal (PolicyDetailsModal)
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [selectedPolicy, setSelectedPolicy] = useState<Policy | null>(null);

  // Delete modal (type-to-confirm)
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleteTargetIds, setDeleteTargetIds] = useState<string[]>([]);
  const [deleteConfirmText, setDeleteConfirmText] = useState("");
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const loadPolicies = useCallback(async () => {
    try {
      setLoading(true);
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
    loadPolicies();
  }, [loadPolicies]);

  /* ── Selection ── */
  const filteredPolicies = policies.filter((p) => {
    if (searchText && !p.name.toLowerCase().includes(searchText.toLowerCase())) return false;
    if (typeFilter.length > 0 && !typeFilter.includes(p.type)) return false;
    if (statusFilter.length > 0 && !statusFilter.includes(p.state)) return false;
    if (bindingsFilter === "has") { /* Future */ }
    else if (bindingsFilter === "none") { /* Future */ }
    if (recentlyModified) {
      const sevenDaysAgo = new Date();
      sevenDaysAgo.setDate(sevenDaysAgo.getDate() - 7);
      if (new Date(p.updated_at) < sevenDaysAgo) return false;
    }
    return true;
  });

  const isAllSelected =
    filteredPolicies.length > 0 && selectedIds.size === filteredPolicies.length;

  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(filteredPolicies.map((p) => p.id)));
    }
  };

  const toggleSelectPolicy = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) { next.delete(id); } else { next.add(id); }
      return next;
    });
  };

  /* ── Create / Edit ── */
  const handleCreate = () => {
    setSelectedPolicy(null);
    setIsModalOpen(true);
  };

  const handleEdit = (policy: Policy) => {
    setSelectedPolicy(policy);
    setIsModalOpen(true);
  };

  const handleModalClose = () => {
    setIsModalOpen(false);
    setSelectedPolicy(null);
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
          <FlexItem><Spinner size="xl" /></FlexItem>
        </Flex>
      </PageSection>
    );
  }

  return (
    <>
      <PageSection variant="light">
        <Flex
          justifyContent={{ default: "justifyContentSpaceBetween" }}
          alignItems={{ default: "alignItemsCenter" }}
        >
          <FlexItem>
            <Title headingLevel="h1" size="2xl">Policies</Title>
            <p style={{ color: "#6a6e73", marginTop: "0.25rem" }}>
              Manage desktop policies for your Linux fleet. Each update creates a new version.
            </p>
          </FlexItem>
          <FlexItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={handleCreate}>
              Create a policy
            </Button>
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        {error && (
          <Alert variant="danger" title="Error loading policies" style={{ marginBottom: "1rem" }}>
            {error}
          </Alert>
        )}

        {/* Toolbar with filters + bulk Actions */}
        <Toolbar clearAllFilters={() => {
          setTypeFilter([]);
          setStatusFilter([]);
          setBindingsFilter(null);
          setRecentlyModified(false);
          setSearchText("");
        }}>
          <ToolbarContent>
            <ToolbarItem>
              <SearchInput
                placeholder="Search by name..."
                value={searchText}
                onChange={(_ev, val) => setSearchText(val)}
                onClear={() => setSearchText("")}
              />
            </ToolbarItem>

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
          </ToolbarContent>
        </Toolbar>

        {/* Policies table */}
        {filteredPolicies.length === 0 ? (
          <div
            style={{
              padding: "3rem",
              textAlign: "center",
              color: "#6a6e73",
              border: "1px solid #d2d2d2",
              borderRadius: "4px",
              marginTop: "1rem",
            }}
          >
            {policies.length === 0
              ? "No policies configured. Click \"Create a policy\" to get started."
              : "No policies match the current filters."}
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
                <Th>Name</Th>
                <Th>Type</Th>
                <Th>Version</Th>
                <Th>Status</Th>
                <Th>Bindings</Th>
                <Th>Last Updated</Th>
                <Th>Actions</Th>
              </Tr>
            </Thead>
            <Tbody>
              {filteredPolicies.map((policy, rowIndex) => (
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
                      <div style={{ fontSize: "0.8rem", color: "#6a6e73" }}>
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
                  <Td dataLabel="Bindings">0</Td>
                  <Td dataLabel="Last Updated">
                    {new Date(policy.updated_at).toLocaleString()}
                  </Td>
                  <Td dataLabel="Actions">
                    <Button
                      variant="plain"
                      aria-label={`Edit policy ${policy.name}`}
                      onClick={() => handleEdit(policy)}
                    >
                      <PencilAltIcon />
                    </Button>
                  </Td>
                </Tr>
              ))}
            </Tbody>
          </Table>
        )}
      </PageSection>

      {/* ── Create / Edit Modal ── */}
      <PolicyDetailsModal
        isOpen={isModalOpen}
        onClose={handleModalClose}
        onSaved={loadPolicies}
        onDeleted={() => { handleModalClose(); loadPolicies(); }}
        policy={selectedPolicy}
      />

      {/* ── Delete Confirmation Modal ── */}
      <Modal
        variant={ModalVariant.small}
        title={`Delete Polic${deleteTargetIds.length !== 1 ? "ies" : "y"}`}
        titleIconVariant="warning"
        isOpen={deleteModalOpen}
        onClose={() => setDeleteModalOpen(false)}
      >
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
          {deleteError && (
            <Alert variant="danger" title="Error" isInline>{deleteError}</Alert>
          )}
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
      </Modal>
    </>
  );
};
