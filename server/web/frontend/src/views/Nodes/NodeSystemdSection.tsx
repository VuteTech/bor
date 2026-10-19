// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * NodeSystemdSection - the "Services & timers" block of the node drawer:
 * the units the node reported (searchable, filtered by type) with their
 * state, the next run of timers, the sandboxing score of services, and
 * start / stop / restart actions for holders of systemd:operate. An
 * action runs on the node at once and the row shows the state the agent
 * reported back. Self-contained: fetches its own data.
 */

import React, { useCallback, useEffect, useId, useMemo, useState } from "react";
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownList,
  FormSelect,
  FormSelectOption,
  Label,
  MenuToggle,
  MenuToggleElement,
  SearchInput,
  Spinner,
  Title,
  Tooltip,
} from "@patternfly/react-core";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import SyncAltIcon from "@patternfly/react-icons/dist/esm/icons/sync-alt-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import { hasPermission } from "../../apiClient/permissions";
import {
  fetchNodeSystemdUnits,
  runSystemdUnitAction,
  type SystemdCatalogueUnit,
  type SystemdUnitActionVerb,
} from "../../apiClient/systemdApi";

/** How many rows the drawer lists before asking for a narrower search. */
const MAX_ROWS = 60;

const TYPE_OPTIONS: { value: string; label: string }[] = [
  { value: "", label: "All types" },
  { value: "service", label: "Services" },
  { value: "timer", label: "Timers" },
  { value: "socket", label: "Sockets" },
  { value: "path", label: "Paths" },
  { value: "target", label: "Targets" },
];

type LabelColor = React.ComponentProps<typeof Label>["color"];

function activeColor(state: string): LabelColor {
  switch (state) {
    case "active":
    case "reloading":
    case "activating":
      return "green";
    case "failed":
      return "red";
    default:
      return "grey";
  }
}

function scoreColor(predicate: string): LabelColor {
  switch (predicate) {
    case "SAFE":
    case "OK":
      return "green";
    case "MEDIUM":
      return "yellow";
    case "EXPOSED":
      return "orange";
    case "UNSAFE":
      return "red";
    default:
      return "grey";
  }
}

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  if (Number.isNaN(diff)) return "";
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours} h ago`;
  return `${Math.floor(hours / 24)} d ago`;
}

const VERB_LABEL: Record<SystemdUnitActionVerb, string> = { start: "Start", stop: "Stop", restart: "Restart" };

interface Props {
  nodeId: string;
  nodeName: string;
  /** Actions need a connected agent; the menu is disabled otherwise. */
  online: boolean;
}

interface PendingAction {
  unit: SystemdCatalogueUnit;
  verb: SystemdUnitActionVerb;
}

export const NodeSystemdSection: React.FC<Props> = ({ nodeId, nodeName, online }) => {
  const idp = useId();
  const canOperate = hasPermission("systemd:operate");
  const { addToast } = useToast();

  const [units, setUnits] = useState<SystemdCatalogueUnit[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [type, setType] = useState("");
  const [menuOpen, setMenuOpen] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<PendingAction | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setUnits(await fetchNodeSystemdUnits(nodeId, { q: query.trim(), type }));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to load the units");
    } finally {
      setLoading(false);
    }
  }, [nodeId, query, type]);

  // Debounce the search; a node change reloads at once.
  useEffect(() => {
    setUnits(null);
    const handle = window.setTimeout(() => { void load(); }, query ? 250 : 0);
    return () => window.clearTimeout(handle);
  }, [load, query]);

  const reportedAt = useMemo(() => {
    if (!units || units.length === 0) return null;
    return units.reduce((latest, u) => (u.reportedAt > latest ? u.reportedAt : latest), units[0].reportedAt);
  }, [units]);

  const run = async (unit: SystemdCatalogueUnit, verb: SystemdUnitActionVerb) => {
    setBusy(unit.name);
    setActionError(null);
    try {
      const res = await runSystemdUnitAction(nodeId, unit.name, verb);
      if (res.unitState) {
        const next = res.unitState;
        setUnits((prev) => prev?.map((u) => (u.name === next.name ? { ...u, ...next } : u)) ?? prev);
      }
      addToast({
        variant: res.success ? "success" : "danger",
        title: `${VERB_LABEL[verb]} ${unit.name} on ${nodeName}`,
        detail: res.message,
      });
    } catch (e: unknown) {
      setActionError(e instanceof Error ? e.message : `${VERB_LABEL[verb]} failed`);
    } finally {
      setBusy(null);
    }
  };

  const request = (unit: SystemdCatalogueUnit, verb: SystemdUnitActionVerb) => {
    setMenuOpen(null);
    if (unit.risky && verb !== "start") {
      setConfirm({ unit, verb });
      return;
    }
    void run(unit, verb);
  };

  const shown = units ? units.slice(0, MAX_ROWS) : [];
  const hidden = units ? units.length - shown.length : 0;

  return (
    <>
      <Title headingLevel="h3" size="md" style={{ marginTop: "1.5rem", marginBottom: "0.5rem" }}>
        Services &amp; timers
      </Title>
      <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap", marginBottom: "0.5rem" }}>
        <SearchInput
          aria-label="Search units"
          placeholder="Search units"
          value={query}
          onChange={(_ev, v) => setQuery(v)}
          onClear={() => setQuery("")}
          style={{ flex: "1 1 10rem" }}
        />
        <FormSelect
          id={`${idp}-type`}
          value={type}
          onChange={(_ev, v) => setType(v)}
          aria-label="Unit type"
          style={{ flex: "0 1 8rem" }}
        >
          {TYPE_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
        </FormSelect>
        <Tooltip content="Reload the list the node reported">
          <Button variant="plain" aria-label="Reload units" onClick={() => { void load(); }} isDisabled={loading}>
            <SyncAltIcon />
          </Button>
        </Tooltip>
      </div>
      {loading && <Spinner size="md" aria-label="Loading" />}
      <LiveAlert id={`${idp}-error`} variant="danger" isInline message={error} />
      <LiveAlert id={`${idp}-action-error`} variant="danger" isInline message={actionError} style={{ marginBottom: "0.5rem" }} />
      {units && units.length === 0 && !loading && (
        <p className="bor-text-secondary" style={{ fontSize: "0.85rem" }}>
          {query || type
            ? "No unit matches."
            : "The node has not reported its units yet. Select Update metadata below once the agent is connected."}
        </p>
      )}
      {reportedAt && (
        <p className="bor-text-secondary" style={{ fontSize: "0.8rem", marginBottom: "0.5rem" }}>
          {units?.length} {units?.length === 1 ? "unit" : "units"}, reported {timeAgo(reportedAt)}.
          {canOperate ? "" : " Actions need the systemd:operate permission."}
        </p>
      )}
      <ul style={{ listStyle: "none", margin: 0, padding: 0 }} aria-label="Units reported by the node">
        {shown.map((u) => (
          <li
            key={u.name}
            style={{
              display: "flex", gap: "0.5rem", alignItems: "flex-start", justifyContent: "space-between",
              padding: "0.4rem 0", borderBottom: "1px solid var(--pf-t--global--border--color--default)",
            }}
          >
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{ display: "flex", gap: "0.4rem", alignItems: "center", flexWrap: "wrap" }}>
                <code style={{ fontSize: "0.85rem", wordBreak: "break-all" }}>{u.name}</code>
                <Label isCompact color={activeColor(u.activeState)}>
                  {u.activeState || "unknown"}{u.subState ? ` (${u.subState})` : ""}
                </Label>
                {u.risky && (
                  <Tooltip content="Stopping this unit can cut users or administrators off from the node">
                    <Label isCompact color="orange">risky</Label>
                  </Tooltip>
                )}
              </div>
              <div className="bor-text-secondary" style={{ fontSize: "0.8rem", marginTop: "0.15rem" }}>
                {u.unitFileState || "no unit-file state"}
                {u.description ? ` · ${u.description}` : ""}
                {u.type === "timer" && u.onCalendar.length > 0 ? ` · schedule ${u.onCalendar.join(" | ")}` : ""}
                {u.triggers && u.type !== "service" ? ` · activates ${u.triggers}` : ""}
              </div>
              {u.type === "service" && u.securityPredicate && (
                <div style={{ marginTop: "0.2rem" }}>
                  <Tooltip content="Sandboxing exposure as systemd-analyze security rates it: 0 is fully sandboxed, 10 is unrestricted">
                    <Label isCompact color={scoreColor(u.securityPredicate)}>
                      exposure {u.securityExposure} {u.securityPredicate}
                    </Label>
                  </Tooltip>
                </div>
              )}
            </div>
            {canOperate && u.type !== "target" && (
              <Dropdown
                isOpen={menuOpen === u.name}
                onOpenChange={(open) => setMenuOpen(open ? u.name : null)}
                onSelect={(_ev, value) => request(u, String(value) as SystemdUnitActionVerb)}
                popperProps={{ position: "right" }}
                toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={toggleRef}
                    variant="plain"
                    aria-label={`Actions for ${u.name}`}
                    onClick={() => setMenuOpen((cur) => (cur === u.name ? null : u.name))}
                    isExpanded={menuOpen === u.name}
                    isDisabled={!online || busy !== null}
                    icon={busy === u.name ? <Spinner size="sm" aria-label="Loading" /> : <EllipsisVIcon />}
                  />
                )}
              >
                <DropdownList aria-label={`Actions for ${u.name}`}>
                  <DropdownItem value="start" isDisabled={u.activeState === "active"}>Start</DropdownItem>
                  <DropdownItem value="stop" isDisabled={u.activeState !== "active" && u.activeState !== "failed"}>Stop</DropdownItem>
                  <DropdownItem value="restart">Restart</DropdownItem>
                </DropdownList>
              </Dropdown>
            )}
          </li>
        ))}
      </ul>
      {hidden > 0 && (
        <p className="bor-text-secondary" style={{ fontSize: "0.8rem", marginTop: "0.5rem" }}>
          Showing {shown.length} of {units?.length} units; narrow the search to see the rest.
        </p>
      )}
      {canOperate && !online && units && units.length > 0 && (
        <p className="bor-text-secondary" style={{ fontSize: "0.8rem", marginTop: "0.5rem" }}>
          Actions are available while the node is online.
        </p>
      )}
      <ConfirmModal
        isOpen={confirm !== null}
        title={confirm ? `${VERB_LABEL[confirm.verb]} ${confirm.unit.name} on ${nodeName}?` : ""}
        confirmLabel={confirm ? VERB_LABEL[confirm.verb] : "Confirm"}
        isDanger
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          const pending = confirm;
          setConfirm(null);
          if (pending) void run(pending.unit, pending.verb);
        }}
      >
        {confirm?.verb === "stop"
          ? "This unit can cut users or administrators off from the node. Stopping it takes effect at once; a bound policy that keeps it running starts it again on its next pass."
          : "This unit can cut users or administrators off from the node while it restarts."}
      </ConfirmModal>
    </>
  );
};
