// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ImportReportModal — dry-run preview and result view for policy bundle
 * imports. The user always sees the dry-run report first and confirms before
 * anything is written.
 */

import React from "react";
import {
  Button,
  FormSelect,
  FormSelectOption,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  Spinner,
} from "@patternfly/react-core";
import { Table, Thead, Tbody, Tr, Th, Td } from "@patternfly/react-table";

import { LiveAlert } from "../../components/LiveAlert";
import type { ImportReport, OnConflict } from "../../apiClient/exportApi";

interface ImportReportModalProps {
  isOpen: boolean;
  fileName: string;
  report: ImportReport | null;
  /** true while the dry run or the final import request is in flight */
  isBusy: boolean;
  /** null → previewing a dry run; string → final import finished (message) */
  completedMessage: string | null;
  errorMessage: string | null;
  onConflict: OnConflict;
  onConflictChange: (v: OnConflict) => void;
  onConfirm: () => void;
  onClose: () => void;
}

const statusColor: Record<string, "green" | "teal" | "orange" | "red"> = {
  created: "green",
  updated: "teal",
  skipped: "orange",
  error: "red",
};

export const ImportReportModal: React.FC<ImportReportModalProps> = ({
  isOpen,
  fileName,
  report,
  isBusy,
  completedMessage,
  errorMessage,
  onConflict,
  onConflictChange,
  onConfirm,
  onClose,
}) => (
  <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={onClose}>
    <ModalHeader title={completedMessage ? "Import complete" : `Import preview — ${fileName}`} />
    <ModalBody>
      {errorMessage && (
        <LiveAlert variant="danger" title="Import failed" isInline id="import-error-alert">
          {errorMessage}
        </LiveAlert>
      )}
      {completedMessage && (
        <LiveAlert variant="success" title={completedMessage} isInline />
      )}
      {isBusy && (
        <div style={{ display: "flex", alignItems: "center", gap: "0.5rem", marginBottom: "1rem" }}>
          <Spinner size="md" aria-label="Loading" />
          <span>Validating bundle…</span>
        </div>
      )}
      {report && !completedMessage && (
        <p style={{ marginBottom: "0.75rem" }}>
          {report.ok
            ? "The bundle is valid. Review the planned changes below — nothing has been imported yet."
            : "The bundle was rejected. Fix the errors below and try again; no changes were made."}
        </p>
      )}
      {report && (
        <div style={{ overflowX: "auto" }}>
          <Table aria-label="Import report" variant="compact">
            <Thead>
              <Tr>
                <Th width={10}>Doc</Th>
                <Th width={20}>Kind</Th>
                <Th width={30}>Name</Th>
                <Th width={10}>Status</Th>
                <Th width={30}>Details</Th>
              </Tr>
            </Thead>
            <Tbody>
              {report.results.map((r) => (
                <Tr key={`${r.doc}-${r.kind}-${r.name}`}>
                  <Td dataLabel="Doc">{r.doc}</Td>
                  <Td dataLabel="Kind">{r.kind || "—"}</Td>
                  <Td dataLabel="Name">{r.name || "—"}</Td>
                  <Td dataLabel="Status">
                    <Label isCompact color={statusColor[r.status] ?? "grey"}>
                      {r.status}
                    </Label>
                  </Td>
                  <Td dataLabel="Details">{r.message || ""}</Td>
                </Tr>
              ))}
            </Tbody>
          </Table>
        </div>
      )}
      {report && !completedMessage && (
        <div style={{ marginTop: "1rem", maxWidth: "24rem" }}>
          <label htmlFor="import-on-conflict" style={{ display: "block", marginBottom: "0.25rem", fontWeight: 600 }}>
            If a policy name already exists
          </label>
          {/* Changing the mode re-runs the dry run so the preview always
              reflects the selected behavior. */}
          <FormSelect
            id="import-on-conflict"
            value={onConflict}
            onChange={(_ev, v) => onConflictChange(v as OnConflict)}
            isDisabled={isBusy}
          >
            <FormSelectOption value="error" label="Fail the import (recommended)" />
            <FormSelectOption value="skip" label="Skip that policy" />
            <FormSelectOption value="new-version" label="Update the existing draft" />
          </FormSelect>
        </div>
      )}
    </ModalBody>
    <ModalFooter>
      {report && !completedMessage && report.ok && (
        <Button variant="primary" onClick={onConfirm} isDisabled={isBusy}>
          Import {report.created + report.updated} resource{report.created + report.updated !== 1 ? "s" : ""}
        </Button>
      )}
      <Button variant="link" onClick={onClose}>
        {completedMessage ? "Close" : "Cancel"}
      </Button>
    </ModalFooter>
  </Modal>
);
