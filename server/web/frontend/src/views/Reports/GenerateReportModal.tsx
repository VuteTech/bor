// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useState } from "react";
import {
  Button,
  Checkbox,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  MenuToggle,
  MenuToggleElement,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  Radio,
  Select,
  SelectList,
  SelectOption,
  TextArea,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { fetchNodeGroups, NodeGroup } from "../../apiClient/nodeGroupsApi";
import { createReport, ReportSummary } from "../../apiClient/reportsApi";
import { POLICY_TYPE_LIST } from "../../policyTypes/registry";

interface GenerateReportModalProps {
  isOpen: boolean;
  onClose: () => void;
  /** Called with the generated report; the caller navigates and notifies. */
  onGenerated: (report: ReportSummary) => void;
}

const MAX_LABEL = 255;

export const GenerateReportModal: React.FC<GenerateReportModalProps> = ({ isOpen, onClose, onGenerated }) => {
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [groupsError, setGroupsError] = useState<string | null>(null);

  const [allNodes, setAllNodes] = useState(true);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [policyTypes, setPolicyTypes] = useState<string[]>([]);
  const [typesOpen, setTypesOpen] = useState(false);
  const [label, setLabel] = useState("");
  const [notes, setNotes] = useState("");
  const [includeItems, setIncludeItems] = useState(true);

  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setError(null);
    setGroupsError(null);
    let cancelled = false;
    fetchNodeGroups()
      .then((list) => {
        if (!cancelled) setGroups(list);
      })
      .catch((err) => {
        if (!cancelled) setGroupsError(err instanceof Error ? err.message : "Failed to load node groups");
      });
    return () => {
      cancelled = true;
    };
  }, [isOpen]);

  const reset = () => {
    setAllNodes(true);
    setGroupIds([]);
    setPolicyTypes([]);
    setLabel("");
    setNotes("");
    setIncludeItems(true);
    setError(null);
  };

  const close = () => {
    if (submitting) return;
    reset();
    onClose();
  };

  const toggleGroup = (id: string) => {
    setGroupIds((prev) => (prev.includes(id) ? prev.filter((g) => g !== id) : [...prev, id]));
  };

  const toggleType = (id: string) => {
    setPolicyTypes((prev) => (prev.includes(id) ? prev.filter((t) => t !== id) : [...prev, id]));
  };

  const scopeInvalid = !allNodes && groupIds.length === 0;

  const submit = async () => {
    if (scopeInvalid) {
      setError("Select at least one node group, or cover all nodes.");
      return;
    }
    if (label.length > MAX_LABEL) {
      setError(`The label must be at most ${MAX_LABEL} characters.`);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const report = await createReport({
        scope: { all_nodes: allNodes, node_group_ids: allNodes ? [] : groupIds, policy_types: policyTypes },
        label: label.trim(),
        notes: notes.trim(),
        include_items: includeItems,
      });
      reset();
      onGenerated(report);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to generate the report");
    } finally {
      setSubmitting(false);
    }
  };

  const groupName = (id: string) => groups.find((g) => g.id === id)?.name ?? id;

  return (
    <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={close} aria-label="Generate a compliance report">
      <ModalHeader title="Generate report" description="A report records the compliance and posture of the nodes in its scope at this moment. It cannot be edited afterwards." />
      <ModalBody>
        <LiveAlert id="err-report-form" message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
        <LiveAlert message={groupsError} variant="warning" isInline style={{ marginBottom: "1rem" }} />
        <Form onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <FormGroup label="Nodes" isRequired fieldId="report-scope-all" role="radiogroup">
            <Radio
              id="report-scope-all"
              name="report-scope"
              label="All nodes"
              isChecked={allNodes}
              onChange={() => setAllNodes(true)}
            />
            <Radio
              id="report-scope-groups"
              name="report-scope"
              label="Node groups"
              isChecked={!allNodes}
              onChange={() => setAllNodes(false)}
            />
          </FormGroup>
          {!allNodes && (
            <FormGroup label="Node groups" isRequired fieldId="report-groups-toggle">
              <Select
                id="report-groups"
                role="menu"
                isOpen={groupsOpen}
                onOpenChange={setGroupsOpen}
                selected={groupIds}
                onSelect={(_ev, value) => toggleGroup(String(value))}
                toggle={(ref: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    id="report-groups-toggle"
                    ref={ref}
                    onClick={() => setGroupsOpen((v) => !v)}
                    isExpanded={groupsOpen}
                    isFullWidth
                    aria-invalid={error && scopeInvalid ? true : undefined}
                    aria-describedby={error && scopeInvalid ? "err-report-form" : undefined}
                  >
                    {groupIds.length === 0
                      ? "Select node groups"
                      : groupIds.length <= 3
                        ? groupIds.map(groupName).join(", ")
                        : `${groupIds.length} node groups`}
                  </MenuToggle>
                )}
              >
                <SelectList aria-label="Node groups">
                  {groups.length === 0 ? (
                    <SelectOption isDisabled value="">No node groups</SelectOption>
                  ) : (
                    groups.map((g) => (
                      <SelectOption key={g.id} value={g.id} hasCheckbox isSelected={groupIds.includes(g.id)}>
                        {g.name}
                      </SelectOption>
                    ))
                  )}
                </SelectList>
              </Select>
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>Nodes that belong to any of the selected groups are included.</HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
          )}
          <FormGroup label="Policy types" fieldId="report-types-toggle">
            <Select
              id="report-types"
              role="menu"
              isOpen={typesOpen}
              onOpenChange={setTypesOpen}
              selected={policyTypes}
              onSelect={(_ev, value) => toggleType(String(value))}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle
                  id="report-types-toggle"
                  ref={ref}
                  onClick={() => setTypesOpen((v) => !v)}
                  isExpanded={typesOpen}
                  isFullWidth
                >
                  {policyTypes.length === 0
                    ? "All policy types"
                    : `${policyTypes.length} of ${POLICY_TYPE_LIST.length} policy types`}
                </MenuToggle>
              )}
            >
              <SelectList aria-label="Policy types">
                {POLICY_TYPE_LIST.map((t) => (
                  <SelectOption key={t.id} value={t.id} hasCheckbox isSelected={policyTypes.includes(t.id)}>
                    {t.label}
                  </SelectOption>
                ))}
              </SelectList>
            </Select>
            <FormHelperText>
              <HelperText>
                <HelperTextItem>Leave empty to include every policy type.</HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>
          <FormGroup label="Label" fieldId="report-label">
            <TextInput
              id="report-label"
              value={label}
              onChange={(_ev, v) => setLabel(v)}
              maxLength={MAX_LABEL}
              placeholder="e.g. Q4 lab audit"
              aria-invalid={label.length > MAX_LABEL ? true : undefined}
            />
          </FormGroup>
          <FormGroup label="Notes" fieldId="report-notes">
            <TextArea
              id="report-notes"
              value={notes}
              onChange={(_ev, v) => setNotes(v)}
              rows={3}
              placeholder="Why this report was generated, who asked for it, what it will be used for."
            />
          </FormGroup>
          <FormGroup fieldId="report-include-items">
            <Checkbox
              id="report-include-items"
              label="Include per-setting items"
              description="The breakdown of every policy result by setting. Switch it off for scopes where setting keys may be sensitive, such as login mappings or user names; the per-policy status is kept either way."
              isChecked={includeItems}
              onChange={(_ev, checked) => setIncludeItems(checked)}
            />
          </FormGroup>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={() => void submit()} isLoading={submitting} isDisabled={submitting}>
          Generate
        </Button>
        <Button variant="link" onClick={close} isDisabled={submitting}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};
