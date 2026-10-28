// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import { Button, Flex, FlexItem, Label, MenuToggle, Spinner } from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import ExclamationCircleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-circle-icon";
import OutlinedClockIcon from "@patternfly/react-icons/dist/esm/icons/outlined-clock-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import {
  deleteSchedule,
  listSchedules,
  ReportSchedule,
  runSchedule,
  SchedulesResponse,
} from "../../apiClient/reportsApi";
import { ScheduleModal } from "./ScheduleModal";
import { formatDate } from "./reportsShared";

const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

function cadenceText(s: ReportSchedule): string {
  const time = `${String(Math.floor(s.at_minute / 60)).padStart(2, "0")}:${String(s.at_minute % 60).padStart(2, "0")}`;
  switch (s.cadence) {
    case "daily":
      return `Daily at ${time}`;
    case "weekly":
      return `${WEEKDAYS[s.weekday ?? 1]}s at ${time}`;
    default:
      return `${s.month_day === 0 ? "Last day" : `Day ${s.month_day}`} of each month at ${time}`;
  }
}

const StatusLabel: React.FC<{ status?: string; error?: string }> = ({ status, error }) => {
  switch (status) {
    case "ok":
      return <Label color="green" isCompact icon={<CheckCircleIcon />}>Succeeded</Label>;
    case "error":
      return <Label color="red" isCompact icon={<ExclamationCircleIcon />} title={error}>Failed</Label>;
    case "skipped":
      return <Label color="orange" isCompact title={error}>Skipped</Label>;
    default:
      return <span className="bor-text-secondary">Not run yet</span>;
  }
};

/** The state of the latest run's deliveries (webhook and email). */
const DeliveryLabel: React.FC<{ status?: string; error?: string }> = ({ status, error }) => {
  switch (status) {
    case "ok":
      return <><br /><Label color="green" isCompact icon={<CheckCircleIcon />}>Delivered</Label></>;
    case "queued":
      return <><br /><Label color="blue" isCompact icon={<OutlinedClockIcon />}>Queued</Label></>;
    case "retrying":
      return (
        <>
          <br />
          <Label color="orange" isCompact icon={<OutlinedClockIcon />}>Retrying</Label>{" "}
          <small className="bor-text-secondary">{error}</small>
        </>
      );
    case "error":
      return (
        <>
          <br />
          <Label color="red" isCompact icon={<ExclamationCircleIcon />}>Delivery failed</Label>{" "}
          <small className="bor-text-secondary">{error}</small>
        </>
      );
    default:
      return null;
  }
};

export const SchedulesTab: React.FC<{ canRefresh: boolean }> = ({ canRefresh }) => {
  const { addToast } = useToast();
  const [data, setData] = useState<SchedulesResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<ReportSchedule | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [running, setRunning] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<ReportSchedule | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setError(null);
      setData(await listSchedules());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load schedules");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const runNow = async (s: ReportSchedule) => {
    setRunning(s.id);
    try {
      const out = await runSchedule(s.id);
      if (out.status === "ok") {
        addToast({
          variant: out.delivery_status === "error" ? "warning" : "success",
          title:
            out.delivery_status === "error"
              ? `Report generated; queueing its delivery failed: ${out.delivery_error ?? ""}`
              : out.delivery_status === "queued"
                ? `Schedule "${s.name}" ran; deliveries are queued`
                : `Schedule "${s.name}" ran`,
        });
      } else {
        setError(`Schedule "${s.name}" failed: ${out.error ?? out.status}`);
      }
      void load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to run the schedule");
    } finally {
      setRunning(null);
    }
  };

  const doDelete = async () => {
    if (!confirmDelete) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await deleteSchedule(confirmDelete.id);
      addToast({ variant: "success", title: "Schedule deleted" });
      setConfirmDelete(null);
      void load();
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : "Failed to delete the schedule");
    } finally {
      setDeleting(false);
    }
  };

  const actions = (s: ReportSchedule): IAction[] => [
    { title: "Run now", isDisabled: running !== null, onClick: () => void runNow(s) },
    { title: "Edit", onClick: () => { setEditing(s); setModalOpen(true); } },
    { isSeparator: true },
    { title: "Delete", isDanger: true, onClick: () => { setDeleteError(null); setConfirmDelete(s); } },
  ];

  const newButton = (
    <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => { setEditing(null); setModalOpen(true); }}>
      New schedule
    </Button>
  );

  return (
    <div style={{ paddingTop: "1rem" }}>
      <Flex style={{ marginBottom: "1rem" }}>
        <FlexItem>{newButton}</FlexItem>
      </Flex>
      <LiveAlert message={error} variant="danger" style={{ marginBottom: "1rem" }} />
      {data === null ? (
        <Spinner size="lg" aria-label="Loading schedules" />
      ) : data.items.length === 0 ? (
        <BorEmptyState
          isEmptyData
          itemsLabel="schedules"
          emptyTitle="No schedules"
          emptyBody="Schedules generate reports on a fixed cadence (for example every Monday at 06:00) and can deliver them by webhook or email."
          action={newButton}
        />
      ) : (
        <Table aria-label="Report schedules" variant="compact">
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>When</Th>
              <Th>Next run</Th>
              <Th>Last run</Th>
              <Th>Delivery</Th>
              <Th screenReaderText="Actions" />
            </Tr>
          </Thead>
          <Tbody>
            {data.items.map((s) => (
              <Tr key={s.id}>
                <Td dataLabel="Name">
                  <strong>{s.name}</strong>{" "}
                  {!s.enabled && <Label isCompact color="grey">Disabled</Label>}
                  {s.refresh && <Label isCompact color="blue">Fresh evidence</Label>}
                </Td>
                <Td dataLabel="When">
                  {cadenceText(s)}
                  <br />
                  <small className="bor-text-secondary">{s.time_zone}</small>
                </Td>
                <Td dataLabel="Next run">{s.enabled ? formatDate(s.next_run_at) : "-"}</Td>
                <Td dataLabel="Last run">
                  {running === s.id ? (
                    <Spinner size="md" aria-label="Running" />
                  ) : (
                    <>
                      <StatusLabel status={s.last_status} error={s.last_error} />
                      {s.last_run_at && <> <span className="bor-text-secondary">{formatDate(s.last_run_at)}</span></>}
                      {s.last_report_id && (
                        <>
                          <br />
                          <Link to={`/reports/${s.last_report_id}`}>Open the report</Link>
                        </>
                      )}
                      {s.last_status !== "ok" && s.last_error && (
                        <>
                          <br />
                          <small className="bor-text-secondary">{s.last_error}</small>
                        </>
                      )}
                    </>
                  )}
                </Td>
                <Td dataLabel="Delivery">
                  {[s.webhook_url && "Webhook", s.email_to.length > 0 && `${s.email_to.length} email ${s.email_to.length === 1 ? "recipient" : "recipients"}`].filter(Boolean).join(", ") || "-"}
                  <DeliveryLabel status={s.last_delivery_status} error={s.last_delivery_error} />
                </Td>
                <Td dataLabel="Actions" isActionCell>
                  <ActionsColumn
                    items={actions(s)}
                    actionsToggle={({ onToggle, isOpen, isDisabled, toggleRef }) => (
                      <MenuToggle ref={toggleRef} aria-label={`Actions for schedule ${s.name}`} variant="plain" onClick={onToggle} isExpanded={isOpen} isDisabled={isDisabled} icon={<EllipsisVIcon />} />
                    )}
                  />
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}

      {data && (
        <ScheduleModal
          isOpen={modalOpen}
          schedule={editing}
          delivery={data.delivery}
          canRefresh={canRefresh}
          onClose={() => setModalOpen(false)}
          onSaved={(s) => {
            setModalOpen(false);
            addToast({ variant: "success", title: editing ? "Schedule saved" : `Schedule "${s.name}" created` });
            void load();
          }}
        />
      )}

      <ConfirmModal
        isOpen={confirmDelete !== null}
        title="Delete this schedule?"
        isDanger
        confirmLabel="Delete"
        confirmPhrase={confirmDelete?.name}
        confirmPhraseLabel={confirmDelete ? `Type "${confirmDelete.name}" to confirm` : undefined}
        isBusy={deleting}
        error={deleteError}
        onConfirm={() => void doDelete()}
        onCancel={() => setConfirmDelete(null)}
      >
        The schedule stops generating reports. Reports it already generated are kept.
      </ConfirmModal>
    </div>
  );
};
