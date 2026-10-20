// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * SystemdPolicyEditor - a `Systemd` ("Services & timers") policy: for each
 * existing unit its enablement, its activity and an optional drop-in with
 * timer or service overrides and raw directives; Bor-deployed scheduled
 * tasks (a script or program with a timer or boot trigger); and the preset
 * file switch. Content is protojson (camelCase, enum names); see
 * systemdModel.ts. Unit names come from the catalogue nodes report (free
 * text is allowed for units no node has reported yet) and a node picker
 * shows the chosen node's current states. Tasks and raw Exec* directives
 * make the policy command-running: saving or releasing it needs the
 * systemd:exec permission, which the editor points out.
 */

import React, { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import {
  Button,
  Card,
  CardBody,
  CardHeader,
  Checkbox,
  Dropdown,
  DropdownItem,
  DropdownList,
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
  Radio,
  Select,
  SelectList,
  SelectOption,
  Switch,
  TextArea,
  TextInput,
  TextInputGroup,
  TextInputGroupMain,
  Title,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ListEditor } from "./ListEditor";
import { hasPermission } from "../../apiClient/permissions";
import { fetchNodesPaged, type Node } from "../../apiClient/nodesApi";
import { fetchSystemdUnits, previewSystemdCalendar, type SystemdCatalogueUnit } from "../../apiClient/systemdApi";
import {
  ACTIVITY_OPTIONS,
  DIRECTIVE_SECTIONS,
  ENABLEMENT_OPTIONS,
  MAX_TASK_SCRIPT_BYTES,
  PROTECT_HOME_OPTIONS,
  PROTECT_SYSTEM_OPTIONS,
  RESTART_OPTIONS,
  SCHEDULE_PRESETS,
  SCOPE_OPTIONS,
  SystemdActivity,
  SystemdContent,
  SystemdDirectiveEntry,
  SystemdEnablement,
  SystemdScope,
  SystemdServiceOverrides,
  SystemdTaskEntry,
  SystemdTaskTrigger,
  SystemdTimerOverrides,
  SystemdUnitEntry,
  TASK_HARDENED_DIRECTIVES,
  TASK_TRIGGER_OPTIONS,
  calendarProblem,
  contentRunsCommands,
  directiveKeyProblem,
  directiveProblem,
  isExecDirective,
  isGlobalScope,
  isRiskyUnitEntry,
  newDirectiveEntry,
  newTaskEntry,
  newUnitEntry,
  parseSystemdContent,
  serializeSystemdContent,
  serviceIsEmpty,
  systemdTemplates,
  taskEntryProblem,
  taskNameProblem,
  timeSpanProblem,
  timerIsEmpty,
  unitEntryProblem,
  unitNameProblem,
  unitType,
  userNameProblem,
  validateSystemdContent,
  apparmorProfileProblem,
  selinuxContextProblem,
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
/** The three selects of a unit row: narrow enough to sit next to the unit name at 1280px. */
const unitSelect: React.CSSProperties = { flex: "1 1 9rem", maxWidth: "16rem" };
const wide: React.CSSProperties = { flex: "1 1 20rem" };

/* ── calendar preview ── */

/**
 * Shows the next elapse times of a calendar expression, computed by the
 * server's systemd-analyze, or the parser's message for a bad one.
 */
const CalendarPreview: React.FC<{ expression: string; id: string }> = ({ expression, id }) => {
  const [state, setState] = useState<{ expr: string; text: string; error: boolean } | null>(null);
  useEffect(() => {
    const expr = expression.trim();
    if (!expr || calendarProblem(expr)) { setState(null); return; }
    let cancelled = false;
    const handle = window.setTimeout(() => {
      previewSystemdCalendar(expr, 3)
        .then((res) => {
          if (cancelled) return;
          if (!res.available) { setState({ expr, text: "Preview unavailable on this server (no systemd-analyze); the node checks the expression.", error: false }); return; }
          const next = res.next.length ? `Next: ${res.next.join("; ")}` : "Never elapses";
          setState({ expr, text: `${res.normalized && res.normalized !== expr ? `${res.normalized}. ` : ""}${next}`, error: false });
        })
        .catch((e: unknown) => {
          if (!cancelled) setState({ expr, text: e instanceof Error ? e.message : "Invalid calendar expression", error: true });
        });
    }, 400);
    return () => { cancelled = true; window.clearTimeout(handle); };
  }, [expression]);
  if (!state || state.expr !== expression.trim()) return null;
  if (state.error) return <LiveAlert id={id} message={state.text} variant="danger" isInline isPlain />;
  return (
    <FormHelperText>
      <HelperText><HelperTextItem>{state.text}</HelperTextItem></HelperText>
    </FormHelperText>
  );
};

/* ── timer overrides ── */

const TimerOverrides: React.FC<{
  idp: string;
  value: SystemdTimerOverrides;
  onChange: (t: SystemdTimerOverrides) => void;
  isDisabled?: boolean;
  /** Task schedules do not override a vendor schedule. */
  forTask?: boolean;
}> = ({ idp, value, onChange, isDisabled, forTask }) => {
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
            <HelperText><HelperTextItem>{forTask ? "When the task runs." : "Replaces the vendor OnCalendar schedule."}</HelperTextItem></HelperText>
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
                  {!error && <CalendarPreview expression={expr} id={`${idp}-cal-${i}-preview`} />}
                </div>
              );
            }}
            onRemove={(i) => onChange({ ...value, onCalendar: value.onCalendar.filter((_, j) => j !== i) })}
            onAdd={() => onChange({ ...value, onCalendar: [...value.onCalendar, ""] })}
            addLabel="Add expression"
            removeAriaLabel={(i) => `Remove calendar expression ${i + 1}`}
            addVariant="link"
            emptyText={forTask ? "No calendar expression yet: pick a preset or add one, or use a monotonic trigger below." : "No calendar expression: the vendor schedule stays."}
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

/* ── raw directives ── */

const RawDirectives: React.FC<{
  idp: string;
  unitTypeName: string;
  value: SystemdDirectiveEntry[];
  onChange: (raw: SystemdDirectiveEntry[]) => void;
  isDisabled?: boolean;
}> = ({ idp, unitTypeName, value, onChange, isDisabled }) => {
  const sections = DIRECTIVE_SECTIONS.filter((sec) => !["Service", "Timer", "Socket", "Path"].includes(sec) || sec.toLowerCase() === unitTypeName);
  const update = (i: number, d: SystemdDirectiveEntry) => onChange(value.map((x, j) => (j === i ? d : x)));
  return (
    <FormGroup label="Raw directives" fieldId={`${idp}-raw`} style={{ marginTop: "0.75rem" }}>
      <FormHelperText>
        <HelperText>
          <HelperTextItem>
            Any directive the fields above do not cover, written verbatim into the drop-in. A directive is
            reset before it is set, so the values replace the vendor's; leave the values empty to only reset it.
            <strong> Exec* directives run commands on every node and need the systemd:exec permission.</strong>
          </HelperTextItem>
        </HelperText>
      </FormHelperText>
      <ListEditor
        items={value}
        renderItem={(d, i) => {
          const keyError = d.key ? directiveKeyProblem(d.key) : null;
          const rowError = !keyError && d.key ? directiveProblem(d, unitTypeName) : null;
          const error = keyError ?? rowError;
          return (
            <div style={{ flex: 1 }}>
              <div style={row}>
                <FormGroup label="Section" fieldId={`${idp}-raw-${i}-sec`} style={{ flex: "0 1 9rem" }}>
                  <FormSelect id={`${idp}-raw-${i}-sec`} value={d.section} onChange={(_ev, v) => update(i, { ...d, section: v })} isDisabled={isDisabled} aria-label={`Directive ${i + 1} section`}>
                    {sections.map((sec) => <FormSelectOption key={sec} value={sec} label={`[${sec}]`} />)}
                  </FormSelect>
                </FormGroup>
                <FormGroup label="Directive" fieldId={`${idp}-raw-${i}-key`} style={{ flex: "1 1 12rem" }}>
                  <TextInput
                    id={`${idp}-raw-${i}-key`}
                    value={d.key}
                    onChange={(_ev, v) => update(i, { ...d, key: v })}
                    placeholder="IPAddressDeny"
                    isDisabled={isDisabled}
                    aria-label={`Directive ${i + 1} name`}
                    aria-invalid={error ? true : undefined}
                    aria-describedby={error ? `${idp}-raw-${i}-error` : undefined}
                    validated={error ? "error" : "default"}
                    style={{ fontFamily: "monospace" }}
                  />
                </FormGroup>
                <FormGroup label="Values (one per line)" fieldId={`${idp}-raw-${i}-val`} style={{ flex: "2 1 16rem" }}>
                  <TextArea
                    id={`${idp}-raw-${i}-val`}
                    value={d.values.join("\n")}
                    onChange={(_ev, v) => update(i, { ...d, values: v === "" ? [] : v.split("\n") })}
                    rows={Math.min(4, Math.max(1, d.values.length))}
                    resizeOrientation="vertical"
                    isDisabled={isDisabled}
                    aria-label={`Directive ${i + 1} values`}
                    style={{ fontFamily: "monospace" }}
                  />
                </FormGroup>
              </div>
              {isExecDirective(d.key) && (
                <LiveAlert variant="warning" isInline isPlain message={`${d.key} runs a command as root on every node this policy applies to.`} />
              )}
              <LiveAlert id={`${idp}-raw-${i}-error`} message={error} variant="danger" isInline isPlain />
            </div>
          );
        }}
        onRemove={(i) => onChange(value.filter((_, j) => j !== i))}
        onAdd={() => onChange([...value, newDirectiveEntry()])}
        addLabel="Add directive"
        removeAriaLabel={(i) => `Remove directive ${i + 1}`}
        addVariant="link"
        emptyText="No raw directives."
        isDisabled={isDisabled}
      />
    </FormGroup>
  );
};

/* ── one task ── */

const TaskRow: React.FC<{
  idp: string;
  index: number;
  task: SystemdTaskEntry;
  onChange: (t: SystemdTaskEntry) => void;
  onRemove: () => void;
  /** The picked node and what it reports for the task's service, for the sandboxing score. */
  nodeName: string | null;
  nodeInfo: SystemdCatalogueUnit | null | undefined;
  isDisabled?: boolean;
}> = ({ idp, index, task, onChange, onRemove, nodeName, nodeInfo, isDisabled }) => {
  const n = index + 1;
  const global = isGlobalScope(task.scope);
  const nameError = task.name ? taskNameProblem(task.name) : null;
  const rowError = !nameError ? taskEntryProblem(task) : null;
  const scriptBytes = new TextEncoder().encode(task.script).length;
  const scriptError = task.mode === "script" && task.script && !task.script.startsWith("#!")
    ? "The script must start with an interpreter line such as #!/bin/sh"
    : task.mode === "script" && scriptBytes > MAX_TASK_SCRIPT_BYTES ? "The script is larger than 64 KiB" : null;
  const nameId = `${idp}-t${index}-name`;
  const unitName = task.name && !nameError ? `bor-${task.name}` : "bor-<name>";
  return (
    <Card isCompact style={{ marginBottom: "1rem" }}>
      <CardHeader
        actions={{
          actions: (
            <Button
              variant="plain"
              onClick={onRemove}
              isDisabled={isDisabled}
              aria-label={`Remove task ${n}${task.name ? ` (${task.name})` : ""}`}
              style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
            >
              <TrashIcon />
            </Button>
          ),
          hasNoOffset: true,
        }}
      >
        <Title headingLevel="h5" size="md">Task {n}{task.name ? `: ${task.name}` : ""}</Title>
      </CardHeader>
      <CardBody>
        <div style={row}>
          <FormGroup label="Name" fieldId={nameId} isRequired style={narrow}>
            <TextInput
              id={nameId}
              value={task.name}
              onChange={(_ev, v) => onChange({ ...task, name: v })}
              placeholder="nightly-cleanup"
              isDisabled={isDisabled}
              aria-label={`Task ${n} name`}
              aria-invalid={nameError ? true : undefined}
              aria-describedby={nameError ? `${nameId}-error` : undefined}
              validated={nameError ? "error" : "default"}
            />
            <LiveAlert id={`${nameId}-error`} message={nameError} variant="danger" isInline isPlain />
            {!nameError && (
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Units {unitName}.service{task.trigger === "SYSTEMD_TASK_TRIGGER_TIMER" ? ` and ${unitName}.timer` : ""}
                    {global ? " in every user's manager" : ""}.
                    {!global && nodeName && nodeInfo && nodeInfo.securityPredicate
                      ? ` On ${nodeName}: sandboxing exposure ${nodeInfo.securityExposure} ${nodeInfo.securityPredicate}.`
                      : ""}
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            )}
          </FormGroup>
          <FormGroup label="Description" fieldId={`${idp}-t${index}-desc`} style={wide}>
            <TextInput
              id={`${idp}-t${index}-desc`}
              value={task.description}
              onChange={(_ev, v) => onChange({ ...task, description: v })}
              placeholder="Becomes the unit's Description="
              isDisabled={isDisabled}
              aria-label={`Task ${n} description`}
            />
          </FormGroup>
          <FormGroup label="Scope" fieldId={`${idp}-t${index}-scope`} style={narrow}>
            <FormSelect
              id={`${idp}-t${index}-scope`}
              value={global ? "SYSTEMD_SCOPE_GLOBAL" : "SYSTEMD_SCOPE_SYSTEM"}
              onChange={(_ev, v) => onChange({ ...task, scope: v as SystemdScope })}
              isDisabled={isDisabled}
              aria-label={`Task ${n} scope`}
            >
              {SCOPE_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
            </FormSelect>
          </FormGroup>
          <FormGroup label="Trigger" fieldId={`${idp}-t${index}-trig`} style={narrow}>
            <FormSelect
              id={`${idp}-t${index}-trig`}
              value={task.trigger}
              onChange={(_ev, v) => onChange({ ...task, trigger: v as SystemdTaskTrigger })}
              isDisabled={isDisabled}
              aria-label={`Task ${n} trigger`}
            >
              {TASK_TRIGGER_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={global && o.value === "SYSTEMD_TASK_TRIGGER_BOOT" ? "At login" : o.label} />)}
            </FormSelect>
          </FormGroup>
        </div>
        {global && (
          <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.5rem" }}>
            All users: the units go to <code>/etc/systemd/user</code>, the timer is started in the manager of every logged-in user,
            and the task runs as that user with the user's own permissions.
          </p>
        )}
        {task.trigger === "SYSTEMD_TASK_TRIGGER_TIMER" && (
          <div style={{ marginTop: "0.75rem" }}>
            <TimerOverrides idp={`${idp}-t${index}-s`} value={task.schedule} onChange={(schedule) => onChange({ ...task, schedule })} isDisabled={isDisabled} forTask />
          </div>
        )}
        <FormGroup label="What runs" fieldId={`${idp}-t${index}-mode`} style={{ marginTop: "0.75rem" }} role="radiogroup">
          <div style={row}>
            <Radio id={`${idp}-t${index}-mode-script`} name={`${idp}-t${index}-mode`} label="A script (written to /etc/bor/systemd on the node)" isChecked={task.mode === "script"} onChange={() => onChange({ ...task, mode: "script" })} isDisabled={isDisabled} />
            <Radio id={`${idp}-t${index}-mode-program`} name={`${idp}-t${index}-mode`} label="A program with arguments" isChecked={task.mode === "program"} onChange={() => onChange({ ...task, mode: "program", execStart: task.execStart.length ? task.execStart : [""] })} isDisabled={isDisabled} />
          </div>
        </FormGroup>
        {task.mode === "script" ? (
          <FormGroup label="Script" fieldId={`${idp}-t${index}-script`} isRequired>
            <TextArea
              id={`${idp}-t${index}-script`}
              value={task.script}
              onChange={(_ev, v) => onChange({ ...task, script: v })}
              rows={8}
              resizeOrientation="vertical"
              isDisabled={isDisabled}
              aria-label={`Task ${n} script`}
              aria-invalid={scriptError ? true : undefined}
              aria-describedby={scriptError ? `${idp}-t${index}-script-error` : undefined}
              validated={scriptError ? "error" : "default"}
              style={{ fontFamily: "monospace" }}
              spellCheck={false}
            />
            <LiveAlert id={`${idp}-t${index}-script-error`} message={scriptError} variant="danger" isInline isPlain />
            <FormHelperText>
              <HelperText>
                <HelperTextItem>
                  {scriptBytes.toLocaleString()} of {MAX_TASK_SCRIPT_BYTES.toLocaleString()} bytes. The interpreter comes from the first line; {global ? "the script runs as each logged-in user" : "the script runs as root unless a user is set"}.
                </HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>
        ) : (
          <FormGroup label="Program and arguments" fieldId={`${idp}-t${index}-argv`} isRequired>
            <ListEditor
              items={task.execStart}
              renderItem={(a, i) => {
                const error = i === 0 && a && !a.startsWith("/") ? "The program must be an absolute path" : null;
                return (
                  <div style={{ flex: 1, maxWidth: "40rem" }}>
                    <TextInput
                      id={`${idp}-t${index}-argv-${i}`}
                      value={a}
                      onChange={(_ev, v) => onChange({ ...task, execStart: task.execStart.map((x, j) => (j === i ? v : x)) })}
                      placeholder={i === 0 ? "/usr/bin/program" : "argument"}
                      isDisabled={isDisabled}
                      aria-label={i === 0 ? `Task ${n} program` : `Task ${n} argument ${i}`}
                      aria-invalid={error ? true : undefined}
                      aria-describedby={error ? `${idp}-t${index}-argv-${i}-error` : undefined}
                      validated={error ? "error" : "default"}
                      style={{ fontFamily: "monospace" }}
                    />
                    <LiveAlert id={`${idp}-t${index}-argv-${i}-error`} message={error} variant="danger" isInline isPlain />
                  </div>
                );
              }}
              onRemove={(i) => onChange({ ...task, execStart: task.execStart.filter((_, j) => j !== i) })}
              onAdd={() => onChange({ ...task, execStart: [...task.execStart, ""] })}
              addLabel="Add argument"
              removeAriaLabel={(i) => (i === 0 ? "Remove the program" : `Remove argument ${i}`)}
              addVariant="link"
              isDisabled={isDisabled}
            />
            <FormHelperText>
              <HelperText><HelperTextItem>Arguments are passed literally: no shell, no variable or specifier expansion.</HelperTextItem></HelperText>
            </FormHelperText>
          </FormGroup>
        )}
        <div style={{ ...row, marginTop: "0.75rem" }}>
          <ValidatedTextInput id={`${idp}-t${index}-user`} label="User" value={task.user} onChange={(v) => onChange({ ...task, user: v })} placeholder={global ? "the logged-in user" : "root"} problem={(v) => userNameProblem(v)} isDisabled={isDisabled || global} style={narrow} />
          <ValidatedTextInput id={`${idp}-t${index}-group`} label="Group" value={task.group} onChange={(v) => onChange({ ...task, group: v })} placeholder={global ? "the user's group" : "(user's group)"} problem={(v) => userNameProblem(v)} isDisabled={isDisabled || global} style={narrow} />
          <ValidatedTextInput id={`${idp}-t${index}-wd`} label="Working directory" value={task.workingDirectory} onChange={(v) => onChange({ ...task, workingDirectory: v })} placeholder="/" problem={(v) => (v.startsWith("/") ? null : "Use an absolute path")} isDisabled={isDisabled} style={narrow} />
          <ValidatedTextInput id={`${idp}-t${index}-timeout`} label="TimeoutStartSec" value={task.timeoutStartSec} onChange={(v) => onChange({ ...task, timeoutStartSec: v })} placeholder="1h" problem={timeSpanProblem} helper="The run is killed after this long." isDisabled={isDisabled} style={narrow} />
        </div>
        <div style={{ ...row, marginTop: "0.75rem" }}>
          <ValidatedTextInput id={`${idp}-t${index}-aa`} label="AppArmorProfile" value={task.apparmorProfile} onChange={(v) => onChange({ ...task, apparmorProfile: v })} placeholder="bor-corpapp" problem={apparmorProfileProblem} helper="Profile the task runs under, by its loaded name; a custom profile of an AppArmor policy is bor-<name>. A leading - tolerates a missing profile." isDisabled={isDisabled} style={narrow} />
          <ValidatedTextInput id={`${idp}-t${index}-se`} label="SELinuxContext" value={task.selinuxContext} onChange={(v) => onChange({ ...task, selinuxContext: v })} placeholder="system_u:system_r:bor_task_t:s0" problem={selinuxContextProblem} helper="user:role:type the task runs as; the type can come from a custom module of an SELinux policy. A leading - tolerates a failed transition." isDisabled={isDisabled} style={narrow} />
        </div>
        <FormGroup label="Environment" fieldId={`${idp}-t${index}-env`} style={{ marginTop: "0.75rem" }}>
          <ListEditor
            items={task.environment}
            renderItem={(e, i) => {
              const error = e && !/^[A-Za-z_][A-Za-z0-9_]*=/.test(e) ? "Must be KEY=value" : null;
              return (
                <div style={{ flex: 1, maxWidth: "32rem" }}>
                  <TextInput
                    id={`${idp}-t${index}-env-${i}`}
                    value={e}
                    onChange={(_ev, v) => onChange({ ...task, environment: task.environment.map((x, j) => (j === i ? v : x)) })}
                    placeholder="KEY=value"
                    isDisabled={isDisabled}
                    aria-label={`Task ${n} environment entry ${i + 1}`}
                    aria-invalid={error ? true : undefined}
                    aria-describedby={error ? `${idp}-t${index}-env-${i}-error` : undefined}
                    validated={error ? "error" : "default"}
                    style={{ fontFamily: "monospace" }}
                  />
                  <LiveAlert id={`${idp}-t${index}-env-${i}-error`} message={error} variant="danger" isInline isPlain />
                </div>
              );
            }}
            onRemove={(i) => onChange({ ...task, environment: task.environment.filter((_, j) => j !== i) })}
            onAdd={() => onChange({ ...task, environment: [...task.environment, ""] })}
            addLabel="Add variable"
            removeAriaLabel={(i) => `Remove environment entry ${i + 1}`}
            addVariant="link"
            emptyText="No environment variables."
            isDisabled={isDisabled}
          />
        </FormGroup>
        <div style={{ ...row, marginTop: "0.75rem" }}>
          <Switch
            id={`${idp}-t${index}-hardened`}
            label="Sandboxed (hardened unit defaults)"
            isChecked={task.hardened}
            onChange={(_ev, checked) => onChange({ ...task, hardened: checked })}
            isDisabled={isDisabled}
          />
          <Switch
            id={`${idp}-t${index}-lrs`}
            label="Last run must have succeeded"
            isChecked={task.requireLastRunSuccess}
            onChange={(_ev, checked) => onChange({ ...task, requireLastRunSuccess: checked })}
            isDisabled={isDisabled}
          />
          <Switch
            id={`${idp}-t${index}-enabled`}
            label="Enabled"
            isChecked={task.enabled}
            onChange={(_ev, checked) => onChange({ ...task, enabled: checked })}
            isDisabled={isDisabled}
          />
        </div>
        <FormHelperText>
          <HelperText>
            <HelperTextItem>
              {task.hardened
                ? global
                  ? "Sandboxing in a user manager adds NoNewPrivileges=yes and RestrictSUIDSGID=yes; the namespace protections need privileges a user manager does not have."
                  : `Sandboxing adds ${TASK_HARDENED_DIRECTIVES.join(", ")}. ProtectSystem= and ProtectHome= are left out because maintenance scripts commonly write under /etc, /var and /home.`
                : "Without sandboxing the task runs with the plain service defaults."}
              {task.enabled ? "" : " Not enabled: the files are deployed but the timer or boot trigger stays off."}
            </HelperTextItem>
          </HelperText>
        </FormHelperText>
        <LiveAlert id={`${idp}-t${index}-error`} message={rowError} variant="danger" isInline isPlain style={{ marginTop: "0.5rem" }} />
      </CardBody>
    </Card>
  );
};

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
  const global = isGlobalScope(unit.scope);
  const nameError = unit.name ? unitNameProblem(unit.name) : null;
  const rowError = !nameError ? unitEntryProblem(unit) : null;
  const risky = !nameError && isRiskyUnitEntry(unit);
  const hasOverrides = (type === "timer" && !timerIsEmpty(unit.timer)) || (type === "service" && !serviceIsEmpty(unit.service)) || unit.raw.length > 0;
  const [overridesOpen, setOverridesOpen] = useState(hasOverrides);
  const n = index + 1;
  const nameId = `${idp}-u${index}-name`;

  const staticHint =
    !global && nodeInfo && nodeInfo.unitFileState === "static" &&
    (unit.enablement === "SYSTEMD_ENABLEMENT_ENABLED" || unit.enablement === "SYSTEMD_ENABLEMENT_DISABLED")
      ? `${unit.name} is static on ${nodeName}: it has no [Install] section and cannot be enabled or disabled. Set an activity instead.`
      : null;
  const scoreHint =
    nodeInfo && nodeInfo.type === "service" && nodeInfo.securityPredicate
      ? `; sandboxing exposure ${nodeInfo.securityExposure} ${nodeInfo.securityPredicate}`
      : "";

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
                    {global
                      ? "Nodes report their system units; a unit of all users is checked in each logged-in session when the policy applies."
                      : nodeInfo === undefined
                        ? `Loading the state on ${nodeName}...`
                        : nodeInfo
                          ? `Now on ${nodeName}: ${describeState(nodeInfo)}${nodeInfo.onCalendar.length ? `; schedule ${nodeInfo.onCalendar.join(" | ")}` : ""}${scoreHint}${nodeInfo.description ? ` - ${nodeInfo.description}` : ""}`
                          : `Not reported by ${nodeName}: the unit is not installed there, or the node has not reported yet.`}
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            )}
          </FormGroup>
          <FormGroup label="Scope" fieldId={`${idp}-u${index}-scope`} style={unitSelect}>
            <FormSelect
              id={`${idp}-u${index}-scope`}
              value={global ? "SYSTEMD_SCOPE_GLOBAL" : "SYSTEMD_SCOPE_SYSTEM"}
              onChange={(_ev, v) => onChange({ ...unit, scope: v as SystemdScope })}
              isDisabled={isDisabled}
              aria-label={`Unit ${n} scope`}
            >
              {SCOPE_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
            </FormSelect>
          </FormGroup>
          <FormGroup label="Enablement" fieldId={`${idp}-u${index}-en`} style={unitSelect}>
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
            <FormGroup label="Activity" fieldId={`${idp}-u${index}-act`} style={unitSelect}>
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
        {global && (
          <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.5rem" }}>
            All users: enablement is applied as <code>systemctl --global</code> does it (symlinks under <code>/etc/systemd/user</code>);
            activity is applied in the manager of every logged-in user on each sync and reconcile pass.
          </p>
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
            title={global ? `${unit.name} can break every user's desktop session` : `${unit.name} can cut users or administrators off from the node`}
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
          {type && (
            <RawDirectives idp={`${idp}-u${index}-r`} unitTypeName={type} value={unit.raw} onChange={(raw) => onChange({ ...unit, raw })} isDisabled={isDisabled} />
          )}
          {type && (
            <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.5rem" }}>
              Written to <code>/etc/systemd/{global ? "user" : "system"}/{unit.name}.d/90-bor.conf</code>; the vendor unit file stays untouched.
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
    () => new Set(parseSystemdContent(contentRaw).units.filter(isRiskyUnitEntry).map((u) => u.name)),
  );
  const unconfirmed = useMemo(
    () => content.units.filter((u) => u.name && !unitNameProblem(u.name) && isRiskyUnitEntry(u) && !confirmed.has(u.name)),
    [content.units, confirmed],
  );

  /* command-running content needs systemd:exec */
  const runsCommands = useMemo(() => contentRunsCommands(content), [content]);
  const canExec = hasPermission("systemd:exec");
  const execBlocked = runsCommands && !canExec;

  /* validity */
  const problem = useMemo(() => validateSystemdContent(content), [content]);
  const valid = problem === null && unconfirmed.length === 0 && !execBlocked;
  useEffect(() => {
    onValidityChange?.(valid);
  }, [valid, onValidityChange]);
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);

  const updateUnit = (index: number, u: SystemdUnitEntry) =>
    push({ ...content, units: content.units.map((x, i) => (i === index ? u : x)) });
  const removeUnit = (index: number) => push({ ...content, units: content.units.filter((_, i) => i !== index) });
  const addUnit = () => push({ ...content, units: [...content.units, newUnitEntry()] });
  const updateTask = (index: number, t: SystemdTaskEntry) =>
    push({ ...content, tasks: content.tasks.map((x, i) => (i === index ? t : x)) });
  const removeTask = (index: number) => push({ ...content, tasks: content.tasks.filter((_, i) => i !== index) });
  const addTask = () => push({ ...content, tasks: [...content.tasks, newTaskEntry()] });

  /* templates: add the units of a template that the policy does not list yet */
  const [templatesOpen, setTemplatesOpen] = useState(false);
  const templates = useMemo(() => systemdTemplates(), []);
  const applyTemplate = (id: string) => {
    setTemplatesOpen(false);
    const tpl = templates.find((t) => t.id === id);
    if (!tpl) return;
    const present = new Set(content.units.map((u) => u.name));
    const added = tpl.units.filter((u) => !present.has(u.name));
    push({ ...content, units: [...content.units, ...added] });
  };

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
        <p style={{ ...subtle, fontSize: "0.85rem" }}>No units yet. Add one, or start from a template.</p>
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
      <div style={{ ...row, alignItems: "center" }}>
        <Button variant="secondary" icon={<PlusCircleIcon />} onClick={addUnit} isDisabled={isDisabled} size="sm">
          Add unit
        </Button>
        <Dropdown
          isOpen={templatesOpen}
          onOpenChange={setTemplatesOpen}
          onSelect={(_ev, value) => applyTemplate(String(value))}
          toggle={(toggleRef: React.Ref<MenuToggleElement>) => (
            <MenuToggle ref={toggleRef} onClick={() => setTemplatesOpen((o) => !o)} isExpanded={templatesOpen} isDisabled={isDisabled} size="sm" variant="secondary">
              Add template
            </MenuToggle>
          )}
          popperProps={{ position: "left" }}
        >
          <DropdownList aria-label="Unit templates">
            {templates.map((t) => (
              <DropdownItem key={t.id} value={t.id} description={t.description}>
                {t.label}
              </DropdownItem>
            ))}
          </DropdownList>
        </Dropdown>
      </div>

      <Switch
        id={`${idp}-presets`}
        label="Also write a preset file (10-bor.preset) so the enablement decisions above apply to units installed later"
        isChecked={content.writePresets}
        onChange={(_ev, checked) => push({ ...content, writePresets: checked })}
        isDisabled={isDisabled}
      />

      <div style={{ marginTop: "1rem" }}>
        <Title headingLevel="h4" size="md">Scheduled tasks</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginTop: "0.25rem" }}>
          Scripts or programs Bor deploys as <code>bor-&lt;name&gt;.service</code> with a timer or a boot trigger,
          sandboxed by default, with the last result in the compliance report. The agent adds the <code>bor-</code> prefix;
          a task can never replace a vendor unit.
        </p>
      </div>
      <LiveAlert
        variant={execBlocked ? "danger" : "info"}
        isInline
        isPlain={!runsCommands}
        message={
          execBlocked
            ? "This policy runs commands as root on every node it applies to. Saving or releasing it needs the systemd:exec permission, which your account does not hold."
            : runsCommands
              ? "This policy runs commands as root on every node it applies to. Saving and releasing it needs the systemd:exec permission and is recorded in the audit log with a hash of every script."
              : "Tasks run commands as root on every node the policy applies to; saving or releasing a policy with tasks needs the systemd:exec permission."
        }
      />
      {content.tasks.length === 0 && (
        <p style={{ ...subtle, fontSize: "0.85rem" }}>No tasks.</p>
      )}
      {content.tasks.map((t, i) => (
        <TaskRow
          key={i}
          idp={idp}
          index={i}
          task={t}
          onChange={(nt) => updateTask(i, nt)}
          onRemove={() => removeTask(i)}
          nodeName={nodeName}
          nodeInfo={nodeId ? (nodeUnits ? nodeUnits.get(`bor-${t.name}.service`) ?? null : undefined) : null}
          isDisabled={isDisabled}
        />
      ))}
      <div>
        <Button variant="secondary" icon={<PlusCircleIcon />} onClick={addTask} isDisabled={isDisabled} size="sm">
          Add task
        </Button>
      </div>

      {unconfirmed.length > 0 && (
        <LiveAlert
          variant="warning"
          isInline
          message={`Confirm the risky ${unconfirmed.length === 1 ? "unit" : "units"} ${unconfirmed.map((u) => u.name).join(", ")} before saving`}
        />
      )}
      {problem && (content.units.length > 0 || content.tasks.length > 0) && (
        <LiveAlert message={problem} variant="danger" isInline />
      )}
    </Form>
  );
};
