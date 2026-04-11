// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * PolkitPolicyEditor — structured editor for a Polkit JavaScript rules policy.
 *
 * Renders a list of PolkitRule cards, each containing:
 *  - Description (rule label / JS comment)
 *  - Action IDs (exact matches — typeahead from catalogue)
 *  - Action Prefixes (prefix matches — typeahead from derived prefixes)
 *  - Subject filter (group, user, local/active session, system unit)
 *  - Result (polkit.Result.* value)
 *  - Remove button
 *
 * Also shows a file_prefix field and a Preview button (modal with generated JS).
 *
 * The parent passes contentRaw (JSON string) and an onChange callback.
 * On every change the new JSON is pushed up via onChange.
 */

import React, { useState, useCallback, useEffect, useId, useMemo, useRef } from "react";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  CodeBlock,
  CodeBlockCode,
  Form,
  FormGroup,
  MenuToggle,
  MenuToggleElement,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Select,
  SelectList,
  SelectOption,
  TextInput,
  TextInputGroup,
  TextInputGroupMain,
  Title,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import {
  PolkitAction,
  PolkitPolicyContent,
  PolkitRule,
  PolkitResultValue,
  fetchPolkitActions,
  parsePolkitContent,
  serializePolkitContent,
  polkitContentToJS,
} from "../../apiClient/polkitApi";

/* ── constants ── */

const RESULT_LABELS: { value: PolkitResultValue; label: string }[] = [
  { value: "POLKIT_RESULT_YES",             label: "Allow (always grant)" },
  { value: "POLKIT_RESULT_NO",              label: "Deny (always block)" },
  { value: "POLKIT_RESULT_AUTH_SELF",       label: "Ask user to authenticate" },
  { value: "POLKIT_RESULT_AUTH_SELF_KEEP",  label: "Ask user to authenticate (cached)" },
  { value: "POLKIT_RESULT_AUTH_ADMIN",      label: "Ask administrator to authenticate" },
  { value: "POLKIT_RESULT_AUTH_ADMIN_KEEP", label: "Ask administrator to authenticate (cached)" },
];

const DEFAULT_RULE: PolkitRule = {
  description: "",
  action_ids: [],
  action_prefixes: [],
  subject: {},
  result: "POLKIT_RESULT_NO",
};

/* ── helpers ── */

interface ActionOption {
  value: string;
  description?: string;
}

/** Derive all common dot-delimited prefixes from a list of action IDs. */
function deriveActionPrefixes(actions: PolkitAction[]): ActionOption[] {
  const set = new Set<string>();
  for (const a of actions) {
    const parts = a.action_id.split(".");
    // e.g. "org.freedesktop.NetworkManager.wifi.enable"
    // → "org.", "org.freedesktop.", "org.freedesktop.NetworkManager.", ...
    for (let i = 1; i < parts.length; i++) {
      set.add(parts.slice(0, i).join(".") + ".");
    }
  }
  return Array.from(set)
    .sort()
    .map(v => ({ value: v }));
}

/* ── sub-component: typeahead combobox for a single action ID / prefix ── */

interface ActionComboboxProps {
  id: string;
  value: string;
  options: ActionOption[];
  onChange: (v: string) => void;
  isDisabled?: boolean;
  placeholder?: string;
}

const VISIBLE_LIMIT = 60;

const ActionCombobox: React.FC<ActionComboboxProps> = ({
  id, value, options, onChange, isDisabled, placeholder,
}) => {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState(value);

  // Keep filter text in sync when the parent resets the value.
  useEffect(() => { setFilter(value); }, [value]);

  const filtered = useMemo(() => {
    const q = filter.toLowerCase();
    const matches = q
      ? options.filter(o => o.value.toLowerCase().includes(q))
      : options;
    return matches.slice(0, VISIBLE_LIMIT);
  }, [filter, options]);

  const handleFilterChange = (_ev: React.FormEvent<HTMLInputElement>, val: string) => {
    setFilter(val);
    onChange(val);
    if (!open) setOpen(true);
  };

  const handleSelect = (_ev: React.MouseEvent | undefined, selected: string | number | undefined) => {
    if (typeof selected === "string" && selected !== "_no-results") {
      onChange(selected);
      setFilter(selected);
    }
    setOpen(false);
  };

  return (
    <Select
      id={id}
      isOpen={open}
      onOpenChange={(isOpen) => setOpen(isOpen)}
      onSelect={handleSelect}
      toggle={(ref: React.Ref<MenuToggleElement>) => (
        <MenuToggle
          ref={ref}
          variant="typeahead"
          onClick={() => { if (!isDisabled) setOpen(v => !v); }}
          isExpanded={open}
          isDisabled={isDisabled}
          style={{ width: "100%" }}
        >
          <TextInputGroup isPlain>
            <TextInputGroupMain
              value={filter}
              onClick={() => { if (!isDisabled) setOpen(true); }}
              onChange={handleFilterChange}
              placeholder={placeholder}
              inputId={`${id}-input`}
              aria-label="Action value"
              inputProps={{ autoComplete: "off", disabled: isDisabled }}
            />
          </TextInputGroup>
        </MenuToggle>
      )}
    >
      <SelectList id={`${id}-listbox`} style={{ maxHeight: "18rem", overflowY: "auto" }}>
        {filtered.length === 0 ? (
          <SelectOption isDisabled key="_no-results" value="_no-results">
            {filter ? "No matching actions — typed value will be used" : "No actions available"}
          </SelectOption>
        ) : (
          filtered.map(opt => (
            <SelectOption
              key={opt.value}
              value={opt.value}
              description={opt.description}
            >
              {opt.value}
            </SelectOption>
          ))
        )}
      </SelectList>
    </Select>
  );
};

/* ── sub-component: list of action typeahead inputs ── */

interface ActionListEditorProps {
  label: string;
  items: string[];
  options: ActionOption[];
  onChange: (items: string[]) => void;
  isDisabled?: boolean;
  idPrefix: string;
  placeholder?: string;
}

const ActionListEditor: React.FC<ActionListEditorProps> = ({
  label, items, options, onChange, isDisabled, idPrefix, placeholder,
}) => {
  const addItem = () => onChange([...items, ""]);
  const removeItem = (idx: number) => onChange(items.filter((_, i) => i !== idx));
  const updateItem = (idx: number, val: string) =>
    onChange(items.map((item, i) => (i === idx ? val : item)));

  return (
    <div>
      {items.length === 0 && (
        <p style={{ fontSize: "0.85rem", color: "var(--pf-t--global--text--color--subtle)", marginBottom: "0.4rem" }}>
          None — add one below.
        </p>
      )}
      {items.map((item, idx) => (
        <div key={idx} style={{ display: "flex", gap: "0.4rem", marginBottom: "0.4rem", alignItems: "center" }}>
          <div style={{ flex: 1 }}>
            <ActionCombobox
              id={`${idPrefix}-item-${idx}`}
              value={item}
              options={options}
              onChange={val => updateItem(idx, val)}
              isDisabled={isDisabled}
              placeholder={placeholder}
            />
          </div>
          <Button
            variant="plain"
            onClick={() => removeItem(idx)}
            isDisabled={isDisabled}
            aria-label={`Remove ${label} item ${idx + 1}`}
            style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
          >
            <TrashIcon />
          </Button>
        </div>
      ))}
      <Button
        variant="link"
        icon={<PlusCircleIcon />}
        onClick={addItem}
        isDisabled={isDisabled}
        style={{ paddingLeft: 0 }}
      >
        Add
      </Button>
    </div>
  );
};

/* ── sub-component: group select ── */

type GroupMode = "everyone" | "in_group" | "not_in_group";

interface GroupSelectProps {
  inGroup?: string;
  negateGroup?: boolean;
  onChange: (inGroup: string | undefined, negate: boolean) => void;
  isDisabled?: boolean;
  idPrefix: string;
}

const GroupSelect: React.FC<GroupSelectProps> = ({
  inGroup,
  negateGroup,
  onChange,
  isDisabled,
  idPrefix,
}) => {
  const [open, setOpen] = useState(false);

  const mode: GroupMode = inGroup === undefined
    ? "everyone"
    : negateGroup ? "not_in_group" : "in_group";

  const modeOptions: { value: GroupMode; label: string }[] = [
    { value: "everyone",     label: "Everyone" },
    { value: "in_group",     label: "In group [...]" },
    { value: "not_in_group", label: "NOT in group [...]" },
  ];

  const handleModeSelect = (_ev: React.MouseEvent | undefined, selected: GroupMode) => {
    setOpen(false);
    if (selected === "everyone") {
      onChange(undefined, false);
    } else {
      onChange(inGroup ?? "", selected === "not_in_group");
    }
  };

  const currentLabel = modeOptions.find(o => o.value === mode)?.label ?? "Everyone";

  return (
    <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap" }}>
      <Select
        id={`${idPrefix}-group-mode`}
        isOpen={open}
        onOpenChange={setOpen}
        selected={mode}
        onSelect={(_ev, val) => handleModeSelect(_ev as React.MouseEvent | undefined, val as GroupMode)}
        toggle={(ref: React.Ref<MenuToggleElement>) => (
          <MenuToggle
            ref={ref}
            onClick={() => setOpen(v => !v)}
            isExpanded={open}
            isDisabled={isDisabled}
            aria-label="Select group filter mode"
          >
            {currentLabel}
          </MenuToggle>
        )}
      >
        <SelectList>
          {modeOptions.map(o => (
            <SelectOption key={o.value} value={o.value}>{o.label}</SelectOption>
          ))}
        </SelectList>
      </Select>

      {mode !== "everyone" && (
        <TextInput
          id={`${idPrefix}-group-name`}
          value={inGroup ?? ""}
          onChange={(_ev, v) => onChange(v, mode === "not_in_group")}
          placeholder="group name"
          isDisabled={isDisabled}
          aria-label="Group name"
          style={{ maxWidth: "16rem" }}
        />
      )}
    </div>
  );
};

/* ── sub-component: result select ── */

interface ResultSelectProps {
  value: PolkitResultValue;
  onChange: (v: PolkitResultValue) => void;
  isDisabled?: boolean;
  idPrefix: string;
}

const ResultSelect: React.FC<ResultSelectProps> = ({ value, onChange, isDisabled, idPrefix }) => {
  const [open, setOpen] = useState(false);
  const currentLabel = RESULT_LABELS.find(r => r.value === value)?.label ?? value;

  return (
    <Select
      id={`${idPrefix}-result`}
      isOpen={open}
      onOpenChange={setOpen}
      selected={value}
      onSelect={(_ev, val) => {
        onChange(val as PolkitResultValue);
        setOpen(false);
      }}
      toggle={(ref: React.Ref<MenuToggleElement>) => (
        <MenuToggle
          ref={ref}
          onClick={() => setOpen(v => !v)}
          isExpanded={open}
          isDisabled={isDisabled}
          aria-label="Select polkit result"
        >
          {currentLabel}
        </MenuToggle>
      )}
    >
      <SelectList>
        {RESULT_LABELS.map(r => (
          <SelectOption key={r.value} value={r.value}>{r.label}</SelectOption>
        ))}
      </SelectList>
    </Select>
  );
};

/* ── main component ── */

interface PolkitPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
}

export const PolkitPolicyEditor: React.FC<PolkitPolicyEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled,
}) => {
  const idPrefix = useId();
  const [previewOpen, setPreviewOpen] = useState(false);
  const [parseError, setParseError] = useState<string | null>(null);
  const previewButtonRef = useRef<HTMLButtonElement>(null);

  /* ── action catalogue ── */
  const [availableActions, setAvailableActions] = useState<PolkitAction[]>([]);

  useEffect(() => {
    fetchPolkitActions()
      .then(actions => setAvailableActions(actions))
      .catch(() => { /* catalogue unavailable — free-text still works */ });
  }, []);

  const actionIdOptions: ActionOption[] = useMemo(
    () => availableActions.map(a => ({ value: a.action_id, description: a.description })),
    [availableActions],
  );

  const actionPrefixOptions: ActionOption[] = useMemo(
    () => deriveActionPrefixes(availableActions),
    [availableActions],
  );

  /* ── policy content ── */

  const content: PolkitPolicyContent = (() => {
    try {
      const parsed = parsePolkitContent(contentRaw);
      if (parseError) setParseError(null);
      return parsed;
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to parse policy content";
      setParseError(msg);
      return { rules: [], file_prefix: "50" };
    }
  })();

  const pushChange = useCallback(
    (updated: PolkitPolicyContent) => {
      onChange(serializePolkitContent(updated));
    },
    [onChange],
  );

  /* ── rule handlers ── */

  const addRule = () => {
    pushChange({ ...content, rules: [...content.rules, { ...DEFAULT_RULE }] });
  };

  const removeRule = (idx: number) => {
    pushChange({ ...content, rules: content.rules.filter((_, i) => i !== idx) });
  };

  const updateRule = (idx: number, patch: Partial<PolkitRule>) => {
    const rules = content.rules.map((r, i) => (i === idx ? { ...r, ...patch } : r));
    pushChange({ ...content, rules });
  };

  const updateSubject = (idx: number, patch: Partial<NonNullable<PolkitRule["subject"]>>) => {
    const rule = content.rules[idx];
    if (!rule) return;
    updateRule(idx, { subject: { ...rule.subject, ...patch } });
  };

  /* ── preview ── */

  const openPreview = () => setPreviewOpen(true);
  const closePreview = () => {
    setPreviewOpen(false);
    previewButtonRef.current?.focus();
  };

  const previewJS = polkitContentToJS(content);

  /* ── render ── */

  return (
    <div>
      <LiveAlert message={parseError} variant="danger" style={{ marginBottom: "0.75rem" }} />

      {/* Rule cards */}
      {content.rules.length === 0 && (
        <p style={{ color: "var(--pf-t--global--text--color--subtle)", marginBottom: "1rem" }}>
          No rules yet. Click "Add Rule" to begin.
        </p>
      )}

      {content.rules.map((rule, idx) => {
        const ruleId = `${idPrefix}-rule-${idx}`;
        const subject = rule.subject ?? {};

        return (
          <Card key={idx} style={{ marginBottom: "1rem" }}>
            <CardTitle>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <Title headingLevel="h4" size="md">
                  {rule.description ? rule.description : `Rule ${idx + 1}`}
                </Title>
                <Button
                  variant="plain"
                  onClick={() => removeRule(idx)}
                  isDisabled={isDisabled}
                  aria-label={`Remove rule ${idx + 1}`}
                  style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
                >
                  <TrashIcon />
                </Button>
              </div>
            </CardTitle>
            <CardBody>
              <Form>
                {/* Description */}
                <FormGroup label="Description" fieldId={`${ruleId}-desc`}>
                  <TextInput
                    id={`${ruleId}-desc`}
                    value={rule.description}
                    onChange={(_ev, v) => updateRule(idx, { description: v })}
                    placeholder="Human-readable rule label"
                    isDisabled={isDisabled}
                    aria-label="Rule description"
                  />
                </FormGroup>

                {/* Action IDs */}
                <FormGroup label="Exact Action IDs" fieldId={`${ruleId}-action-ids`}>
                  <ActionListEditor
                    label="Exact Action IDs"
                    items={rule.action_ids}
                    options={actionIdOptions}
                    onChange={ids => updateRule(idx, { action_ids: ids })}
                    isDisabled={isDisabled}
                    idPrefix={`${ruleId}-action-ids`}
                    placeholder="Search or type an action ID…"
                  />
                </FormGroup>

                {/* Action Prefixes */}
                <FormGroup label="Action Prefixes" fieldId={`${ruleId}-action-prefixes`}>
                  <ActionListEditor
                    label="Action Prefixes"
                    items={rule.action_prefixes}
                    options={actionPrefixOptions}
                    onChange={prefixes => updateRule(idx, { action_prefixes: prefixes })}
                    isDisabled={isDisabled}
                    idPrefix={`${ruleId}-action-prefixes`}
                    placeholder="Search or type a prefix…"
                  />
                </FormGroup>

                {/* Subject */}
                <FormGroup label="Group" fieldId={`${ruleId}-subject-group`}>
                  <GroupSelect
                    inGroup={subject.in_group}
                    negateGroup={subject.negate_group}
                    onChange={(inGroup, negate) =>
                      updateSubject(idx, { in_group: inGroup, negate_group: negate })
                    }
                    isDisabled={isDisabled}
                    idPrefix={`${ruleId}-subject`}
                  />
                </FormGroup>

                <FormGroup label="Specific user" fieldId={`${ruleId}-subject-user`}>
                  <TextInput
                    id={`${ruleId}-subject-user`}
                    value={subject.is_user ?? ""}
                    onChange={(_ev, v) => updateSubject(idx, { is_user: v || undefined })}
                    placeholder="username (empty = any user)"
                    isDisabled={isDisabled}
                    aria-label="Specific user"
                    style={{ maxWidth: "20rem" }}
                  />
                </FormGroup>

                <FormGroup fieldId={`${ruleId}-subject-checks`} label="Session requirements">
                  <div style={{ display: "flex", gap: "1.5rem", flexWrap: "wrap" }}>
                    <Checkbox
                      id={`${ruleId}-subject-local`}
                      label="Require local console session"
                      isChecked={!!subject.require_local}
                      onChange={(_ev, checked) => updateSubject(idx, { require_local: checked || undefined })}
                      isDisabled={isDisabled}
                      aria-label="Require local console session"
                    />
                    <Checkbox
                      id={`${ruleId}-subject-active`}
                      label="Require active session"
                      isChecked={!!subject.require_active}
                      onChange={(_ev, checked) => updateSubject(idx, { require_active: checked || undefined })}
                      isDisabled={isDisabled}
                      aria-label="Require active session"
                    />
                  </div>
                </FormGroup>

                <FormGroup label="System unit" fieldId={`${ruleId}-subject-unit`}>
                  <TextInput
                    id={`${ruleId}-subject-unit`}
                    value={subject.system_unit ?? ""}
                    onChange={(_ev, v) => updateSubject(idx, { system_unit: v || undefined })}
                    placeholder="e.g. packagekit.service (empty = not set)"
                    isDisabled={isDisabled}
                    aria-label="System unit"
                    style={{ maxWidth: "24rem" }}
                  />
                </FormGroup>

                {/* Result */}
                <FormGroup label="Result" fieldId={`${ruleId}-result`} isRequired>
                  <ResultSelect
                    value={rule.result}
                    onChange={v => updateRule(idx, { result: v })}
                    isDisabled={isDisabled}
                    idPrefix={ruleId}
                  />
                </FormGroup>
              </Form>
            </CardBody>
          </Card>
        );
      })}

      {/* Add Rule */}
      <Button
        variant="secondary"
        icon={<PlusCircleIcon />}
        onClick={addRule}
        isDisabled={isDisabled}
        style={{ marginBottom: "1.25rem" }}
      >
        Add Rule
      </Button>

      {/* File prefix */}
      <FormGroup
        label="File prefix"
        fieldId={`${idPrefix}-file-prefix`}
        style={{ maxWidth: "12rem", marginBottom: "1rem" }}
      >
        <TextInput
          id={`${idPrefix}-file-prefix`}
          value={content.file_prefix}
          onChange={(_ev, v) => pushChange({ ...content, file_prefix: v })}
          placeholder="50"
          isDisabled={isDisabled}
          aria-label="File prefix (numeric, e.g. 50)"
        />
      </FormGroup>

      {/* Preview button */}
      <Button
        variant="tertiary"
        onClick={openPreview}
        ref={previewButtonRef}
        aria-haspopup="dialog"
      >
        Preview JavaScript
      </Button>

      {/* Preview modal */}
      <Modal
        isOpen={previewOpen}
        onClose={closePreview}
        aria-labelledby={`${idPrefix}-preview-title`}
        variant="large"
      >
        <ModalHeader
          title="Generated Polkit Rules (JavaScript)"
          labelId={`${idPrefix}-preview-title`}
        />
        <ModalBody>
          <CodeBlock>
            <CodeBlockCode>{previewJS}</CodeBlockCode>
          </CodeBlock>
        </ModalBody>
        <ModalFooter>
          <Button variant="primary" onClick={closePreview}>
            Close
          </Button>
        </ModalFooter>
      </Modal>
    </div>
  );
};
