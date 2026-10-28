// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Report detail tabs beyond the basics: framework controls with a drill-down
 * into the results behind each control, comparison with an earlier report,
 * and attestations (sign-off by someone other than the generator).
 */

import React, { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Flex,
  FlexItem,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Label,
  Spinner,
  TextArea,
  ToggleGroup,
  ToggleGroupItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ExpandableRowContent } from "@patternfly/react-table";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import TimesCircleIcon from "@patternfly/react-icons/dist/esm/icons/times-circle-icon";
import ExclamationCircleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-circle-icon";
import HourglassHalfIcon from "@patternfly/react-icons/dist/esm/icons/hourglass-half-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { useToast } from "../../components/ToastHost";
import {
  attestReport,
  AttestationSummary,
  compareReports,
  ControlFinding,
  fetchAttestations,
  fetchControlFindings,
  fetchReportControls,
  listReports,
  ReportChange,
  ReportComparison,
  ReportControlRow,
  ReportFramework,
  ReportSummary,
} from "../../apiClient/reportsApi";
import { policyTypeLabel } from "../../policyTypes/registry";
import { STATUS_COLORS, STATUS_LABELS, formatDate, formatScore } from "./reportsShared";

/* ── Controls ── */

const ControlFindings: React.FC<{ reportId: string; control: ReportControlRow }> = ({ reportId, control }) => {
  const [rows, setRows] = useState<ControlFinding[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchControlFindings(reportId, control.framework, control.id)
      .then((r) => !cancelled && setRows(r))
      .catch((err) => !cancelled && setError(err instanceof Error ? err.message : "Failed to load findings"));
    return () => {
      cancelled = true;
    };
  }, [reportId, control.framework, control.id]);
  if (error) return <LiveAlert message={error} variant="danger" isInline />;
  if (rows === null) return <Spinner size="md" aria-label="Loading findings" />;
  if (rows.length === 0) return <p className="bor-text-secondary">The report kept no per-setting items for this control.</p>;
  return (
    <Table aria-label={`Results behind ${control.id}`} variant="compact" borders={false}>
      <Thead>
        <Tr>
          <Th>Node</Th>
          <Th>Policy</Th>
          <Th>Setting</Th>
          <Th>Status</Th>
          <Th>Message</Th>
        </Tr>
      </Thead>
      <Tbody>
        {rows.map((f, i) => (
          <Tr key={`${f.node_id}-${f.policy_id}-${f.schema_id ?? ""}-${f.key ?? ""}-${i}`}>
            <Td dataLabel="Node">{f.node}</Td>
            <Td dataLabel="Policy">{f.policy_name} <span className="bor-text-secondary">{policyTypeLabel(f.policy_type)}</span></Td>
            <Td dataLabel="Setting">{f.schema_id ? `${f.schema_id} ${f.key ?? ""}` : <span className="bor-text-secondary">whole policy</span>}</Td>
            <Td dataLabel="Status">
              <Label color={STATUS_COLORS[f.status] ?? "grey"} isCompact>{STATUS_LABELS[f.status] ?? f.status}</Label>
              {f.waiver && <> <Label isCompact color="purple">Waived</Label></>}
              {f.stale && <> <Label isCompact color="orange">Stale</Label></>}
            </Td>
            <Td dataLabel="Message">{f.message || "-"}</Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
};

export const ControlsTab: React.FC<{ reportId: string; frameworks: ReportFramework[] }> = ({ reportId, frameworks }) => {
  const [rows, setRows] = useState<ReportControlRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [framework, setFramework] = useState<string>("");
  const [expanded, setExpanded] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchReportControls(reportId)
      .then((r) => !cancelled && setRows(r))
      .catch((err) => !cancelled && setError(err instanceof Error ? err.message : "Failed to load controls"));
    return () => {
      cancelled = true;
    };
  }, [reportId]);

  const present = useMemo(() => Array.from(new Set((rows ?? []).map((r) => r.framework))), [rows]);
  useEffect(() => {
    if (present.length > 0 && !present.includes(framework)) setFramework(present[0]);
  }, [present, framework]);

  const shortName = (name: string) => frameworks.find((f) => f.name === name)?.short ?? name;
  const visible = (rows ?? []).filter((r) => r.framework === framework);

  return (
    <div style={{ paddingTop: "1rem" }}>
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      {rows === null ? (
        <Spinner size="lg" aria-label="Loading controls" />
      ) : rows.length === 0 ? (
        <BorEmptyState isEmptyData itemsLabel="controls" emptyTitle="No framework controls" emptyBody="None of the results in this report map to a framework control." />
      ) : (
        <>
          <ToggleGroup aria-label="Framework" style={{ marginBottom: "1rem" }}>
            {present.map((f) => (
              <ToggleGroupItem key={f} text={shortName(f)} buttonId={`fw-${f}`} isSelected={f === framework} onChange={() => setFramework(f)} />
            ))}
          </ToggleGroup>
          <p className="bor-text-secondary" style={{ marginBottom: "0.5rem" }}>
            {framework}. Pass and fail count the results and settings that map to each control; expand a control to see them.
          </p>
          <Table aria-label={`Controls of ${framework}`} variant="compact">
            <Thead>
              <Tr>
                <Th screenReaderText="Row expand" />
                <Th>Control</Th>
                <Th>Title</Th>
                <Th>Pass</Th>
                <Th>Fail</Th>
                <Th>Nodes failing</Th>
              </Tr>
            </Thead>
            {visible.map((c, idx) => {
              const key = `${c.framework}|${c.id}`;
              const isExpanded = expanded === key;
              const failed = c.fail + c.error;
              return (
                <Tbody key={key} isExpanded={isExpanded}>
                  <Tr>
                    <Td expand={{ rowIndex: idx, isExpanded, onToggle: () => setExpanded(isExpanded ? null : key), expandId: `control-${idx}` }} />
                    <Td dataLabel="Control"><strong>{c.id}</strong></Td>
                    <Td dataLabel="Title">{c.title}</Td>
                    <Td dataLabel="Pass">{c.pass}</Td>
                    <Td dataLabel="Fail">
                      {failed > 0 ? <Label color="red" isCompact icon={<ExclamationCircleIcon />}>{failed}</Label> : <Label color="green" isCompact icon={<CheckCircleIcon />}>0</Label>}
                    </Td>
                    <Td dataLabel="Nodes failing">{c.nodes_failing}</Td>
                  </Tr>
                  <Tr isExpanded={isExpanded}>
                    <Td colSpan={6} noPadding>
                      <ExpandableRowContent>{isExpanded && <ControlFindings reportId={reportId} control={c} />}</ExpandableRowContent>
                    </Td>
                  </Tr>
                </Tbody>
              );
            })}
          </Table>
        </>
      )}
    </div>
  );
};

/* ── Compare ── */

const ChangesTable: React.FC<{ label: string; rows: ReportChange[]; total: number }> = ({ label, rows, total }) => (
  <>
    <Table aria-label={label} variant="compact">
      <Thead>
        <Tr>
          <Th>Node</Th>
          <Th>Policy</Th>
          <Th>Before</Th>
          <Th>After</Th>
        </Tr>
      </Thead>
      <Tbody>
        {rows.map((c) => (
          <Tr key={`${c.node_id}-${c.policy_id}`}>
            <Td dataLabel="Node">{c.node_name}</Td>
            <Td dataLabel="Policy">{c.policy_name} <span className="bor-text-secondary">{policyTypeLabel(c.policy_type)}</span></Td>
            <Td dataLabel="Before">{c.before ? <Label isCompact color={STATUS_COLORS[c.before] ?? "grey"}>{STATUS_LABELS[c.before] ?? c.before}</Label> : <span className="bor-text-secondary">not in report</span>}</Td>
            <Td dataLabel="After">{c.after ? <Label isCompact color={STATUS_COLORS[c.after] ?? "grey"}>{STATUS_LABELS[c.after] ?? c.after}</Label> : <span className="bor-text-secondary">not in report</span>}</Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
    {total > rows.length && <p className="bor-text-secondary">Showing {rows.length} of {total}.</p>}
  </>
);

export const CompareTab: React.FC<{ report: ReportSummary }> = ({ report }) => {
  const [candidates, setCandidates] = useState<ReportSummary[] | null>(null);
  const [baseId, setBaseId] = useState("");
  const [result, setResult] = useState<ReportComparison | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listReports({ per_page: 100 })
      .then((resp) => {
        if (cancelled) return;
        const earlier = resp.items.filter((r) => r.id !== report.id && r.generated_at < report.generated_at);
        setCandidates(earlier);
        // Default to the latest earlier report over the same scope.
        const same = earlier.find((r) => JSON.stringify(r.scope) === JSON.stringify(report.scope));
        setBaseId((same ?? earlier[0])?.id ?? "");
      })
      .catch((err) => !cancelled && setError(err instanceof Error ? err.message : "Failed to load reports"));
    return () => {
      cancelled = true;
    };
  }, [report]);

  useEffect(() => {
    if (!baseId) {
      setResult(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    compareReports(report.id, baseId)
      .then((r) => !cancelled && setResult(r))
      .catch((err) => !cancelled && setError(err instanceof Error ? err.message : "Failed to compare"))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [report.id, baseId]);

  if (candidates !== null && candidates.length === 0) {
    return <div style={{ paddingTop: "1rem" }}><BorEmptyState isEmptyData itemsLabel="reports" emptyTitle="Nothing to compare with" emptyBody="There is no earlier report to compare this one with." /></div>;
  }
  return (
    <div style={{ paddingTop: "1rem" }}>
      <Form isHorizontal style={{ marginBottom: "1rem", maxWidth: "48rem" }}>
        <FormGroup label="Compare with" fieldId="compare-base">
          <FormSelect id="compare-base" value={baseId} onChange={(_e, v) => setBaseId(v)} isDisabled={candidates === null}>
            {(candidates ?? []).map((r) => (
              <FormSelectOption key={r.id} value={r.id} label={`${formatDate(r.generated_at)} - ${r.label || "(no label)"} (${formatScore(r.score_pct)})`} />
            ))}
          </FormSelect>
        </FormGroup>
      </Form>
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      {loading && <Spinner size="lg" aria-label="Comparing" />}
      {result && !loading && (
        <>
          <Flex spaceItems={{ default: "spaceItemsMd" }} style={{ marginBottom: "1rem" }}>
            <FlexItem><strong>Score</strong> {formatScore(result.base.score_pct)} {"->"} {formatScore(result.target.score_pct)}</FlexItem>
            <FlexItem><Label color={result.new_failures_total > 0 ? "red" : "grey"} isCompact>{result.new_failures_total} new failures</Label></FlexItem>
            <FlexItem><Label color={result.fixed_total > 0 ? "green" : "grey"} isCompact>{result.fixed_total} fixed</Label></FlexItem>
            <FlexItem><Label color="grey" isCompact>{result.still_failing} still failing</Label></FlexItem>
          </Flex>
          {result.base.scope && JSON.stringify(result.base.scope) !== JSON.stringify(result.target.scope) && (
            <LiveAlert message="The two reports cover different scopes; nodes and policies outside the common scope show up as added or removed." variant="info" isInline style={{ marginBottom: "1rem" }} />
          )}
          <Card isCompact style={{ marginBottom: "1rem" }}>
            <CardTitle>New failures</CardTitle>
            <CardBody>{result.new_failures.length ? <ChangesTable label="New failures" rows={result.new_failures} total={result.new_failures_total} /> : <span className="bor-text-secondary">None.</span>}</CardBody>
          </Card>
          <Card isCompact style={{ marginBottom: "1rem" }}>
            <CardTitle>Fixed</CardTitle>
            <CardBody>{result.fixed.length ? <ChangesTable label="Fixed" rows={result.fixed} total={result.fixed_total} /> : <span className="bor-text-secondary">None.</span>}</CardBody>
          </Card>
          <Card isCompact>
            <CardTitle>Scope changes</CardTitle>
            <CardBody>
              <dl>
                <dt><strong>Nodes added</strong></dt>
                <dd>{result.nodes_added.map((n) => n.node_name).join(", ") || "None"}</dd>
                <dt><strong>Nodes removed</strong></dt>
                <dd>{result.nodes_removed.map((n) => n.node_name).join(", ") || "None"}</dd>
                <dt><strong>Policies added</strong></dt>
                <dd>{result.policies_added.map((p) => p.policy_name).join(", ") || "None"}</dd>
                <dt><strong>Policies removed</strong></dt>
                <dd>{result.policies_removed.map((p) => p.policy_name).join(", ") || "None"}</dd>
              </dl>
              <p className="bor-text-secondary" style={{ marginTop: "0.5rem" }}>
                Compared with <Link to={`/reports/${result.base.id}`}>{result.base.label || "the earlier report"}</Link>.
              </p>
            </CardBody>
          </Card>
        </>
      )}
    </div>
  );
};

/* ── Attestations ── */

const STATE_LABEL: Record<string, React.ReactNode> = {
  approved: <Label color="green" icon={<CheckCircleIcon />}>Approved</Label>,
  rejected: <Label color="red" icon={<TimesCircleIcon />}>Rejected</Label>,
  pending: <Label color="grey" icon={<HourglassHalfIcon />}>Awaiting sign-off</Label>,
};

export const AttestationLabel: React.FC<{ state: string }> = ({ state }) => <>{STATE_LABEL[state] ?? <Label>{state}</Label>}</>;

export const AttestationsTab: React.FC<{ report: ReportSummary; canAttest: boolean; onChange: (s: AttestationSummary) => void }> = ({ report, canAttest, onChange }) => {
  const { addToast } = useToast();
  const [summary, setSummary] = useState<AttestationSummary | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [decision, setDecision] = useState<"approved" | "rejected">("approved");
  const [comment, setComment] = useState("");
  const [busy, setBusy] = useState(false);

  const load = React.useCallback(async () => {
    try {
      const s = await fetchAttestations(report.id);
      setSummary(s);
      onChange(s);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load attestations");
    }
  }, [report.id, onChange]);

  useEffect(() => {
    void load();
  }, [load]);

  const isGenerator = !!summary?.self_generated;
  const alreadyAttested = !!summary?.self_attested;
  const commentMissing = decision === "rejected" && comment.trim() === "";

  const submit = async () => {
    if (commentMissing) {
      setError("A rejection needs a comment.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await attestReport(report.id, decision, comment.trim());
      addToast({ variant: "success", title: decision === "approved" ? "Report approved" : "Report rejected" });
      setComment("");
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to record the attestation");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div style={{ paddingTop: "1rem" }}>
      <LiveAlert id="err-attest" message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
      {summary === null ? (
        <Spinner size="lg" aria-label="Loading attestations" />
      ) : (
        <>
          <Flex alignItems={{ default: "alignItemsCenter" }} spaceItems={{ default: "spaceItemsMd" }} style={{ marginBottom: "1rem" }}>
            <FlexItem><AttestationLabel state={summary.state} /></FlexItem>
            <FlexItem className="bor-text-secondary">
              {summary.approvals} of {summary.required_approvals} required {summary.required_approvals === 1 ? "approval" : "approvals"}. Someone other than the person who generated the report signs it off; each decision is a statement naming the report's hash, signed by the server.
            </FlexItem>
          </Flex>
          {summary.items.length > 0 && (
            <Table aria-label="Attestations" variant="compact" style={{ marginBottom: "1rem" }}>
              <Thead>
                <Tr>
                  <Th>Decision</Th>
                  <Th>By</Th>
                  <Th>When</Th>
                  <Th>Comment</Th>
                  <Th>Signature</Th>
                </Tr>
              </Thead>
              <Tbody>
                {summary.items.map((a) => (
                  <Tr key={a.id}>
                    <Td dataLabel="Decision"><AttestationLabel state={a.decision} /></Td>
                    <Td dataLabel="By">{a.attested_by}</Td>
                    <Td dataLabel="When">{formatDate(a.attested_at)}</Td>
                    <Td dataLabel="Comment" style={{ whiteSpace: "pre-wrap" }}>{a.comment || "-"}</Td>
                    <Td dataLabel="Signature">
                      {a.signature === "valid" ? <Label color="green" isCompact icon={<CheckCircleIcon />}>Valid</Label> : <Label color="red" isCompact icon={<ExclamationCircleIcon />}>Invalid</Label>}
                    </Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          )}
          {canAttest && report.kind === "full" && (
            isGenerator ? (
              <p className="bor-text-secondary">You generated this report, so someone else has to sign it off.</p>
            ) : alreadyAttested ? null : (
              <Card isCompact>
                <CardTitle>Your decision</CardTitle>
                <CardBody>
                  <Form onSubmit={(e) => { e.preventDefault(); void submit(); }}>
                    <FormGroup fieldId="attest-decision" label="Decision" role="radiogroup">
                      <ToggleGroup aria-label="Decision">
                        <ToggleGroupItem text="Approve" buttonId="attest-approve" isSelected={decision === "approved"} onChange={() => setDecision("approved")} />
                        <ToggleGroupItem text="Reject" buttonId="attest-reject" isSelected={decision === "rejected"} onChange={() => setDecision("rejected")} />
                      </ToggleGroup>
                    </FormGroup>
                    <FormGroup fieldId="attest-comment" label="Comment" isRequired={decision === "rejected"}>
                      <TextArea
                        id="attest-comment"
                        value={comment}
                        onChange={(_e, v) => setComment(v)}
                        rows={2}
                        aria-invalid={error && commentMissing ? true : undefined}
                        aria-describedby={error && commentMissing ? "err-attest" : undefined}
                      />
                    </FormGroup>
                    <Flex>
                      <FlexItem>
                        <Button variant={decision === "approved" ? "primary" : "danger"} onClick={() => void submit()} isLoading={busy} isDisabled={busy}>
                          {decision === "approved" ? "Approve report" : "Reject report"}
                        </Button>
                      </FlexItem>
                    </Flex>
                  </Form>
                </CardBody>
              </Card>
            )
          )}
          {summary.items.length === 0 && !canAttest && <p className="bor-text-secondary">Nobody has signed this report off yet.</p>}
        </>
      )}
    </div>
  );
};
