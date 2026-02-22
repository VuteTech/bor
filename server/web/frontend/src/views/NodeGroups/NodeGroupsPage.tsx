// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  PageSection,
  Title,
  Alert,
  Spinner,
  Flex,
  FlexItem,
  Button,
  Modal,
  ModalVariant,
  Form,
  FormGroup,
  TextInput,
  TextArea,
  ActionGroup,
  EmptyState,
  EmptyStateBody,
  EmptyStateHeader,
  EmptyStateIcon,
  ClipboardCopy,
  Label,
  Dropdown,
  DropdownItem,
  DropdownList,
  MenuToggle,
  MenuToggleElement,
  Toolbar,
  ToolbarContent,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import CubesIcon from "@patternfly/react-icons/dist/esm/icons/cubes-icon";
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

  /* ── Selection ── */
  const isAllSelected = groups.length > 0 && selectedIds.size === groups.length;

  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(groups.map((g) => g.id)));
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
    try {
      await Promise.all(deleteTargetIds.map((id) => deleteNodeGroup(id)));
      setSelectedIds(new Set());
      setDeleteModalOpen(false);
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
          <FlexItem><Spinner size="xl" /></FlexItem>
        </Flex>
      </PageSection>
    );
  }

  if (error) {
    return (
      <PageSection>
        <Alert variant="danger" title="Error loading node groups">{error}</Alert>
      </PageSection>
    );
  }

  const selectedGroups = groups.filter((g) => selectedIds.has(g.id));

  return (
    <>
      <PageSection variant="light">
        <Flex
          justifyContent={{ default: "justifyContentSpaceBetween" }}
          alignItems={{ default: "alignItemsCenter" }}
        >
          <FlexItem>
            <Title headingLevel="h1" size="2xl">Node Groups</Title>
            <p style={{ color: "#6a6e73", marginTop: "0.25rem" }}>
              Manage node groups and generate enrollment tokens for agent registration.
            </p>
          </FlexItem>
          <FlexItem>
            <Button variant="primary" icon={<PlusCircleIcon />} onClick={openCreateModal}>
              Create Node Group
            </Button>
          </FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        {groups.length === 0 ? (
          <EmptyState>
            <EmptyStateHeader
              titleText="No node groups"
              headingLevel="h2"
              icon={<EmptyStateIcon icon={CubesIcon} />}
            />
            <EmptyStateBody>
              Create a node group to organize your desktop agents and generate enrollment tokens.
            </EmptyStateBody>
            <Button variant="primary" onClick={openCreateModal}>Create Node Group</Button>
          </EmptyState>
        ) : (
          <>
            {selectedIds.size > 0 && (
              <Toolbar style={{ padding: 0, marginBottom: "0.5rem" }}>
                <ToolbarContent>
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
                </ToolbarContent>
              </Toolbar>
            )}

            <Table aria-label="Node groups table" variant="compact">
              <Thead>
                <Tr>
                  <Th
                    select={{
                      onSelect: toggleSelectAll,
                      isSelected: isAllSelected,
                    }}
                  />
                  <Th>Name</Th>
                  <Th>Description</Th>
                  <Th>Nodes</Th>
                  <Th>Created</Th>
                  <Th>Actions</Th>
                </Tr>
              </Thead>
              <Tbody>
                {groups.map((group, rowIndex) => (
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
                    <Td dataLabel="Actions">
                      <Flex>
                        <FlexItem>
                          <Button
                            variant="secondary"
                            size="sm"
                            onClick={() => openTokenModal(group)}
                          >
                            Generate Token
                          </Button>
                        </FlexItem>
                        <FlexItem>
                          <Button
                            variant="plain"
                            size="sm"
                            onClick={() => openEditModal(group)}
                          >
                            Edit
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
        title={editingGroup ? "Edit Node Group" : "Create Node Group"}
        isOpen={isFormOpen}
        onClose={() => setIsFormOpen(false)}
        actions={[
          <Button
            key="save"
            variant="primary"
            onClick={handleFormSave}
            isLoading={formSaving}
            isDisabled={formSaving}
          >
            {editingGroup ? "Save" : "Create"}
          </Button>,
          <Button key="cancel" variant="link" onClick={() => setIsFormOpen(false)}>
            Cancel
          </Button>,
        ]}
      >
        {formError && (
          <Alert variant="danger" title={formError} isInline style={{ marginBottom: "1rem" }} />
        )}
        <Form>
          <FormGroup label="Name" isRequired fieldId="ng-name">
            <TextInput
              id="ng-name"
              value={formName}
              onChange={(_ev, val) => setFormName(val)}
              isRequired
              placeholder="e.g. Engineering Desktops"
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
      </Modal>

      {/* ── Delete Confirmation Modal ── */}
      <Modal
        variant={ModalVariant.small}
        title={`Delete Node Group${deleteTargetIds.length !== 1 ? "s" : ""}`}
        titleIconVariant="warning"
        isOpen={deleteModalOpen}
        onClose={() => setDeleteModalOpen(false)}
      >
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
              <Alert variant="warning" title="Some groups have nodes assigned" isInline>
                {withNodes
                  .map((g) => `${g.name} (${g.node_count} node${g.node_count !== 1 ? "s" : ""})`)
                  .join(", ")}
                {" — these nodes will lose their group membership."}
              </Alert>
            );
          })()}
          {deleteError && (
            <Alert variant="danger" title="Error" isInline>{deleteError}</Alert>
          )}
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
      </Modal>

      {/* ── Token Generation Modal ── */}
      <Modal
        variant={ModalVariant.medium}
        title={`Enrollment Token — ${tokenGroup?.name || ""}`}
        isOpen={tokenGroup !== null}
        onClose={() => { setTokenGroup(null); setGeneratedToken(null); }}
        actions={
          generatedToken
            ? [
                <Button
                  key="close"
                  variant="primary"
                  onClick={() => { setTokenGroup(null); setGeneratedToken(null); }}
                >
                  Done
                </Button>,
              ]
            : [
                <Button
                  key="generate"
                  variant="primary"
                  onClick={handleGenerateToken}
                  isLoading={tokenLoading}
                  isDisabled={tokenLoading}
                >
                  Generate Token
                </Button>,
                <Button key="cancel" variant="link" onClick={() => setTokenGroup(null)}>
                  Cancel
                </Button>,
              ]
        }
      >
        {tokenError && (
          <Alert variant="danger" title={tokenError} isInline style={{ marginBottom: "1rem" }} />
        )}
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
      </Modal>
    </>
  );
};
