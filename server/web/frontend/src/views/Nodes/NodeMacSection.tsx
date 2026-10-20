// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * NodeMacSection - the "Mandatory access control" block of the node
 * drawer: which LSM the node runs and its state, the booleans and modules
 * (SELinux) or profiles (AppArmor) it reported, and the denial summary of
 * the last report window with a proposal action for holders of
 * mac:operate. Self-contained: fetches its own data.
 */

import React, { useCallback, useEffect, useId, useMemo, useState } from "react";
import {
  Button,
  ClipboardCopy,
  ClipboardCopyVariant,
  Label,
  Modal,
  ModalBody,
  ModalHeader,
  ModalVariant,
  SearchInput,
  Spinner,
  Tab,
  TabTitleText,
  Tabs,
  Title,
  Tooltip,
} from "@patternfly/react-core";
import SyncAltIcon from "@patternfly/react-icons/dist/esm/icons/sync-alt-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { hasPermission } from "../../apiClient/permissions";
import {
  fetchNodeMac,
  fetchNodeMacItems,
  macLsmLabel,
  runMacAction,
  type MacActionKind,
  type MacCatalogueItem,
  type MacDenialGroup,
  type MacItemKind,
  type MacNodeStatus,
} from "../../apiClient/macApi";
import { usernsStubTemplate } from "../Policies/apparmorModel";

const MAX_ROWS = 80;

type LabelColor = React.ComponentProps<typeof Label>["color"];

function modeColor(lsm: string, mode: string): LabelColor {
  if (lsm === "selinux") return mode === "enforcing" ? "green" : mode === "permissive" ? "orange" : "red";
  if (lsm === "apparmor") return mode === "enabled" ? "green" : "red";
  return "grey";
}

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

/** A readable name for a stub from the executable path. */
function stubName(execpath: string): string {
  const base = execpath.split("/").filter(Boolean).pop() ?? "application";
  return base.toLowerCase().replace(/[^a-z0-9_.-]+/g, "-").replace(/^-+|-+$/g, "") || "application";
}

interface Props {
  nodeId: string;
  nodeName: string;
  online: boolean;
}

interface Proposal {
  title: string;
  text: string;
  message: string;
}

export const NodeMacSection: React.FC<Props> = ({ nodeId, nodeName, online }) => {
  const idp = useId();
  const canOperate = hasPermission("mac:operate");
  const [status, setStatus] = useState<MacNodeStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<string | number>("items");
  const [kind, setKind] = useState<MacItemKind | "">("");
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<MacCatalogueItem[] | null>(null);
  const [itemsError, setItemsError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [proposal, setProposal] = useState<Proposal | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setStatus(await fetchNodeMac(nodeId));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load the mandatory access control status");
    } finally {
      setLoading(false);
    }
  }, [nodeId]);

  useEffect(() => { setStatus(null); void load(); }, [load]);

  const lsm = status?.lsm ?? "";
  const effectiveKind: MacItemKind | "" = kind || (lsm === "apparmor" ? "profile" : lsm === "selinux" ? "boolean" : "");

  useEffect(() => {
    if (!effectiveKind) { setItems([]); return; }
    let cancelled = false;
    setItems(null);
    const handle = window.setTimeout(() => {
      fetchNodeMacItems(nodeId, { kind: effectiveKind, q: query.trim(), limit: 200 })
        .then((res) => { if (!cancelled) { setItems(res); setItemsError(null); } })
        .catch((e: unknown) => { if (!cancelled) setItemsError(e instanceof Error ? e.message : "Failed to load the inventory"); });
    }, query ? 250 : 0);
    return () => { cancelled = true; window.clearTimeout(handle); };
  }, [nodeId, effectiveKind, query]);

  const denials = useMemo(() => status?.denials ?? [], [status]);
  const denialTotal = useMemo(() => denials.reduce((n, d) => n + d.count, 0), [denials]);

  const propose = async (d: MacDenialGroup) => {
    const k: MacActionKind = lsm === "selinux" ? "selinux_propose_module" : "apparmor_propose_rules";
    setBusy(d.subject);
    setActionError(null);
    try {
      const res = await runMacAction(nodeId, k, d.subject, 24);
      if (!res.success) { setActionError(res.message || "The node could not build a proposal"); return; }
      setProposal({ title: `${lsm === "selinux" ? "Module proposal" : "Rule proposal"} for ${d.subject} on ${nodeName}`, text: res.text, message: res.message });
    } catch (e: unknown) {
      setActionError(e instanceof Error ? e.message : "Proposal failed");
    } finally {
      setBusy(null);
    }
  };

  const showStub = (d: MacDenialGroup) => {
    const path = d.execpath.startsWith("/") ? d.execpath : "";
    const name = stubName(d.execpath);
    setProposal({
      title: `Stub profile for ${d.execpath || d.subject}`,
      text: usernsStubTemplate(name, path),
      message: path ? "Paste it as a custom profile of an AppArmor policy bound to this node's group." : "The record named only the command; replace the executable path before using the profile.",
    });
  };

  const shown = items ? items.slice(0, MAX_ROWS) : [];

  return (
    <>
      <Title headingLevel="h3" size="md" style={{ marginTop: "1.5rem", marginBottom: "0.5rem" }}>
        Mandatory access control
      </Title>
      {loading && !status && <Spinner size="md" aria-label="Loading" />}
      <LiveAlert id={`${idp}-error`} variant="danger" isInline message={error} />
      {status && (
        <>
          <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap", marginBottom: "0.5rem" }}>
            <Label color={modeColor(status.lsm, status.mode)}>{macLsmLabel(status.lsm) || "Not reported"}{status.mode ? ` · ${status.mode}` : ""}</Label>
            {status.selinux && (
              <>
                {status.selinux.policyType && <Label isCompact>policy {status.selinux.policyType}{status.selinux.policyVersion ? ` v${status.selinux.policyVersion}` : ""}</Label>}
                {status.selinux.configMode && status.selinux.configMode !== status.mode && <Tooltip content="/etc/selinux/config differs from the runtime mode"><Label isCompact color="orange">config {status.selinux.configMode}</Label></Tooltip>}
                <Label isCompact>{status.selinux.booleanCount} booleans</Label>
                <Label isCompact>{status.selinux.moduleCount} modules</Label>
                {status.selinux.policyloadLocked && <Tooltip content="secure_mode_policyload is on: no policy change until reboot"><Label isCompact color="red">policy locked</Label></Tooltip>}
                {status.selinux.relabelPending && <Label isCompact color="orange">relabel at next boot</Label>}
                {!status.selinux.semanagePresent && <Tooltip content="policycoreutils-python-utils is not installed: login mappings and proposals are unavailable"><Label isCompact color="grey">no semanage</Label></Tooltip>}
              </>
            )}
            {status.apparmor && (
              <>
                {status.apparmor.parserVersion && <Label isCompact>parser {status.apparmor.parserVersion}</Label>}
                <Label isCompact color={status.apparmor.serviceActive ? "green" : "orange"}>service {status.apparmor.serviceActive ? "running" : "not running"}</Label>
                <Label isCompact>{status.apparmor.enforceCount} enforce</Label>
                <Label isCompact>{status.apparmor.complainCount} complain</Label>
                <Label isCompact>{status.apparmor.unconfinedCount} unconfined</Label>
                {status.apparmor.restrictUserns >= 0 && (
                  <Tooltip content="kernel.apparmor_restrict_unprivileged_userns">
                    <Label isCompact color={status.apparmor.restrictUserns === 1 ? "green" : "orange"}>userns restriction {status.apparmor.restrictUserns === 1 ? "on" : "off"}</Label>
                  </Tooltip>
                )}
                {status.apparmor.restrictUserns < 0 && <Tooltip content="This kernel has no apparmor_restrict_unprivileged_userns switch"><Label isCompact color="grey">no userns restriction</Label></Tooltip>}
              </>
            )}
            {(status.selinux?.bootFlags?.length || status.apparmor?.bootFlags?.length) ? (
              <Tooltip content="Kernel command line switches that affect the LSM"><Label isCompact color="orange">{(status.selinux?.bootFlags ?? status.apparmor?.bootFlags ?? []).join(" ")}</Label></Tooltip>
            ) : null}
            <Tooltip content="Reload what the node reported">
              <Button variant="plain" aria-label="Reload mandatory access control status" onClick={() => { void load(); }} isDisabled={loading}><SyncAltIcon /></Button>
            </Tooltip>
          </div>
          {status.reportedAt && (
            <p className="bor-text-secondary" style={{ fontSize: "0.8rem", marginBottom: "0.5rem" }}>
              Reported {timeAgo(status.reportedAt)}.{status.denialsSince ? ` Denials since ${new Date(status.denialsSince).toLocaleString()}: ${denialTotal} in ${denials.length} group${denials.length === 1 ? "" : "s"}.` : ""}
              {canOperate ? "" : " Proposals need the mac:operate permission."}
            </p>
          )}
          {!status.reportedAt && status.lsm && status.lsm !== "none" && (
            <p className="bor-text-secondary" style={{ fontSize: "0.85rem" }}>The node has not reported its inventory yet. Select Update metadata below once the agent is connected.</p>
          )}
          {status.lsm === "none" && <p className="bor-text-secondary" style={{ fontSize: "0.85rem" }}>This node runs neither SELinux nor AppArmor; SELinux and AppArmor policies report inapplicable here.</p>}
          {status.lsm && status.lsm !== "none" && (
            <Tabs activeKey={tab} onSelect={(_ev, k) => setTab(k)} aria-label="Mandatory access control details">
              <Tab eventKey="items" title={<TabTitleText>{lsm === "apparmor" ? "Profiles" : "Booleans & modules"}</TabTitleText>}>
                <div style={{ padding: "0.5rem 0" }}>
                  <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap", marginBottom: "0.5rem" }}>
                    <SearchInput aria-label="Search the inventory" placeholder="Search" value={query} onChange={(_ev, v) => setQuery(v)} onClear={() => setQuery("")} style={{ flex: "1 1 10rem" }} />
                    {lsm === "selinux" && (
                      <div role="group" aria-label="Item kind" style={{ display: "flex", gap: "0.25rem" }}>
                        <Button variant={effectiveKind === "boolean" ? "primary" : "secondary"} size="sm" onClick={() => setKind("boolean")}>Booleans</Button>
                        <Button variant={effectiveKind === "module" ? "primary" : "secondary"} size="sm" onClick={() => setKind("module")}>Modules</Button>
                      </div>
                    )}
                  </div>
                  <LiveAlert id={`${idp}-items-error`} variant="danger" isInline message={itemsError} />
                  {items === null && <Spinner size="sm" aria-label="Loading" />}
                  {items && items.length === 0 && <p className="bor-text-secondary" style={{ fontSize: "0.85rem" }}>{query ? "Nothing matches." : "Nothing reported yet."}</p>}
                  <ul style={{ listStyle: "none", margin: 0, padding: 0 }} aria-label="Inventory reported by the node">
                    {shown.map((it) => (
                      <li key={`${it.kind}/${it.name}`} style={{ display: "flex", gap: "0.5rem", alignItems: "flex-start", padding: "0.3rem 0", borderBottom: "1px solid var(--pf-t--global--border--color--default)" }}>
                        <div style={{ minWidth: 0, flex: 1 }}>
                          <div style={{ display: "flex", gap: "0.4rem", alignItems: "center", flexWrap: "wrap" }}>
                            <code style={{ fontSize: "0.85rem", wordBreak: "break-all" }}>{it.name}</code>
                            {it.kind === "boolean" && <Label isCompact color={it.info.current ? "green" : "grey"}>{it.info.current ? "on" : "off"}</Label>}
                            {it.kind === "boolean" && it.info.persistent !== undefined && it.info.persistent !== it.info.current && <Tooltip content="Changes at the next boot"><Label isCompact color="orange">pending {it.info.persistent ? "on" : "off"}</Label></Tooltip>}
                            {it.kind === "module" && <Label isCompact color={it.info.enabled ? "green" : "grey"}>{it.info.enabled ? "enabled" : "disabled"}</Label>}
                            {it.kind === "module" && typeof it.info.priority === "number" && <Label isCompact>priority {it.info.priority}</Label>}
                            {it.kind === "profile" && <Label isCompact color={it.info.loadedMode === "enforce" ? "green" : it.info.loadedMode === "complain" ? "orange" : it.info.loadedMode ? "grey" : "red"}>{it.info.loadedMode || "not loaded"}</Label>}
                            {it.kind === "profile" && it.info.managedBy && it.info.managedBy !== "vendor" && <Label isCompact color="grey">{it.info.managedBy}</Label>}
                            {it.kind === "profile" && it.info.unconfinedWithProfile && <Tooltip content="A process of this profile started before the profile was loaded; restart it to confine it"><Label isCompact color="orange">needs restart</Label></Tooltip>}
                            {it.kind === "profile" && it.info.attachmentConflict && <Tooltip content="Another profile attaches to the same executable; the kernel runs it unconfined"><Label isCompact color="red">attachment conflict</Label></Tooltip>}
                            {it.kind === "profile" && (it.info.foreignDisableLink || it.info.foreignComplainLink) && <Tooltip content="A disable or force-complain link exists that Bor did not create"><Label isCompact color="grey">local link</Label></Tooltip>}
                          </div>
                          <div className="bor-text-secondary" style={{ fontSize: "0.8rem" }}>
                            {it.description || (it.kind === "profile" && it.info.file && it.info.file !== it.name ? `file ${it.info.file}` : "")}
                            {it.kind === "profile" && it.info.attachedProcesses ? ` · ${it.info.attachedProcesses} process${it.info.attachedProcesses === 1 ? "" : "es"}` : ""}
                          </div>
                        </div>
                      </li>
                    ))}
                  </ul>
                  {items && items.length > shown.length && <p className="bor-text-secondary" style={{ fontSize: "0.8rem", marginTop: "0.5rem" }}>Showing {shown.length} of {items.length}; narrow the search to see the rest.</p>}
                </div>
              </Tab>
              <Tab eventKey="denials" title={<TabTitleText>Denials{denials.length ? ` (${denials.length})` : ""}</TabTitleText>}>
                <div style={{ padding: "0.5rem 0" }}>
                  <LiveAlert id={`${idp}-action-error`} variant="danger" isInline message={actionError} style={{ marginBottom: "0.5rem" }} />
                  {denials.length === 0 && <p className="bor-text-secondary" style={{ fontSize: "0.85rem" }}>No denials in the last report window.</p>}
                  <ul style={{ listStyle: "none", margin: 0, padding: 0 }} aria-label="Denials reported by the node">
                    {denials.map((d, i) => (
                      <li key={i} style={{ display: "flex", gap: "0.5rem", alignItems: "flex-start", justifyContent: "space-between", padding: "0.4rem 0", borderBottom: "1px solid var(--pf-t--global--border--color--default)" }}>
                        <div style={{ minWidth: 0, flex: 1 }}>
                          <div style={{ display: "flex", gap: "0.4rem", alignItems: "center", flexWrap: "wrap" }}>
                            <code style={{ fontSize: "0.85rem" }}>{d.subject}</code>
                            <Label isCompact color={d.permissive ? "orange" : "red"}>{d.count} × {d.operation || "denied"}{d.permissive ? " (logged only)" : ""}</Label>
                            {d.class && <Label isCompact>{d.class}</Label>}
                            {d.target && <span className="bor-text-secondary" style={{ fontSize: "0.8rem" }}>{d.target}</span>}
                            {d.execpath && <span className="bor-text-secondary" style={{ fontSize: "0.8rem" }}>by {d.execpath}</span>}
                            {d.lastSeen && <span className="bor-text-secondary" style={{ fontSize: "0.75rem" }}>last {timeAgo(d.lastSeen)}</span>}
                          </div>
                          {d.example && <div className="bor-text-secondary" style={{ fontSize: "0.75rem", fontFamily: "monospace", wordBreak: "break-all", marginTop: "0.15rem" }}>{d.example}</div>}
                        </div>
                        <div style={{ display: "flex", gap: "0.25rem", flexDirection: "column", alignItems: "flex-end" }}>
                          {lsm === "apparmor" && d.operation === "userns_create" && (
                            <Button variant="secondary" size="sm" onClick={() => showStub(d)}>Stub profile</Button>
                          )}
                          {canOperate && (
                            <Tooltip content={lsm === "selinux" ? "Ask the node for an audit2allow proposal from this domain's denials (nothing is applied)" : "Ask the node for rules derived from this profile's denials (nothing is applied)"}>
                              <Button variant="link" size="sm" isInline onClick={() => { void propose(d); }} isDisabled={!online || busy !== null} isLoading={busy === d.subject}>
                                Propose {lsm === "selinux" ? "module" : "rules"}
                              </Button>
                            </Tooltip>
                          )}
                        </div>
                      </li>
                    ))}
                  </ul>
                </div>
              </Tab>
            </Tabs>
          )}
        </>
      )}
      <Modal variant={ModalVariant.large} isOpen={proposal !== null} onClose={() => setProposal(null)} aria-labelledby={`${idp}-proposal-title`}>
        <ModalHeader title={proposal?.title ?? ""} labelId={`${idp}-proposal-title`} />
        <ModalBody>
          <p className="bor-text-secondary" style={{ fontSize: "0.85rem", marginBottom: "0.5rem" }}>{proposal?.message} Review every rule before adding it to a policy.</p>
          <ClipboardCopy isReadOnly isCode variant={ClipboardCopyVariant.expansion} isExpanded hoverTip="Copy" clickTip="Copied">
            {proposal?.text ?? ""}
          </ClipboardCopy>
        </ModalBody>
      </Modal>
    </>
  );
};
