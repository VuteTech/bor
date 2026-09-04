// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useState } from "react";
import {
  Button,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Switch,
  TextArea,
  TextInput,
  Title,
} from "@patternfly/react-core";

import type { PolicyFieldDef } from "../../../generated/proto/policy_ui_types";
import { PolicyTreePanel } from "../PolicyTreePanel";
import { FieldHelp } from "./FieldHelp";
import type { PolicyContentEditorProps } from "./PolicyContentEditorProps";
import {
  buildFieldTree,
  detectConfiguredKeys,
  extractContentValue,
  removeContentKey,
  setContentKey,
} from "./policyContentJson";

interface TreePolicyEditorProps extends PolicyContentEditorProps {
  /** Generated field catalogue for this product (`*_ui.ts`). */
  fields: readonly PolicyFieldDef[];
  /** Product name used in headings ("Firefox", "Microsoft Edge"). */
  productLabel: string;
  /** Short prefix for element ids ("ff"). */
  idPrefix: string;
  /** Accessible name for the tree. */
  ariaLabel: string;
}

const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)" };

/** Default a freshly-selected (not yet configured) key starts from — preview only. */
function defaultValueFor(def: PolicyFieldDef): unknown {
  switch (def.type) {
    case "boolean":
      return true;
    case "string":
      return "";
    case "string-enum":
      return def.stringOptions?.[0] ?? "";
    case "integer":
    case "integer-enum":
      return def.intOptions?.[0]?.value ?? 0;
    case "list":
      return [];
    case "json":
      return {};
    case "object": {
      const obj: Record<string, unknown> = {};
      for (const f of def.subFields || []) {
        if (f.type === "boolean") obj[f.key] = false;
        else if (f.type === "string" || f.type === "string-enum") obj[f.key] = "";
        else if (f.type === "list") obj[f.key] = [];
      }
      return obj;
    }
    default:
      return undefined;
  }
}

/**
 * Master-detail editor for the catalogue-driven policy types (Firefox,
 * Thunderbird, Chrome, Edge): a tree of groups and keys on the left, the
 * selected key's value on the right.
 *
 * Selecting a key only *previews* it — its value is loaded into local state
 * without touching the content. The key enters the policy when the user edits
 * the value or clicks "Add to policy", and leaves it with "Remove from policy"
 * (here or on the tree item). Every write goes straight to `onChange`.
 */
export const TreePolicyEditor: React.FC<TreePolicyEditorProps> = ({
  fields,
  productLabel,
  idPrefix,
  ariaLabel,
  contentRaw,
  onChange,
  isDisabled = false,
  onValidityChange,
}) => {
  // Initial selection: the first configured key, with every group that holds a
  // configured key expanded, so an existing policy opens on its own settings.
  const [initial] = useState(() => {
    const keys = detectConfiguredKeys(fields, contentRaw);
    const first = keys[0] ?? null;
    const groups = new Set<string>();
    for (const key of keys) {
      const def = fields.find((f) => f.key === key);
      if (def) groups.add(def.group);
    }
    return { first, value: first ? extractContentValue(contentRaw, first) : undefined, groups };
  });
  const [selectedKey, setSelectedKey] = useState<string | null>(initial.first);
  const [value, setValue] = useState<unknown>(initial.value);
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(initial.groups);
  // Whether the JSON value field for the selected key holds text that does not parse.
  const [jsonInvalid, setJsonInvalid] = useState(false);

  // A stale JSON error must not linger on a different (or no) selection.
  useEffect(() => {
    setJsonInvalid(false);
  }, [selectedKey]);

  useEffect(() => {
    onValidityChange?.(!jsonInvalid);
  }, [jsonInvalid, onValidityChange]);

  // Never leave the caller blocked by an editor that is no longer mounted.
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);

  const tree = buildFieldTree(fields);
  const configuredKeys = detectConfiguredKeys(fields, contentRaw);

  const selectPolicy = (def: PolicyFieldDef) => {
    setSelectedKey(def.key);
    const existing = extractContentValue(contentRaw, def.key);
    setValue(existing !== undefined ? existing : defaultValueFor(def));
  };

  const removePolicy = (key: string) => {
    onChange(removeContentKey(key, contentRaw));
    if (selectedKey === key) {
      setSelectedKey(null);
      setValue(undefined);
    }
  };

  const updateValue = (next: unknown) => {
    setValue(next);
    if (selectedKey) onChange(setContentKey(selectedKey, next, contentRaw));
  };

  const toggleGroup = (group: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  const renderPropertyEditor = () => {
    if (!selectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", ...subtle }}>
          <Title headingLevel="h3" size="lg">Select a {productLabel} policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its value.</p>
        </div>
      );
    }

    const def = fields.find((f) => f.key === selectedKey);
    if (!def) return null;
    const isConfigured = configuredKeys.includes(def.key);
    const jsonErrorId = `${idPrefix}-prop-json-error`;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: def.description ? "0.25rem" : "1rem" }}>
          {def.label}
        </Title>
        {def.description && (
          <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>{def.description}</p>
        )}
        <Form>
          {def.type === "boolean" && (
            <FormGroup label="Value" fieldId={`${idPrefix}-prop-bool`}>
              <Switch
                id={`${idPrefix}-prop-bool`}
                isChecked={value === true}
                onChange={(_ev, checked) => updateValue(checked)}
                label={value === true ? "Enabled" : "Disabled"}
              />
            </FormGroup>
          )}
          {def.type === "string" && (
            <FormGroup label="Value" fieldId={`${idPrefix}-prop-string`}>
              <TextInput
                id={`${idPrefix}-prop-string`}
                value={(value as string) || ""}
                onChange={(_ev, val) => updateValue(val)}
              />
            </FormGroup>
          )}
          {def.type === "integer" && (
            <FormGroup label="Value" fieldId={`${idPrefix}-prop-int`}>
              <TextInput
                id={`${idPrefix}-prop-int`}
                type="number"
                value={String(value ?? 0)}
                onChange={(_ev, val) => updateValue(parseInt(val, 10) || 0)}
              />
            </FormGroup>
          )}
          {def.type === "integer-enum" && def.intOptions && (
            <FormGroup label="Value" fieldId={`${idPrefix}-prop-int-enum`}>
              <FormSelect
                id={`${idPrefix}-prop-int-enum`}
                value={String(value ?? def.intOptions[0]?.value ?? 0)}
                onChange={(_ev, val) => updateValue(parseInt(val, 10))}
              >
                {def.intOptions.map((opt) => (
                  <FormSelectOption key={String(opt.value)} value={String(opt.value)} label={opt.label} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {def.type === "string-enum" && (
            <FormGroup label="Value" fieldId={`${idPrefix}-prop-str-enum`}>
              <FormSelect
                id={`${idPrefix}-prop-str-enum`}
                value={(value as string) || (def.stringOptions?.[0] ?? "")}
                onChange={(_ev, val) => updateValue(val)}
              >
                {(def.stringOptions || []).map((opt) => (
                  <FormSelectOption key={opt} value={opt} label={opt} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {def.type === "list" && (
            <FormGroup label="Values (one per line)" fieldId={`${idPrefix}-prop-list`}>
              <TextArea
                id={`${idPrefix}-prop-list`}
                value={((value as string[]) || []).join("\n")}
                onChange={(_ev, val) => updateValue(val.split("\n").filter(Boolean))}
                rows={5}
                placeholder="One item per line"
              />
              <FieldHelp>One item per line</FieldHelp>
            </FormGroup>
          )}
          {def.type === "json" && (
            <FormGroup label="Value (JSON)" fieldId={`${idPrefix}-prop-json`}>
              <TextArea
                id={`${idPrefix}-prop-json`}
                validated={jsonInvalid ? "error" : "default"}
                aria-invalid={jsonInvalid || undefined}
                aria-describedby={jsonInvalid ? jsonErrorId : undefined}
                value={typeof value === "string" ? value : JSON.stringify(value ?? {}, null, 2)}
                onChange={(_ev, val) => {
                  try {
                    updateValue(JSON.parse(val));
                    setJsonInvalid(false);
                  } catch {
                    // Keep the raw text so the user can fix it, and flag it
                    // invalid so save stays blocked until it parses.
                    updateValue(val);
                    setJsonInvalid(true);
                  }
                }}
                rows={8}
                style={{ fontFamily: "monospace", fontSize: "0.82rem" }}
                placeholder="{}"
              />
              <FieldHelp>Enter a valid JSON object or array</FieldHelp>
              {jsonInvalid && (
                <div
                  id={jsonErrorId}
                  aria-live="polite"
                  style={{ color: "var(--pf-t--global--text--color--status--danger--default)", fontSize: "0.85rem", marginTop: 4 }}
                >
                  Invalid JSON — fix it before saving.
                </div>
              )}
            </FormGroup>
          )}
          {def.type === "object" && def.subFields && (
            <>
              {def.subFields.map((field) => {
                const objVal = (value as Record<string, unknown>) || {};
                const fieldId = `${idPrefix}-prop-${field.key}`;
                return (
                  <FormGroup key={field.key} label={field.label} fieldId={fieldId}>
                    {field.type === "boolean" && (
                      <Switch
                        id={fieldId}
                        isChecked={objVal[field.key] === true}
                        onChange={(_ev, checked) => updateValue({ ...objVal, [field.key]: checked })}
                        label={objVal[field.key] === true ? "Yes" : "No"}
                      />
                    )}
                    {field.type === "string" && (
                      <TextInput
                        id={fieldId}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateValue({ ...objVal, [field.key]: val })}
                      />
                    )}
                    {field.type === "string-enum" && (
                      <FormSelect
                        id={fieldId}
                        value={(objVal[field.key] as string) || ""}
                        onChange={(_ev, val) => updateValue({ ...objVal, [field.key]: val })}
                      >
                        <FormSelectOption key="" value="" label="(not set)" />
                        {(field.stringOptions || []).map((v) => (
                          <FormSelectOption key={v} value={v} label={v} />
                        ))}
                      </FormSelect>
                    )}
                    {field.type === "list" && (
                      <TextArea
                        id={fieldId}
                        value={((objVal[field.key] as string[]) || []).join("\n")}
                        onChange={(_ev, val) =>
                          updateValue({ ...objVal, [field.key]: val.split("\n").filter(Boolean) })
                        }
                        rows={3}
                        placeholder="One item per line"
                      />
                    )}
                  </FormGroup>
                );
              })}
            </>
          )}
          {!isDisabled && (
            <FormGroup fieldId={`${idPrefix}-prop-actions`}>
              {isConfigured ? (
                <Button variant="danger" size="sm" onClick={() => removePolicy(def.key)}>
                  Remove from policy
                </Button>
              ) : (
                <>
                  <div aria-live="polite" style={{ marginBottom: "0.5rem", ...subtle, fontSize: "0.85rem" }}>
                    Previewing this setting — it is not part of the policy yet. Edit its value or add it below.
                  </div>
                  <Button
                    variant="primary"
                    size="sm"
                    isDisabled={jsonInvalid}
                    onClick={() => onChange(setContentKey(def.key, value, contentRaw))}
                  >
                    Add to policy
                  </Button>
                </>
              )}
            </FormGroup>
          )}
        </Form>
      </div>
    );
  };

  return (
    <div style={{ display: "flex", minHeight: "400px" }}>
      <PolicyTreePanel
        ariaLabel={ariaLabel}
        tree={tree}
        configuredKeys={configuredKeys}
        selectedKey={selectedKey}
        expandedGroups={expandedGroups}
        onToggleGroup={toggleGroup}
        onSelect={selectPolicy}
        onRemove={removePolicy}
      />
      <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>{renderPropertyEditor()}</div>
    </div>
  );
};
