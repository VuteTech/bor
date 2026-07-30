// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback, useMemo } from "react";
import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { useToast } from "../../components/ToastHost";
import {
  PageSection,
  Alert,
  Spinner,
  Flex,
  FlexItem,
  Button,
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Form,
  FormGroup,
  TextInput,
  TextArea,
  ActionGroup,
  ClipboardCopy,
  Label,
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
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction, ThProps } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import {
  fetchNodeGroups,
  createNodeGroup,
  updateNodeGroup,
  deleteNodeGroup,
  generateEnrollmentToken,
  NodeGroup,
  EnrollmentToken,
} from "../../apiClient/nodeGroupsApi";

/* ── Helpers ── */

const formatDate = (dateStr: string): string => new Date(dateStr).toLocaleString();

/* ── Component ── */

export const NodeGroupsPage: React.FC = () => {
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const { addToast } = useToast();

  // Search + client-side sort
  const [searchText, setSearchText] = useState("");
  const [sortIndex, setSortIndex] = useState<number | undefined>(undefined);
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc");

  // Selection
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [bulkOpen, setBulkOpen] = useState(false);

  // Create/Edit modal
  const [isFormOpen, setIsFormOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<NodeGroup | null>(null);
  const [formName, setFormName] = useState("");
  const [formDescription, setFormDescription] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [formSaving, setFormSaving] = useState(false);

  // Delete modal (type-to-confirm)
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleteTargetIds, setDeleteTargetIds] = useState<string[]>([]);
  const [deleteConfirmText, setDeleteConfirmText] = useState("");
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  // Token modal
  const [tokenGroup, setTokenGroup] = useState<NodeGroup | null>(null);
  const [generatedToken, setGeneratedToken] = useState<EnrollmentToken | null>(null);
  const [tokenLoading, setTokenLoading] = useState(false);
  const [tokenError, setTokenError] = useState<string | null>(null);

  /* ── Load data ── */
  const loadGroups = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const result = await fetchNodeGroups();
      setGroups(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load node groups");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadGroups();
  }, [loadGroups]);

  /* ── Filter + sort (client-side) ──
     Column indices match header order (0 = select cell):
     Name=1, Nodes=3, Created=4. */
  const view = useMemo(() => {
    const q = searchText.trim().toLowerCase();
    let list = q
      ? groups.filter(
          (g) =>
            g.name.toLowerCase().includes(q) || (g.description ?? "").toLowerCase().includes(q),
        )
      : groups;
    if (sortIndex !== undefined) {
      const cmp = (a: NodeGroup, b: NodeGroup): number => {
        switch (sortIndex) {
          case 1: return a.name.localeCompare(b.name);
          case 3: return a.node_count - b.node_count;
          case 4: return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
          default: return 0;
        }
      };
      list = [...list].sort((a, b) => (sortDir === "asc" ? cmp(a, b) : -cmp(a, b)));
    }
    return list;
  }, [groups, searchText, sortIndex, sortDir]);

  const getSort = (columnIndex: number): ThProps["sort"] => ({
    sortBy: { index: sortIndex, direction: sortDir },
    onSort: (_e, index, direction) => {
      setSortIndex(index);
      setSortDir(direction);
    },
    columnIndex,
  });

  /* ── Selection (over the visible rows) ── */
  const isAllSelected = view.length > 0 && view.every((g) => selectedIds.has(g.id));

  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(view.map((g) => g.id)));
    }
  };

  const toggleSelectGroup = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) { next.delete(id); } else { next.add(id); }
      return next;
    });
  };

  /* ── Create / Edit ── */
  const openCreateModal = () => {
    setEditingGroup(null);
    setFormName("");
    setFormDescription("");
    setFormError(null);
    setIsFormOpen(true);
  };

  const openEditModal = (group: NodeGroup) => {
    setEditingGroup(group);
    setFormName(group.name);
    setFormDescription(group.description);
    setFormError(null);
    setIsFormOpen(true);
  };

  const handleFormSave = async () => {
    if (!formName.trim()) {
      setFormError("Name is required");
      return;
    }
    try {
      setFormSaving(true);
      setFormError(null);
      if (editingGroup) {
        await updateNodeGroup(editingGroup.id, {
          name: formName.trim(),
          description: formDescription.trim(),
        });
      } else {
        await createNodeGroup({
          name: formName.trim(),
          description: formDescription.trim(),
        });
      }
      setIsFormOpen(false);
      addToast({
        variant: "success",
        title: editingGroup ? "Node group updated" : "Node group created",
        detail: formName.trim(),
      });
      loadGroups();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setFormSaving(false);
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

  // Single: type group name. Multiple: type "Yes".
  const deletePrompt = deleteTargetIds.length === 1
    ? groups.find((g) => g.id === deleteTargetIds[0])?.name ?? ""
    : "Yes";
  const deleteConfirmLabel = deleteTargetIds.length === 1
    ? `Type the group name "${deletePrompt}" to confirm`
    : `Type "Yes" to confirm deleting ${deleteTargetIds.length} groups`;
  const deleteValid = deleteConfirmText === deletePrompt;

  const handleBulkDelete = async () => {
    if (!deleteValid) return;
    setDeleteLoading(true);
    setDeleteError(null);
    const count = deleteTargetIds.length;
    try {
      await Promise.all(deleteTargetIds.map((id) => deleteNodeGroup(id)));
      setSelectedIds(new Set());
      setDeleteModalOpen(false);
      addToast({ variant: "success", title: `Deleted ${count} node group${count === 1 ? "" : "s"}` });
      loadGroups();
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Failed to delete");
    } finally {
      setDeleteLoading(false);
    }
  };

  /* ── Token generation ── */
  const openTokenModal = (group: NodeGroup) => {
    setTokenGroup(group);
    setGeneratedToken(null);
    setTokenError(null);
    setTokenLoading(false);
  };

  const handleGenerateToken = async () => {
    if (!tokenGroup) return;
    try {
      setTokenLoading(true);
      setTokenError(null);
      const token = await generateEnrollmentToken(tokenGroup.id);
      setGeneratedToken(token);
    } catch (err) {
      setTokenError(err instanceof Error ? err.message : "Failed to generate token");
    } finally {
      setTokenLoading(false);
    }
  };

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
          <Alert variant="danger" title="Error loading node groups">{error}</Alert>
        </div>
      </PageSection>
    );
  }

  const selectedGroups = groups.filter((g) => selectedIds.has(g.id));

  return (
    <>
      <PageSection>
        <Flex
          justifyContent={{ default: "justifyContentSpaceBetween" }}
          alignItems={{ default: "alignItemsCenter" }}
        >
          <FlexItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={openCreateModal}>
              Create Node Group
            </Button>
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <Toolbar style={{ padding: 0, marginBottom: "0.5rem" }} clearAllFilters={() => setSearchText("")}>
          <ToolbarContent>
            <ToolbarItem>
              <SearchInput
                aria-label="Search node groups"
                placeholder="Search by name or description…"
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
                        const g = selectedGroups[0];
                        if (g) openEditModal(g);
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
            isEmptyData={groups.length === 0}
            itemsLabel="node groups"
            emptyTitle="No node groups"
            emptyBody="Create a node group to organize your desktop agents and generate enrollment tokens."
            action={
              <Button variant="primary" onClick={openCreateModal}>
                Create Node Group
              </Button>
            }
            onClearFilters={() => setSearchText("")}
          />
        ) : (
          <Table aria-label="Node groups table" variant="compact">
            <Thead>
              <Tr>
                <Th
                  select={{
                    onSelect: toggleSelectAll,
                    isSelected: isAllSelected,
                  }}
                />
                <Th sort={getSort(1)}>Name</Th>
                <Th>Description</Th>
                <Th sort={getSort(3)}>Nodes</Th>
                <Th sort={getSort(4)}>Created</Th>
                <Th screenReaderText="Actions" />
              </Tr>
            </Thead>
            <Tbody>
              {view.map((group, rowIndex) => {
                const rowActions: IAction[] = [
                  { title: "Generate token", onClick: () => openTokenModal(group) },
                  { title: "Edit", onClick: () => openEditModal(group) },
                  { isSeparator: true },
                  { title: "Delete", isDanger: true, onClick: () => openDeleteModal([group.id]) },
                ];
                return (
                  <Tr key={group.id}>
                    <Td
                      select={{
                        rowIndex,
                        onSelect: () => toggleSelectGroup(group.id),
                        isSelected: selectedIds.has(group.id),
                      }}
                    />
                    <Td dataLabel="Name"><strong>{group.name}</strong></Td>
                    <Td dataLabel="Description">{group.description || "—"}</Td>
                    <Td dataLabel="Nodes">
                      <Label color={group.node_count > 0 ? "blue" : "grey"}>
                        {group.node_count}
                      </Label>
                    </Td>
                    <Td dataLabel="Created">{formatDate(group.created_at)}</Td>
                    <Td dataLabel="Actions" isActionCell>
                      <ActionsColumn
                        items={rowActions}
                        actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                          <MenuToggle
                            ref={toggleRef}
                            aria-label={`Actions for node group ${group.name}`}
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
                );
              })}
            </Tbody>
          </Table>
        )}
      </PageSection>

      {/* ── Create / Edit Modal ── */}
      <Modal
        variant={ModalVariant.small}
        isOpen={isFormOpen}
        onClose={() => setIsFormOpen(false)}
      >
        <ModalHeader title={editingGroup ? "Edit Node Group" : "Create Node Group"} />
        <ModalBody>
          <LiveAlert id="err-ng-form" message={formError} isInline style={{ marginBottom: "1rem" }} />
          <Form>
            {editingGroup && (
              <FormGroup label="Group ID" fieldId="ng-id">
                <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied!" id="ng-id">
                  {editingGroup.id}
                </ClipboardCopy>
                <p style={{ marginTop: "0.25rem", color: "#6a6e73", fontSize: "0.875rem" }}>
                  Use this ID for <abbr title="BOR_KERBEROS_DEFAULT_NODE_GROUP">Kerberos auto-enrollment</abbr> configuration.
                </p>
              </FormGroup>
            )}
            <FormGroup label="Name" isRequired fieldId="ng-name">
              <TextInput
                id="ng-name"
                value={formName}
                onChange={(_ev, val) => setFormName(val)}
                isRequired
                placeholder="e.g. Engineering Desktops"
                aria-invalid={formError ? true : undefined}
                aria-describedby={formError ? "err-ng-form" : undefined}
              />
            </FormGroup>
            <FormGroup label="Description" fieldId="ng-description">
              <TextArea
                id="ng-description"
                value={formDescription}
                onChange={(_ev, val) => setFormDescription(val)}
                placeholder="Optional description for this group"
                rows={3}
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
            {editingGroup ? "Save" : "Create"}
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
        <ModalHeader
          title={`Delete Node Group${deleteTargetIds.length !== 1 ? "s" : ""}`}
          titleIconVariant="warning"
        />
        <ModalBody>
          <Form>
          <p>
            {deleteTargetIds.length === 1 ? (
              <>
                This will permanently delete the node group. Nodes will remain but will
                lose their membership in this group.
              </>
            ) : (
              <>
                This will permanently delete{" "}
                <strong>{deleteTargetIds.length} node groups</strong> and remove all
                their memberships.
              </>
            )}
          </p>
          {(() => {
            const targets = deleteTargetIds
              .map((id) => groups.find((g) => g.id === id))
              .filter(Boolean) as NodeGroup[];
            const withNodes = targets.filter((g) => g.node_count > 0);
            if (withNodes.length === 0) return null;
            return (
              <div aria-live="assertive" aria-atomic="true">
                <Alert variant="warning" title="Some groups have nodes assigned" isInline>
                  {withNodes
                    .map((g) => `${g.name} (${g.node_count} node${g.node_count !== 1 ? "s" : ""})`)
                    .join(", ")}
                  {" — these nodes will lose their group membership."}
                </Alert>
              </div>
            );
          })()}
          <div aria-live="assertive" aria-atomic="true">
            {deleteError && (
              <Alert variant="danger" title="Error" isInline>{deleteError}</Alert>
            )}
          </div>
          <FormGroup label={deleteConfirmLabel} isRequired fieldId="delete-confirm">
            <TextInput
              id="delete-confirm"
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

      {/* ── Token Generation Modal ── */}
      <Modal
        variant={ModalVariant.medium}
        isOpen={tokenGroup !== null}
        onClose={() => { setTokenGroup(null); setGeneratedToken(null); }}
      >
        <ModalHeader title={`Enrollment Token — ${tokenGroup?.name || ""}`} />
        <ModalBody>
          <LiveAlert message={tokenError} isInline style={{ marginBottom: "1rem" }} />
          {!generatedToken ? (
            <div>
              <p>
                Generate a one-time enrollment token for the node group{" "}
                <strong>{tokenGroup?.name}</strong>.
              </p>
              <Alert variant="info" title="Token details" isInline style={{ marginTop: "1rem" }}>
                <ul style={{ margin: 0, paddingLeft: "1.25rem" }}>
                  <li>The token expires in <strong>5 minutes</strong></li>
                  <li>The token is <strong>single-use</strong> — it can only be used once</li>
                  <li>Copy the token immediately — it will not be shown again</li>
                </ul>
              </Alert>
            </div>
          ) : (
            <div>
              <div aria-live="polite" aria-atomic="true">
                <Alert
                  variant="success"
                  title="Token generated successfully"
                  isInline
                  style={{ marginBottom: "1rem" }}
                >
                  Copy the token below. It will <strong>expire at{" "}
                  {formatDate(generatedToken.expires_at)}</strong> and can only be used{" "}
                  <strong>once</strong>.
                </Alert>
              </div>
              <FormGroup label="Enrollment Command" fieldId="enroll-command">
                <p style={{ marginBottom: "0.5rem", color: "#6a6e73", fontSize: "0.875rem" }}>
                  Run this command on the target machine to enroll the agent:
                </p>
                <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied!">
                  {`sudo bor-agent --token ${generatedToken.token}`}
                </ClipboardCopy>
              </FormGroup>
              <FormGroup label="Token Only" fieldId="token-value" style={{ marginTop: "1rem" }}>
                <ClipboardCopy
                  isReadOnly
                  hoverTip="Copy"
                  clickTip="Copied!"
                  variant="expansion"
                >
                  {generatedToken.token}
                </ClipboardCopy>
              </FormGroup>
              <Alert variant="warning" title="Save this token now" isInline style={{ marginTop: "1rem" }}>
                This token will not be shown again after closing this dialog. Generate a new
                token if needed.
              </Alert>
            </div>
          )}
        </ModalBody>
        <ModalFooter>
          {generatedToken
            ? (
                <Button
                  key="close"
                  variant="primary"
                  onClick={() => { setTokenGroup(null); setGeneratedToken(null); }}
                >
                  Done
                </Button>
              )
            : (
                <>
                  <Button
                    key="generate"
                    variant="primary"
                    onClick={handleGenerateToken}
                    isLoading={tokenLoading}
                    isDisabled={tokenLoading}
                  >
                    Generate Token
                  </Button>
                  <Button key="cancel" variant="link" onClick={() => setTokenGroup(null)}>
                    Cancel
                  </Button>
                </>
              )
          }
        </ModalFooter>
      </Modal>
    </>
  );
};
