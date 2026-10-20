// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * UpdatesPolicyEditor - a `Updates` (Software updates) policy: what the
 * node installs unattended, on which schedule, which packages are held
 * back, and when the node reboots afterwards. Content is protojson
 * (camelCase, enum names); see updatesModel.ts.
 */

import React, { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownList,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Label,
  MenuToggle,
  MenuToggleElement,
  Radio,
  Switch,
  Tab,
  TabTitleText,
  Tabs,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { ConfirmModal } from "../../components/ConfirmModal";
import { ListEditor } from "./ListEditor";
import { fetchNodesPaged, type Node } from "../../apiClient/nodesApi";
import { fetchNodeUpdates, updatesEngineLabel, updatesModeLabel as inventoryModeLabel, type UpdatesNodeStatus } from "../../apiClient/updatesApi";
import { SCHEDULE_PRESETS } from "./systemdModel";
import {
  hasLimits,
  limitProblem,
  UPDATES_MODE_OPTIONS,
  UPDATES_REBOOT_OPTIONS,
  UPDATES_TEMPLATES,
  UpdatesContent,
  UpdatesRebootPolicy,
  calendarProblem,
  delayProblem,
  holdProblem,
  parseUpdatesContent,
  serializeUpdatesContent,
  validateUpdatesContent,
  windowProblem,
} from "./updatesModel";

export interface UpdatesPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
  onValidityChange?: (ok: boolean) => void;
}

const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)", fontSize: "0.85rem" };
const mono: React.CSSProperties = { fontFamily: "monospace", fontSize: "0.85rem" };
const narrow: React.CSSProperties = { maxWidth: "20rem" };

function useNodes(): { nodes: Node[]; error: string | null } {
  const [nodes, setNodes] = useState<Node[]>([]);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchNodesPaged({ per_page: 200, sort_field: "name", sort_order: "asc" })
      .then((res) => { if (!cancelled) setNodes(res.items); })
      .catch((e: unknown) => { if (!cancelled) setError(e instanceof Error ? e.message : "Failed to load nodes"); });
    return () => { cancelled = true; };
  }, []);
  return { nodes, error };
}

/** One text field with its own validation message. */
const Field: React.FC<{
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  helper?: React.ReactNode;
  problem?: string | null;
  isDisabled?: boolean;
  style?: React.CSSProperties;
  type?: "text" | "number";
}> = ({ id, label, value, onChange, placeholder, helper, problem, isDisabled, style, type = "text" }) => (
  <FormGroup label={label} fieldId={id} style={style}>
    <TextInput
      id={id}
      type={type}
      value={value}
      onChange={(_ev, v) => onChange(v)}
      placeholder={placeholder}
      isDisabled={isDisabled}
      aria-invalid={problem ? true : undefined}
      aria-describedby={problem ? `${id}-error` : undefined}
      validated={problem ? "error" : "default"}
      style={type === "text" ? mono : undefined}
    />
    {helper && (
      <FormHelperText><HelperText><HelperTextItem>{helper}</HelperTextItem></HelperText></FormHelperText>
    )}
    <LiveAlert id={`${id}-error`} message={problem ?? null} variant="danger" isInline isPlain />
  </FormGroup>
);

export const UpdatesPolicyEditor: React.FC<UpdatesPolicyEditorProps> = ({ contentRaw, onChange, isDisabled, onValidityChange }) => {
  const idp = useId();
  const [model, setModel] = useState<UpdatesContent>(() => parseUpdatesContent(contentRaw));
  const lastRaw = useRef(contentRaw);
  const [tab, setTab] = useState<string | number>("mode");
  const [templateOpen, setTemplateOpen] = useState(false);
  const [pendingForce, setPendingForce] = useState<number | null>(null);
  const [pendingWithUsers, setPendingWithUsers] = useState(false);
  const [nodeId, setNodeId] = useState("");
  const [nodeState, setNodeState] = useState<UpdatesNodeStatus | null>(null);
  const [nodeError, setNodeError] = useState<string | null>(null);
  const { nodes, error: nodesError } = useNodes();

  useEffect(() => {
    if (contentRaw !== lastRaw.current) {
      lastRaw.current = contentRaw;
      setModel(parseUpdatesContent(contentRaw));
    }
  }, [contentRaw]);

  const update = useCallback((fn: (m: UpdatesContent) => UpdatesContent) => {
    setModel((prev) => {
      const next = fn(prev);
      const raw = serializeUpdatesContent(next);
      lastRaw.current = raw;
      onChange(raw);
      return next;
    });
  }, [onChange]);

  const problem = useMemo(() => validateUpdatesContent(model), [model]);
  useEffect(() => { onValidityChange?.(problem === null); }, [problem, onValidityChange]);

  useEffect(() => {
    if (!nodeId) { setNodeState(null); setNodeError(null); return; }
    let cancelled = false;
    fetchNodeUpdates(nodeId)
      .then((s) => { if (!cancelled) { setNodeState(s); setNodeError(null); } })
      .catch((e: unknown) => { if (!cancelled) setNodeError(e instanceof Error ? e.message : "Failed to load the node's update state"); });
    return () => { cancelled = true; };
  }, [nodeId]);

  const inv = nodeState?.inventory;
  const reboots = model.reboot.policy === "UPDATES_REBOOT_WHEN_NEEDED" || model.reboot.policy === "UPDATES_REBOOT_ALWAYS";
  const presetId = SCHEDULE_PRESETS.find((p) => p.expression && p.expression === model.schedule.onCalendar)?.id ?? (model.schedule.onCalendar ? "custom" : "");

  const setForceDays = (raw: string) => {
    const n = Math.max(0, Math.trunc(Number(raw) || 0));
    if (n > 0 && model.reboot.forceAfterDays === 0) { setPendingForce(n); return; }
    update((m) => ({ ...m, reboot: { ...m.reboot, forceAfterDays: n } }));
  };

  return (
    <div>
      <LiveAlert id={`${idp}-problem`} variant="warning" isInline message={problem} style={{ marginBottom: "0.75rem" }} />

      <div style={{ display: "flex", gap: "1rem", alignItems: "center", flexWrap: "wrap", marginBottom: "0.75rem" }}>
        <Dropdown
          isOpen={templateOpen}
          onOpenChange={setTemplateOpen}
          onSelect={() => setTemplateOpen(false)}
          toggle={(ref: React.Ref<MenuToggleElement>) => (
            <MenuToggle ref={ref} onClick={() => setTemplateOpen((o) => !o)} isExpanded={templateOpen} isDisabled={isDisabled} variant="secondary">Add a template</MenuToggle>
          )}
        >
          <DropdownList>
            {UPDATES_TEMPLATES.map((t) => (
              <DropdownItem key={t.id} description={t.description} onClick={() => update(() => ({ ...t.content }))}>{t.label}</DropdownItem>
            ))}
          </DropdownList>
        </Dropdown>
        <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap" }}>
          <label htmlFor={`${idp}-node`} style={{ fontSize: "0.85rem" }}>Show the state of</label>
          <FormSelect id={`${idp}-node`} value={nodeId} onChange={(_ev, v) => setNodeId(v)} aria-label="Node whose update state is shown" isDisabled={isDisabled} style={{ width: "16rem" }}>
            <FormSelectOption value="" label={nodes.length ? "no node" : "no nodes enrolled yet"} />
            {nodes.map((n) => <FormSelectOption key={n.id} value={n.id} label={n.name} />)}
          </FormSelect>
          <LiveAlert id={`${idp}-nodes-error`} variant="danger" isInline message={nodesError ?? nodeError} />
        </div>
      </div>
      {nodeId && nodeState && !inv && <p style={subtle}>This node has not reported its update state yet.</p>}
      {inv && (
        <div style={{ display: "flex", gap: "0.35rem", flexWrap: "wrap", marginBottom: "0.75rem" }}>
          <Label isCompact>{updatesEngineLabel(inv.engine ?? "")}{inv.engine && inv.engine !== "none" && !inv.engineInstalled ? " (not installed)" : ""}</Label>
          <Label isCompact>{inventoryModeLabel(inv.effectiveMode ?? "")}</Label>
          <Label isCompact color={(inv.pendingSecurity ?? 0) > 0 ? "red" : "grey"}>{inv.pendingTotal ?? 0} pending, {inv.pendingSecurity ?? 0} security</Label>
          <Label isCompact color={inv.reboot?.required ? "orange" : "green"}>{inv.reboot?.required ? "reboot pending" : "no reboot pending"}</Label>
        </div>
      )}

      <Tabs activeKey={tab} onSelect={(_ev, k) => setTab(k)} aria-label="Software updates policy sections">
        <Tab eventKey="mode" title={<TabTitleText>Mode</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <FormGroup label="Unattended updates" fieldId={`${idp}-mode`} role="radiogroup">
              {UPDATES_MODE_OPTIONS.map((o) => (
                <Radio
                  key={o.value}
                  id={`${idp}-mode-${o.value}`}
                  name={`${idp}-mode`}
                  label={o.label}
                  description={o.help}
                  isChecked={model.mode === o.value}
                  onChange={() => update((m) => ({ ...m, mode: o.value }))}
                  isDisabled={isDisabled}
                />
              ))}
            </FormGroup>
            <p style={{ ...subtle, marginTop: "0.75rem" }}>
              The engine is the distribution's own: dnf-automatic on Fedora and RHEL, unattended-upgrades on Debian and Ubuntu, os-update on SUSE, and rpm-ostree's automatic update policy on Kinoite and Silverblue. GNOME Software and Discover keep offering updates to users unless their automatic downloads are switched off with a Dconf or KDE Plasma policy.
            </p>
            <div style={{ display: "flex", gap: "1.5rem", flexWrap: "wrap", marginTop: "0.75rem" }}>
              <Switch id={`${idp}-install`} label="Install the engine when it is missing" isChecked={model.installEngine} onChange={(_ev, v) => update((m) => ({ ...m, installEngine: v }))} isDisabled={isDisabled} />
              <Switch id={`${idp}-require`} label="Treat nodes without an engine as non-compliant" isChecked={model.requireEngine} onChange={(_ev, v) => update((m) => ({ ...m, requireEngine: v }))} isDisabled={isDisabled} />
              <Switch id={`${idp}-unused`} label="Remove dependencies updates left unused" isChecked={model.removeUnusedDependencies} onChange={(_ev, v) => update((m) => ({ ...m, removeUnusedDependencies: v }))} isDisabled={isDisabled} />
            </div>
            {(model.mode === "UPDATES_MODE_SECURITY" || model.mode === "UPDATES_MODE_ALL") && (
              <div style={{ marginTop: "0.75rem" }}>
                <Switch id={`${idp}-offline`} label="Install offline at the reboot (Fedora 41 and later)" isChecked={model.offline} onChange={(_ev, v) => update((m) => ({ ...m, offline: v }))} isDisabled={isDisabled} />
                <p style={{ ...subtle, marginTop: "0.25rem" }}>
                  The engine only downloads; the agent stores a dnf5 offline transaction and the reboot of the Reboot tab installs it before the desktop comes up, so running programs never see half-applied updates. Other engines report this as inapplicable.
                </p>
              </div>
            )}
          </div>
        </Tab>

        <Tab eventKey="schedule" title={<TabTitleText>Schedule{model.schedule.onCalendar ? " (set)" : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, marginBottom: "0.5rem" }}>
              When the engine runs. Written as a drop-in of the distribution's timer; empty keeps the vendor schedule (daily, around 06:00 on most distributions).
            </p>
            <FormGroup label="Preset" fieldId={`${idp}-preset`} style={narrow}>
              <FormSelect id={`${idp}-preset`} value={presetId} onChange={(_ev, v) => {
                const p = SCHEDULE_PRESETS.find((x) => x.id === v);
                if (!p) { update((m) => ({ ...m, schedule: { ...m.schedule, onCalendar: "" } })); return; }
                if (p.expression) update((m) => ({ ...m, schedule: { ...m.schedule, onCalendar: p.expression } }));
              }} aria-label="Schedule preset" isDisabled={isDisabled}>
                <FormSelectOption value="" label="Vendor default" />
                {SCHEDULE_PRESETS.map((p) => <FormSelectOption key={p.id} value={p.id} label={p.label} />)}
              </FormSelect>
            </FormGroup>
            <div style={{ display: "flex", gap: "1rem", flexWrap: "wrap", marginTop: "0.75rem" }}>
              <Field id={`${idp}-cal`} label="Calendar expression" value={model.schedule.onCalendar} onChange={(v) => update((m) => ({ ...m, schedule: { ...m.schedule, onCalendar: v } }))} placeholder="*-*-* 02:00" helper="systemd calendar syntax, e.g. *-*-* 02:00 or Sat *-*-* 03:00." problem={model.schedule.onCalendar ? calendarProblem(model.schedule.onCalendar) : null} isDisabled={isDisabled} style={narrow} />
              <Field id={`${idp}-delay`} label="Random delay" value={model.schedule.randomizedDelay} onChange={(v) => update((m) => ({ ...m, schedule: { ...m.schedule, randomizedDelay: v } }))} placeholder="1h" helper="Spreads a fleet over the window after the scheduled time." problem={delayProblem(model.schedule.randomizedDelay)} isDisabled={isDisabled} style={{ maxWidth: "10rem" }} />
            </div>
            <Switch id={`${idp}-persistent`} label="Catch up a run missed while the node was off" isChecked={model.schedule.persistent !== false} onChange={(_ev, v) => update((m) => ({ ...m, schedule: { ...m.schedule, persistent: v ? null : false } }))} isDisabled={isDisabled || !model.schedule.onCalendar} style={{ marginTop: "0.75rem" }} />
          </div>
        </Tab>

        <Tab eventKey="reboot" title={<TabTitleText>Reboot{model.reboot.policy !== "UPDATES_REBOOT_UNSPECIFIED" ? " (set)" : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, marginBottom: "0.5rem" }}>
              Every engine's own reboot is switched off; the agent decides reboots with these rules, checked every minute while the policy applies.
            </p>
            <FormGroup label="After updates" fieldId={`${idp}-reboot`} role="radiogroup">
              {UPDATES_REBOOT_OPTIONS.map((o) => (
                <Radio
                  key={o.value}
                  id={`${idp}-reboot-${o.value}`}
                  name={`${idp}-reboot`}
                  label={o.label}
                  description={o.help}
                  isChecked={model.reboot.policy === o.value}
                  onChange={() => update((m) => ({ ...m, reboot: { ...m.reboot, policy: o.value as UpdatesRebootPolicy } }))}
                  isDisabled={isDisabled}
                />
              ))}
            </FormGroup>
            {reboots && (
              <>
                <div style={{ display: "flex", gap: "1rem", flexWrap: "wrap", marginTop: "0.75rem" }}>
                  <Field id={`${idp}-window`} label="Reboot window" value={model.reboot.window} onChange={(v) => update((m) => ({ ...m, reboot: { ...m.reboot, window: v } }))} placeholder="03:00-05:00" helper="Daily, local time, HH:MM-HH:MM; a window past midnight wraps. Empty: any time." problem={windowProblem(model.reboot.window)} isDisabled={isDisabled} style={{ maxWidth: "12rem" }} />
                  <Field id={`${idp}-force`} label="Force after days" type="number" value={String(model.reboot.forceAfterDays)} onChange={setForceDays} helper="Reboot despite logged-in users once the reboot has been pending this long; 0 never forces." isDisabled={isDisabled} style={{ maxWidth: "10rem" }} />
                  <Field id={`${idp}-warn`} label="Warning minutes" type="number" value={String(model.reboot.warnMinutes)} onChange={(v) => update((m) => ({ ...m, reboot: { ...m.reboot, warnMinutes: Math.max(0, Math.trunc(Number(v) || 0)) } }))} helper="Desktop warning before a reboot that affects logged-in users." isDisabled={isDisabled} style={{ maxWidth: "10rem" }} />
                </div>
                <Switch
                  id={`${idp}-sessions`}
                  label="Only reboot when nobody is logged in"
                  isChecked={model.reboot.onlyWithoutSessions}
                  onChange={(_ev, v) => { if (!v) { setPendingWithUsers(true); return; } update((m) => ({ ...m, reboot: { ...m.reboot, onlyWithoutSessions: true } })); }}
                  isDisabled={isDisabled}
                  style={{ marginTop: "0.75rem" }}
                />
                <Field id={`${idp}-message`} label="Warning message" value={model.reboot.message} onChange={(v) => update((m) => ({ ...m, reboot: { ...m.reboot, message: v } }))} placeholder="This computer will restart in 10 minutes to finish installing updates. Save your work." helper="Shown as a desktop notification; a default is used when empty." isDisabled={isDisabled} style={{ maxWidth: "40rem", marginTop: "0.75rem" }} />
              </>
            )}
          </div>
        </Tab>

        <Tab eventKey="holds" title={<TabTitleText>Holds{model.hold.length ? ` (${model.hold.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, marginBottom: "0.5rem" }}>
              Packages the engine must not upgrade: a name or a pattern with * and ?. A hold keeps a vulnerable package on every node of the group and is audit-logged; image systems ignore holds.
            </p>
            <ListEditor
              items={model.hold}
              renderItem={(h, i) => {
                const p = holdProblem(h);
                return (
                  <div style={{ flex: 1, maxWidth: "24rem" }}>
                    <TextInput
                      id={`${idp}-hold-${i}`}
                      value={h}
                      onChange={(_ev, v) => update((m) => ({ ...m, hold: m.hold.map((x, j) => (j === i ? v : x)) }))}
                      placeholder="linux-image-*"
                      isDisabled={isDisabled}
                      aria-label={`Hold ${i + 1}`}
                      aria-invalid={p ? true : undefined}
                      aria-describedby={p ? `${idp}-hold-${i}-error` : undefined}
                      validated={p ? "error" : "default"}
                      style={mono}
                    />
                    <LiveAlert id={`${idp}-hold-${i}-error`} message={p} variant="danger" isInline isPlain />
                  </div>
                );
              }}
              onRemove={(i) => update((m) => ({ ...m, hold: m.hold.filter((_, j) => j !== i) }))}
              onAdd={() => update((m) => ({ ...m, hold: [...m.hold, ""] }))}
              addLabel="Add a hold"
              removeAriaLabel={(i) => `Remove hold ${i + 1}`}
              emptyText="No packages are held back."
              isDisabled={isDisabled}
            />
          </div>
        </Tab>

        <Tab eventKey="limits" title={<TabTitleText>Limits{hasLimits(model.limits) ? " (set)" : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, marginBottom: "0.5rem" }}>
              Compliance thresholds, in days. A node that misses one is non-compliant on the Compliance page, where the items carry the CIS patching control. 0 leaves a threshold unchecked. The ages are measured on the node: how long each security update has been pending there, since when a reboot is pending, and when the package metadata was last refreshed.
            </p>
            <div style={{ display: "flex", gap: "1rem", flexWrap: "wrap" }}>
              <Field id={`${idp}-lim-security`} label="Security update pending for more than" type="number" value={String(model.limits.securityMaxAgeDays)} onChange={(v) => update((m) => ({ ...m, limits: { ...m.limits, securityMaxAgeDays: Math.max(0, Math.trunc(Number(v) || 0)) } }))} helper="Days a security update may stay pending on a node. 14 is a common patch SLA." problem={limitProblem(model.limits.securityMaxAgeDays)} isDisabled={isDisabled} style={{ maxWidth: "14rem" }} />
              <Field id={`${idp}-lim-reboot`} label="Reboot pending for more than" type="number" value={String(model.limits.rebootMaxAgeDays)} onChange={(v) => update((m) => ({ ...m, limits: { ...m.limits, rebootMaxAgeDays: Math.max(0, Math.trunc(Number(v) || 0)) } }))} helper="Days a node may run with a reboot pending after updates." problem={limitProblem(model.limits.rebootMaxAgeDays)} isDisabled={isDisabled} style={{ maxWidth: "14rem" }} />
              <Field id={`${idp}-lim-check`} label="Metadata older than" type="number" value={String(model.limits.checkMaxAgeDays)} onChange={(v) => update((m) => ({ ...m, limits: { ...m.limits, checkMaxAgeDays: Math.max(0, Math.trunc(Number(v) || 0)) } }))} helper="Days since the engine last refreshed its metadata; catches nodes whose engine stopped running." problem={limitProblem(model.limits.checkMaxAgeDays)} isDisabled={isDisabled} style={{ maxWidth: "14rem" }} />
            </div>
          </div>
        </Tab>

        <Tab eventKey="firmware" title={<TabTitleText>Firmware{model.firmware.refreshMetadata !== null || model.firmware.reportPending ? " (set)" : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, marginBottom: "0.5rem" }}>
              Firmware updates come from fwupd, when it is installed. Nodes always list the pending firmware updates in their inventory; these settings control fwupd's daily metadata refresh and whether pending firmware counts against compliance. Firmware is never installed by policy.
            </p>
            <FormGroup label="Metadata refresh (fwupd-refresh.timer)" fieldId={`${idp}-fw-refresh`} role="radiogroup">
              {([["keep", "Keep the node's state", null], ["on", "Enabled: refresh the firmware metadata daily", true], ["off", "Disabled", false]] as const).map(([id, label, value]) => (
                <Radio key={id} id={`${idp}-fw-refresh-${id}`} name={`${idp}-fw-refresh`} label={label} isChecked={model.firmware.refreshMetadata === value} onChange={() => update((m) => ({ ...m, firmware: { ...m.firmware, refreshMetadata: value } }))} isDisabled={isDisabled} />
              ))}
            </FormGroup>
            <Switch id={`${idp}-fw-report`} label="Report pending firmware updates as non-compliant" isChecked={model.firmware.reportPending} onChange={(_ev, v) => update((m) => ({ ...m, firmware: { ...m.firmware, reportPending: v } }))} isDisabled={isDisabled} style={{ marginTop: "0.75rem" }} />
          </div>
        </Tab>
      </Tabs>

      <ConfirmModal
        isOpen={pendingForce !== null}
        title="Reboot nodes with users logged in?"
        confirmLabel="Allow forced reboots"
        isDanger
        onCancel={() => setPendingForce(null)}
        onConfirm={() => { const n = pendingForce ?? 0; setPendingForce(null); update((m) => ({ ...m, reboot: { ...m.reboot, forceAfterDays: n, warnMinutes: m.reboot.warnMinutes || 10 } })); }}
      >
        After {pendingForce} days with a reboot pending, every node this policy reaches restarts even while someone is working on it. Unsaved work is lost. Users get the warning notification first; keep the warning minutes long enough to react.
      </ConfirmModal>
      <ConfirmModal
        isOpen={pendingWithUsers}
        title="Reboot while users are logged in?"
        confirmLabel="Reboot with users logged in"
        isDanger
        onCancel={() => setPendingWithUsers(false)}
        onConfirm={() => { setPendingWithUsers(false); update((m) => ({ ...m, reboot: { ...m.reboot, onlyWithoutSessions: false, warnMinutes: m.reboot.warnMinutes || 10 } })); }}
      >
        With this off the node restarts in the window whether or not someone is working on it, after the warning. Unsaved work is lost. Prefer leaving it on with a force-after period.
      </ConfirmModal>
    </div>
  );
};
