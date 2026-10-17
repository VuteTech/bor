// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * SystemdPolicyEditor - the units of a `Systemd` ("Services & timers")
 * policy: for each existing unit its enablement, its activity and an
 * optional drop-in with timer or service overrides. Content is protojson
 * (camelCase, enum names); see systemdModel.ts. Unit names come from the
 * catalogue nodes report (free text is allowed for units no node has
 * reported yet) and a node picker shows the chosen node's current states.
 */

import React, { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import {
  Button,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  ExpandableSection,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectList,
  SelectOption,
  Switch,
  TextInput,
  TextInputGroup,
  TextInputGroupMain,
  Title,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ListEditor } from "./ListEditor";
import { fetchNodesPaged, type Node } from "../../apiClient/nodesApi";
import { fetchSystemdUnits, type SystemdCatalogueUnit } from "../../apiClient/systemdApi";
import {
  ACTIVITY_OPTIONS,
  ENABLEMENT_OPTIONS,
  PROTECT_HOME_OPTIONS,
  PROTECT_SYSTEM_OPTIONS,
  RESTART_OPTIONS,
  SCHEDULE_PRESETS,
  SystemdActivity,
  SystemdContent,
  SystemdEnablement,
  SystemdServiceOverrides,
  SystemdTimerOverrides,
  SystemdUnitEntry,
  calendarProblem,
  isRiskyUnit,
  newUnitEntry,
  parseSystemdContent,
  serializeSystemdContent,
  serviceIsEmpty,
  timeSpanProblem,
  timerIsEmpty,
  unitEntryProblem,
  unitNameProblem,
  unitType,
  validateSystemdContent,
} from "./systemdModel";

export interface SystemdPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
  onValidityChange?: (ok: boolean) => void;
}

const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)" };

/** "enabled, active (running)" from a catalogue unit. */
function describeState(u: SystemdCatalogueUnit): string {
  const file = u.unitFileState || "unknown";
  const active = u.activeState ? `${u.activeState}${u.subState ? ` (${u.subState})` : ""}` : "state unknown";
  return `${file}, ${active}`;
}

/* ── creatable unit-name typeahead ── */

interface UnitNameTypeaheadProps {
  id: string;
  value: string;
  onChange: (name: string) => void;
  /** Called with the text typed so far so the caller can refresh `suggestions`. */
  onQueryChange: (q: string) => void;
  suggestions: SystemdCatalogueUnit[];
  /** The option description: per-node state when a node is picked, otherwise the node count. */
  describe: (u: SystemdCatalogueUnit) => string;
  isDisabled?: boolean;
  ariaLabel: string;
  isInvalid: boolean;
  describedBy?: string;
}

const MAX_SUGGESTIONS = 50;

/**
 * A typeahead that accepts any text (the policy may name a unit no node has
 * reported yet) and offers the catalogue as suggestions. The typed text is
 * the value: every keystroke updates the policy content.
 */
const UnitNameTypeahead: React.FC<UnitNameTypeaheadProps> = ({
  id, value, onChange, onQueryChange, suggestions, describe, isDisabled, ariaLabel, isInvalid, describedBy,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const [focusedIndex, setFocusedIndex] = useState<number | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listboxId = `${id}-listbox`;
  const optionId = (i: number) => `${id}-option-${i}`;
  const shown = suggestions.slice(0, MAX_SUGGESTIONS);

  const close = () => {
    setIsOpen(false);
    setFocusedIndex(null);
  };
  const choose = (name: string) => {
    onChange(name);
    close();
  };
  const onText = (_ev: React.FormEvent<HTMLInputElement>, text: string) => {
    onChange(text);
    onQueryChange(text);
    setFocusedIndex(null);
    if (!isOpen) setIsOpen(true);
  };
  const onKeyDown = (ev: React.KeyboardEvent<HTMLInputElement>) => {
    const count = shown.length;
    switch (ev.key) {
      case "ArrowDown":
      case "ArrowUp": {
        ev.preventDefault();
        if (!isOpen) { setIsOpen(true); onQueryChange(value); return; }
        if (count === 0) return;
        const cur = focusedIndex ?? -1;
        setFocusedIndex(ev.key === "ArrowDown" ? (cur + 1) % count : (cur - 1 + count) % count);
        break;
      }
      case "Enter":
        if (isOpen && focusedIndex !== null && shown[focusedIndex]) {
          ev.preventDefault();
          choose(shown[focusedIndex].name);
        } else if (isOpen) {
          ev.preventDefault();
          close();
        }
        break;
      case "Escape":
        close();
        break;
    }
  };

  const toggle = (toggleRef: React.Ref<MenuToggleElement>) => (
    <MenuToggle
      ref={toggleRef}
      variant="typeahead"
      aria-label={ariaLabel}
      onClick={() => { if (isOpen) close(); else { setIsOpen(true); onQueryChange(value); } }}
      isExpanded={isOpen}
      isFullWidth
      isDisabled={isDisabled}
    >
      <TextInputGroup isPlain>
        <TextInputGroupMain
          value={value}
          onClick={(ev) => { ev.stopPropagation(); if (!isOpen) { setIsOpen(true); onQueryChange(value); } }}
          onChange={onText}
          innerRef={inputRef}
          placeholder="bluetooth.service, fstrim.timer, cups.socket"
          role="combobox"
          aria-label={ariaLabel}
          aria-controls={listboxId}
          aria-activedescendant={focusedIndex !== null ? optionId(focusedIndex) : undefined}
          aria-invalid={isInvalid ? true : undefined}
          aria-describedby={describedBy}
          isExpanded={isOpen}
          inputProps={{ autoComplete: "off", onKeyDown, spellCheck: false }}
        />
      </TextInputGroup>
    </MenuToggle>
  );

  return (
    <Select
      id={id}
      isOpen={isOpen}
      selected={value}
      onSelect={(_ev, v) => v != null && choose(String(v))}
      onOpenChange={(open) => (open ? setIsOpen(true) : close())}
      toggle={toggle}
      isScrollable
      shouldFocusFirstItemOnOpen={false}
      popperProps={{ width: "trigger" }}
    >
      <SelectList id={listboxId} aria-label="Units reported by nodes">
        {shown.length === 0 ? (
          <SelectOption isDisabled value="__none__" id={`${id}-empty`}>
            {value ? "No reported unit matches; the name is used as typed" : "Type a unit name or pick one reported by a node"}
          </SelectOption>
        ) : (
          shown.map((u, i) => (
            <SelectOption
              key={u.name}
              id={optionId(i)}
              value={u.name}
              isFocused={focusedIndex === i}
              description={describe(u)}
            >
              {u.name}
            </SelectOption>
          ))
        )}
      </SelectList>
    </Select>
  );
};

/* ── small field helpers ── */

/** A text field whose value is validated by `problem` and linked to its own LiveAlert. */
const ValidatedTextInput: React.FC<{
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  problem: (v: string) => string | null;
  helper?: React.ReactNode;
  isDisabled?: boolean;
  style?: React.CSSProperties;
}> = ({ id, label, value, onChange, placeholder, problem, helper, isDisabled, style }) => {
  const error = value ? problem(value) : null;
  return (
    <FormGroup label={label} fieldId={id} style={style}>
      <TextInput
        id={id}
        value={value}
        onChange={(_ev, v) => onChange(v)}
        placeholder={placeholder}
        isDisabled={isDisabled}
        aria-label={label}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? `${id}-error` : undefined}
        validated={error ? "error" : "default"}
      />
      {helper && !error && (
        <FormHelperText>
          <HelperText><HelperTextItem>{helper}</HelperTextItem></HelperText>
        </FormHelperText>
      )}
      <LiveAlert id={`${id}-error`} message={error} variant="danger" isInline isPlain />
    </FormGroup>
  );
};

/** Keep / Yes / No for an optional boolean directive. */
const TriStateSelect: React.FC<{
  id: string;
  label: string;
  value: boolean | undefined;
  onChange: (v: boolean | undefined) => void;
  helper?: React.ReactNode;
  isDisabled?: boolean;
  style?: React.CSSProperties;
}> = ({ id, label, value, onChange, helper, isDisabled, style }) => (
  <FormGroup label={label} fieldId={id} style={style}>
    <FormSelect
      id={id}
      value={value === undefined ? "" : value ? "yes" : "no"}
      onChange={(_ev, v) => onChange(v === "" ? undefined : v === "yes")}
      isDisabled={isDisabled}
      aria-label={label}
    >
      <FormSelectOption value="" label="Keep" />
      <FormSelectOption value="yes" label="Yes" />
      <FormSelectOption value="no" label="No" />
    </FormSelect>
    {helper && (
      <FormHelperText>
        <HelperText><HelperTextItem>{helper}</HelperTextItem></HelperText>
      </FormHelperText>
    )}
  </FormGroup>
);

const row: React.CSSProperties = { display: "flex", gap: "1rem", flexWrap: "wrap", alignItems: "flex-start" };
const narrow: React.CSSProperties = { flex: "1 1 12rem", maxWidth: "16rem" };
const wide: React.CSSProperties = { flex: "1 1 20rem" };

/* ── timer overrides ── */

const TimerOverrides: React.FC<{
  idp: string;
  value: SystemdTimerOverrides;
  onChange: (t: SystemdTimerOverrides) => void;
  isDisabled?: boolean;
}> = ({ idp, value, onChange, isDisabled }) => {
  const [preset, setPreset] = useState("");
  const applyPreset = (id: string) => {
    setPreset(id);
    const p = SCHEDULE_PRESETS.find((x) => x.id === id);
    if (!p) return;
    if (p.id === "custom") {
      if (value.onCalendar.length === 0) onChange({ ...value, onCalendar: [""] });
      return;
    }
    onChange({ ...value, onCalendar: [p.expression] });
  };
  return (
    <>
      <div style={row}>
        <FormGroup label="Schedule preset" fieldId={`${idp}-preset`} style={narrow}>
          <FormSelect id={`${idp}-preset`} value={preset} onChange={(_ev, v) => applyPreset(v)} isDisabled={isDisabled} aria-label="Schedule preset">
            <FormSelectOption value="" label="Choose a preset..." isDisabled />
            {SCHEDULE_PRESETS.map((p) => <FormSelectOption key={p.id} value={p.id} label={p.label} />)}
          </FormSelect>
          <FormHelperText>
            <HelperText><HelperTextItem>Replaces the vendor OnCalendar schedule.</HelperTextItem></HelperText>
          </FormHelperText>
        </FormGroup>
        <FormGroup label="OnCalendar" fieldId={`${idp}-cal`} style={wide}>
          <ListEditor
            items={value.onCalendar}
            renderItem={(expr, i) => {
              const error = expr ? calendarProblem(expr) : null;
              return (
                <div style={{ flex: 1 }}>
                  <TextInput
                    id={`${idp}-cal-${i}`}
                    value={expr}
                    onChange={(_ev, v) => onChange({ ...value, onCalendar: value.onCalendar.map((x, j) => (j === i ? v : x)) })}
                    placeholder="Sat *-*-* 03:00:00"
                    isDisabled={isDisabled}
                    aria-label={`Calendar expression ${i + 1}`}
                    aria-invalid={error ? true : undefined}
                    aria-describedby={error ? `${idp}-cal-${i}-error` : undefined}
                    validated={error ? "error" : "default"}
                    style={{ fontFamily: "monospace" }}
                  />
                  <LiveAlert id={`${idp}-cal-${i}-error`} message={error} variant="danger" isInline isPlain />
                </div>
              );
            }}
            onRemove={(i) => onChange({ ...value, onCalendar: value.onCalendar.filter((_, j) => j !== i) })}
            onAdd={() => onChange({ ...value, onCalendar: [...value.onCalendar, ""] })}
            addLabel="Add expression"
            removeAriaLabel={(i) => `Remove calendar expression ${i + 1}`}
            addVariant="link"
            emptyText="No calendar expression: the vendor schedule stays."
            isDisabled={isDisabled}
          />
        </FormGroup>
      </div>
      <div style={row}>
        <ValidatedTextInput id={`${idp}-onboot`} label="OnBootSec" value={value.onBootSec} onChange={(v) => onChange({ ...value, onBootSec: v })} placeholder="15min" problem={timeSpanProblem} helper="Run this long after boot." isDisabled={isDisabled} style={narrow} />
        <ValidatedTextInput id={`${idp}-onunitactive`} label="OnUnitActiveSec" value={value.onUnitActiveSec} onChange={(v) => onChange({ ...value, onUnitActiveSec: v })} placeholder="1h" problem={timeSpanProblem} helper="Repeat this long after the last activation." isDisabled={isDisabled} style={narrow} />
        <ValidatedTextInput id={`${idp}-random`} label="RandomizedDelaySec" value={value.randomizedDelaySec} onChange={(v) => onChange({ ...value, randomizedDelaySec: v })} placeholder="1h" problem={timeSpanProblem} helper="Spreads the fleet over a window." isDisabled={isDisabled} style={narrow} />
        <ValidatedTextInput id={`${idp}-accuracy`} label="AccuracySec" value={value.accuracySec} onChange={(v) => onChange({ ...value, accuracySec: v })} placeholder="1min" problem={timeSpanProblem} isDisabled={isDisabled} style={narrow} />
      </div>
      <div style={row}>
        <TriStateSelect id={`${idp}-persistent`} label="Persistent" value={value.persistent} onChange={(v) => onChange({ ...value, persistent: v })} helper="Catch up a run missed while the node was off." isDisabled={isDisabled} style={narrow} />
        <TriStateSelect id={`${idp}-wake`} label="WakeSystem" value={value.wakeSystem} onChange={(v) => onChange({ ...value, wakeSystem: v })} helper="Wake a suspended node for the run." isDisabled={isDisabled} style={narrow} />
      </div>
    </>
  );
};

/* ── service overrides ── */

const ServiceOverrides: React.FC<{
  idp: string;
  value: SystemdServiceOverrides;
  onChange: (s: SystemdServiceOverrides) => void;
  isDisabled?: boolean;
}> = ({ idp, value, onChange, isDisabled }) => (
  <>
    <div style={row}>
      <FormGroup label="Restart" fieldId={`${idp}-restart`} style={narrow}>
        <FormSelect id={`${idp}-restart`} value={value.restart} onChange={(_ev, v) => onChange({ ...value, restart: v })} isDisabled={isDisabled} aria-label="Restart policy">
          {RESTART_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
        </FormSelect>
      </FormGroup>
      <ValidatedTextInput id={`${idp}-restartsec`} label="RestartSec" value={value.restartSec} onChange={(v) => onChange({ ...value, restartSec: v })} placeholder="5s" problem={timeSpanProblem} isDisabled={isDisabled} style={narrow} />
      <ValidatedTextInput id={`${idp}-tstart`} label="TimeoutStartSec" value={value.timeoutStartSec} onChange={(v) => onChange({ ...value, timeoutStartSec: v })} placeholder="90s" problem={timeSpanProblem} isDisabled={isDisabled} style={narrow} />
      <ValidatedTextInput id={`${idp}-tstop`} label="TimeoutStopSec" value={value.timeoutStopSec} onChange={(v) => onChange({ ...value, timeoutStopSec: v })} placeholder="30s" problem={timeSpanProblem} isDisabled={isDisabled} style={narrow} />
    </div>
    <FormGroup label="Environment" fieldId={`${idp}-env`}>
      <ListEditor
        items={value.environment}
        renderItem={(e, i) => {
          const error = e && !/^[A-Za-z_][A-Za-z0-9_]*=/.test(e) ? "Must be KEY=value" : null;
          return (
            <div style={{ flex: 1, maxWidth: "32rem" }}>
              <TextInput
                id={`${idp}-env-${i}`}
                value={e}
                onChange={(_ev, v) => onChange({ ...value, environment: value.environment.map((x, j) => (j === i ? v : x)) })}
                placeholder="KEY=value"
                isDisabled={isDisabled}
                aria-label={`Environment entry ${i + 1}`}
                aria-invalid={error ? true : undefined}
                aria-describedby={error ? `${idp}-env-${i}-error` : undefined}
                validated={error ? "error" : "default"}
                style={{ fontFamily: "monospace" }}
              />
              <LiveAlert id={`${idp}-env-${i}-error`} message={error} variant="danger" isInline isPlain />
            </div>
          );
        }}
        onRemove={(i) => onChange({ ...value, environment: value.environment.filter((_, j) => j !== i) })}
        onAdd={() => onChange({ ...value, environment: [...value.environment, ""] })}
        addLabel="Add variable"
        removeAriaLabel={(i) => `Remove environment entry ${i + 1}`}
        addVariant="link"
        isDisabled={isDisabled}
      />
      <FormHelperText>
        <HelperText>
          <HelperTextItem>
            Added to the vendor environment. Values are stored in the policy and visible to
            everyone who can read it; keep secrets in a file referenced by <code>EnvironmentFile=</code> instead.
          </HelperTextItem>
        </HelperText>
      </FormHelperText>
    </FormGroup>
    <div style={row}>
      <ValidatedTextInput id={`${idp}-mem`} label="MemoryMax" value={value.memoryMax} onChange={(v) => onChange({ ...value, memoryMax: v })} placeholder="512M or 20%" problem={(v) => (/^(\d+(\.\d+)?[KMGTPE]?|\d+(\.\d+)?%|infinity)$/.test(v) ? null : "Use a size such as 512M, a percentage such as 20%, or infinity")} isDisabled={isDisabled} style={narrow} />
      <ValidatedTextInput id={`${idp}-cpu`} label="CPUQuota" value={value.cpuQuota} onChange={(v) => onChange({ ...value, cpuQuota: v })} placeholder="50%" problem={(v) => (/^\d+(\.\d+)?%$/.test(v) ? null : "Use a percentage such as 50%")} isDisabled={isDisabled} style={narrow} />
      <ValidatedTextInput id={`${idp}-tasks`} label="TasksMax" value={value.tasksMax} onChange={(v) => onChange({ ...value, tasksMax: v })} placeholder="200" problem={(v) => (/^(\d+(\.\d+)?[KMGTPE]?|\d+(\.\d+)?%|infinity)$/.test(v) ? null : "Use a number, a percentage or infinity")} isDisabled={isDisabled} style={narrow} />
      <ValidatedTextInput id={`${idp}-nice`} label="Nice" value={value.nice} onChange={(v) => onChange({ ...value, nice: v })} placeholder="-20 to 19" problem={(v) => (/^-?\d+$/.test(v) && Number(v) >= -20 && Number(v) <= 19 ? null : "Use an integer between -20 and 19")} isDisabled={isDisabled} style={narrow} />
    </div>
    <div style={row}>
      <TriStateSelect id={`${idp}-nnp`} label="NoNewPrivileges" value={value.noNewPrivileges} onChange={(v) => onChange({ ...value, noNewPrivileges: v })} isDisabled={isDisabled} style={narrow} />
      <FormGroup label="ProtectSystem" fieldId={`${idp}-psys`} style={narrow}>
        <FormSelect id={`${idp}-psys`} value={value.protectSystem} onChange={(_ev, v) => onChange({ ...value, protectSystem: v })} isDisabled={isDisabled} aria-label="ProtectSystem">
          {PROTECT_SYSTEM_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
        </FormSelect>
      </FormGroup>
      <FormGroup label="ProtectHome" fieldId={`${idp}-phome`} style={narrow}>
        <FormSelect id={`${idp}-phome`} value={value.protectHome} onChange={(_ev, v) => onChange({ ...value, protectHome: v })} isDisabled={isDisabled} aria-label="ProtectHome">
          {PROTECT_HOME_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
        </FormSelect>
      </FormGroup>
      <TriStateSelect id={`${idp}-ptmp`} label="PrivateTmp" value={value.privateTmp} onChange={(v) => onChange({ ...value, privateTmp: v })} isDisabled={isDisabled} style={narrow} />
    </div>
  </>
);

/* ── one unit row ── */

interface UnitRowProps {
  idp: string;
  index: number;
  unit: SystemdUnitEntry;
  onChange: (u: SystemdUnitEntry) => void;
  onRemove: () => void;
  onQueryChange: (q: string) => void;
  suggestions: SystemdCatalogueUnit[];
  describeSuggestion: (u: SystemdCatalogueUnit) => string;
  nodeName: string | null;
  nodeInfo: SystemdCatalogueUnit | null | undefined;
  riskyConfirmed: boolean;
  onConfirmRisky: (ok: boolean) => void;
  isDisabled?: boolean;
}

const UnitRow: React.FC<UnitRowProps> = ({
  idp, index, unit, onChange, onRemove, onQueryChange, suggestions, describeSuggestion,
  nodeName, nodeInfo, riskyConfirmed, onConfirmRisky, isDisabled,
}) => {
  const type = unitType(unit.name);
  const nameError = unit.name ? unitNameProblem(unit.name) : null;
  const rowError = !nameError ? unitEntryProblem(unit) : null;
  const risky = !nameError && isRiskyUnit(unit.name);
  const hasOverrides = (type === "timer" && !timerIsEmpty(unit.timer)) || (type === "service" && !serviceIsEmpty(unit.service));
  const [overridesOpen, setOverridesOpen] = useState(hasOverrides);
  const n = index + 1;
  const nameId = `${idp}-u${index}-name`;

  const staticHint =
    nodeInfo && nodeInfo.unitFileState === "static" &&
    (unit.enablement === "SYSTEMD_ENABLEMENT_ENABLED" || unit.enablement === "SYSTEMD_ENABLEMENT_DISABLED")
      ? `${unit.name} is static on ${nodeName}: it has no [Install] section and cannot be enabled or disabled. Set an activity instead.`
      : null;

  return (
    <Card isCompact style={{ marginBottom: "1rem" }}>
      <CardHeader
        actions={{
          actions: (
            <Button
              variant="plain"
              onClick={onRemove}
              isDisabled={isDisabled}
              aria-label={`Remove unit ${n}${unit.name ? ` (${unit.name})` : ""}`}
              style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
            >
              <TrashIcon />
            </Button>
          ),
          hasNoOffset: true,
        }}
      >
        <Title headingLevel="h5" size="md">Unit {n}{unit.name ? `: ${unit.name}` : ""}</Title>
      </CardHeader>
      <CardBody>
        <div style={row}>
          <FormGroup label="Unit" fieldId={nameId} isRequired style={{ flex: "2 1 20rem" }}>
            <UnitNameTypeahead
              id={nameId}
              value={unit.name}
              onChange={(name) => onChange({ ...unit, name })}
              onQueryChange={onQueryChange}
              suggestions={suggestions}
              describe={describeSuggestion}
              isDisabled={isDisabled}
              ariaLabel={`Unit ${n} name`}
              isInvalid={!!nameError}
              describedBy={nameError ? `${nameId}-error` : undefined}
            />
            <LiveAlert id={`${nameId}-error`} message={nameError} variant="danger" isInline isPlain />
            {!nameError && nodeName && (
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    {nodeInfo === undefined
                      ? `Loading the state on ${nodeName}...`
                      : nodeInfo
                        ? `Now on ${nodeName}: ${describeState(nodeInfo)}${nodeInfo.onCalendar.length ? `; schedule ${nodeInfo.onCalendar.join(" | ")}` : ""}${nodeInfo.description ? ` - ${nodeInfo.description}` : ""}`
                        : `Not reported by ${nodeName}: the unit is not installed there, or the node has not reported yet.`}
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            )}
          </FormGroup>
          <FormGroup label="Enablement" fieldId={`${idp}-u${index}-en`} style={narrow}>
            <FormSelect
              id={`${idp}-u${index}-en`}
              value={unit.enablement}
              onChange={(_ev, v) => onChange({ ...unit, enablement: v as SystemdEnablement })}
              isDisabled={isDisabled}
              aria-label={`Unit ${n} enablement`}
            >
              {ENABLEMENT_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
            </FormSelect>
          </FormGroup>
          {type !== "target" && (
            <FormGroup label="Activity" fieldId={`${idp}-u${index}-act`} style={narrow}>
              <FormSelect
                id={`${idp}-u${index}-act`}
                value={unit.activity}
                onChange={(_ev, v) => onChange({ ...unit, activity: v as SystemdActivity })}
                isDisabled={isDisabled}
                aria-label={`Unit ${n} activity`}
              >
                {ACTIVITY_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
              </FormSelect>
            </FormGroup>
          )}
        </div>
        {staticHint && (
          <LiveAlert message={staticHint} variant="warning" isInline isPlain />
        )}
        <div style={{ ...row, marginTop: "0.75rem" }}>
          {type === "service" && (
            <Switch
              id={`${idp}-u${index}-roc`}
              label="Restart after override changes"
              isChecked={unit.restartOnChange}
              onChange={(_ev, checked) => onChange({ ...unit, restartOnChange: checked })}
              isDisabled={isDisabled}
            />
          )}
          {(type === "timer" || type === "service") && (
            <Switch
              id={`${idp}-u${index}-lrs`}
              label="Last run must have succeeded"
              isChecked={unit.requireLastRunSuccess}
              onChange={(_ev, checked) => onChange({ ...unit, requireLastRunSuccess: checked })}
              isDisabled={isDisabled}
            />
          )}
        </div>
        <FormGroup label="Description" fieldId={`${idp}-u${index}-desc`} style={{ marginTop: "0.75rem" }}>
          <TextInput
            id={`${idp}-u${index}-desc`}
            value={unit.description}
            onChange={(_ev, v) => onChange({ ...unit, description: v })}
            placeholder="Why this unit is managed (optional)"
            isDisabled={isDisabled}
            aria-label={`Unit ${n} description`}
          />
        </FormGroup>
        {risky && (
          <LiveAlert
            variant="warning"
            isInline
            title={`${unit.name} can cut users or administrators off from the node`}
            style={{ marginTop: "0.75rem" }}
          >
            <p>Stopping or masking it on a whole group is one release away. Bind the policy to a pilot group first.</p>
            <Checkbox
              id={`${idp}-u${index}-risky`}
              label={`I understand; manage ${unit.name} anyway`}
              isChecked={riskyConfirmed}
              onChange={(_ev, checked) => onConfirmRisky(checked)}
              isDisabled={isDisabled}
            />
          </LiveAlert>
        )}
        <LiveAlert id={`${idp}-u${index}-error`} message={rowError} variant="danger" isInline isPlain style={{ marginTop: "0.5rem" }} />
        <ExpandableSection
          toggleText={overridesOpen ? "Hide overrides" : `Overrides${hasOverrides ? " (set)" : ""}`}
          isExpanded={overridesOpen}
          onToggle={(_ev, open) => setOverridesOpen(open)}
          isIndented
          style={{ marginTop: "0.75rem" }}
        >
          {type === "timer" ? (
            <TimerOverrides idp={`${idp}-u${index}-t`} value={unit.timer} onChange={(timer) => onChange({ ...unit, timer })} isDisabled={isDisabled} />
          ) : type === "service" ? (
            <ServiceOverrides idp={`${idp}-u${index}-s`} value={unit.service} onChange={(service) => onChange({ ...unit, service })} isDisabled={isDisabled} />
          ) : (
            <p style={{ ...subtle, fontSize: "0.85rem" }}>
              Overrides are available for .timer units (schedule) and .service units (restart, timeouts, environment, resources, sandboxing).
              {type ? "" : " Name the unit first."}
            </p>
          )}
          {(type === "timer" || type === "service") && (
            <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.5rem" }}>
              Written to <code>/etc/systemd/system/{unit.name}.d/90-bor.conf</code>; the vendor unit file stays untouched.
            </p>
          )}
        </ExpandableSection>
      </CardBody>
    </Card>
  );
};

/* ── main component ── */

export const SystemdPolicyEditor: React.FC<SystemdPolicyEditorProps> = ({ contentRaw, onChange, isDisabled, onValidityChange }) => {
  const idp = useId();
  const content = useMemo(() => parseSystemdContent(contentRaw), [contentRaw]);
  const push = useCallback((next: SystemdContent) => onChange(serializeSystemdContent(next)), [onChange]);

  /* nodes and per-node state */
  const [nodes, setNodes] = useState<Node[]>([]);
  const [nodeId, setNodeId] = useState("");
  const [nodeUnits, setNodeUnits] = useState<Map<string, SystemdCatalogueUnit> | null>(null);
  const [catalogueError, setCatalogueError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchNodesPaged({ per_page: 200, sort_field: "name", sort_order: "asc" })
      .then((res) => { if (!cancelled) setNodes(res.items); })
      .catch(() => { /* the picker stays hidden */ });
    return () => { cancelled = true; };
  }, []);
  useEffect(() => {
    if (!nodeId) { setNodeUnits(null); return; }
    let cancelled = false;
    setNodeUnits(null);
    fetchSystemdUnits({ nodeId })
      .then((units) => {
        if (cancelled) return;
        setNodeUnits(new Map(units.map((u) => [u.name, u])));
        setCatalogueError(null);
      })
      .catch((e: unknown) => {
        if (!cancelled) setCatalogueError(e instanceof Error ? `Unit catalogue unavailable: ${e.message}` : "Unit catalogue unavailable");
      });
    return () => { cancelled = true; };
  }, [nodeId]);
  const nodeName = nodes.find((n) => n.id === nodeId)?.name ?? null;

  /* suggestions for the name typeahead: one shared query, debounced */
  const [query, setQuery] = useState<string | null>(null);
  const [unionUnits, setUnionUnits] = useState<SystemdCatalogueUnit[]>([]);
  useEffect(() => {
    if (query === null || nodeId) return;
    let cancelled = false;
    const handle = window.setTimeout(() => {
      fetchSystemdUnits({ q: query })
        .then((units) => { if (!cancelled) { setUnionUnits(units); setCatalogueError(null); } })
        .catch((e: unknown) => {
          if (!cancelled) setCatalogueError(e instanceof Error ? `Unit catalogue unavailable: ${e.message}` : "Unit catalogue unavailable");
        });
    }, 250);
    return () => { cancelled = true; window.clearTimeout(handle); };
  }, [query, nodeId]);
  const suggestions = useMemo(() => {
    const needle = (query ?? "").trim().toLowerCase();
    const matches = (u: SystemdCatalogueUnit) =>
      !needle || u.name.toLowerCase().includes(needle) || u.description.toLowerCase().includes(needle);
    if (nodeId) {
      return nodeUnits ? Array.from(nodeUnits.values()).filter(matches) : [];
    }
    return unionUnits.filter(matches);
  }, [query, nodeId, nodeUnits, unionUnits]);
  const describeSuggestion = useCallback(
    (u: SystemdCatalogueUnit) =>
      nodeId
        ? `${u.type}; now ${describeState(u)}${u.description ? ` - ${u.description}` : ""}`
        : `${u.type}; reported by ${u.nodeCount} ${u.nodeCount === 1 ? "node" : "nodes"}${u.description ? ` - ${u.description}` : ""}`,
    [nodeId],
  );

  /* risky-unit confirmations: units already in a saved draft count as confirmed */
  const [confirmed, setConfirmed] = useState<Set<string>>(
    () => new Set(parseSystemdContent(contentRaw).units.map((u) => u.name).filter(isRiskyUnit)),
  );
  const unconfirmed = useMemo(
    () => content.units.filter((u) => u.name && !unitNameProblem(u.name) && isRiskyUnit(u.name) && !confirmed.has(u.name)),
    [content.units, confirmed],
  );

  /* validity */
  const problem = useMemo(() => validateSystemdContent(content), [content]);
  const valid = problem === null && unconfirmed.length === 0;
  useEffect(() => {
    onValidityChange?.(valid);
  }, [valid, onValidityChange]);
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);

  const updateUnit = (index: number, u: SystemdUnitEntry) =>
    push({ ...content, units: content.units.map((x, i) => (i === index ? u : x)) });
  const removeUnit = (index: number) => push({ ...content, units: content.units.filter((_, i) => i !== index) });
  const addUnit = () => push({ ...content, units: [...content.units, newUnitEntry()] });

  return (
    <Form onSubmit={(e) => e.preventDefault()}>
      <div style={{ ...row, alignItems: "flex-end" }}>
        <div style={{ flex: "1 1 20rem" }}>
          <Title headingLevel="h4" size="md">Units</Title>
          <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.25rem" }}>
            Existing services, timers, sockets, targets and paths and the state they must be in.
            Masked units cannot start at all, not even as a dependency.
          </p>
        </div>
        {nodes.length > 0 && (
          <FormGroup label="Show the state on node" fieldId={`${idp}-node`} style={{ flex: "0 1 20rem" }}>
            <FormSelect id={`${idp}-node`} value={nodeId} onChange={(_ev, v) => setNodeId(v)} aria-label="Node whose unit states are shown">
              <FormSelectOption value="" label="Units reported by any node" />
              {nodes.map((n) => <FormSelectOption key={n.id} value={n.id} label={n.name} />)}
            </FormSelect>
          </FormGroup>
        )}
      </div>
      <LiveAlert message={catalogueError} variant="warning" isInline isPlain />

      {content.units.length === 0 && (
        <p style={{ ...subtle, fontSize: "0.85rem" }}>No units yet. Add one to start.</p>
      )}
      {content.units.map((u, i) => (
        <UnitRow
          key={i}
          idp={idp}
          index={i}
          unit={u}
          onChange={(nu) => updateUnit(i, nu)}
          onRemove={() => removeUnit(i)}
          onQueryChange={setQuery}
          suggestions={suggestions}
          describeSuggestion={describeSuggestion}
          nodeName={nodeName}
          nodeInfo={nodeId ? (nodeUnits ? nodeUnits.get(u.name) ?? null : undefined) : null}
          riskyConfirmed={confirmed.has(u.name)}
          onConfirmRisky={(ok) =>
            setConfirmed((prev) => {
              const next = new Set(prev);
              if (ok) next.add(u.name); else next.delete(u.name);
              return next;
            })
          }
          isDisabled={isDisabled}
        />
      ))}
      <div>
        <Button variant="secondary" icon={<PlusCircleIcon />} onClick={addUnit} isDisabled={isDisabled} size="sm">
          Add unit
        </Button>
      </div>

      {unconfirmed.length > 0 && (
        <LiveAlert
          variant="warning"
          isInline
          message={`Confirm the risky ${unconfirmed.length === 1 ? "unit" : "units"} ${unconfirmed.map((u) => u.name).join(", ")} before saving`}
        />
      )}
      {problem && content.units.length > 0 && (
        <LiveAlert message={problem} variant="danger" isInline />
      )}
    </Form>
  );
};
