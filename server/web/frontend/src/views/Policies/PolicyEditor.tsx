// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useRef } from "react";
import {
  Modal,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalVariant,
  PageSection,
  Button,
  Tabs,
  Tab,
  TabTitleText,
  Form,
  FormGroup,
  TextInput,
  TextArea,
  FormSelect,
  FormSelectOption,
  DescriptionList,
  DescriptionListGroup,
  DescriptionListTerm,
  DescriptionListDescription,
  Alert,
  Label,
  Title,
  Flex,
  FlexItem,
  Card,
  CardBody,
  CardTitle,
} from "@patternfly/react-core";
import { Table, Thead, Tbody, Tr, Th, Td } from "@patternfly/react-table";

import type { Policy, UpdatePolicyRequest } from "../../apiClient/policiesApi";
import { updatePolicy, setPolicyState, deletePolicy } from "../../apiClient/policiesApi";
import { LiveAlert } from "../../components/LiveAlert";
import { PolicyTypeIcon } from "../../components/PolicyTypeIcon";
import {
  POLICY_TYPE_LIST,
  defaultContentForType,
  policyTypeLabel,
  validatePolicyContent,
} from "../../policyTypes/registry";
import { PolicyContentEditor } from "./editors/PolicyContentEditor";
import { normalizePolicyContent } from "./editors/policyContentJson";
import { buildSettingsRows } from "./policySummary";

/* ── Props ── */

interface PolicyEditorProps {
  onClose: () => void;
  onSaved: () => void;
  onDeleted?: () => void;
  policy: Policy;
}

/* ── Component ── */

/**
 * The policy edit page: lifecycle bar, Overview / Details / Configuration tabs
 * and the save/delete footer. The per-type configuration UI itself lives
 * behind `PolicyContentEditor`; this component only owns the policy envelope
 * (name, description, type, state) and the content string. Creation is the
 * wizard at `/policies/new` (`wizard/PolicyCreateWizard`).
 */
export const PolicyEditor: React.FC<PolicyEditorProps> = ({
  onClose,
  onSaved,
  onDeleted,
  policy,
}) => {
  // Form state, seeded from the loaded policy so child editors see the real
  // content on their first render (they initialise their selection from it).
  const [name, setName] = useState(policy.name);
  const [description, setDescription] = useState(policy.description);
  const [policyType, setPolicyType] = useState(policy.type);
  const [status, setStatus] = useState(policy.state);
  const [contentRaw, setContentRaw] = useState(policy.content || "{}");
  const [activeTab, setActiveTab] = useState(0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  // Track whether the policy was modified in this session (state transition or save)
  const [dirty, setDirty] = useState(false);

  // Baseline of the last-persisted form values, captured on open, on type change,
  // and after a save. Comparing the live form against it detects unsaved edits so
  // we can warn before discarding them (close/Escape) and block lifecycle
  // transitions that would otherwise silently drop them.
  const baselineRef = useRef({
    name: policy.name,
    description: policy.description,
    policyType: policy.type,
    content: normalizePolicyContent(policy.content || "{}"),
  });
  const [showDiscardConfirm, setShowDiscardConfirm] = useState(false);
  // Pending policy-type switch awaiting confirmation (it would clear configured content).
  const [pendingTypeChange, setPendingTypeChange] = useState<string | null>(null);
  // Whether the content editor currently holds something that cannot be saved
  // (e.g. a JSON field mid-edit). Reported by the editor; `setState` is stable,
  // so it can be passed straight down as the callback.
  const [contentValid, setContentValid] = useState(true);

  // Derived: whether policy fields are editable (DRAFT state only)
  const isEditable = status === "draft";

  // Derived: does the live form differ from the last-persisted baseline?
  const hasUnsavedChanges =
    name !== baselineRef.current.name ||
    description !== baselineRef.current.description ||
    policyType !== baselineRef.current.policyType ||
    normalizePolicyContent(contentRaw) !== baselineRef.current.content;

  // Initialize the form from the target policy. The editor is mounted fresh by
  // its route for each edit, so this runs on mount (and if `policy` changes
  // underneath it).
  useEffect(() => {
    setName(policy.name);
    setDescription(policy.description);
    setPolicyType(policy.type);
    setStatus(policy.state);
    setContentRaw(policy.content || "{}");
    setActiveTab(0);
    setError(null);
    setShowDeleteConfirm(false);
    setDirty(false);
    setShowDiscardConfirm(false);
    setPendingTypeChange(null);
    setContentValid(true);
    // Capture the persisted baseline these setters just established so
    // hasUnsavedChanges reads false until the user actually edits something.
    baselineRef.current = {
      name: policy.name,
      description: policy.description,
      policyType: policy.type,
      content: normalizePolicyContent(policy.content || "{}"),
    };
  }, [policy]);

  // Whether the content holds anything beyond the type's empty default — the
  // thing a type switch would throw away.
  const contentHasEdits = normalizePolicyContent(contentRaw) !== normalizePolicyContent(defaultContentForType(policyType));

  // When policy type changes, reset content for the target type.
  const applyTypeChange = (newType: string) => {
    setPolicyType(newType);
    setContentValid(true);
    setContentRaw(defaultContentForType(newType));
    // Content is intentionally reset for the new type, so move the content/type
    // baseline with it — switching type shouldn't count as unsaved work. Any
    // name/description the user already entered stays in the baseline comparison.
    baselineRef.current = {
      ...baselineRef.current,
      policyType: newType,
      content: normalizePolicyContent(defaultContentForType(newType)),
    };
  };

  // Type-change entry point: switching type clears the configured content for
  // the current type, so confirm first when there is configured content to lose.
  const handleTypeChange = (newType: string) => {
    if (newType === policyType) return;
    if (contentHasEdits) {
      setPendingTypeChange(newType);
      return;
    }
    applyTypeChange(newType);
  };

  /* ── Save handler ── */
  const handleSave = async () => {
    setError(null);
    if (!contentValid) {
      setError("Fix the invalid JSON value before saving.");
      return;
    }
    const validationMessage = validatePolicyContent(policyType, contentRaw);
    if (validationMessage) {
      setError(validationMessage);
      return;
    }
    setSaving(true);

    try {
      const req: UpdatePolicyRequest = { name, description, type: policyType, content: contentRaw };
      await updatePolicy(policy.id, req);

      setDirty(false);
      // The just-saved values are now the persisted baseline.
      baselineRef.current = { name, description, policyType, content: normalizePolicyContent(contentRaw) };
      onSaved();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save policy");
    } finally {
      setSaving(false);
    }
  };

  /* ── State transition handler ── */
  const handleStateTransition = async (newState: string) => {
    try {
      setSaving(true);
      setError(null);
      const updated = await setPolicyState(policy.id, { state: newState });
      setStatus(updated.state);
      setDirty(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to change policy state");
    } finally {
      setSaving(false);
    }
  };

  /* ── Delete handler ── */
  const handleDelete = async () => {
    try {
      setSaving(true);
      setError(null);
      await deletePolicy(policy.id);
      setShowDeleteConfirm(false);
      if (onDeleted) onDeleted();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete policy");
      setShowDeleteConfirm(false);
    } finally {
      setSaving(false);
    }
  };

  /* ── Close handlers ── */
  // Actually close: refresh the parent list if a state transition happened.
  const finalizeClose = () => {
    setShowDiscardConfirm(false);
    if (dirty) onSaved();
    onClose();
  };

  // Close entry point (X button, Escape, Cancel): warn first if there are
  // unsaved edits so they aren't silently discarded.
  const handleClose = () => {
    if (hasUnsavedChanges) {
      setShowDiscardConfirm(true);
      return;
    }
    finalizeClose();
  };

  /* ── Overview summary tab (read-only, edit mode only) ── */
  const renderOverviewSummaryTab = () => {
    const rows = buildSettingsRows(policyType, contentRaw);
    const hasLockedColumn = rows.some((r) => r.locked !== null);

    return (
      <div style={{ padding: "1rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "1rem" }}>Policy Settings</Title>
        {rows.length > 0 ? (
          <Table aria-label="Policy settings summary" variant="compact">
            <Thead>
              <Tr>
                <Th>Setting</Th>
                <Th>Value</Th>
                {hasLockedColumn && <Th>Locked</Th>}
              </Tr>
            </Thead>
            <Tbody>
              {rows.map((row) => (
                <Tr key={row.setting}>
                  <Td dataLabel="Setting">{row.setting}</Td>
                  <Td dataLabel="Value">{row.value}</Td>
                  {hasLockedColumn && (
                    <Td dataLabel="Locked">
                      {row.locked !== null ? (
                        <Label color={row.locked === "Yes" ? "orange" : "grey"} isCompact>
                          {row.locked}
                        </Label>
                      ) : (
                        "—"
                      )}
                    </Td>
                  )}
                </Tr>
              ))}
            </Tbody>
          </Table>
        ) : (
          <Alert variant="info" isInline title="No settings configured">
            This policy does not have any settings configured yet.
          </Alert>
        )}

        {description && (
          <Card isPlain isCompact style={{ marginTop: "1.5rem" }}>
            <CardTitle>Description</CardTitle>
            <CardBody>{description}</CardBody>
          </Card>
        )}
      </div>
    );
  };

  /* ── Details tab ── */
  const renderDetailsTab = () => (
    <div style={{ padding: "1rem 0" }}>
      <Form>
        <FormGroup label="Name" isRequired fieldId="policy-name">
          <TextInput
            id="policy-name"
            value={name}
            onChange={(_ev, val) => setName(val)}
            placeholder="Enter policy name"
            isRequired
            isDisabled={!isEditable}
          />
        </FormGroup>
        <FormGroup label="Description" fieldId="policy-description">
          <TextArea
            id="policy-description"
            value={description}
            onChange={(_ev, val) => setDescription(val)}
            rows={3}
            placeholder="Describe this policy"
            isDisabled={!isEditable}
          />
        </FormGroup>
        <FormGroup label="Type" isRequired fieldId="policy-type">
          <Flex alignItems={{ default: "alignItemsCenter" }} spaceItems={{ default: "spaceItemsSm" }} flexWrap={{ default: "nowrap" }}>
            <FlexItem style={{ color: "var(--pf-t--global--icon--color--regular)" }}>
              <PolicyTypeIcon type={policyType} size="md" />
            </FlexItem>
            <FlexItem grow={{ default: "grow" }}>
              <FormSelect
                id="policy-type"
                value={policyType}
                onChange={(_ev, val) => handleTypeChange(val)}
                isDisabled={!isEditable}
              >
                {POLICY_TYPE_LIST.map((t) => (
                  <FormSelectOption
                    key={t.id}
                    value={t.id}
                    label={t.technicalName ? `${t.label} (${t.technicalName})` : t.label}
                  />
                ))}
                {!POLICY_TYPE_LIST.some((t) => t.id === policyType) && (
                  <FormSelectOption key={policyType} value={policyType} label={policyTypeLabel(policyType)} />
                )}
              </FormSelect>
            </FlexItem>
          </Flex>
        </FormGroup>
        <FormGroup label="State" fieldId="policy-status">
          {/* Lifecycle buttons live in the status bar above the tabs. */}
          <Label color={status === "released" ? "green" : status === "archived" ? "red" : "blue"}>
            {status.charAt(0).toUpperCase() + status.slice(1)}
          </Label>
        </FormGroup>

        <DescriptionList isHorizontal style={{ marginTop: "1rem" }}>
            <DescriptionListGroup>
              <DescriptionListTerm>Version</DescriptionListTerm>
              <DescriptionListDescription>v{policy.version} (auto-incremented on save)</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Created by</DescriptionListTerm>
              <DescriptionListDescription>{policy.created_by || "—"}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Last modified</DescriptionListTerm>
              <DescriptionListDescription>{new Date(policy.updated_at).toLocaleString()}</DescriptionListDescription>
            </DescriptionListGroup>
            <DescriptionListGroup>
              <DescriptionListTerm>Created at</DescriptionListTerm>
              <DescriptionListDescription>{new Date(policy.created_at).toLocaleString()}</DescriptionListDescription>
            </DescriptionListGroup>
          </DescriptionList>
      </Form>
    </div>
  );

  /* ── Configuration tab ── */
  const renderConfigurationTab = () => (
    <PolicyContentEditor
      key={policy.id}
      type={policyType}
      contentRaw={contentRaw}
      onChange={setContentRaw}
      isDisabled={!isEditable}
      onValidityChange={setContentValid}
    />
  );

  const editorTitle = `Edit Policy: ${policy.name}`;
  const canRelease = validatePolicyContent(policyType, contentRaw) === null;

  const editorBody = (
    <>
      <LiveAlert message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />

      {/* Lifecycle status bar — visible on every tab */}
      <Flex
          alignItems={{ default: "alignItemsCenter" }}
          spaceItems={{ default: "spaceItemsSm" }}
          style={{ marginBottom: "1rem" }}
        >
          <FlexItem>
            <Label color={status === "released" ? "green" : status === "archived" ? "red" : "blue"}>
              {status.charAt(0).toUpperCase() + status.slice(1)}
            </Label>
          </FlexItem>
          {status === "draft" && (
            <FlexItem>
              <Button
                variant="primary"
                size="sm"
                onClick={() => handleStateTransition("released")}
                isLoading={saving}
                isDisabled={saving || !name.trim() || !policyType || !canRelease || hasUnsavedChanges}
              >
                Release
              </Button>
            </FlexItem>
          )}
          {status === "released" && (
            <>
              <FlexItem>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => handleStateTransition("draft")}
                  isLoading={saving}
                  isDisabled={saving || hasUnsavedChanges}
                >
                  Unpublish
                </Button>
              </FlexItem>
              <FlexItem>
                <Button
                  variant="warning"
                  size="sm"
                  onClick={() => handleStateTransition("archived")}
                  isLoading={saving}
                  isDisabled={saving || hasUnsavedChanges}
                >
                  Archive
                </Button>
              </FlexItem>
            </>
          )}
          {status === "archived" && (
            <FlexItem>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => handleStateTransition("draft")}
                isLoading={saving}
                isDisabled={saving || hasUnsavedChanges}
              >
                Restore to draft
              </Button>
            </FlexItem>
          )}
          {hasUnsavedChanges && (
            <FlexItem>
              <span className="bor-text-secondary">Unsaved changes — save to enable lifecycle actions.</span>
            </FlexItem>
          )}
        </Flex>

      <Tabs
        activeKey={activeTab}
        onSelect={(_ev, key) => setActiveTab(key as number)}
        aria-label="Policy details tabs"
      >
        <Tab eventKey={0} title={<TabTitleText>Overview</TabTitleText>}>
          {renderOverviewSummaryTab()}
        </Tab>
        <Tab eventKey={1} title={<TabTitleText>Details</TabTitleText>}>
          {renderDetailsTab()}
        </Tab>
        <Tab eventKey={2} title={<TabTitleText>Configuration</TabTitleText>}>
          {!isEditable ? (
            // Released/archived policies are read-only: show the configuration
            // but disable every form control via a disabled fieldset. Tree
            // navigation still works so the configured settings can be browsed;
            // nothing can be edited or saved.
            <>
              <Alert
                variant="info"
                isInline
                title={`This policy is ${status} and its configuration is read-only. Move it back to draft to make changes.`}
                style={{ margin: "1rem 0" }}
              />
              <fieldset disabled style={{ border: "none", margin: 0, padding: 0, minWidth: 0, minInlineSize: "auto" }}>
                {renderConfigurationTab()}
              </fieldset>
            </>
          ) : (
            renderConfigurationTab()
          )}
        </Tab>
      </Tabs>
    </>
  );

  const editorFooter = (
    <>
      {isEditable && (
        <Button
          key="save"
          variant="primary"
          onClick={handleSave}
          isLoading={saving}
          isDisabled={saving || !name.trim() || !contentValid}
        >
          Save Changes
        </Button>
      )}
      <Button key="delete" variant="danger" onClick={() => setShowDeleteConfirm(true)} isDisabled={saving}>
        Delete
      </Button>
      <Button key="cancel" variant="link" onClick={handleClose}>
        Cancel
      </Button>
    </>
  );

  return (
    <>
      <PageSection isFilled style={{ display: "flex", flexDirection: "column", padding: "1.5rem 2rem" }}>
        {/* The Shell's PageHeader already renders the page <h1>; this is the section heading. */}
        <Title headingLevel="h2" size="2xl" style={{ marginBottom: "1rem" }}>
          {editorTitle}
        </Title>
        <div style={{ flex: 1, minHeight: 0, overflowY: "auto" }}>{editorBody}</div>
        <div
          style={{
            display: "flex",
            gap: "0.5rem",
            marginTop: "1.5rem",
            paddingTop: "1rem",
            borderTop: "1px solid var(--pf-t--global--border--color--default)",
          }}
        >
          {editorFooter}
        </div>
      </PageSection>

      {/* Discard unsaved changes confirmation */}
      <Modal variant={ModalVariant.small} isOpen={showDiscardConfirm} onClose={() => setShowDiscardConfirm(false)}>
        <ModalHeader title="Discard unsaved changes?" titleIconVariant="warning" />
        <ModalBody>
          You have unsaved changes to this policy. If you close now, those changes will be lost.
        </ModalBody>
        <ModalFooter>
          <Button key="discard" variant="danger" onClick={finalizeClose}>
            Discard changes
          </Button>
          <Button key="keep" variant="link" onClick={() => setShowDiscardConfirm(false)}>
            Keep editing
          </Button>
        </ModalFooter>
      </Modal>

      {/* Confirm a policy-type switch that would clear configured content */}
      <Modal variant={ModalVariant.small} isOpen={pendingTypeChange !== null} onClose={() => setPendingTypeChange(null)}>
        <ModalHeader title="Change policy type?" titleIconVariant="warning" />
        <ModalBody>
          Switching to <strong>{pendingTypeChange ? policyTypeLabel(pendingTypeChange) : ""}</strong> will clear the
          settings you&rsquo;ve configured for <strong>{policyTypeLabel(policyType)}</strong>. This can&rsquo;t be undone.
        </ModalBody>
        <ModalFooter>
          <Button
            key="change-type"
            variant="danger"
            onClick={() => {
              if (pendingTypeChange) applyTypeChange(pendingTypeChange);
              setPendingTypeChange(null);
            }}
          >
            Change type and clear settings
          </Button>
          <Button key="keep-type" variant="link" onClick={() => setPendingTypeChange(null)}>
            Cancel
          </Button>
        </ModalFooter>
      </Modal>

      {/* Delete confirmation dialog */}
      <Modal variant={ModalVariant.small} isOpen={showDeleteConfirm} onClose={() => setShowDeleteConfirm(false)}>
        <ModalHeader title="Delete Policy" />
        <ModalBody>
          Are you sure you want to delete the policy <strong>{name}</strong>? This action cannot be undone. All
          associated bindings will also be removed.
        </ModalBody>
        <ModalFooter>
          <Button key="confirm-delete" variant="danger" onClick={handleDelete} isLoading={saving} isDisabled={saving}>
            Delete
          </Button>
          <Button key="cancel-delete" variant="link" onClick={() => setShowDeleteConfirm(false)}>
            Cancel
          </Button>
        </ModalFooter>
      </Modal>
    </>
  );
};
