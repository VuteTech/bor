// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Shared presentation helpers of the Reports pages: labels and colors of
 * statuses and triggers, date formatting, scope summaries and the download
 * menu that both the list and the detail page offer.
 */

import React from "react";
import { DropdownItem, Label } from "@patternfly/react-core";
import LockIcon from "@patternfly/react-icons/dist/esm/icons/lock-icon";
import UnlockIcon from "@patternfly/react-icons/dist/esm/icons/unlock-icon";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import ExclamationCircleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-circle-icon";
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";

import { ReportSummary, ReportScope } from "../../apiClient/reportsApi";
import { policyTypeLabel } from "../../policyTypes/registry";

export type LabelColor = "green" | "red" | "grey" | "yellow" | "orange" | "blue";

export const STATUS_LABELS: Record<string, string> = {
  unknown: "Unknown",
  compliant: "Compliant",
  non_compliant: "Non-Compliant",
  inapplicable: "Inapplicable",
  error: "Error",
};

export const STATUS_COLORS: Record<string, LabelColor> = {
  unknown: "grey",
  compliant: "green",
  non_compliant: "red",
  inapplicable: "grey",
  error: "yellow",
};

export const RESULT_STATUSES = ["compliant", "non_compliant", "error", "inapplicable", "unknown"];

export const TRIGGER_LABELS: Record<string, string> = {
  manual: "Manual",
  scheduled: "Scheduled",
  daily_summary: "Daily summary",
  api: "API",
};

export function triggerLabel(trigger: string): string {
  return TRIGGER_LABELS[trigger] ?? trigger;
}

export function formatDate(raw?: string | null): string {
  if (!raw) return "-";
  try {
    return new Date(raw).toLocaleString();
  } catch {
    return raw;
  }
}

export function formatScore(score: number | null | undefined): string {
  if (score === null || score === undefined) return "n/a";
  return `${score.toFixed(1)}%`;
}

/** One line describing what a report covered. */
export function scopeSummary(scope: ReportScope): string {
  const where = scope.all_nodes
    ? "All nodes"
    : scope.node_groups.length > 0
      ? scope.node_groups.map((g) => g.name || g.id).join(", ")
      : "No node groups";
  const types = scope.policy_types.length > 0
    ? scope.policy_types.map((t) => policyTypeLabel(t)).join(", ")
    : "all policy types";
  return `${where}; ${types}`;
}

export const SignedLabel: React.FC<{ signed: boolean }> = ({ signed }) =>
  signed ? (
    <Label color="green" isCompact icon={<LockIcon />}>Signed</Label>
  ) : (
    <Label color="grey" isCompact icon={<UnlockIcon />}>Unsigned</Label>
  );

export const HashLabel: React.FC<{ state: string }> = ({ state }) => {
  switch (state) {
    case "ok":
      return <Label color="green" isCompact icon={<CheckCircleIcon />}>Hash verified</Label>;
    case "mismatch":
      return <Label color="red" isCompact icon={<ExclamationCircleIcon />}>Hash mismatch: the stored bundle changed</Label>;
    case "missing":
      return <Label color="red" isCompact icon={<ExclamationCircleIcon />}>Bundle missing</Label>;
    default:
      return <Label color="grey" isCompact>{state}</Label>;
  }
};

export const SignatureLabel: React.FC<{ state: string }> = ({ state }) => {
  switch (state) {
    case "valid":
      return <Label color="green" isCompact icon={<LockIcon />}>Signature valid</Label>;
    case "invalid":
      return <Label color="red" isCompact icon={<ExclamationCircleIcon />}>Signature invalid</Label>;
    case "unverified":
      return <Label color="orange" isCompact icon={<ExclamationTriangleIcon />}>Signed, not verified on this server</Label>;
    case "unsigned":
      return <Label color="grey" isCompact icon={<UnlockIcon />}>Unsigned</Label>;
    default:
      return <Label color="grey" isCompact>{state}</Label>;
  }
};

export interface DownloadChoice {
  key: string;
  label: string;
  format: string;
  part?: string;
}

const BUILT_IN_FORMATS = new Set(["json", "csv", "jws"]);
const CSV_PARTS: { part: string; label: string }[] = [
  { part: "results", label: "results" },
  { part: "nodes", label: "nodes" },
  { part: "items", label: "items" },
  { part: "controls", label: "controls" },
];

/** The download choices a report offers, in menu order. */
export function downloadChoices(report: ReportSummary): DownloadChoice[] {
  const formats = report.formats ?? ["json", "csv"];
  const choices: DownloadChoice[] = [];
  if (formats.includes("json")) {
    choices.push({ key: "json", label: "Download JSON bundle", format: "json" });
  }
  if (formats.includes("csv")) {
    for (const p of CSV_PARTS) {
      choices.push({ key: `csv-${p.part}`, label: `Download CSV: ${p.label}`, format: "csv", part: p.part });
    }
  }
  if (report.signed && formats.includes("jws")) {
    choices.push({ key: "jws", label: "Download signature (JWS)", format: "jws" });
  }
  for (const f of formats) {
    if (!BUILT_IN_FORMATS.has(f)) {
      choices.push({ key: f, label: `Download ${f.toUpperCase()}`, format: f });
    }
  }
  return choices;
}

/** Dropdown items for the download choices of a report. */
export function downloadDropdownItems(
  report: ReportSummary,
  onDownload: (choice: DownloadChoice) => void,
): React.ReactNode[] {
  return downloadChoices(report).map((c) => (
    <DropdownItem key={c.key} onClick={() => onDownload(c)}>
      {c.label}
    </DropdownItem>
  ));
}
