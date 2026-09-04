// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState } from "react";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Flex,
  FlexItem,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Label,
  Switch,
  TextArea,
  TextInput,
  Title,
} from "@patternfly/react-core";

import { PolicyTreePanel } from "../PolicyTreePanel";
import { FieldHelp } from "./FieldHelp";
import type { PolicyContentEditorProps } from "./PolicyContentEditorProps";
import {
  FILL_MODE_LABELS,
  KCM_MODULES,
  KCONFIG_ALL_POLICIES,
  KIO_PROTOCOLS,
  buildKConfigContent,
  buildKConfigTree,
  buildKcmRestrictionContent,
  buildUrlRestrictionContent,
  detectKConfigConfiguredKeys,
  extractKConfigEntry,
  hexToRgb,
  parseKcmRestrictions,
  parseUrlRestrictionRules,
  removeKConfigContentKey,
  rgbToHex,
  type KConfigPolicyDef,
  type UrlRestrictionRule,
} from "./kconfigModel";

const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)" };
const dangerText: React.CSSProperties = { color: "var(--pf-t--global--text--color--status--danger--default)" };

const DEFAULT_URL_RULE: UrlRestrictionRule = {
  action: "open",
  referrerProtocol: "",
  referrerHost: "",
  referrerPath: "",
  protocol: "",
  host: "",
  path: "",
  enabled: true,
};

/** Indices of rules whose protocol is not in the KIO list (rendered as "Custom..."). */
function customProtocolIndicesOf(rules: UrlRestrictionRule[]): Set<number> {
  const idxs = new Set<number>();
  rules.forEach((r, i) => {
    if (r.protocol !== "" && !KIO_PROTOCOLS.includes(r.protocol)) idxs.add(i);
  });
  return idxs;
}

/**
 * KDE Plasma (Kconfig) editor: the Kiosk catalogue tree on the left; on the
 * right a value + "Enforced [$i]" form for plain keys, or the structured
 * URL-restriction / System-Settings-module editors for those two entries.
 *
 * Same preview-vs-enable rule as the browser tree editors: selecting a key
 * loads it locally, editing or "Add to policy" writes it to the content.
 */
export const KconfigPolicyEditor: React.FC<PolicyContentEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled = false,
}) => {
  const [initial] = useState(() => {
    const configuredKeys = detectKConfigConfiguredKeys(contentRaw);
    const first = configuredKeys[0] ?? null;
    const groups = new Set<string>();
    for (const key of configuredKeys) {
      const def = KCONFIG_ALL_POLICIES.find((p) => p.key === key);
      if (def) groups.add(def.group);
    }
    const entry = first ? extractKConfigEntry(contentRaw, first) : undefined;
    const rules = configuredKeys.includes("urlRestrictions") ? parseUrlRestrictionRules(contentRaw) : [];
    return {
      first,
      value: entry?.value ?? "",
      enforced: entry?.enforced ?? false,
      groups,
      rules,
      customIdxs: customProtocolIndicesOf(rules),
      modules: configuredKeys.includes("kcmRestrictions") ? parseKcmRestrictions(contentRaw) : [],
    };
  });

  const [selectedKey, setSelectedKey] = useState<string | null>(initial.first);
  const [value, setValue] = useState<string>(initial.value);
  const [enforced, setEnforced] = useState<boolean>(initial.enforced);
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(initial.groups);
  const [urlRules, setUrlRules] = useState<UrlRestrictionRule[]>(initial.rules);
  const [customProtocolIndices, setCustomProtocolIndices] = useState<Set<number>>(initial.customIdxs);
  const [kcmModules, setKcmModules] = useState<string[]>(initial.modules);
  const [kcmCustomInput, setKcmCustomInput] = useState("");

  const tree = buildKConfigTree();
  const configuredKeys = detectKConfigConfiguredKeys(contentRaw);

  const selectPolicy = (def: KConfigPolicyDef) => {
    setSelectedKey(def.key);
    if (def.type === "url-restrictions") {
      const rules = parseUrlRestrictionRules(contentRaw);
      if (rules.length > 0) {
        setUrlRules(rules);
        setCustomProtocolIndices(customProtocolIndicesOf(rules));
      } else {
        // Seed one default rule for preview only — it is written to content
        // when the user edits or adds a rule.
        setUrlRules([DEFAULT_URL_RULE]);
        setCustomProtocolIndices(new Set());
      }
      return;
    }
    if (def.type === "kcm-restrictions") {
      setKcmModules(parseKcmRestrictions(contentRaw));
      setKcmCustomInput("");
      return;
    }
    const existing = extractKConfigEntry(contentRaw, def.key);
    if (existing !== undefined) {
      setValue(existing.value);
      setEnforced(existing.enforced);
    } else {
      // Preview only until the user edits the value or clicks "Add to policy".
      setValue(def.type === "boolean" ? "true" : def.type === "int" ? "0" : "");
      setEnforced(true);
    }
  };

  const removePolicy = (key: string) => {
    onChange(removeKConfigContentKey(key, contentRaw));
    if (selectedKey === key) {
      setSelectedKey(null);
      setValue("");
      setEnforced(false);
    }
  };

  const currentDef = selectedKey ? KCONFIG_ALL_POLICIES.find((p) => p.key === selectedKey) : undefined;

  const updateValue = (next: string) => {
    setValue(next);
    if (currentDef) onChange(buildKConfigContent(currentDef, next, enforced, contentRaw));
  };

  const updateEnforced = (next: boolean) => {
    setEnforced(next);
    if (currentDef) onChange(buildKConfigContent(currentDef, value, next, contentRaw));
  };

  const toggleGroup = (group: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  /* ── System Settings (KCM) module restrictions ── */
  const renderKcmRestrictionsEditor = () => {
    const updateModules = (next: string[]) => {
      setKcmModules(next);
      onChange(buildKcmRestrictionContent(next, contentRaw));
    };
    const addModule = (moduleId: string) => {
      if (!moduleId || kcmModules.includes(moduleId)) return;
      updateModules([...kcmModules, moduleId]);
    };
    const removeModule = (moduleId: string) => updateModules(kcmModules.filter((m) => m !== moduleId));
    const addCustomModules = () => {
      const ids = kcmCustomInput.split(/[\n,]+/).map((s) => s.trim()).filter(Boolean);
      const toAdd = ids.filter((id) => !kcmModules.includes(id));
      if (toAdd.length > 0) updateModules([...kcmModules, ...toAdd]);
      setKcmCustomInput("");
    };
    const availableModules = KCM_MODULES.filter((m) => !kcmModules.includes(m.id));

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>System Settings Module Restrictions</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          Files: <code>/etc/kde5rc</code>, <code>/etc/kde6rc</code> &nbsp; Group: <code>[KDE Control Module Restrictions]</code>
          <br />
          Selected modules will be <strong>restricted</strong> (users will not be able to access them in System Settings).
        </p>

        <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem", alignItems: "flex-end" }}>
          <FormGroup label="Add module" fieldId="kcm-add-select" style={{ flex: 1 }}>
            <FormSelect id="kcm-add-select" value="" onChange={(_ev, val) => { if (val) addModule(val); }}>
              <FormSelectOption
                value=""
                label={availableModules.length > 0 ? "Select a module to restrict..." : "(all known modules added)"}
                isDisabled
              />
              {availableModules.map((m) => (
                <FormSelectOption key={m.id} value={m.id} label={`${m.label} (${m.id})`} />
              ))}
            </FormSelect>
          </FormGroup>
        </div>

        <details style={{ marginBottom: "1rem" }}>
          <summary style={{ cursor: "pointer", ...subtle, fontSize: "0.85rem" }}>Add custom modules</summary>
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.5rem", alignItems: "flex-end" }}>
            <FormGroup label="Custom module IDs" fieldId="kcm-custom-input" style={{ flex: 1 }}>
              <TextArea
                id="kcm-custom-input"
                value={kcmCustomInput}
                onChange={(_ev, val) => setKcmCustomInput(val)}
                rows={2}
                placeholder="kcm_example, kcm_other"
              />
              <FieldHelp>One per line or comma-separated</FieldHelp>
            </FormGroup>
            <Button variant="secondary" size="sm" onClick={addCustomModules} style={{ marginBottom: "0.25rem" }}>Add</Button>
          </div>
        </details>

        {kcmModules.length > 0 ? (
          <div>
            <Title headingLevel="h4" size="md" style={{ marginBottom: "0.5rem" }}>Restricted modules ({kcmModules.length})</Title>
            {kcmModules.map((moduleId) => {
              const mod = KCM_MODULES.find((m) => m.id === moduleId);
              return (
                <div
                  key={moduleId}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    padding: "0.4rem 0.75rem",
                    borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                    fontSize: "0.85rem",
                  }}
                >
                  <span>
                    <strong>{mod ? mod.label : moduleId}</strong>
                    {mod && <span style={{ ...subtle, marginLeft: "0.5rem" }}>({moduleId})</span>}
                    {!mod && <Label isCompact color="orange" style={{ marginLeft: "0.5rem" }}>custom</Label>}
                  </span>
                  <Button
                    variant="plain"
                    size="sm"
                    onClick={() => removeModule(moduleId)}
                    style={{ ...dangerText, padding: "0 0.25rem", minWidth: "auto" }}
                    aria-label={`Remove ${moduleId}`}
                  >
                    Remove
                  </Button>
                </div>
              );
            })}
          </div>
        ) : (
          <p style={{ ...subtle, fontStyle: "italic" }}>No modules restricted. Use the dropdown above to add modules.</p>
        )}
      </div>
    );
  };

  /* ── KIO URL restrictions ── */
  const renderUrlRestrictionsEditor = () => {
    const updateRules = (next: UrlRestrictionRule[]) => {
      setUrlRules(next);
      onChange(buildUrlRestrictionContent(next, contentRaw));
    };
    const updateRule = (index: number, partial: Partial<UrlRestrictionRule>) =>
      updateRules(urlRules.map((r, i) => (i === index ? { ...r, ...partial } : r)));
    const addRule = () => updateRules([...urlRules, { ...DEFAULT_URL_RULE }]);
    const removeRule = (index: number) => {
      const next = new Set<number>();
      for (const ci of customProtocolIndices) {
        if (ci < index) next.add(ci);
        else if (ci > index) next.add(ci - 1);
      }
      setCustomProtocolIndices(next);
      updateRules(urlRules.filter((_, i) => i !== index));
    };

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>URL Restrictions</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          File: <code>kdeglobals</code> &nbsp; Group: <code>[KDE URL Restrictions]</code>
        </p>
        <Button variant="secondary" size="sm" onClick={addRule} style={{ marginBottom: "1rem" }}>+ Add Rule</Button>
        {urlRules.map((rule, idx) => (
          <Card key={idx} isCompact style={{ marginBottom: "0.75rem" }}>
            <CardTitle>
              <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }}>
                <FlexItem><strong>Rule {idx + 1}</strong></FlexItem>
                <FlexItem>
                  <Button variant="plain" size="sm" onClick={() => removeRule(idx)} aria-label={`Remove rule ${idx + 1}`} style={dangerText}>
                    Remove
                  </Button>
                </FlexItem>
              </Flex>
            </CardTitle>
            <CardBody>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem", marginBottom: "0.75rem" }}>
                <FormGroup label="Action" fieldId={`url-action-${idx}`}>
                  <FormSelect id={`url-action-${idx}`} value={rule.action} onChange={(_ev, val) => updateRule(idx, { action: val as UrlRestrictionRule["action"] })}>
                    <FormSelectOption value="open" label="open" />
                    <FormSelectOption value="list" label="list" />
                    <FormSelectOption value="redirect" label="redirect" />
                  </FormSelect>
                </FormGroup>
                <FormGroup label="Protocol" fieldId={`url-protocol-${idx}`}>
                  <FormSelect
                    id={`url-protocol-${idx}`}
                    value={
                      customProtocolIndices.has(idx)
                        ? "__custom__"
                        : KIO_PROTOCOLS.includes(rule.protocol)
                          ? rule.protocol
                          : rule.protocol === ""
                            ? ""
                            : "__custom__"
                    }
                    onChange={(_ev, val) => {
                      if (val === "__custom__") {
                        setCustomProtocolIndices((prev) => new Set(prev).add(idx));
                        updateRule(idx, { protocol: "" });
                      } else {
                        setCustomProtocolIndices((prev) => { const next = new Set(prev); next.delete(idx); return next; });
                        updateRule(idx, { protocol: val });
                      }
                    }}
                  >
                    <FormSelectOption value="" label="(any protocol)" />
                    {KIO_PROTOCOLS.map((p) => <FormSelectOption key={p} value={p} label={p} />)}
                    <FormSelectOption value="__custom__" label="Custom..." />
                  </FormSelect>
                  {customProtocolIndices.has(idx) && (
                    <TextInput
                      id={`url-protocol-custom-${idx}`}
                      value={rule.protocol}
                      onChange={(_ev, val) => updateRule(idx, { protocol: val })}
                      placeholder="Enter custom protocol"
                      style={{ marginTop: "0.5rem" }}
                    />
                  )}
                  <FieldHelp>Without ! suffix = prefix-matches (e.g. http matches https)</FieldHelp>
                </FormGroup>
                <FormGroup label="Access" fieldId={`url-enabled-${idx}`}>
                  <Switch id={`url-enabled-${idx}`} isChecked={rule.enabled} onChange={(_ev, checked) => updateRule(idx, { enabled: checked })} label={rule.enabled ? "Allow" : "Deny"} />
                </FormGroup>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.75rem", marginBottom: "0.75rem" }}>
                <FormGroup label="Host" fieldId={`url-host-${idx}`}>
                  <TextInput id={`url-host-${idx}`} value={rule.host} onChange={(_ev, val) => updateRule(idx, { host: val })} placeholder="blank = all" />
                  <FieldHelp>*.example.com, blank = all</FieldHelp>
                </FormGroup>
                <FormGroup label="Path" fieldId={`url-path-${idx}`}>
                  <TextInput id={`url-path-${idx}`} value={rule.path} onChange={(_ev, val) => updateRule(idx, { path: val })} placeholder="blank = all" />
                  <FieldHelp>/path, blank = all, ! = exact only</FieldHelp>
                </FormGroup>
              </div>
              <details style={{ marginTop: "0.25rem" }}>
                <summary style={{ cursor: "pointer", ...subtle, fontSize: "0.85rem" }}>Referrer Matching (advanced)</summary>
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem", marginTop: "0.5rem" }}>
                  <FormGroup label="Referrer Protocol" fieldId={`url-ref-proto-${idx}`}>
                    <TextInput id={`url-ref-proto-${idx}`} value={rule.referrerProtocol} onChange={(_ev, val) => updateRule(idx, { referrerProtocol: val })} placeholder="blank = all" />
                  </FormGroup>
                  <FormGroup label="Referrer Host" fieldId={`url-ref-host-${idx}`}>
                    <TextInput id={`url-ref-host-${idx}`} value={rule.referrerHost} onChange={(_ev, val) => updateRule(idx, { referrerHost: val })} placeholder="blank = all" />
                  </FormGroup>
                  <FormGroup label="Referrer Path" fieldId={`url-ref-path-${idx}`}>
                    <TextInput id={`url-ref-path-${idx}`} value={rule.referrerPath} onChange={(_ev, val) => updateRule(idx, { referrerPath: val })} placeholder="blank = all" />
                  </FormGroup>
                </div>
              </details>
            </CardBody>
          </Card>
        ))}
        {urlRules.length === 0 && (
          <p style={{ ...subtle, fontStyle: "italic" }}>No rules configured. Click &quot;+ Add Rule&quot; to begin.</p>
        )}
      </div>
    );
  };

  /* ── Plain key: value + enforced ── */
  const renderPropertyEditor = () => {
    if (!selectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", ...subtle }}>
          <Title headingLevel="h3" size="lg">Select a KDE Kiosk policy</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a policy from the tree on the left to configure its properties.</p>
        </div>
      );
    }
    if (selectedKey === "urlRestrictions") return renderUrlRestrictionsEditor();
    if (selectedKey === "kcmRestrictions") return renderKcmRestrictionsEditor();
    if (!currentDef) return null;
    const def = currentDef;

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{def.label}</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>{def.group}</p>
        <Form>
          {def.type === "boolean" && (
            <FormGroup label="Value" fieldId="kc-prop-bool">
              <Switch
                id="kc-prop-bool"
                isChecked={value === "true"}
                onChange={(_ev, checked) => updateValue(checked ? "true" : "false")}
                label={value === "true" ? "true" : "false"}
              />
            </FormGroup>
          )}
          {def.type === "string" && (
            <FormGroup label="Value" fieldId="kc-prop-string">
              <TextInput id="kc-prop-string" value={value} onChange={(_ev, val) => updateValue(val)} />
            </FormGroup>
          )}
          {def.type === "int" && (
            <FormGroup label="Value" fieldId="kc-prop-int">
              <TextInput id="kc-prop-int" type="number" value={value} onChange={(_ev, val) => updateValue(val)} />
            </FormGroup>
          )}
          {def.type === "select" && (
            <FormGroup label="Value" fieldId="kc-prop-select">
              <FormSelect id="kc-prop-select" value={value} onChange={(_ev, val) => updateValue(val)}>
                <FormSelectOption key="" value="" label="(not set)" />
                {(def.selectOptions || []).map((v) => (
                  <FormSelectOption key={v} value={v} label={FILL_MODE_LABELS[v] || v} />
                ))}
              </FormSelect>
            </FormGroup>
          )}
          {def.type === "color" && (
            <FormGroup label="Value" fieldId="kc-prop-color">
              <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
                <input
                  type="color"
                  id="kc-prop-color"
                  value={rgbToHex(value || "0,0,0")}
                  onChange={(ev) => updateValue(hexToRgb(ev.target.value))}
                  style={{ width: "48px", height: "36px", padding: "2px", border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "4px", cursor: "pointer" }}
                />
                <TextInput
                  id="kc-prop-color-text"
                  value={value}
                  onChange={(_ev, val) => updateValue(val)}
                  placeholder="R,G,B"
                  style={{ maxWidth: "140px" }}
                  aria-label="Colour as R,G,B"
                />
              </div>
            </FormGroup>
          )}
          <FormGroup label="Enforced (Immutable)" fieldId="kc-prop-enforced">
            <Switch
              id="kc-prop-enforced"
              isChecked={enforced}
              onChange={(_ev, checked) => updateEnforced(checked)}
              label={enforced ? "Enforced [$i]" : "Not enforced"}
            />
          </FormGroup>
          {!isDisabled && (
            <FormGroup fieldId="kc-prop-actions">
              {configuredKeys.includes(def.key) ? (
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
                    onClick={() => onChange(buildKConfigContent(def, value, enforced, contentRaw))}
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
        ariaLabel="KDE Kiosk policy settings"
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
