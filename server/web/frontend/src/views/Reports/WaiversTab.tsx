// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useCallback, useEffect, useState } from "react";
import {
  Button,
  Flex,
  FlexItem,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Label,
  MenuToggle,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  Spinner,
  Switch,
  TextArea,
  TextInput,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import { createWaiver, listWaivers, revokeWaiver, Waiver, WaiverInput } from "../../apiClient/reportsApi";
import { policyTypeLabel } from "../../policyTypes/registry";
import { formatDate } from "./reportsShared";

/** What a new waiver is prefilled with (from a report result). */
export interface WaiverDraft {
  policy_id: string;
  policy_name: string;
  node_id?: string;
  node_name?: string;
  schema_id?: string;
  key?: string;
}

interface WaiverModalProps {
  isOpen: boolean;
  draft: WaiverDraft | null;
  maxDays: number;
  onClose: () => void;
  onCreated: () => void;
}

function isoDate(d: Date): string {
  return d.toISOString().slice(0, 10);
}

/** Records an accepted exception for a failing result or item. */
export const WaiverModal: React.FC<WaiverModalProps> = ({ isOpen, draft, maxDays, onClose, onCreated }) => {
  const [allNodes, setAllNodes] = useState(false);
  const [itemOnly, setItemOnly] = useState(true);
  const [reason, setReason] = useState("");
  const [expires, setExpires] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setAllNodes(false);
    setItemOnly(!!draft?.schema_id);
    setReason("");
    setExpires(isoDate(new Date(Date.now() + 90 * 24 * 3600 * 1000)));
    setError(null);
  }, [isOpen, draft]);

  const reasonInvalid = reason.trim().length < 10;
  const save = async () => {
    if (!draft) return;
    if (reasonInvalid) {
      setError("Explain why the risk is accepted (at least 10 characters).");
      return;
    }
    const body: WaiverInput = {
      policy_id: draft.policy_id,
      node_id: allNodes ? undefined : draft.node_id,
      schema_id: itemOnly ? draft.schema_id : undefined,
      key: itemOnly ? draft.key : undefined,
      reason: reason.trim(),
      expires_at: new Date(`${expires}T23:59:59`).toISOString(),
    };
    setSaving(true);
    setError(null);
    try {
      await createWaiver(body);
      onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to record the waiver");
    } finally {
      setSaving(false);
    }
  };

  const maxDate = isoDate(new Date(Date.now() + maxDays * 24 * 3600 * 1000));
  return (
    <Modal variant={ModalVariant.small} isOpen={isOpen} onClose={() => !saving && onClose()} aria-label="Accept an exception">
      <ModalHeader
        title="Accept an exception"
        description="A waiver records that a failure is a known, accepted risk until a date. Reports generated while it is in force list it and mark the failures it covers; they stay failures in the score."
      />
      <ModalBody>
        <LiveAlert id="err-waiver" message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
        {draft && (
          <Form onSubmit={(e) => { e.preventDefault(); void save(); }}>
            <FormGroup label="Policy" fieldId="waiver-policy">
              <TextInput id="waiver-policy" value={draft.policy_name} readOnlyVariant="default" />
            </FormGroup>
            {draft.node_id && (
              <FormGroup fieldId="waiver-all-nodes">
                <Switch id="waiver-all-nodes" label={`Every node (not only ${draft.node_name ?? "this node"})`} isChecked={allNodes} onChange={(_e, v) => setAllNodes(v)} />
              </FormGroup>
            )}
            {draft.schema_id && (
              <FormGroup fieldId="waiver-item-only">
                <Switch id="waiver-item-only" label={`Only the setting ${draft.schema_id} ${draft.key ?? ""}`.trim()} isChecked={itemOnly} onChange={(_e, v) => setItemOnly(v)} />
              </FormGroup>
            )}
            <FormGroup label="Reason" isRequired fieldId="waiver-reason">
              <TextArea
                id="waiver-reason"
                value={reason}
                onChange={(_e, v) => setReason(v)}
                rows={3}
                aria-invalid={error && reasonInvalid ? true : undefined}
                aria-describedby={error && reasonInvalid ? "err-waiver" : undefined}
                placeholder="Why the risk is accepted, the compensating control, the ticket."
              />
            </FormGroup>
            <FormGroup label="Expires" isRequired fieldId="waiver-expires">
              <TextInput id="waiver-expires" type="date" value={expires} max={maxDate} onChange={(_e, v) => setExpires(v)} />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>At most {maxDays} days from today.</HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
          </Form>
        )}
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={() => void save()} isLoading={saving} isDisabled={saving}>
          Accept exception
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={saving}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};

const STATE_COLOR: Record<string, "green" | "grey" | "orange"> = { active: "green", expired: "grey", revoked: "orange" };

/** The waivers list on the Reports page. New waivers start from a report's results. */
export const WaiversTab: React.FC = () => {
  const { addToast } = useToast();
  const [items, setItems] = useState<Waiver[] | null>(null);
  const [showAll, setShowAll] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<Waiver | null>(null);
  const [busy, setBusy] = useState(false);
  const [confirmError, setConfirmError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setError(null);
      setItems((await listWaivers(showAll)).items);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load waivers");
    }
  }, [showAll]);

  useEffect(() => {
    void load();
  }, [load]);

  const revoke = async () => {
    if (!confirm) return;
    setBusy(true);
    setConfirmError(null);
    try {
      await revokeWaiver(confirm.id);
      addToast({ variant: "success", title: "Waiver revoked" });
      setConfirm(null);
      void load();
    } catch (err) {
      setConfirmError(err instanceof Error ? err.message : "Failed to revoke the waiver");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ paddingTop: "1rem" }}>
      <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }} style={{ marginBottom: "1rem" }}>
        <FlexItem>
          <span className="bor-text-secondary">
            <PlusCircleIcon /> To accept an exception, open a report's Results tab and use "Accept exception" on a failing result.
          </span>
        </FlexItem>
        <FlexItem>
          <FormSelect id="waiver-filter" aria-label="Which waivers" value={showAll ? "all" : "active"} onChange={(_e, v) => setShowAll(v === "all")}>
            <FormSelectOption value="active" label="Active waivers" />
            <FormSelectOption value="all" label="All waivers, including expired and revoked" />
          </FormSelect>
        </FlexItem>
      </Flex>
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      {items === null ? (
        <Spinner size="lg" aria-label="Loading waivers" />
      ) : items.length === 0 ? (
        <BorEmptyState isEmptyData itemsLabel="waivers" emptyTitle="No waivers" emptyBody="No accepted exceptions are recorded." />
      ) : (
        <Table aria-label="Waivers" variant="compact">
          <Thead>
            <Tr>
              <Th>Covers</Th>
              <Th>Reason</Th>
              <Th>Approved</Th>
              <Th>Expires</Th>
              <Th>State</Th>
              <Th screenReaderText="Actions" />
            </Tr>
          </Thead>
          <Tbody>
            {items.map((w) => (
              <Tr key={w.id}>
                <Td dataLabel="Covers">
                  <strong>{w.policy_name || w.policy_id}</strong>{" "}
                  <span className="bor-text-secondary">{policyTypeLabel(w.policy_type)}</span>
                  <br />
                  <small className="bor-text-secondary">
                    {w.node_name ? `Node ${w.node_name}` : "Every node"}
                    {w.schema_id ? `; setting ${w.schema_id} ${w.key ?? ""}` : "; whole result"}
                  </small>
                </Td>
                <Td dataLabel="Reason" style={{ whiteSpace: "pre-wrap" }}>{w.reason}</Td>
                <Td dataLabel="Approved">{w.approved_by}<br /><small className="bor-text-secondary">{formatDate(w.approved_at)}</small></Td>
                <Td dataLabel="Expires">{formatDate(w.expires_at)}</Td>
                <Td dataLabel="State">
                  <Label isCompact color={STATE_COLOR[w.state] ?? "grey"}>{w.state}</Label>
                  {w.revoked_by && <><br /><small className="bor-text-secondary">by {w.revoked_by}</small></>}
                </Td>
                <Td dataLabel="Actions" isActionCell>
                  {w.state === "active" && (
                    <ActionsColumn
                      items={[{ title: "Revoke", isDanger: true, onClick: () => { setConfirmError(null); setConfirm(w); } }]}
                      actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                        <MenuToggle ref={toggleRef} aria-label={`Actions for the waiver of ${w.policy_name}`} variant="plain" onClick={onToggle} isExpanded={isOpen} isDisabled={isDisabled} icon={<EllipsisVIcon />} />
                      )}
                    />
                  )}
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}
      <ConfirmModal
        isOpen={confirm !== null}
        title="Revoke this waiver?"
        isDanger
        confirmLabel="Revoke"
        confirmPhrase="Yes"
        confirmPhraseLabel='Type "Yes" to confirm'
        isBusy={busy}
        error={confirmError}
        onConfirm={() => void revoke()}
        onCancel={() => setConfirm(null)}
      >
        Reports generated from now on no longer mark these failures as accepted. Reports already generated keep the waiver they recorded.
      </ConfirmModal>
    </div>
  );
};
