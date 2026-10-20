// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  ClipboardCopy,
  ClipboardCopyVariant,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  Dropdown,
  DropdownList,
  Flex,
  FlexItem,
  Label,
  LabelGroup,
  MenuToggle,
  MenuToggleElement,
  PageSection,
  Pagination,
  Select,
  SelectList,
  SelectOption,
  Spinner,
  Tab,
  TabTitleText,
  Tabs,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ExpandableRowContent } from "@patternfly/react-table";
import ArrowLeftIcon from "@patternfly/react-icons/dist/esm/icons/arrow-left-icon";
import DownloadIcon from "@patternfly/react-icons/dist/esm/icons/download-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { BorToolbar } from "../../components/BorToolbar";
import { StatCard } from "../../components/StatCard";
import { useToast } from "../../components/ToastHost";
import { ComplianceItem, ComplianceStatus } from "../../apiClient/dconfApi";
import {
  downloadReport,
  fetchReportNodes,
  fetchReportPolicies,
  fetchReportResults,
  getReport,
  ReportItem,
  ReportNodeRow,
  ReportPolicyRow,
  ReportResultRow,
  ReportStatusCounts,
  ReportSummary,
} from "../../apiClient/reportsApi";
import { policyTypeLabel } from "../../policyTypes/registry";
import { ItemsTable } from "../Compliance/CompliancePage";
import {
  DownloadChoice,
  HashLabel,
  RESULT_STATUSES,
  STATUS_COLORS,
  STATUS_LABELS,
  SignatureLabel,
  downloadDropdownItems,
  formatDate,
  formatScore,
  triggerLabel,
} from "./reportsShared";

const EMPTY_SUMMARY_INDEX = new Map<string, string>();

/** The per-item breakdown of the Compliance page reads its item shape. */
function toComplianceItems(items: ReportItem[]): ComplianceItem[] {
  return items.map((it) => ({
    schema_id: it.schema_id,
    key: it.key,
    status: it.status as ComplianceStatus,
    message: it.message,
    controls: it.controls?.map((c) => ({ framework: c.framework, id: c.id, title: "", short: c.id })),
  }));
}

const CountLabels: React.FC<{ counts: ReportStatusCounts }> = ({ counts }) => (
  <LabelGroup aria-label="Results by status" isCompact>
    <Label color="green" isCompact>{counts.compliant} compliant</Label>
    {counts.non_compliant > 0 && <Label color="red" isCompact>{counts.non_compliant} non-compliant</Label>}
    {counts.error > 0 && <Label color="yellow" isCompact>{counts.error} error</Label>}
    {counts.inapplicable > 0 && <Label color="grey" isCompact>{counts.inapplicable} inapplicable</Label>}
    {counts.unknown > 0 && <Label color="grey" isCompact>{counts.unknown} unknown</Label>}
  </LabelGroup>
);

/* ── Overview tab ── */

const OverviewTab: React.FC<{ reportId: string; notes: string }> = ({ reportId, notes }) => {
  const [policies, setPolicies] = useState<ReportPolicyRow[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchReportPolicies(reportId)
      .then((rows) => {
        if (!cancelled) setPolicies(rows);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load policies");
      });
    return () => {
      cancelled = true;
    };
  }, [reportId]);

  return (
    <div style={{ paddingTop: "1rem" }}>
      {notes && (
        <Card isCompact style={{ marginBottom: "1rem" }}>
          <CardTitle>Notes</CardTitle>
          <CardBody style={{ whiteSpace: "pre-wrap" }}>{notes}</CardBody>
        </Card>
      )}
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      {policies === null ? (
        <Spinner size="lg" aria-label="Loading policies" />
      ) : policies.length === 0 ? (
        <BorEmptyState isEmptyData itemsLabel="policies" emptyTitle="No policy results" emptyBody="No node in scope had a result for any policy when the report was generated." />
      ) : (
        <Table aria-label="Policies in this report" variant="compact">
          <Thead>
            <Tr>
              <Th>Policy</Th>
              <Th>Type</Th>
              <Th>Version</Th>
              <Th>Nodes</Th>
              <Th>Compliant</Th>
              <Th>Non-compliant</Th>
              <Th>Error</Th>
              <Th>Inapplicable</Th>
            </Tr>
          </Thead>
          <Tbody>
            {policies.map((p) => (
              <Tr key={p.policy_id}>
                <Td dataLabel="Policy">{p.policy_name}</Td>
                <Td dataLabel="Type">{policyTypeLabel(p.policy_type)}</Td>
                <Td dataLabel="Version">{p.policy_version}</Td>
                <Td dataLabel="Nodes">{p.nodes}</Td>
                <Td dataLabel="Compliant">{p.by_status.compliant}</Td>
                <Td dataLabel="Non-compliant">
                  {p.by_status.non_compliant > 0 ? <Label color="red" isCompact>{p.by_status.non_compliant}</Label> : 0}
                </Td>
                <Td dataLabel="Error">
                  {p.by_status.error > 0 ? <Label color="yellow" isCompact>{p.by_status.error}</Label> : 0}
                </Td>
                <Td dataLabel="Inapplicable">{p.by_status.inapplicable}</Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}
    </div>
  );
};

/* ── Nodes tab ── */

const NODE_STATUSES = ["online", "offline", "unknown"];

const NodesTab: React.FC<{ reportId: string }> = ({ reportId }) => {
  const [rows, setRows] = useState<ReportNodeRow[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("All");
  const [statusOpen, setStatusOpen] = useState(false);
  const [reach, setReach] = useState("All");
  const [reachOpen, setReachOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const resp = await fetchReportNodes(reportId, {
        page,
        per_page: perPage,
        q: search || undefined,
        status: status !== "All" ? status : undefined,
        unreachable: reach === "All" ? undefined : reach === "unreachable",
      });
      setRows(resp.items);
      setTotal(resp.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load nodes");
    } finally {
      setLoading(false);
    }
  }, [reportId, page, perPage, search, status, reach]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    setPage(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search, status, reach]);

  const clearAll = () => {
    setSearch("");
    setStatus("All");
    setReach("All");
  };

  const yesNo = (v: boolean | undefined, yes: string, no: string) => (v === undefined ? "-" : v ? yes : no);

  return (
    <div style={{ paddingTop: "1rem" }}>
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      <BorToolbar searchValue={search} onSearchChange={setSearch} searchAriaLabel="Search nodes in the report" searchPlaceholder="Search by node name" onClearAll={clearAll}>
        <ToolbarItem>
          <Select
            id="report-node-status-filter"
            isOpen={statusOpen}
            onOpenChange={setStatusOpen}
            selected={status}
            onSelect={(_ev, val) => { setStatus(String(val)); setStatusOpen(false); }}
            toggle={(ref: React.Ref<MenuToggleElement>) => (
              <MenuToggle ref={ref} onClick={() => setStatusOpen((v) => !v)} isExpanded={statusOpen} aria-label="Filter by node status">
                Status: {status}
              </MenuToggle>
            )}
          >
            <SelectList>
              <SelectOption value="All">All</SelectOption>
              {NODE_STATUSES.map((s) => <SelectOption key={s} value={s}>{s}</SelectOption>)}
            </SelectList>
          </Select>
        </ToolbarItem>
        <ToolbarItem>
          <Select
            id="report-node-reach-filter"
            isOpen={reachOpen}
            onOpenChange={setReachOpen}
            selected={reach}
            onSelect={(_ev, val) => { setReach(String(val)); setReachOpen(false); }}
            toggle={(ref: React.Ref<MenuToggleElement>) => (
              <MenuToggle ref={ref} onClick={() => setReachOpen((v) => !v)} isExpanded={reachOpen} aria-label="Filter by reachability">
                {reach === "All" ? "Reachability: All" : reach === "unreachable" ? "Unreachable only" : "Reachable only"}
              </MenuToggle>
            )}
          >
            <SelectList>
              <SelectOption value="All">All</SelectOption>
              <SelectOption value="unreachable">Unreachable only</SelectOption>
              <SelectOption value="reachable">Reachable only</SelectOption>
            </SelectList>
          </Select>
        </ToolbarItem>
      </BorToolbar>
      {loading ? (
        <Spinner size="lg" aria-label="Loading nodes" />
      ) : rows.length === 0 ? (
        <BorEmptyState isEmptyData={total === 0 && search === "" && status === "All" && reach === "All"} itemsLabel="nodes" emptyTitle="No nodes in this report" emptyBody="The scope contained no nodes when the report was generated." onClearFilters={clearAll} />
      ) : (
        <>
          <Table aria-label="Nodes in this report" variant="compact">
            <Thead>
              <Tr>
                <Th>Node</Th>
                <Th>Node groups</Th>
                <Th>Status</Th>
                <Th>Last seen</Th>
                <Th>Disk encryption</Th>
                <Th>Recovery key</Th>
                <Th>Security updates</Th>
                <Th>Reboot</Th>
                <Th>Agent</Th>
                <Th>Results</Th>
              </Tr>
            </Thead>
            <Tbody>
              {rows.map((n) => (
                <Tr key={n.node_id}>
                  <Td dataLabel="Node">
                    <strong>{n.node_name}</strong>
                    {(n.os_name || n.lsm) && (
                      <>
                        <br />
                        <small className="bor-text-secondary">
                          {[n.os_name && `${n.os_name} ${n.os_version ?? ""}`.trim(), n.lsm && n.lsm !== "none" && `${n.lsm} ${n.lsm_mode ?? ""}`.trim()].filter(Boolean).join(", ")}
                        </small>
                      </>
                    )}
                  </Td>
                  <Td dataLabel="Node groups">{n.groups.length > 0 ? n.groups.join(", ") : "-"}</Td>
                  <Td dataLabel="Status">
                    <Label color={n.status === "online" ? "green" : "grey"} isCompact>{n.status}</Label>
                    {n.unreachable && (
                      <>
                        {" "}
                        <Label color="orange" isCompact>Unreachable</Label>
                      </>
                    )}
                  </Td>
                  <Td dataLabel="Last seen">{formatDate(n.last_seen)}</Td>
                  <Td dataLabel="Disk encryption">{yesNo(n.encrypted, "Encrypted", "Not encrypted")}</Td>
                  <Td dataLabel="Recovery key">{yesNo(n.escrowed, "Escrowed", "Not escrowed")}</Td>
                  <Td dataLabel="Security updates">
                    {n.pending_security === undefined ? "-" : n.pending_security > 0 ? <Label color="red" isCompact>{n.pending_security} pending</Label> : "None pending"}
                  </Td>
                  <Td dataLabel="Reboot">{yesNo(n.reboot_required, "Pending", "No")}</Td>
                  <Td dataLabel="Agent">{n.agent_version || "-"}</Td>
                  <Td dataLabel="Results"><CountLabels counts={n.by_status} /></Td>
                </Tr>
              ))}
            </Tbody>
          </Table>
          {total > 0 && (
            <Pagination itemCount={total} page={page} perPage={perPage} onSetPage={(_ev, p) => setPage(p)} onPerPageSelect={(_ev, pp) => { setPerPage(pp); setPage(1); }} variant="bottom" aria-label="Nodes pagination" />
          )}
        </>
      )}
    </div>
  );
};

/* ── Results tab ── */

const ResultsTab: React.FC<{ reportId: string }> = ({ reportId }) => {
  const [rows, setRows] = useState<ReportResultRow[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("All");
  const [statusOpen, setStatusOpen] = useState(false);
  const [stale, setStale] = useState("All");
  const [staleOpen, setStaleOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const resp = await fetchReportResults(reportId, {
        page,
        per_page: perPage,
        q: search || undefined,
        status: status !== "All" ? status : undefined,
        stale: stale === "All" ? undefined : stale === "stale",
        include_items: true,
      });
      setRows(resp.items);
      setTotal(resp.total);
      setExpanded(new Set());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load results");
    } finally {
      setLoading(false);
    }
  }, [reportId, page, perPage, search, status, stale]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    setPage(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search, status, stale]);

  const clearAll = () => {
    setSearch("");
    setStatus("All");
    setStale("All");
  };

  const rowKey = (r: ReportResultRow) => `${r.node_id}-${r.policy_id}`;
  const toggle = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  return (
    <div style={{ paddingTop: "1rem" }}>
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      <BorToolbar searchValue={search} onSearchChange={setSearch} searchAriaLabel="Search results in the report" searchPlaceholder="Search by node or policy" onClearAll={clearAll}>
        <ToolbarItem>
          <Select
            id="report-result-status-filter"
            isOpen={statusOpen}
            onOpenChange={setStatusOpen}
            selected={status}
            onSelect={(_ev, val) => { setStatus(String(val)); setStatusOpen(false); }}
            toggle={(ref: React.Ref<MenuToggleElement>) => (
              <MenuToggle ref={ref} onClick={() => setStatusOpen((v) => !v)} isExpanded={statusOpen} aria-label="Filter by compliance status">
                Status: {status === "All" ? "All" : STATUS_LABELS[status] ?? status}
              </MenuToggle>
            )}
          >
            <SelectList>
              <SelectOption value="All">All</SelectOption>
              {RESULT_STATUSES.map((s) => <SelectOption key={s} value={s}>{STATUS_LABELS[s]}</SelectOption>)}
            </SelectList>
          </Select>
        </ToolbarItem>
        <ToolbarItem>
          <Select
            id="report-result-stale-filter"
            isOpen={staleOpen}
            onOpenChange={setStaleOpen}
            selected={stale}
            onSelect={(_ev, val) => { setStale(String(val)); setStaleOpen(false); }}
            toggle={(ref: React.Ref<MenuToggleElement>) => (
              <MenuToggle ref={ref} onClick={() => setStaleOpen((v) => !v)} isExpanded={staleOpen} aria-label="Filter by freshness">
                {stale === "All" ? "Freshness: All" : stale === "stale" ? "Stale only" : "Fresh only"}
              </MenuToggle>
            )}
          >
            <SelectList>
              <SelectOption value="All">All</SelectOption>
              <SelectOption value="stale">Stale only</SelectOption>
              <SelectOption value="fresh">Fresh only</SelectOption>
            </SelectList>
          </Select>
        </ToolbarItem>
      </BorToolbar>
      {loading ? (
        <Spinner size="lg" aria-label="Loading results" />
      ) : rows.length === 0 ? (
        <BorEmptyState isEmptyData={total === 0 && search === "" && status === "All" && stale === "All"} itemsLabel="results" emptyTitle="No results in this report" emptyBody="No node in scope had a compliance result when the report was generated." onClearFilters={clearAll} />
      ) : (
        <>
          <Table aria-label="Results in this report" variant="compact">
            <Thead>
              <Tr>
                <Th screenReaderText="Row expand" />
                <Th>Node</Th>
                <Th>Policy</Th>
                <Th>Status</Th>
                <Th>Message</Th>
                <Th>Reported</Th>
                <Th>Items</Th>
              </Tr>
            </Thead>
            {rows.map((r, idx) => {
              const key = rowKey(r);
              const items = r.item_list ?? [];
              const hasItems = items.length > 0;
              const isExpanded = expanded.has(key);
              return (
                <Tbody key={key} isExpanded={hasItems ? isExpanded : undefined}>
                  <Tr>
                    {hasItems ? (
                      <Td expand={{ rowIndex: idx, isExpanded, onToggle: () => toggle(key), expandId: `report-result-${key}` }} />
                    ) : (
                      <Td className="pf-v6-c-table__toggle" />
                    )}
                    <Td dataLabel="Node">{r.node_name}</Td>
                    <Td dataLabel="Policy">
                      {r.policy_name}
                      <br />
                      <small className="bor-text-secondary">{policyTypeLabel(r.policy_type)} v{r.policy_version}</small>
                    </Td>
                    <Td dataLabel="Status">
                      <Label color={STATUS_COLORS[r.status] ?? "grey"} isCompact>{STATUS_LABELS[r.status] ?? r.status}</Label>
                    </Td>
                    <Td dataLabel="Message">{r.message || "-"}</Td>
                    <Td dataLabel="Reported">
                      {formatDate(r.reported_at)}
                      {r.stale && (
                        <>
                          {" "}
                          <Label color="orange" isCompact>Stale</Label>
                        </>
                      )}
                    </Td>
                    <Td dataLabel="Items">
                      {r.items === 0 ? "-" : r.items_failed > 0 ? `${r.items} (${r.items_failed} failed)` : r.items}
                    </Td>
                  </Tr>
                  {hasItems && (
                    <Tr isExpanded={isExpanded}>
                      <Td colSpan={7} noPadding>
                        <ExpandableRowContent>
                          <ItemsTable items={toComplianceItems(items)} summaryIndex={EMPTY_SUMMARY_INDEX} />
                        </ExpandableRowContent>
                      </Td>
                    </Tr>
                  )}
                </Tbody>
              );
            })}
          </Table>
          {total > 0 && (
            <Pagination itemCount={total} page={page} perPage={perPage} onSetPage={(_ev, p) => setPage(p)} onPerPageSelect={(_ev, pp) => { setPerPage(pp); setPage(1); }} variant="bottom" aria-label="Results pagination" />
          )}
        </>
      )}
    </div>
  );
};

/* ── Page ── */

export const ReportDetailPage: React.FC = () => {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { addToast } = useToast();

  const [report, setReport] = useState<ReportSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<string | number>("overview");
  const [downloadOpen, setDownloadOpen] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    getReport(id)
      .then((r) => {
        if (!cancelled) setReport(r);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load the report");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const onDownload = async (choice: DownloadChoice) => {
    if (!report) return;
    try {
      const filename = await downloadReport(report.id, choice.format, choice.part);
      addToast({ variant: "success", title: `Downloaded ${filename}` });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Download failed");
    }
  };

  const integrityWarning = useMemo(() => {
    const i = report?.integrity;
    if (!i) return null;
    if (i.hash !== "ok") return "Integrity check failed: the stored bundle no longer matches the hash recorded when the report was generated. Treat this report as evidence of tampering, not of compliance.";
    if (i.signature === "invalid") return "The signature of this report does not verify. The bundle still matches its recorded hash, but its signature cannot be trusted.";
    return null;
  }, [report]);

  if (loading) {
    return (
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentCenter" }}>
          <FlexItem>
            <Spinner size="xl" aria-label="Loading the report" />
          </FlexItem>
        </Flex>
      </PageSection>
    );
  }

  if (!report) {
    return (
      <PageSection>
        <Button variant="link" icon={<ArrowLeftIcon />} onClick={() => navigate("/reports")} style={{ paddingLeft: 0 }}>
          Back to reports
        </Button>
        <LiveAlert message={error ?? "Report not found"} variant="danger" />
      </PageSection>
    );
  }

  const integrity = report.integrity;

  return (
    <>
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }} style={{ marginBottom: "1rem" }}>
          <FlexItem>
            <Button variant="link" icon={<ArrowLeftIcon />} onClick={() => navigate("/reports")} style={{ paddingLeft: 0 }}>
              Back to reports
            </Button>
          </FlexItem>
          <FlexItem>
            <Dropdown
              isOpen={downloadOpen}
              onOpenChange={setDownloadOpen}
              onSelect={() => setDownloadOpen(false)}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle ref={ref} onClick={() => setDownloadOpen((v) => !v)} isExpanded={downloadOpen} variant="secondary" icon={<DownloadIcon />}>
                  Download
                </MenuToggle>
              )}
            >
              <DropdownList>{downloadDropdownItems(report, (c) => void onDownload(c))}</DropdownList>
            </Dropdown>
          </FlexItem>
        </Flex>

        <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
        <LiveAlert message={integrityWarning} variant="danger" style={{ marginBottom: "1rem" }} />

        <Card isCompact style={{ marginBottom: "1rem" }}>
          <CardTitle>{report.label || "Compliance report"}</CardTitle>
          <CardBody>
            <DescriptionList isHorizontal isCompact columnModifier={{ default: "1Col", lg: "2Col" }}>
              <DescriptionListGroup>
                <DescriptionListTerm>Generated</DescriptionListTerm>
                <DescriptionListDescription>{formatDate(report.generated_at)}</DescriptionListDescription>
              </DescriptionListGroup>
              <DescriptionListGroup>
                <DescriptionListTerm>Generated by</DescriptionListTerm>
                <DescriptionListDescription>{report.generated_by || "-"} ({triggerLabel(report.trigger)})</DescriptionListDescription>
              </DescriptionListGroup>
              <DescriptionListGroup>
                <DescriptionListTerm>Nodes</DescriptionListTerm>
                <DescriptionListDescription>
                  {report.scope.all_nodes ? (
                    <Label isCompact>All nodes</Label>
                  ) : (
                    <LabelGroup aria-label="Node groups in scope" isCompact>
                      {report.scope.node_groups.map((g) => <Label key={g.id} isCompact>{g.name || g.id}</Label>)}
                    </LabelGroup>
                  )}
                </DescriptionListDescription>
              </DescriptionListGroup>
              <DescriptionListGroup>
                <DescriptionListTerm>Policy types</DescriptionListTerm>
                <DescriptionListDescription>
                  {report.scope.policy_types.length === 0 ? (
                    <Label isCompact>All policy types</Label>
                  ) : (
                    <LabelGroup aria-label="Policy types in scope" isCompact>
                      {report.scope.policy_types.map((t) => <Label key={t} isCompact>{policyTypeLabel(t)}</Label>)}
                    </LabelGroup>
                  )}
                </DescriptionListDescription>
              </DescriptionListGroup>
              <DescriptionListGroup>
                <DescriptionListTerm>SHA-256</DescriptionListTerm>
                <DescriptionListDescription>
                  {report.sha256 ? (
                    <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied" variant={ClipboardCopyVariant.inlineCompact} style={{ fontFamily: "monospace", fontSize: "0.8rem" }}>
                      {report.sha256}
                    </ClipboardCopy>
                  ) : "-"}
                </DescriptionListDescription>
              </DescriptionListGroup>
              <DescriptionListGroup>
                <DescriptionListTerm>Integrity</DescriptionListTerm>
                <DescriptionListDescription>
                  {integrity ? (
                    <LabelGroup aria-label="Integrity" isCompact>
                      <HashLabel state={integrity.hash} />
                      <SignatureLabel state={integrity.signature} />
                    </LabelGroup>
                  ) : "-"}
                  {integrity?.signature_key_id && (
                    <div className="bor-text-secondary" style={{ fontFamily: "monospace", fontSize: "0.75rem", marginTop: "0.25rem", wordBreak: "break-all" }}>
                      Signing key {integrity.signature_key_id}
                    </div>
                  )}
                </DescriptionListDescription>
              </DescriptionListGroup>
            </DescriptionList>
          </CardBody>
        </Card>

        <Flex spaceItems={{ default: "spaceItemsMd" }} flexWrap={{ default: "wrap" }} style={{ marginBottom: "1rem" }}>
          <FlexItem flex={{ default: "flex_1" }}>
            <StatCard title="Compliance score" value={formatScore(report.score_pct)} color={report.score_pct === null ? "grey" : report.score_pct >= 90 ? "green" : report.score_pct >= 70 ? "orange" : "red"} />
          </FlexItem>
          <FlexItem flex={{ default: "flex_1" }}>
            <StatCard title={`Nodes (${report.nodes_online} online, ${report.nodes_unreachable} unreachable)`} value={report.nodes} color="blue" />
          </FlexItem>
          <FlexItem flex={{ default: "flex_1" }}>
            <StatCard title="Non-compliant results" value={report.by_status.non_compliant} color={report.by_status.non_compliant > 0 ? "red" : "green"} />
          </FlexItem>
          <FlexItem flex={{ default: "flex_1" }}>
            <StatCard title="Errors" value={report.by_status.error} color={report.by_status.error > 0 ? "orange" : "green"} />
          </FlexItem>
          <FlexItem flex={{ default: "flex_1" }}>
            <StatCard title={`Stale results (of ${report.results})`} value={report.results_stale} color={report.results_stale > 0 ? "orange" : "grey"} />
          </FlexItem>
        </Flex>

        <Tabs activeKey={tab} onSelect={(_ev, k) => setTab(k)} aria-label="Report sections" mountOnEnter>
          <Tab eventKey="overview" title={<TabTitleText>Overview</TabTitleText>}>
            <OverviewTab reportId={report.id} notes={report.notes} />
          </Tab>
          <Tab eventKey="nodes" title={<TabTitleText>Nodes</TabTitleText>}>
            <NodesTab reportId={report.id} />
          </Tab>
          <Tab eventKey="results" title={<TabTitleText>Results</TabTitleText>}>
            <ResultsTab reportId={report.id} />
          </Tab>
        </Tabs>
      </PageSection>
    </>
  );
};
