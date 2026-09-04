// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useCallback, useId } from "react";
import {
  Alert,
  Button,
  Divider,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Radio,
  Switch,
  TextInput,
  Title,
  ToggleGroup,
  ToggleGroupItem,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { ListEditor } from "./ListEditor";
import { LiveAlert } from "../../components/LiveAlert";

/* ── types (camelCase, matching the protojson schema) ── */

const END_ACTION_LOCK = "SESSION_END_ACTION_LOCK";
const END_ACTION_LOGOUT = "SESSION_END_ACTION_LOGOUT";

interface SaWindow {
  days?: string[];
  start?: string;
  end?: string;
}

interface SaRule {
  description?: string;
  users?: string[];
  groups?: string[];
  windows?: SaWindow[];
  endAction?: string;
  warnMinutes?: number[];
}

interface SessionAccessContent {
  rules?: SaRule[];
  enforcePam?: boolean;
  includeSsh?: boolean;
}

const DAYS: { id: string; label: string }[] = [
  { id: "mon", label: "Mon" },
  { id: "tue", label: "Tue" },
  { id: "wed", label: "Wed" },
  { id: "thu", label: "Thu" },
  { id: "fri", label: "Fri" },
  { id: "sat", label: "Sat" },
  { id: "sun", label: "Sun" },
];

const DEFAULT_WARN = [30, 15, 5];

/* ── parse / serialize ── */

function parseContent(raw: string): SessionAccessContent {
  try {
    const p = JSON.parse(raw || "{}") as Partial<SessionAccessContent>;
    return {
      rules: Array.isArray(p.rules) ? p.rules : [],
      // enforcePam defaults ON when unset (Layer 2 is on by default).
      enforcePam: p.enforcePam === undefined ? true : !!p.enforcePam,
      includeSsh: !!p.includeSsh,
    };
  } catch {
    return { rules: [], enforcePam: true, includeSsh: false };
  }
}

// serializeContent emits camelCase keys the protojson decoder accepts and drops
// empty optional fields. enforcePam and includeSsh are always emitted so a
// UI-created policy is explicit about both toggles.
function serializeContent(c: SessionAccessContent): string {
  const out: SessionAccessContent = { rules: [] };
  out.rules = (c.rules ?? []).map((r) => {
    const rule: SaRule = {};
    if (r.description) rule.description = r.description;
    if (r.users && r.users.length) rule.users = r.users;
    if (r.groups && r.groups.length) rule.groups = r.groups;
    if (r.windows && r.windows.length) {
      rule.windows = r.windows.map((w) => {
        const win: SaWindow = {};
        if (w.days && w.days.length) win.days = w.days;
        if (w.start) win.start = w.start;
        if (w.end) win.end = w.end;
        return win;
      });
    }
    // Emit the action explicitly; LOCK is the default but being explicit keeps
    // exports readable.
    rule.endAction = r.endAction === END_ACTION_LOGOUT ? END_ACTION_LOGOUT : END_ACTION_LOCK;
    if (r.warnMinutes && r.warnMinutes.length) rule.warnMinutes = r.warnMinutes;
    return rule;
  });
  out.enforcePam = c.enforcePam !== false;
  out.includeSsh = !!c.includeSsh;
  return JSON.stringify(out, null, 2);
}

/* ── warn-minutes helpers ── */

function warnToText(warn?: number[]): string {
  return (warn && warn.length ? warn : DEFAULT_WARN).join(", ");
}

// textToWarn parses a comma/space separated list into the strictly descending,
// de-duplicated ladder the server requires — so "5, 15, 30" is accepted and
// normalised rather than rejected at save time.
function textToWarn(s: string): number[] {
  const nums = s
    .split(/[,\s]+/)
    .map((x) => parseInt(x, 10))
    .filter((n) => !Number.isNaN(n) && n > 0);
  return Array.from(new Set(nums)).sort((a, b) => b - a);
}

/* ── reusable string-list editor (users / groups) ── */

const StringListEditor: React.FC<{
  label: string;
  values: string[];
  placeholder: string;
  addLabel: string;
  onChange: (vals: string[]) => void;
  isDisabled?: boolean;
  idPrefix: string;
  helper?: React.ReactNode;
}> = ({ label, values, placeholder, addLabel, onChange, isDisabled, idPrefix, helper }) => (
  <FormGroup label={label} fieldId={idPrefix}>
    <ListEditor
      items={values}
      renderItem={(v, idx) => (
        <TextInput
          id={`${idPrefix}-${idx}`}
          value={v}
          onChange={(_ev, nv) => onChange(values.map((x, i) => (i === idx ? nv : x)))}
          placeholder={placeholder}
          isDisabled={isDisabled}
          aria-label={`${label} ${idx + 1}`}
        />
      )}
      onRemove={(idx) => onChange(values.filter((_, i) => i !== idx))}
      onAdd={() => onChange([...values, ""])}
      addLabel={addLabel}
      removeAriaLabel={(idx) => `Remove ${label} ${idx + 1}`}
      rowGap="0.5rem"
      isDisabled={isDisabled}
    />
    {helper && (
      <FormHelperText>
        <HelperText><HelperTextItem>{helper}</HelperTextItem></HelperText>
      </FormHelperText>
    )}
  </FormGroup>
);

/* ── windows editor for a single rule ── */

const WindowsEditor: React.FC<{
  windows: SaWindow[];
  onChange: (w: SaWindow[]) => void;
  isDisabled?: boolean;
  idPrefix: string;
}> = ({ windows, onChange, isDisabled, idPrefix }) => {
  const toggleDay = (widx: number, day: string) => {
    onChange(
      windows.map((w, i) => {
        if (i !== widx) return w;
        const days = w.days ?? [];
        return { ...w, days: days.includes(day) ? days.filter((d) => d !== day) : [...days, day] };
      }),
    );
  };
  return (
    <FormGroup label="Allowed periods" fieldId={idPrefix} isRequired>
      {windows.map((w, widx) => (
        <div
          key={widx}
          style={{
            border: "1px solid var(--pf-t--global--border--color--default)",
            borderRadius: "4px",
            padding: "0.75rem",
            marginBottom: "0.5rem",
          }}
        >
          <ToggleGroup aria-label={`Days for period ${widx + 1}`}>
            {DAYS.map((d) => (
              <ToggleGroupItem
                key={d.id}
                text={d.label}
                aria-label={d.label}
                isSelected={(w.days ?? []).includes(d.id)}
                isDisabled={isDisabled}
                onChange={() => toggleDay(widx, d.id)}
              />
            ))}
          </ToggleGroup>
          <div style={{ display: "flex", gap: "0.75rem", alignItems: "flex-end", marginTop: "0.5rem", flexWrap: "wrap" }}>
            <FormGroup label="From" fieldId={`${idPrefix}-${widx}-start`}>
              <TextInput
                type="time"
                id={`${idPrefix}-${widx}-start`}
                value={w.start ?? ""}
                onChange={(_ev, v) => onChange(windows.map((x, i) => (i === widx ? { ...x, start: v } : x)))}
                isDisabled={isDisabled}
                aria-label={`Period ${widx + 1} start time`}
                style={{ maxWidth: "9rem" }}
              />
            </FormGroup>
            <FormGroup label="Until" fieldId={`${idPrefix}-${widx}-end`}>
              <TextInput
                type="time"
                id={`${idPrefix}-${widx}-end`}
                value={w.end ?? ""}
                onChange={(_ev, v) => onChange(windows.map((x, i) => (i === widx ? { ...x, end: v } : x)))}
                isDisabled={isDisabled}
                aria-label={`Period ${widx + 1} end time`}
                style={{ maxWidth: "9rem" }}
              />
            </FormGroup>
            <Button
              variant="plain"
              onClick={() => onChange(windows.filter((_, i) => i !== widx))}
              isDisabled={isDisabled}
              aria-label={`Remove period ${widx + 1}`}
              style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
            >
              <TrashIcon />
            </Button>
          </div>
        </div>
      ))}
      <Button
        variant="secondary"
        icon={<PlusCircleIcon />}
        onClick={() => onChange([...windows, { days: [], start: "08:00", end: "17:00" }])}
        isDisabled={isDisabled}
        size="sm"
      >
        Add period
      </Button>
      <FormHelperText>
        <HelperText>
          <HelperTextItem>
            Times are the node&apos;s local clock. A period whose end is at or before
            its start crosses midnight (e.g. 22:00–02:00). Periods in one rule must
            not overlap, but may touch.
          </HelperTextItem>
        </HelperText>
      </FormHelperText>
    </FormGroup>
  );
};

/* ── main component ── */

export interface SessionAccessPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
}

export const SessionAccessPolicyEditor: React.FC<SessionAccessPolicyEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled,
}) => {
  const idPrefix = useId();
  const content = parseContent(contentRaw);
  const rules = content.rules ?? [];

  const push = useCallback(
    (updated: SessionAccessContent) => onChange(serializeContent(updated)),
    [onChange],
  );

  const updateRule = (idx: number, next: SaRule) =>
    push({ ...content, rules: rules.map((r, i) => (i === idx ? next : r)) });

  return (
    <Form>
      <Alert
        variant="info"
        isInline
        title="Node requirements for session access enforcement"
      >
        <p>
          Enforcing nodes need <strong>systemd-logind</strong> (<code>loginctl</code>)
          for locking and logging off sessions, and a screen locker that honours
          logind&apos;s lock signal (GNOME, KDE Plasma and sddm do). To also prevent
          new logins and unlocking outside the allowed hours, the{" "}
          <strong>pam_time</strong> PAM module must be present.
        </p>
        <p style={{ marginTop: "0.5rem" }}>
          A node missing <code>loginctl</code> — or missing <code>pam_time</code> while
          the PAM option below is on — reports a compliance error for this policy.
          Note that on GNOME and KDE the screen <em>unlock</em> is enforced by a
          re-lock watchdog rather than PAM; the log-off action plus PAM is the strict
          option, because re-entry then requires a fresh login.
        </p>
      </Alert>

      <Title headingLevel="h4" size="md">Enforcement options</Title>

      <FormGroup fieldId={`${idPrefix}-pam`}>
        <Switch
          id={`${idPrefix}-pam`}
          label="Prevent login and unlocking outside allowed hours (PAM)"
          isChecked={content.enforcePam !== false}
          onChange={(_ev, checked) => push({ ...content, enforcePam: checked })}
          isDisabled={isDisabled}
        />
        <FormHelperText>
          <HelperText>
            <HelperTextItem>
              Uses <code>pam_time</code> to deny logins during disallowed hours.
              Requires the module on the node — see requirements above.
            </HelperTextItem>
          </HelperText>
        </FormHelperText>
      </FormGroup>

      <FormGroup fieldId={`${idPrefix}-ssh`}>
        <Switch
          id={`${idPrefix}-ssh`}
          label="Also restrict SSH logins"
          isChecked={!!content.includeSsh}
          onChange={(_ev, checked) => push({ ...content, includeSsh: checked })}
          isDisabled={isDisabled || content.enforcePam === false}
        />
        <FormHelperText>
          <HelperText>
            <HelperTextItem>
              Off by default: this is a desktop-usage control, and an SSH lockout can
              surprise administrators. Requires the PAM option above.
            </HelperTextItem>
          </HelperText>
        </FormHelperText>
      </FormGroup>

      <Divider style={{ margin: "0.5rem 0" }} />

      <Title headingLevel="h4" size="md">Rules</Title>

      {rules.map((rule, idx) => (
        <div
          key={idx}
          style={{
            border: "1px solid var(--pf-t--global--border--color--default)",
            borderRadius: "6px",
            padding: "1rem",
            marginBottom: "1rem",
          }}
        >
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: "0.5rem" }}>
            <Title headingLevel="h5" size="md">Rule {idx + 1}</Title>
            <Button
              variant="plain"
              onClick={() => push({ ...content, rules: rules.filter((_, i) => i !== idx) })}
              isDisabled={isDisabled}
              aria-label={`Remove rule ${idx + 1}`}
              style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
            >
              <TrashIcon />
            </Button>
          </div>

          <FormGroup label="Description" fieldId={`${idPrefix}-${idx}-desc`}>
            <TextInput
              id={`${idPrefix}-${idx}-desc`}
              value={rule.description ?? ""}
              onChange={(_ev, v) => updateRule(idx, { ...rule, description: v })}
              placeholder="e.g. Computer lab weekday hours"
              isDisabled={isDisabled}
              aria-label={`Rule ${idx + 1} description`}
            />
          </FormGroup>

          <StringListEditor
            label="Users"
            values={rule.users ?? []}
            placeholder="username"
            addLabel="Add user"
            onChange={(vals) => updateRule(idx, { ...rule, users: vals })}
            isDisabled={isDisabled}
            idPrefix={`${idPrefix}-${idx}-user`}
          />

          <StringListEditor
            label="Groups"
            values={rule.groups ?? []}
            placeholder="group name"
            addLabel="Add group"
            onChange={(vals) => updateRule(idx, { ...rule, groups: vals })}
            isDisabled={isDisabled}
            idPrefix={`${idPrefix}-${idx}-group`}
            helper="Group members are resolved on each node. Provide at least one user or group."
          />

          <WindowsEditor
            windows={rule.windows ?? []}
            onChange={(w) => updateRule(idx, { ...rule, windows: w })}
            isDisabled={isDisabled}
            idPrefix={`${idPrefix}-${idx}-win`}
          />

          <FormGroup label="When an allowed period ends" role="radiogroup" fieldId={`${idPrefix}-${idx}-action`}>
            <Radio
              id={`${idPrefix}-${idx}-action-lock`}
              name={`${idPrefix}-${idx}-action`}
              label="Lock the screen"
              description="Least disruptive — apps keep running and nothing is lost. On GNOME and KDE the screen unlock cannot be blocked by PAM, so a re-lock watchdog locks the screen again within a fraction of a second if the user unlocks it outside allowed hours."
              isChecked={(rule.endAction ?? END_ACTION_LOCK) !== END_ACTION_LOGOUT}
              onChange={() => updateRule(idx, { ...rule, endAction: END_ACTION_LOCK })}
              isDisabled={isDisabled}
            />
            <Radio
              id={`${idPrefix}-${idx}-action-logout`}
              name={`${idPrefix}-${idx}-action`}
              label="Log off — more secure, more intrusive"
              description="Ends the session at the boundary: any unsaved work is lost. In return there is nothing to unlock, so no desktop is ever exposed outside allowed hours, and getting back in requires a fresh login — which PAM does block. Choose this for high-assurance rooms (exams, shared secure terminals); pair it with a longer warning ladder so users can save first."
              isChecked={rule.endAction === END_ACTION_LOGOUT}
              onChange={() => updateRule(idx, { ...rule, endAction: END_ACTION_LOGOUT })}
              isDisabled={isDisabled}
            />
          </FormGroup>

          <FormGroup label="Warn before the end (minutes)" fieldId={`${idPrefix}-${idx}-warn`}>
            <TextInput
              id={`${idPrefix}-${idx}-warn`}
              value={warnToText(rule.warnMinutes)}
              onChange={(_ev, v) => updateRule(idx, { ...rule, warnMinutes: textToWarn(v) })}
              placeholder="30, 15, 5"
              isDisabled={isDisabled}
              aria-label={`Rule ${idx + 1} warning minutes`}
            />
            <FormHelperText>
              <HelperText>
                <HelperTextItem>
                  Comma-separated minutes before the deadline to warn the user.
                  Defaults to 30, 15, 5. The last value is delivered even through
                  do-not-disturb.
                </HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>
        </div>
      ))}

      <Button
        variant="secondary"
        icon={<PlusCircleIcon />}
        onClick={() =>
          push({
            ...content,
            rules: [
              ...rules,
              { users: [], groups: [], windows: [{ days: [], start: "08:00", end: "17:00" }], endAction: END_ACTION_LOCK, warnMinutes: DEFAULT_WARN },
            ],
          })
        }
        isDisabled={isDisabled}
        size="sm"
      >
        Add rule
      </Button>

      {rules.length === 0 && (
        <LiveAlert
          message="This policy has no rules yet. Add a rule to restrict when its target users may use their desktop session."
          variant="info"
        />
      )}
    </Form>
  );
};
