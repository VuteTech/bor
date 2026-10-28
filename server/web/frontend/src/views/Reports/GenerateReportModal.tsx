// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useState } from "react";
import {
  Button,
  Checkbox,
  Form,
  FormGroup,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  TextArea,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { fetchNodeGroups, NodeGroup } from "../../apiClient/nodeGroupsApi";
import { createReport, ReportSummary } from "../../apiClient/reportsApi";
import { ScopeFields } from "./ScopeFields";

interface GenerateReportModalProps {
  isOpen: boolean;
  /** Fresh-evidence collection is available on this server. */
  canRefresh?: boolean;
  onClose: () => void;
  /** Called with the generated report; the caller navigates and notifies. */
  onGenerated: (report: ReportSummary) => void;
}

const MAX_LABEL = 255;

export const GenerateReportModal: React.FC<GenerateReportModalProps> = ({ isOpen, canRefresh = false, onClose, onGenerated }) => {
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [groupsError, setGroupsError] = useState<string | null>(null);

  const [allNodes, setAllNodes] = useState(true);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [policyTypes, setPolicyTypes] = useState<string[]>([]);
  const [refresh, setRefresh] = useState(false);
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
    setRefresh(false);
    setError(null);
  };

  const close = () => {
    if (submitting) return;
    reset();
    onClose();
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
        refresh: canRefresh && refresh,
      });
      reset();
      onGenerated(report);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to generate the report");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={close} aria-label="Generate a compliance report">
      <ModalHeader title="Generate report" description="A report records the compliance and posture of the nodes in its scope at this moment. It cannot be edited afterwards." />
      <ModalBody>
        <LiveAlert id="err-report-form" message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
        <LiveAlert message={groupsError} variant="warning" isInline style={{ marginBottom: "1rem" }} />
        <Form onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <ScopeFields
            idPrefix="report"
            groups={groups}
            allNodes={allNodes}
            onAllNodesChange={setAllNodes}
            groupIds={groupIds}
            onGroupIdsChange={setGroupIds}
            policyTypes={policyTypes}
            onPolicyTypesChange={setPolicyTypes}
            errorId="err-report-form"
            groupsInvalid={!!error && scopeInvalid}
          />
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
          {canRefresh && (
            <FormGroup fieldId="report-refresh">
              <Checkbox
                id="report-refresh"
                label="Collect fresh evidence first"
                description="Ask every online agent in scope to re-check its policies, wait for the new results (up to about a minute), then take the snapshot. The report records how many agents answered in time; the others keep their last result."
                isChecked={refresh}
                onChange={(_ev, checked) => setRefresh(checked)}
              />
            </FormGroup>
          )}
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
