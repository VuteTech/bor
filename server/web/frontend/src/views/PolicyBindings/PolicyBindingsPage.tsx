// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback, useMemo } from "react";
import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import {
  PageSection,
  Alert,
  AlertActionCloseButton,
  Spinner,
  Flex,
  FlexItem,
  Button,
  Modal,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalVariant,
  Form,
  FormGroup,
  TextInput,
  ActionGroup,
  Switch,
  Label,
  FormSelect,
  FormSelectOption,
  Dropdown,
  DropdownItem,
  DropdownList,
  MenuToggle,
  MenuToggleElement,
  SearchInput,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ThProps } from "@patternfly/react-table";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import {
  fetchBindings,
  createBinding,
  updateBinding,
  deleteBinding,
  PolicyBinding,
} from "../../apiClient/bindingsApi";
import { fetchAllPolicies, fetchPolicy, Policy } from "../../apiClient/policiesApi";
import { fetchNodeGroups, NodeGroup } from "../../apiClient/nodeGroupsApi";
import { PolicyDetailsModal } from "../Policies/PolicyDetailsModal";

/* ── Helpers ── */

const formatDate = (dateStr: string): string => new Date(dateStr).toLocaleString();

const statusColor = (status: string): "blue" | "green" | "orange" | "red" | "grey" => {
  switch (status) {
    case "released": return "green";
    case "draft":    return "blue";
    case "archived": return "red";
    default:         return "grey";
  }
};

/* ── Component ── */

export const PolicyBindingsPage: React.FC = () => {
  const [bindings, setBindings] = useState<PolicyBinding[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const { addToast } = useToast();

  // Search + client-side sort
  const [searchText, setSearchText] = useState("");
  const [sortIndex, setSortIndex] = useState<number | undefined>(undefined);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");

  // Enabling a binding pushes a policy to every node in the group, so it is
  // confirmed first when it affects any nodes.
  const [pendingEnable, setPendingEnable] = useState<PolicyBinding | null>(null);
  const [toggling, setToggling] = useState(false);

  // Available policies and groups for the form dropdowns
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [groups, setGroups] = useState<NodeGroup[]>([]);

  // Selection
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [bulkOpen, setBulkOpen] = useState(false);

  // Create/Edit modal
  const [isFormOpen, setIsFormOpen] = useState(false);
  const [editingBinding, setEditingBinding] = useState<PolicyBinding | null>(null);
  const [formPolicyId, setFormPolicyId] = useState("");
  const [formGroupId, setFormGroupId] = useState("");
  const [formState, setFormState] = useState("disabled");
  const [formPriority, setFormPriority] = useState(0);
  const [formError, setFormError] = useState<string | null>(null);
  const [formSaving, setFormSaving] = useState(false);

  // Delete modal (type-to-confirm)
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleteTargetIds, setDeleteTargetIds] = useState<string[]>([]);
  const [deleteConfirmText, setDeleteConfirmText] = useState("");
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Policy details modal (opened by clicking a policy name)
  const [policyDetailTarget, setPolicyDetailTarget] = useState<Policy | null>(null);
  const [isPolicyDetailOpen, setIsPolicyDetailOpen] = useState(false);

  /* ── Load data ── */
  const loadBindings = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await fetchBindings();
      setBindings(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load policy bindings");
    } finally {
      setLoading(false);
    }
  }, []);

  const loadFormData = useCallback(async () => {
    try {
      const [p, g] = await Promise.all([fetchAllPolicies(), fetchNodeGroups()]);
      setPolicies(p);
      setGroups(g);
    } catch {
      /* best effort */
    }
  }, []);

  useEffect(() => {
    loadBindings();
  }, [loadBindings]);

  /* ── Filter + sort (client-side) ──
     Sortable column indices match the header order (index 0 = select cell):
     Policy=1, Group=3, Priority=5, Affected Nodes=6, Updated=7. */
  const view = useMemo(() => {
    const q = searchText.trim().toLowerCase();
    let list = q
      ? bindings.filter(
          (b) =>
            b.policy_name.toLowerCase().includes(q) || b.group_name.toLowerCase().includes(q),
        )
      : bindings;
    if (sortIndex !== undefined) {
      const cmp = (a: PolicyBinding, b: PolicyBinding): number => {
        switch (sortIndex) {
          case 1: return a.policy_name.localeCompare(b.policy_name);
          case 3: return a.group_name.localeCompare(b.group_name);
          case 5: return a.priority - b.priority;
          case 6: return a.node_count - b.node_count;
          case 7: return new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime();
          default: return 0;
        }
      };
      list = [...list].sort((a, b) => (sortDir === "asc" ? cmp(a, b) : -cmp(a, b)));
    }
    return list;
  }, [bindings, searchText, sortIndex, sortDir]);

  const getSort = (columnIndex: number): ThProps["sort"] => ({
    sortBy: { index: sortIndex, direction: sortDir },
    onSort: (_e, index, direction) => {
      setSortIndex(index);
      setSortDir(direction);
    },
    columnIndex,
  });

  /* ── Selection (over the visible rows) ── */
  const isAllSelected = view.length > 0 && view.every((b) => selectedIds.has(b.id));

  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(view.map((b) => b.id)));
    }
  };

  const toggleSelectBinding = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) { next.delete(id); } else { next.add(id); }
      return next;
    });
  };

  /* ── Create / Edit ── */
  const openCreateModal = async () => {
    setEditingBinding(null);
    setFormPolicyId("");
    setFormGroupId("");
    setFormState("disabled");
    setFormPriority(0);
    setFormError(null);
    setIsFormOpen(true);
    await loadFormData();
  };

  const openEditModal = async (binding: PolicyBinding) => {
    setEditingBinding(binding);
    setFormPolicyId(binding.policy_id);
    setFormGroupId(binding.group_id);
    setFormState(binding.state);
    setFormPriority(binding.priority);
    setFormError(null);
    setIsFormOpen(true);
    await loadFormData();
  };

  const handleFormSave = async () => {
    if (editingBinding) {
      try {
        setFormSaving(true);
        setFormError(null);
        await updateBinding(editingBinding.id, {
          state: formState,
          priority: formPriority,
        });
        setIsFormOpen(false);
        addToast({ variant: "success", title: "Binding updated" });
        loadBindings();
      } catch (err) {
        setFormError(err instanceof Error ? err.message : "Failed to save");
      } finally {
        setFormSaving(false);
      }
    } else {
      if (!formPolicyId) { setFormError("Policy is required"); return; }
      if (!formGroupId)  { setFormError("Group is required"); return; }
      try {
        setFormSaving(true);
        setFormError(null);
        await createBinding({ policy_id: formPolicyId, group_id: formGroupId, priority: formPriority });
        setIsFormOpen(false);
        addToast({ variant: "success", title: "Binding created" });
        loadBindings();
      } catch (err) {
        setFormError(err instanceof Error ? err.message : "Failed to save");
      } finally {
        setFormSaving(false);
      }
    }
  };

  /* ── Delete (type-to-confirm) ── */
  const openDeleteModal = (ids: string[]) => {
    setDeleteTargetIds(ids);
    setDeleteConfirmText("");
    setDeleteError(null);
    setDeleteLoading(false);
    setDeleteModalOpen(true);
  };

  // Single: type "policy to group" (keyboard-typeable). Multiple: type "Yes".
  const deletePrompt = deleteTargetIds.length === 1
    ? (() => {
        const b = bindings.find((b) => b.id === deleteTargetIds[0]);
        return b ? `${b.policy_name} to ${b.group_name}` : "";
      })()
    : "Yes";
  const deleteConfirmLabel = deleteTargetIds.length === 1
    ? `Type "${deletePrompt}" to confirm`
    : `Type "Yes" to confirm deleting ${deleteTargetIds.length} bindings`;
  const deleteValid = deleteConfirmText === deletePrompt;

  const handleBulkDelete = async () => {
    if (!deleteValid) return;
    setDeleteLoading(true);
    setDeleteError(null);
    const count = deleteTargetIds.length;
    try {
      await Promise.all(deleteTargetIds.map((id) => deleteBinding(id)));
      setSelectedIds(new Set());
      setDeleteModalOpen(false);
      addToast({ variant: "success", title: `Deleted ${count} binding${count === 1 ? "" : "s"}` });
      loadBindings();
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Failed to delete");
    } finally {
      setDeleteLoading(false);
    }
  };

  /* ── Toggle state inline ── */
  const applyToggle = async (binding: PolicyBinding, newState: "enabled" | "disabled") => {
    setToggling(true);
    try {
      await updateBinding(binding.id, { state: newState });
      addToast({
        variant: "success",
        title: newState === "enabled" ? "Binding enabled" : "Binding disabled",
        detail: `${binding.policy_name} to ${binding.group_name}`,
      });
      loadBindings();
    } catch (err) {
      addToast({
        variant: "danger",
        title: "Couldn’t update binding",
        detail: err instanceof Error ? err.message : String(err),
      });
      loadBindings();
    } finally {
      setToggling(false);
      setPendingEnable(null);
    }
  };

  const handleToggleState = (binding: PolicyBinding) => {
    if (binding.state === "enabled") {
      applyToggle(binding, "disabled");
      return;
    }
    // Enabling delivers the policy to every node in the group — confirm first
    // when that affects one or more nodes.
    if (binding.node_count > 0) {
      setPendingEnable(binding);
      return;
    }
    applyToggle(binding, "enabled");
  };

  /* ── Open policy details modal ── */
  const openPolicyDetail = async (policyId: string) => {
    try {
      const policy = await fetchPolicy(policyId);
      setPolicyDetailTarget(policy);
      setIsPolicyDetailOpen(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load policy details");
    }
  };

  const selectedBindings = bindings.filter((b) => selectedIds.has(b.id));

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

  return (
    <>
      <PageSection>
        <Flex
          justifyContent={{ default: "justifyContentSpaceBetween" }}
          alignItems={{ default: "alignItemsCenter" }}
        >
          <FlexItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={openCreateModal}>
              Create Binding
            </Button>
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <LiveAlert
          message={error}
          isInline
          actionClose={<AlertActionCloseButton onClose={() => setError(null)} />}
          style={{ marginBottom: "1rem" }}
        />

        <Toolbar style={{ padding: 0, marginBottom: "0.5rem" }} clearAllFilters={() => setSearchText("")}>
          <ToolbarContent>
            <ToolbarItem>
              <SearchInput
                aria-label="Search bindings by policy or group"
                placeholder="Search by policy or group…"
                value={searchText}
                onChange={(_ev, val) => setSearchText(val)}
                onClear={() => setSearchText("")}
              />
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
                        const b = selectedBindings[0];
                        if (b) openEditModal(b);
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

        {view.length === 0 ? (
          <BorEmptyState
            isEmptyData={bindings.length === 0}
            itemsLabel="policy bindings"
            emptyTitle="No policy bindings"
            emptyBody="Create a binding to connect a policy to a node group. Nodes get policies only through group membership."
            action={
              <Button variant="primary" onClick={openCreateModal}>
                Create Binding
              </Button>
            }
            onClearFilters={() => setSearchText("")}
          />
        ) : (
          <>
            <Table aria-label="Policy bindings table" variant="compact">
              <Thead>
                <Tr>
                  <Th
                    select={{
                      onSelect: toggleSelectAll,
                      isSelected: isAllSelected,
                    }}
                  />
                  <Th sort={getSort(1)}>Policy</Th>
                  <Th>Policy State</Th>
                  <Th sort={getSort(3)}>Group</Th>
                  <Th>Binding State</Th>
                  <Th sort={getSort(5)}>Priority</Th>
                  <Th sort={getSort(6)}>Affected Nodes</Th>
                  <Th sort={getSort(7)}>Updated</Th>
                  <Th>Actions</Th>
                </Tr>
              </Thead>
              <Tbody>
                {view.map((b, rowIndex) => (
                  <Tr key={b.id}>
                    <Td
                      select={{
                        rowIndex,
                        onSelect: () => toggleSelectBinding(b.id),
                        isSelected: selectedIds.has(b.id),
                      }}
                    />
                    <Td dataLabel="Policy">
                      <Button variant="link" isInline onClick={() => openPolicyDetail(b.policy_id)}>
                        <strong>{b.policy_name}</strong>
                      </Button>
                    </Td>
                    <Td dataLabel="Policy State">
                      <Label color={statusColor(b.policy_state)}>{b.policy_state}</Label>
                    </Td>
                    <Td dataLabel="Group">{b.group_name}</Td>
                    <Td dataLabel="Binding State">
                      <Switch
                        id={`toggle-${b.id}`}
                        aria-label={`${b.state === "enabled" ? "Disable" : "Enable"} binding ${b.policy_name} on ${b.group_name}`}
                        isChecked={b.state === "enabled"}
                        onChange={() => handleToggleState(b)}
                        isDisabled={toggling}
                        hasCheckIcon
                      />
                    </Td>
                    <Td dataLabel="Priority">{b.priority}</Td>
                    <Td dataLabel="Affected Nodes">
                      <Label color={b.node_count > 0 ? "blue" : "grey"}>
                        {b.node_count}
                      </Label>
                    </Td>
                    <Td dataLabel="Updated">{formatDate(b.updated_at)}</Td>
                    <Td dataLabel="Actions">
                      <Flex>
                        <FlexItem>
                          <Button variant="plain" size="sm" onClick={() => openEditModal(b)}>
                            Edit
                          </Button>
                        </FlexItem>
                        <FlexItem>
                          <Button
                            variant="plain"
                            size="sm"
                            isDanger
                            onClick={() => openDeleteModal([b.id])}
                          >
                            Delete
                          </Button>
                        </FlexItem>
                      </Flex>
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </>
        )}
      </PageSection>

      {/* ── Create / Edit Modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={isFormOpen}
        onClose={() => setIsFormOpen(false)}
      >
        <ModalHeader title={editingBinding ? "Edit Binding" : "Create Binding"} />
        <ModalBody>
          <LiveAlert message={formError} isInline style={{ marginBottom: "1rem" }} />
          <Form>
            <FormGroup label="Policy" isRequired fieldId="bind-policy">
              <FormSelect
                id="bind-policy"
                value={formPolicyId}
                onChange={(_ev, val) => setFormPolicyId(val)}
                isDisabled={!!editingBinding}
                aria-label="Select a policy"
              >
                <FormSelectOption key="" value="" label="Select a policy…" isPlaceholder />
                {policies.map((p) => (
                  <FormSelectOption key={p.id} value={p.id} label={`${p.name} (${p.state})`} />
                ))}
              </FormSelect>
            </FormGroup>
            <FormGroup label="Node Group" isRequired fieldId="bind-group">
              <FormSelect
                id="bind-group"
                value={formGroupId}
                onChange={(_ev, val) => setFormGroupId(val)}
                isDisabled={!!editingBinding}
                aria-label="Select a node group"
              >
                <FormSelectOption key="" value="" label="Select a group…" isPlaceholder />
                {groups.map((g) => (
                  <FormSelectOption
                    key={g.id}
                    value={g.id}
                    label={`${g.name} (${g.node_count} nodes)`}
                  />
                ))}
              </FormSelect>
            </FormGroup>
            <FormGroup label="Binding State" fieldId="bind-state">
              <Switch
                id="bind-state"
                label="Enabled"
                labelOff="Disabled"
                isChecked={formState === "enabled"}
                onChange={(_ev, val) => setFormState(val ? "enabled" : "disabled")}
                aria-label="Binding state"
                hasCheckIcon
              />
            </FormGroup>
            <FormGroup label="Priority" fieldId="bind-priority">
              <TextInput
                id="bind-priority"
                type="number"
                value={formPriority}
                onChange={(_ev, val) => setFormPriority(parseInt(val, 10) || 0)}
              />
            </FormGroup>
          </Form>
        </ModalBody>
        <ModalFooter>
          <Button
            key="save"
            variant="primary"
            onClick={handleFormSave}
            isLoading={formSaving}
            isDisabled={formSaving}
          >
            {editingBinding ? "Save" : "Create"}
          </Button>
          <Button key="cancel" variant="link" onClick={() => setIsFormOpen(false)}>
            Cancel
          </Button>
        </ModalFooter>
      </Modal>

      {/* ── Delete Confirmation Modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={deleteModalOpen}
        onClose={() => setDeleteModalOpen(false)}
      >
        <ModalHeader title={`Delete Binding${deleteTargetIds.length !== 1 ? "s" : ""}`} titleIconVariant="warning" />
        <ModalBody>
        <Form>
          <p>
            {deleteTargetIds.length === 1 ? (
              <>
                This will permanently remove the binding between{" "}
                <strong>
                  {bindings.find((b) => b.id === deleteTargetIds[0])?.policy_name}
                </strong>{" "}
                and{" "}
                <strong>
                  {bindings.find((b) => b.id === deleteTargetIds[0])?.group_name}
                </strong>.
              </>
            ) : (
              <>
                This will permanently delete{" "}
                <strong>{deleteTargetIds.length} bindings</strong>.
              </>
            )}
          </p>
          <div aria-live="assertive" aria-atomic="true">
            {deleteError && (
              <Alert variant="danger" title="Error" isInline>{deleteError}</Alert>
            )}
          </div>
          <FormGroup label={deleteConfirmLabel} isRequired fieldId="binding-delete-confirm">
            <TextInput
              id="binding-delete-confirm"
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

      {/* ── Confirm enabling a binding (delivers policy to all group nodes) ── */}
      <ConfirmModal
        isOpen={pendingEnable !== null}
        title="Enable this binding?"
        confirmLabel="Enable"
        isBusy={toggling}
        onConfirm={() => pendingEnable && applyToggle(pendingEnable, "enabled")}
        onCancel={() => setPendingEnable(null)}
      >
        {pendingEnable && (
          <p>
            Enabling the binding of <strong>{pendingEnable.policy_name}</strong> to{" "}
            <strong>{pendingEnable.group_name}</strong> will deliver this policy to all{" "}
            <strong>{pendingEnable.node_count}</strong> node
            {pendingEnable.node_count === 1 ? "" : "s"} in the group.
          </p>
        )}
      </ConfirmModal>

      {/* ── Policy Details Modal (opened by clicking policy name) ── */}
      <PolicyDetailsModal
        isOpen={isPolicyDetailOpen}
        onClose={() => { setIsPolicyDetailOpen(false); setPolicyDetailTarget(null); }}
        onSaved={() => { loadBindings(); }}
        policy={policyDetailTarget}
      />
    </>
  );
};
