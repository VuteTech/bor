// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * NodeUpdatesSection - the "Software updates" block of the node drawer:
 * the engine and its mode, the last check and run, the pending updates
 * with their advisories, and whether a reboot is pending and why.
 * Self-contained: fetches its own data.
 */

import React, { useCallback, useEffect, useId, useState } from "react";
import { Button, Checkbox, ExpandableSection, Label, SearchInput, Spinner, Title, Tooltip } from "@patternfly/react-core";
import SyncAltIcon from "@patternfly/react-icons/dist/esm/icons/sync-alt-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ConfirmModal } from "../../components/ConfirmModal";
import { hasPermission } from "../../apiClient/permissions";
import {
  fetchNodeUpdates,
  fetchNodeUpdatesPending,
  runUpdatesAction,
  updatesEngineLabel,
  updatesModeLabel,
  type UpdatesActionKind,
  type UpdatesActionResult,
  type UpdatesNodeStatus,
  type UpdatesPendingRow,
} from "../../apiClient/updatesApi";

type LabelColor = React.ComponentProps<typeof Label>["color"];

function timeAgo(dateStr?: string): string {
  if (!dateStr) return "";
  const diff = Date.now() - new Date(dateStr).getTime();
  if (Number.isNaN(diff)) return "";
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} h ago`;
  return `${Math.floor(hours / 24)} d ago`;
}

function severityColor(s: string): LabelColor {
  switch (s) {
    case "critical": return "red";
    case "important": return "orange";
    case "moderate": return "yellow";
    case "low": return "blue";
    default: return "grey";
  }
}

function modeColor(mode: string): LabelColor {
  switch (mode) {
    case "security":
    case "all": return "green";
    case "check": return "blue";
    case "off": return "orange";
    default: return "grey";
  }
}

const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)", fontSize: "0.85rem" };

interface Props {
  nodeId: string;
  nodeName?: string;
}

const ACTION_LABELS: Record<UpdatesActionKind, string> = { check: "Check for updates", install: "Install updates now", reboot: "Reboot" };

export const NodeUpdatesSection: React.FC<Props> = ({ nodeId, nodeName }) => {
  const idp = useId();
  const canOperate = hasPermission("updates:operate");
  const [status, setStatus] = useState<UpdatesNodeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [securityOnly, setSecurityOnly] = useState(false);
  const [pending, setPending] = useState<UpdatesPendingRow[] | null>(null);
  const [pendingError, setPendingError] = useState<string | null>(null);
  const [running, setRunning] = useState<UpdatesActionKind | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<UpdatesActionResult | null>(null);
  const [confirm, setConfirm] = useState<UpdatesActionKind | null>(null);
  const [forceReboot, setForceReboot] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setStatus(await fetchNodeUpdates(nodeId));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load the update state");
    } finally {
      setLoading(false);
    }
  }, [nodeId]);

  useEffect(() => { setStatus(null); void load(); }, [load]);

  const inv = status?.inventory;
  const hasPending = (inv?.pendingTotal ?? 0) + (inv?.pendingFirmware ?? 0) > 0;

  const runAction = useCallback(async (kind: UpdatesActionKind, force: boolean) => {
    setRunning(kind);
    setActionError(null);
    setActionResult(null);
    try {
      const res = await runUpdatesAction(nodeId, kind, force);
      setActionResult(res);
      if (!res.success) setActionError(res.message || `The node could not ${kind}`);
      if (kind !== "reboot") void load();
    } catch (e: unknown) {
      setActionError(e instanceof Error ? e.message : `The ${kind} request failed`);
    } finally {
      setRunning(null);
    }
  }, [nodeId, load]);

  useEffect(() => {
    if (!hasPending) { setPending([]); return; }
    let cancelled = false;
    setPending(null);
    const handle = window.setTimeout(() => {
      fetchNodeUpdatesPending(nodeId, { q: query.trim(), security: securityOnly, limit: 200 })
        .then((rows) => { if (!cancelled) { setPending(rows); setPendingError(null); } })
        .catch((e: unknown) => { if (!cancelled) setPendingError(e instanceof Error ? e.message : "Failed to load the pending updates"); });
    }, query ? 250 : 0);
    return () => { cancelled = true; window.clearTimeout(handle); };
  }, [nodeId, hasPending, query, securityOnly, status?.reportedAt]);

  return (
    <section aria-labelledby={`${idp}-title`} style={{ marginTop: "1.5rem" }}>
      <div style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}>
        <Title headingLevel="h3" size="md" id={`${idp}-title`}>Software updates</Title>
        <Tooltip content="Reload">
          <Button variant="plain" aria-label="Reload the update state" onClick={() => void load()} isDisabled={loading}><SyncAltIcon /></Button>
        </Tooltip>
      </div>
      <LiveAlert id={`${idp}-error`} variant="danger" isInline message={error} />
      {loading && !status && <Spinner size="md" aria-label="Loading" />}
      {status && !inv && <p style={subtle}>Not reported: the node has not sent its update state yet.</p>}
      {inv && canOperate && (
        <div style={{ display: "flex", gap: "0.5rem", flexWrap: "wrap", marginTop: "0.5rem", alignItems: "center" }}>
          <Tooltip content="Refresh the package metadata on the node and report the inventory again">
            <Button variant="secondary" size="sm" onClick={() => void runAction("check", false)} isDisabled={running !== null} isLoading={running === "check"}>{ACTION_LABELS.check}</Button>
          </Tooltip>
          <Tooltip content="Run the update engine once now with the policy's mode (what the schedule would do), then report">
            <Button variant="secondary" size="sm" onClick={() => setConfirm("install")} isDisabled={running !== null} isLoading={running === "install"}>{ACTION_LABELS.install}</Button>
          </Tooltip>
          <Tooltip content="Restart the node now; with users logged in they are warned and the node restarts after one minute">
            <Button variant="danger" size="sm" onClick={() => { setForceReboot(false); setConfirm("reboot"); }} isDisabled={running !== null} isLoading={running === "reboot"}>{ACTION_LABELS.reboot}</Button>
          </Tooltip>
          {running && <span style={subtle} aria-live="polite">Waiting for the node{running === "install" ? " (an engine run can take several minutes)" : ""}...</span>}
        </div>
      )}
      <LiveAlert id={`${idp}-action-error`} variant="danger" isInline message={actionError} style={{ marginTop: "0.5rem" }} />
      {actionResult?.success && <LiveAlert id={`${idp}-action-ok`} variant="success" isInline message={`${ACTION_LABELS[actionResult.kind]}: ${actionResult.message}`} style={{ marginTop: "0.5rem" }} />}
      {actionResult?.text && (
        <ExpandableSection toggleText="Engine output" isIndented style={{ marginTop: "0.25rem" }}>
          <pre style={{ fontSize: "0.75rem", maxHeight: "16rem", overflow: "auto", whiteSpace: "pre-wrap" }}>{actionResult.text}</pre>
        </ExpandableSection>
      )}
      {inv && (
        <>
          <div style={{ display: "flex", gap: "0.35rem", flexWrap: "wrap", marginTop: "0.5rem" }}>
            <Label isCompact color={inv.engine && inv.engine !== "none" ? (inv.engineInstalled ? "green" : "orange") : "grey"}>
              {updatesEngineLabel(inv.engine ?? "")}{inv.engine && inv.engine !== "none" && !inv.engineInstalled ? " · not installed" : ""}
            </Label>
            <Label isCompact color={modeColor(inv.effectiveMode ?? "")}>{updatesModeLabel(inv.effectiveMode ?? "")}</Label>
            {inv.engineManagedBy && <Label isCompact variant="outline">{inv.engineManagedBy === "bor" ? "configured by Bor" : inv.engineManagedBy === "foreign" ? "configured locally" : "vendor defaults"}</Label>}
            {inv.engine && inv.engine !== "none" && <Label isCompact variant="outline">timer {inv.engineTimerEnabled ? "enabled" : "disabled"}</Label>}
            {inv.reboot?.required
              ? <Label isCompact color="orange">reboot pending{inv.reboot.since ? ` since ${new Date(inv.reboot.since).toLocaleString()}` : ""}</Label>
              : <Label isCompact color="green">no reboot pending</Label>}
          </div>
          <p style={{ ...subtle, marginTop: "0.5rem" }}>
            Reported {timeAgo(status?.reportedAt)}.
            {inv.lastCheck ? ` Last check ${timeAgo(inv.lastCheck)}.` : ""}
            {inv.lastRun?.time ? ` Last run ${timeAgo(inv.lastRun.time)}${inv.lastRun.success ? "" : " failed"}: ${inv.lastRun.message || `${inv.lastRun.packages} packages`}.` : " No engine run found."}
            {inv.runningKernel ? ` Kernel ${inv.runningKernel}${inv.newestKernel && inv.newestKernel !== inv.runningKernel ? ` (newest installed ${inv.newestKernel})` : ""}.` : ""}
            {inv.bootTime ? ` Up since ${new Date(inv.bootTime).toLocaleString()}.` : ""}
          </p>
          {inv.reboot?.required && inv.reboot.reasons?.length ? (
            <p style={{ ...subtle, marginTop: "0.25rem" }}>Reboot needed: {inv.reboot.reasons.join("; ")}{inv.reboot.staged ? " (a deployment is staged)" : ""}.</p>
          ) : null}
          {inv.lastPolicyReboot ? <p style={{ ...subtle, marginTop: "0.25rem" }}>Last reboot by policy {new Date(inv.lastPolicyReboot).toLocaleString()}.</p> : null}
          {inv.oldestSecuritySince ? <p style={{ ...subtle, marginTop: "0.25rem" }}>Oldest security update pending since {new Date(inv.oldestSecuritySince).toLocaleString()} ({timeAgo(inv.oldestSecuritySince)}).</p> : null}
          {inv.foreignMechanisms?.length ? <p style={{ ...subtle, marginTop: "0.25rem" }}>Other update mechanisms on this node: {inv.foreignMechanisms.join(", ")}.</p> : null}

          <div style={{ display: "flex", gap: "0.75rem", alignItems: "center", marginTop: "0.75rem", flexWrap: "wrap" }}>
            <strong>{inv.pendingTotal ?? 0} pending update{(inv.pendingTotal ?? 0) === 1 ? "" : "s"}</strong>
            {(inv.pendingSecurity ?? 0) > 0 && <Label isCompact color="red">{inv.pendingSecurity} security</Label>}
            {(inv.pendingFirmware ?? 0) > 0 && <Label isCompact color="purple">{inv.pendingFirmware} firmware</Label>}
          </div>
          {hasPending && (
            <>
              <div style={{ display: "flex", gap: "0.75rem", alignItems: "center", marginTop: "0.5rem", flexWrap: "wrap" }}>
                <SearchInput aria-label="Search pending updates" placeholder="Search by package, advisory or CVE" value={query} onChange={(_ev, v) => setQuery(v)} onClear={() => setQuery("")} style={{ maxWidth: "20rem" }} />
                <Checkbox id={`${idp}-security`} label="Security only" isChecked={securityOnly} onChange={(_ev, v) => setSecurityOnly(v)} />
              </div>
              <LiveAlert id={`${idp}-pending-error`} variant="danger" isInline message={pendingError} />
              {pending === null && !pendingError && <Spinner size="md" aria-label="Loading" />}
              {pending && pending.length === 0 && <p style={subtle}>No pending update matches.</p>}
              {pending && pending.length > 0 && (
                <ul style={{ listStyle: "none", padding: 0, margin: "0.5rem 0 0", maxHeight: "22rem", overflowY: "auto" }}>
                  {pending.map((p) => (
                    <li key={`${p.kind}-${p.name}`} style={{ padding: "0.3rem 0", borderBottom: "1px solid var(--pf-t--global--border--color--default)" }}>
                      <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap" }}>
                        <code style={{ fontSize: "0.85rem" }}>{p.name}</code>
                        <span style={subtle}>{p.version}</span>
                        {p.security && <Label isCompact color={severityColor(p.severity)}>{p.severity ? `security · ${p.severity}` : "security"}</Label>}
                        {p.kind === "firmware" && <Label isCompact variant="outline">firmware</Label>}
                      </div>
                      {(p.advisory || p.cves.length > 0) && (
                        <div style={{ ...subtle, marginTop: "0.15rem" }}>
                          {p.advisory}{p.advisory && p.cves.length ? " · " : ""}{p.cves.join(", ")}
                        </div>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </>
      )}
      <ConfirmModal
        isOpen={confirm === "install"}
        title={`Install updates on ${nodeName ?? "this node"} now?`}
        confirmLabel="Install now"
        onCancel={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void runAction("install", false); }}
      >
        The node runs its update engine once with the policy's mode, as its schedule would. Packages are downloaded and installed at once; the run can take several minutes and is audit-logged. The reboot rule of the policy decides any reboot afterwards.
      </ConfirmModal>
      <ConfirmModal
        isOpen={confirm === "reboot"}
        title={`Reboot ${nodeName ?? "this node"}?`}
        confirmLabel={forceReboot ? "Warn users and reboot" : "Reboot"}
        isDanger
        onCancel={() => setConfirm(null)}
        onConfirm={() => { setConfirm(null); void runAction("reboot", forceReboot); }}
      >
        <p>The node restarts at once when nobody is logged in. With a user logged in the reboot is refused unless you force it: the user gets a desktop warning and the node restarts after one minute, losing unsaved work.</p>
        <Checkbox id={`${idp}-force`} label="Reboot even if users are logged in (they get a one-minute warning)" isChecked={forceReboot} onChange={(_ev, v) => setForceReboot(v)} style={{ marginTop: "0.75rem" }} />
      </ConfirmModal>
    </section>
  );
};
