// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * DConfPolicyEditor — structured editor for a DConf policy.
 *
 * Renders a list of dconf entry rows, each containing:
 *  - Schema (searchable select from /api/v1/dconf/schemas)
 *  - Key    (select filtered to chosen schema's keys)
 *  - Value  (input widget based on key type)
 *  - Lock   (checkbox — "Prevent user from overriding")
 *  - Remove button
 *
 * Also shows a db_name text field.
 *
 * The parent passes contentRaw (JSON string) and an onChange callback.
 * On every change the new JSON is pushed up via onChange.
 */

import React, { useState, useEffect, useCallback, useId } from "react";
import {
  Button,
  Checkbox,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  MenuToggle,
  MenuToggleElement,
  NumberInput,
  Select,
  SelectList,
  SelectOption,
  Spinner,
  Switch,
  TextInput,
  Title,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import {
  fetchDConfSchemas,
  DConfSchema,
  DConfKey,
  DConfEntry,
  DConfPolicyContent,
} from "../../apiClient/dconfApi";

/* ── helpers ── */

function parseDConfContent(raw: string): DConfPolicyContent {
  try {
    const parsed = JSON.parse(raw || "{}") as Partial<DConfPolicyContent>;
    return {
      entries: Array.isArray(parsed.entries) ? parsed.entries : [],
      db_name: typeof parsed.db_name === "string" ? parsed.db_name : "local",
    };
  } catch {
    return { entries: [], db_name: "local" };
  }
}

function serializeDConfContent(content: DConfPolicyContent): string {
  return JSON.stringify(content, null, 2);
}

/**
 * Determine the default GVariant string value for a key based on its type.
 */
function defaultValueForKey(key: DConfKey): string {
  if (key.default_value) return key.default_value;
  if (key.enum_values && key.enum_values.length > 0) return `'${key.enum_values[0].nick}'`;
  if (key.choices && key.choices.length > 0) return `'${key.choices[0]}'`;
  switch (key.type) {
    case "b":  return "true";
    case "u":
    case "i":  return "0";
    case "d":  return "0.0";
    case "as": return "[]";
    default:   return "";
  }
}

/* ── sub-component: value widget ── */

interface ValueWidgetProps {
  keyDef: DConfKey | undefined;
  value: string;
  onChange: (v: string) => void;
  rowId: string;
  disabled?: boolean;
}

const ValueWidget: React.FC<ValueWidgetProps> = ({ keyDef, value, onChange, rowId, disabled }) => {
  const [enumOpen, setEnumOpen] = useState(false);

  if (!keyDef) {
    return (
      <TextInput
        id={`${rowId}-value`}
        value={value}
        onChange={(_ev, v) => onChange(v)}
        placeholder="GVariant value"
        isDisabled={disabled}
        aria-label="Value"
      />
    );
  }

  const { type, enum_values, choices } = keyDef;

  // Boolean → Switch
  if (type === "b") {
    const checked = value === "true";
    return (
      <Switch
        id={`${rowId}-value-bool`}
        isChecked={checked}
        onChange={(_ev, v) => onChange(v ? "true" : "false")}
        label="true"
        labelOff="false"
        isDisabled={disabled}
      />
    );
  }

  // Enum or choices → Select dropdown
  if ((enum_values && enum_values.length > 0) || (choices && choices.length > 0)) {
    const options: string[] = enum_values?.map(ev => ev.nick) ?? choices ?? [];
    // GVariant string values are quoted, e.g. "'prefer-dark'"
    const currentNick = value.replace(/^'|'$/g, "");

    return (
      <Select
        id={`${rowId}-value-enum`}
        isOpen={enumOpen}
        onOpenChange={setEnumOpen}
        selected={currentNick}
        onSelect={(_ev, sel) => {
          onChange(`'${sel as string}'`);
          setEnumOpen(false);
        }}
        toggle={(ref: React.Ref<MenuToggleElement>) => (
          <MenuToggle
            ref={ref}
            onClick={() => setEnumOpen(v => !v)}
            isExpanded={enumOpen}
            isDisabled={disabled}
            aria-label="Select enum value"
            style={{ width: "100%" }}
          >
            {currentNick || "(select)"}
          </MenuToggle>
        )}
      >
        <SelectList>
          {options.map(opt => (
            <SelectOption key={opt} value={opt}>{opt}</SelectOption>
          ))}
        </SelectList>
      </Select>
    );
  }

  // Numeric types → NumberInput
  if (type === "u" || type === "i") {
    const num = parseInt(value, 10);
    return (
      <NumberInput
        id={`${rowId}-value-int`}
        value={isNaN(num) ? 0 : num}
        onMinus={() => onChange(String((isNaN(num) ? 0 : num) - 1))}
        onPlus={() => onChange(String((isNaN(num) ? 0 : num) + 1))}
        onChange={(ev) => onChange(ev.currentTarget.value)}
        isDisabled={disabled}
        aria-label="Numeric value"
      />
    );
  }
  if (type === "d") {
    const num = parseFloat(value);
    return (
      <NumberInput
        id={`${rowId}-value-double`}
        value={isNaN(num) ? 0 : num}
        onMinus={() => onChange(String(parseFloat(((isNaN(num) ? 0 : num) - 1).toFixed(4))))}
        onPlus={() => onChange(String(parseFloat(((isNaN(num) ? 0 : num) + 1).toFixed(4))))}
        onChange={(ev) => onChange(ev.currentTarget.value)}
        isDisabled={disabled}
        aria-label="Decimal value"
      />
    );
  }

  // Array of strings → TextInput with hint
  if (type === "as") {
    return (
      <TextInput
        id={`${rowId}-value-as`}
        value={value}
        onChange={(_ev, v) => onChange(v)}
        placeholder="GVariant array, e.g. ['a', 'b']"
        isDisabled={disabled}
        aria-label="Array value (GVariant format)"
      />
    );
  }

  // Fallback → TextInput with type hint
  return (
    <TextInput
      id={`${rowId}-value-text`}
      value={value}
      onChange={(_ev, v) => onChange(v)}
      placeholder={`GVariant ${type} value`}
      isDisabled={disabled}
      aria-label={`Value (${type})`}
    />
  );
};

/* ── main component ── */

interface DConfPolicyEditorProps {
  contentRaw: string;
  onChange: (newContentRaw: string) => void;
  isDisabled?: boolean;
}

export const DConfPolicyEditor: React.FC<DConfPolicyEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled,
}) => {
  const [schemas, setSchemas] = useState<DConfSchema[]>([]);
  const [loadingSchemas, setLoadingSchemas] = useState(true);
  const [schemaError, setSchemaError] = useState<string | null>(null);

  // Schema search/select open states per row (indexed by row index)
  const [schemaOpenIdx, setSchemaOpenIdx] = useState<number | null>(null);
  const [keyOpenIdx, setKeyOpenIdx] = useState<number | null>(null);
  const [schemaSearch, setSchemaSearch] = useState<Record<number, string>>({});

  const idPrefix = useId();

  // Load schemas on mount
  useEffect(() => {
    let cancelled = false;
    setLoadingSchemas(true);
    fetchDConfSchemas()
      .then(data => { if (!cancelled) { setSchemas(data); setLoadingSchemas(false); } })
      .catch(err => { if (!cancelled) { setSchemaError(err instanceof Error ? err.message : "Failed to load schemas"); setLoadingSchemas(false); } });
    return () => { cancelled = true; };
  }, []);

  const content = parseDConfContent(contentRaw);

  const pushChange = useCallback((updated: DConfPolicyContent) => {
    onChange(serializeDConfContent(updated));
  }, [onChange]);

  const schemaMap = new Map<string, DConfSchema>(schemas.map(s => [s.schema_id, s]));

  /* ── row handlers ── */

  const addEntry = () => {
    pushChange({
      ...content,
      entries: [...content.entries, { schema_id: "", path: "", key: "", value: "", lock: false }],
    });
  };

  const removeEntry = (idx: number) => {
    const entries = content.entries.filter((_, i) => i !== idx);
    pushChange({ ...content, entries });
  };

  const updateEntry = (idx: number, patch: Partial<DConfEntry>) => {
    const entries = content.entries.map((e, i) => i === idx ? { ...e, ...patch } : e);
    pushChange({ ...content, entries });
  };

  const setSchemaForRow = (idx: number, schemaId: string) => {
    const schema = schemaMap.get(schemaId);
    const path = schema?.path ?? "";
    // Reset key/value when schema changes
    updateEntry(idx, { schema_id: schemaId, path, key: "", value: "" });
    setSchemaOpenIdx(null);
    setSchemaSearch(prev => ({ ...prev, [idx]: "" }));
  };

  const setKeyForRow = (idx: number, keyName: string) => {
    const entry = content.entries[idx];
    const schema = entry ? schemaMap.get(entry.schema_id) : undefined;
    const keyDef = schema?.keys.find(k => k.name === keyName);
    const defVal = keyDef ? defaultValueForKey(keyDef) : "";
    updateEntry(idx, { key: keyName, value: defVal });
    setKeyOpenIdx(null);
  };

  if (loadingSchemas) {
    return (
      <div style={{ padding: "1rem", display: "flex", gap: "0.5rem", alignItems: "center" }}>
        <Spinner size="md" aria-label="Loading schemas" />
        <span>Loading GSettings schemas…</span>
      </div>
    );
  }

  return (
    <div>
      <LiveAlert message={schemaError} variant="warning" style={{ marginBottom: "0.75rem" }} />

      {/* db_name field */}
      <FormGroup
        label="dconf database name"
        fieldId={`${idPrefix}-dbname`}
        style={{ marginBottom: "1.25rem", maxWidth: "24rem" }}
      >
        <TextInput
          id={`${idPrefix}-dbname`}
          value={content.db_name}
          onChange={(_ev, v) => pushChange({ ...content, db_name: v })}
          placeholder="local"
          isDisabled={isDisabled}
          aria-label="dconf database name"
        />
      </FormGroup>

      {/* Entry rows */}
      {content.entries.length === 0 && (
        <p style={{ color: "var(--pf-t--global--text--color--subtle)", marginBottom: "1rem" }}>
          No entries yet. Click "Add entry" to begin.
        </p>
      )}

      {content.entries.map((entry, idx) => {
        const rowId = `${idPrefix}-row-${idx}`;
        const schema = schemaMap.get(entry.schema_id);
        const keyDef = schema?.keys.find(k => k.name === entry.key);

        // Filter schemas by search term
        const search = (schemaSearch[idx] ?? "").toLowerCase();
        const filteredSchemas = search
          ? schemas.filter(s => s.schema_id.toLowerCase().includes(search) || s.path.toLowerCase().includes(search))
          : schemas;

        return (
          <div
            key={idx}
            style={{
              border: "1px solid var(--pf-t--global--border--color--default)",
              borderRadius: "6px",
              padding: "1rem",
              marginBottom: "0.75rem",
              background: "var(--pf-t--global--background--color--secondary--default)",
            }}
          >
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr auto auto", gap: "0.75rem", alignItems: "end" }}>

              {/* Schema select */}
              <FormGroup label="Schema" fieldId={`${rowId}-schema`} isRequired>
                <Select
                  id={`${rowId}-schema`}
                  isOpen={schemaOpenIdx === idx}
                  onOpenChange={open => setSchemaOpenIdx(open ? idx : null)}
                  selected={entry.schema_id || undefined}
                  onSelect={(_ev, val) => setSchemaForRow(idx, val as string)}
                  toggle={(ref: React.Ref<MenuToggleElement>) => (
                    <MenuToggle
                      ref={ref}
                      onClick={() => setSchemaOpenIdx(v => v === idx ? null : idx)}
                      isExpanded={schemaOpenIdx === idx}
                      isDisabled={isDisabled}
                      style={{ width: "100%", maxWidth: "100%" }}
                      aria-label="Select GSettings schema"
                    >
                      <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", display: "block", maxWidth: "100%" }}>
                        {entry.schema_id || "(choose schema)"}
                      </span>
                    </MenuToggle>
                  )}
                >
                  <div style={{ padding: "0.25rem 0.5rem" }}>
                    <TextInput
                      id={`${rowId}-schema-search`}
                      value={schemaSearch[idx] ?? ""}
                      onChange={(_ev, v) => setSchemaSearch(prev => ({ ...prev, [idx]: v }))}
                      placeholder="Search schemas…"
                      aria-label="Search GSettings schemas"
                    />
                  </div>
                  <SelectList style={{ maxHeight: "18rem", overflowY: "auto" }}>
                    {filteredSchemas.slice(0, 200).map(s => (
                      <SelectOption
                        key={s.schema_id}
                        value={s.schema_id}
                        description={s.path || (s.relocatable ? "(relocatable)" : "")}
                      >
                        {s.schema_id}
                      </SelectOption>
                    ))}
                    {filteredSchemas.length === 0 && (
                      <SelectOption value="" isDisabled>No matching schemas</SelectOption>
                    )}
                  </SelectList>
                </Select>
              </FormGroup>

              {/* Key select */}
              <FormGroup label="Key" fieldId={`${rowId}-key`} isRequired>
                {schema ? (
                  <Select
                    id={`${rowId}-key`}
                    isOpen={keyOpenIdx === idx}
                    onOpenChange={open => setKeyOpenIdx(open ? idx : null)}
                    selected={entry.key || undefined}
                    onSelect={(_ev, val) => setKeyForRow(idx, val as string)}
                    toggle={(ref: React.Ref<MenuToggleElement>) => (
                      <MenuToggle
                        ref={ref}
                        onClick={() => setKeyOpenIdx(v => v === idx ? null : idx)}
                        isExpanded={keyOpenIdx === idx}
                        isDisabled={isDisabled || !entry.schema_id}
                        style={{ width: "100%" }}
                        aria-label="Select GSettings key"
                      >
                        {entry.key || "(choose key)"}
                      </MenuToggle>
                    )}
                  >
                    <SelectList style={{ maxHeight: "18rem", overflowY: "auto" }}>
                      {schema.keys.map(k => (
                        <SelectOption
                          key={k.name}
                          value={k.name}
                          description={k.summary || k.type}
                        >
                          {k.name}
                        </SelectOption>
                      ))}
                    </SelectList>
                  </Select>
                ) : (
                  <TextInput
                    id={`${rowId}-key-text`}
                    value={entry.key}
                    onChange={(_ev, v) => updateEntry(idx, { key: v })}
                    placeholder="key name"
                    isDisabled={isDisabled || !entry.schema_id}
                    aria-label="GSettings key name"
                  />
                )}
                {keyDef?.summary && (
                  <p style={{ fontSize: "0.78rem", color: "var(--pf-t--global--text--color--subtle)", marginTop: "0.2rem" }}>
                    {keyDef.summary}
                  </p>
                )}
              </FormGroup>

              {/* Value widget */}
              <FormGroup label="Value" fieldId={`${rowId}-value`} isRequired>
                <ValueWidget
                  keyDef={keyDef}
                  value={entry.value}
                  onChange={v => updateEntry(idx, { value: v })}
                  rowId={rowId}
                  disabled={isDisabled || !entry.key}
                />
              </FormGroup>

              {/* Lock checkbox */}
              <FormGroup label="Lock" fieldId={`${rowId}-lock`}>
                <Checkbox
                  id={`${rowId}-lock`}
                  label="Prevent override"
                  isChecked={entry.lock}
                  onChange={(_ev, checked) => updateEntry(idx, { lock: checked })}
                  isDisabled={isDisabled}
                  aria-label="Prevent user from overriding this setting"
                />
              </FormGroup>

              {/* Remove button */}
              <div>
                <Button
                  variant="plain"
                  onClick={() => removeEntry(idx)}
                  isDisabled={isDisabled}
                  aria-label={`Remove entry ${idx + 1}`}
                  style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
                >
                  <TrashIcon />
                </Button>
              </div>
            </div>

            {/* Path override for relocatable schemas */}
            {schema?.relocatable && (
              <FormGroup
                label="dconf path override"
                fieldId={`${rowId}-path`}
                style={{ marginTop: "0.5rem", maxWidth: "32rem" }}
              >
                <TextInput
                  id={`${rowId}-path`}
                  value={entry.path}
                  onChange={(_ev, v) => updateEntry(idx, { path: v })}
                  placeholder="/org/example/custom/"
                  isDisabled={isDisabled}
                  aria-label="dconf path override for relocatable schema"
                />
              </FormGroup>
            )}
          </div>
        );
      })}

      <Button
        variant="secondary"
        icon={<PlusCircleIcon />}
        onClick={addEntry}
        isDisabled={isDisabled}
      >
        Add entry
      </Button>
    </div>
  );
};
