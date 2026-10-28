// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Flex,
  FlexItem,
  Label,
  MenuToggle,
  MenuToggleElement,
  PageSection,
  Pagination,
  Select,
  SelectList,
  SelectOption,
  Spinner,
  Switch,
  Tab,
  TabTitleText,
  Tabs,
  ToolbarItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import ThumbtackIcon from "@patternfly/react-icons/dist/esm/icons/thumbtack-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { BorToolbar } from "../../components/BorToolbar";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import { hasGlobalPermission, hasPermission } from "../../apiClient/permissions";
import {
  deleteReport,
  downloadReport,
  fetchComplianceTrend,
  fetchReportCapabilities,
  listReports,
  ReportCapabilities,
  ReportSummary,
  ReportTrendPoint,
  setReportPinned,
} from "../../apiClient/reportsApi";
import { SchedulesTab } from "./SchedulesTab";
import { WaiversTab } from "./WaiversTab";
import { TrendChart } from "./TrendChart";
import { GenerateReportModal } from "./GenerateReportModal";
import {
  DownloadChoice,
  SignedLabel,
  TRIGGER_LABELS,
  downloadChoices,
  formatDate,
  formatScore,
  scopeSummary,
  triggerLabel,
} from "./reportsShared";

const TRIGGER_OPTIONS = ["manual", "scheduled", "daily_summary", "api"];

/** The fleet's compliance score over the last 30 days, from the daily summaries. */
const TrendCard: React.FC<{ enabled: boolean }> = ({ enabled }) => {
  const [points, setPoints] = useState<ReportTrendPoint[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    fetchComplianceTrend(30)
      .then((p) => !cancelled && setPoints(p))
      .catch((err) => !cancelled && setError(err instanceof Error ? err.message : "Failed to load the trend"));
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  if (!enabled || error) return null;
  return (
    <Card isCompact style={{ marginBottom: "1rem" }}>
      <CardTitle>Fleet compliance score, last 30 days</CardTitle>
      <CardBody>
        {points === null ? (
          <Spinner size="lg" aria-label="Loading the trend" />
        ) : points.length === 0 ? (
          <span className="bor-text-secondary">The server stores one summary a day; the trend appears after the first one.</span>
        ) : (
          <TrendChart points={points} title="Fleet compliance score, last 30 days" />
        )}
      </CardBody>
    </Card>
  );
};

export const ReportsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { addToast } = useToast();
  const canCreate = hasPermission("report:create");
  const canManage = hasPermission("report:manage");
  const canManageGlobally = hasGlobalPermission("report:manage");
  const canAttestGlobally = hasGlobalPermission("report:attest");
  const [caps, setCaps] = useState<ReportCapabilities | null>(null);
  const [tab, setTab] = useState<string | number>(searchParams.get("tab") ?? "reports");
  const [showSummaries, setShowSummaries] = useState(false);
  const has = (c: string) => !!caps?.capabilities.includes(c);

  useEffect(() => {
    fetchReportCapabilities().then(setCaps).catch(() => setCaps(null));
  }, []);

  const [items, setItems] = useState<ReportSummary[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [search, setSearch] = useState("");
  const [trigger, setTrigger] = useState<string>("All");
  const [triggerOpen, setTriggerOpen] = useState(false);
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(25);

  const [generateOpen, setGenerateOpen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<ReportSummary | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const resp = await listReports({
        page,
        per_page: perPage,
        q: search || undefined,
        trigger: trigger !== "All" ? trigger : undefined,
        kind: showSummaries ? "all" : undefined,
      });
      setItems(resp.items);
      setTotal(resp.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load reports");
    } finally {
      setLoading(false);
    }
  }, [page, perPage, search, trigger, showSummaries]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    setPage(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search, trigger]);

  // /reports?new=1 opens the generate dialog once, then drops the parameter.
  useEffect(() => {
    if (searchParams.get("new") === "1" && canCreate) {
      setGenerateOpen(true);
      const next = new URLSearchParams(searchParams);
      next.delete("new");
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams, canCreate]);

  const onDownload = async (report: ReportSummary, choice: DownloadChoice) => {
    try {
      const filename = await downloadReport(report.id, choice.format, choice.part);
      addToast({ variant: "success", title: `Downloaded ${filename}` });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Download failed");
    }
  };

  const doDelete = async () => {
    if (!confirmDelete) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await deleteReport(confirmDelete.id);
      addToast({ variant: "success", title: "Report deleted" });
      setConfirmDelete(null);
      void load();
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Failed to delete the report");
    } finally {
      setDeleting(false);
    }
  };

  const togglePin = async (report: ReportSummary) => {
    try {
      await setReportPinned(report.id, !report.pinned);
      addToast({ variant: "success", title: report.pinned ? "Report unpinned" : "Report pinned: kept until unpinned" });
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to change the legal hold");
    }
  };

  const rowActions = (report: ReportSummary): IAction[] => {
    const actions: IAction[] = [
      { title: "Open", onClick: () => navigate(`/reports/${report.id}`) },
    ];
    const downloads = downloadChoices(report);
    if (downloads.length > 0) {
      actions.push({ isSeparator: true });
      actions.push(...downloads.map((c) => ({ title: c.label, onClick: () => void onDownload(report, c) })));
    }
    if (canManageGlobally) {
      actions.push({ isSeparator: true });
      actions.push({ title: report.pinned ? "Unpin (release legal hold)" : "Pin (legal hold)", onClick: () => void togglePin(report) });
      actions.push({
        title: "Delete",
        isDanger: true,
        isAriaDisabled: report.pinned,
        tooltipProps: report.pinned ? { content: "Pinned reports cannot be deleted; unpin it first." } : undefined,
        onClick: () => { setDeleteError(null); setConfirmDelete(report); },
      });
    }
    return actions;
  };

  const clearAll = () => {
    setSearch("");
    setTrigger("All");
  };

  const generateButton = canCreate ? (
    <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => setGenerateOpen(true)}>
      Generate report
    </Button>
  ) : null;

  const reportsList = (
    <>
        <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />

        <BorToolbar
          searchValue={search}
          onSearchChange={setSearch}
          searchAriaLabel="Search reports"
          searchPlaceholder="Search by label or author"
          onClearAll={clearAll}
        >
          <ToolbarItem>
            <Select
              id="report-trigger-filter"
              isOpen={triggerOpen}
              onOpenChange={setTriggerOpen}
              selected={trigger}
              onSelect={(_ev, val) => { setTrigger(String(val)); setTriggerOpen(false); }}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle
                  ref={ref}
                  onClick={() => setTriggerOpen((v) => !v)}
                  isExpanded={triggerOpen}
                  aria-label="Filter by trigger"
                >
                  Trigger: {trigger === "All" ? "All" : triggerLabel(trigger)}
                </MenuToggle>
              )}
            >
              <SelectList>
                <SelectOption value="All">All</SelectOption>
                {TRIGGER_OPTIONS.map((t) => (
                  <SelectOption key={t} value={t}>{TRIGGER_LABELS[t]}</SelectOption>
                ))}
              </SelectList>
            </Select>
          </ToolbarItem>
          {caps?.daily_summary && (
            <ToolbarItem alignSelf="center">
              <Switch id="reports-show-summaries" label="Show daily summaries" isChecked={showSummaries} onChange={(_e, v) => setShowSummaries(v)} />
            </ToolbarItem>
          )}
        </BorToolbar>

        {loading ? (
          <Flex justifyContent={{ default: "justifyContentCenter" }} style={{ padding: "2rem" }}>
            <FlexItem>
              <Spinner size="xl" aria-label="Loading reports" />
            </FlexItem>
          </Flex>
        ) : items.length === 0 ? (
          <BorEmptyState
            isEmptyData={total === 0 && search === "" && trigger === "All"}
            itemsLabel="reports"
            emptyTitle="No reports yet"
            emptyBody="Generate a report to record the fleet's compliance and posture as it is right now."
            action={generateButton ?? undefined}
            onClearFilters={clearAll}
          />
        ) : (
          <>
            <Table aria-label="Compliance reports" variant="compact">
              <Thead>
                <Tr>
                  <Th>Generated</Th>
                  <Th>Label</Th>
                  <Th>Scope</Th>
                  <Th>Nodes</Th>
                  <Th>Score</Th>
                  <Th>Non-compliant</Th>
                  <Th>Stale</Th>
                  <Th>Integrity</Th>
                  <Th>Generated by</Th>
                  <Th screenReaderText="Actions" />
                </Tr>
              </Thead>
              <Tbody>
                {items.map((r) => (
                  <Tr key={r.id}>
                    <Td dataLabel="Generated">{formatDate(r.generated_at)}</Td>
                    <Td dataLabel="Label">
                      <Link to={`/reports/${r.id}`}>{r.label || "(no label)"}</Link>
                      {r.trigger !== "manual" && (
                        <>
                          {" "}
                          <Label isCompact variant="outline">{triggerLabel(r.trigger)}</Label>
                        </>
                      )}
                      {r.pinned && (
                        <>
                          {" "}
                          <Label isCompact color="blue" icon={<ThumbtackIcon />}>Pinned</Label>
                        </>
                      )}
                    </Td>
                    <Td dataLabel="Scope">{scopeSummary(r.scope)}</Td>
                    <Td dataLabel="Nodes">
                      {r.nodes}
                      {r.nodes_unreachable > 0 && (
                        <span className="bor-text-secondary"> ({r.nodes_unreachable} unreachable)</span>
                      )}
                    </Td>
                    <Td dataLabel="Score">{formatScore(r.score_pct)}</Td>
                    <Td dataLabel="Non-compliant">
                      {r.by_status.non_compliant > 0 ? (
                        <Label color="red" isCompact>{r.by_status.non_compliant}</Label>
                      ) : (
                        <span>0</span>
                      )}
                      {r.by_status.error > 0 && (
                        <>
                          {" "}
                          <Label color="yellow" isCompact>{r.by_status.error} {r.by_status.error === 1 ? "error" : "errors"}</Label>
                        </>
                      )}
                    </Td>
                    <Td dataLabel="Stale">{r.results_stale}</Td>
                    <Td dataLabel="Integrity">{r.kind === "summary" ? <span className="bor-text-secondary">Summary</span> : <SignedLabel signed={r.signed} />}</Td>
                    <Td dataLabel="Generated by">{r.generated_by || "-"}</Td>
                    <Td dataLabel="Actions" isActionCell>
                      <ActionsColumn
                        items={rowActions(r)}
                        actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                          <MenuToggle
                            ref={toggleRef}
                            aria-label={`Actions for report ${r.label || r.id}`}
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
                ))}
              </Tbody>
            </Table>
            {total > 0 && (
              <Pagination
                itemCount={total}
                page={page}
                perPage={perPage}
                onSetPage={(_ev, p) => setPage(p)}
                onPerPageSelect={(_ev, pp) => { setPerPage(pp); setPage(1); }}
                variant="bottom"
                aria-label="Reports pagination"
              />
            )}
          </>
        )}
    </>
  );

  return (
    <>
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }}>
          <FlexItem>{generateButton}</FlexItem>
        </Flex>
      </PageSection>

      <PageSection>
        <TrendCard enabled={!!caps?.daily_summary && hasGlobalPermission("report:view")} />
        <Tabs activeKey={tab} onSelect={(_ev, k) => setTab(k)} aria-label="Reports sections" mountOnEnter>
          <Tab eventKey="reports" title={<TabTitleText>Reports</TabTitleText>}>
            <div style={{ paddingTop: "1rem" }}>{reportsList}</div>
          </Tab>
          {has("schedules") && canManageGlobally ? (
            <Tab eventKey="schedules" title={<TabTitleText>Schedules</TabTitleText>}>
              <SchedulesTab canRefresh={!!caps?.refresh} />
            </Tab>
          ) : null}
          {has("waivers") && canAttestGlobally ? (
            <Tab eventKey="waivers" title={<TabTitleText>Waivers</TabTitleText>}>
              <WaiversTab />
            </Tab>
          ) : null}
        </Tabs>
      </PageSection>

      <GenerateReportModal
        canRefresh={!!caps?.refresh}
        isOpen={generateOpen}
        onClose={() => setGenerateOpen(false)}
        onGenerated={(report) => {
          setGenerateOpen(false);
          addToast({ variant: "success", title: "Report generated" });
          navigate(`/reports/${report.id}`);
        }}
      />

      <ConfirmModal
        isOpen={confirmDelete !== null}
        title="Delete this report?"
        isDanger
        confirmLabel="Delete"
        confirmPhrase={confirmDelete?.label ? confirmDelete.label : "Yes"}
        confirmPhraseLabel={confirmDelete?.label ? `Type "${confirmDelete.label}" to confirm` : 'Type "Yes" to confirm'}
        isBusy={deleting}
        error={deleteError}
        onConfirm={() => void doDelete()}
        onCancel={() => setConfirmDelete(null)}
      >
        This permanently deletes the report, its bundle and its hash. The audit log keeps the record of its generation and of this deletion.
      </ConfirmModal>
    </>
  );
};
