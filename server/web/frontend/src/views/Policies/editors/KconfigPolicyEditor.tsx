// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useMemo, useState } from "react";
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
  KCONFIG_CATALOGUE,
  KCONFIG_CATEGORIES,
  boolDisplay,
  defaultCatalogueValue,
  findCatalogueEntry,
  groupPathLabel,
  targetLabel,
  type KConfigCatalogueEntry,
} from "./kconfigCatalogue";
import {
  DEFAULT_URL_RULE,
  KCM_MODULES,
  KCONFIG_PRESETS,
  KIO_PROTOCOLS,
  URL_RULE_PRESETS,
  applyKConfigPreset,
  catalogueEntryForSetting,
  customSettings,
  findEntrySetting,
  hasAnyKConfigContent,
  importKConfigIni,
  parseGroupPathInput,
  parseKConfigContent,
  removeEntry,
  removeSettingByKey,
  renderKConfigPreview,
  serializeKConfigContent,
  settingKey,
  upsertCustomSetting,
  upsertEntry,
  hexToRgb,
  rgbToHex,
  type KConfigContent,
  type KConfigSettingJson,
  type UrlRestrictionRule,
} from "./kconfigModel";

const subtle: React.CSSProperties = { color: "var(--pf-t--global--text--color--subtle)" };
const dangerText: React.CSSProperties = { color: "var(--pf-t--global--text--color--status--danger--default)" };
const mono: React.CSSProperties = { fontFamily: "monospace", fontSize: "0.82rem" };

/* Tree item keys for the non-catalogue editors. */
const KEY_KCM = "__kcm";
const KEY_URL = "__url";
const KEY_FILE_LOCK = "__file_lock";
const KEY_PREVIEW = "__preview";
const KEY_IMPORT = "__import";
const KEY_CUSTOM_NEW = "__custom_new";
const CUSTOM_PREFIX = "custom:";

const GROUP_RESTRICTIONS = "System Settings & URL Rules";
const GROUP_CUSTOM = "Custom Entries";
const GROUP_ADVANCED = "Advanced";

interface TreeItem {
  key: string;
  label: string;
}

/** Indices of rules whose protocol is not in the KIO list (rendered as "Custom..."). */
function customProtocolIndicesOf(rules: UrlRestrictionRule[]): Set<number> {
  const idxs = new Set<number>();
  rules.forEach((r, i) => {
    if (r.protocol !== "" && !KIO_PROTOCOLS.includes(r.protocol)) idxs.add(i);
  });
  return idxs;
}

function customItemKey(s: KConfigSettingJson): string {
  return CUSTOM_PREFIX + settingKey(s);
}

function customItemLabel(s: KConfigSettingJson): string {
  return `${s.file} ${groupPathLabel(s.groupPath)} ${s.key}`;
}

/** Catalogue search: label, key, id, description and group. */
function matchesFilter(e: KConfigCatalogueEntry, filter: string): boolean {
  const f = filter.toLowerCase();
  return (
    e.label.toLowerCase().includes(f) ||
    e.key.toLowerCase().includes(f) ||
    e.id.toLowerCase().includes(f) ||
    e.file.toLowerCase().includes(f) ||
    (e.description ?? "").toLowerCase().includes(f) ||
    e.groupPath.join("][").toLowerCase().includes(f) ||
    (e.windowsEquivalent ?? "").toLowerCase().includes(f)
  );
}

/**
 * KDE Plasma (Kconfig) editor: the settings catalogue as a searchable tree
 * on the left, the selected setting on the right. Catalogue settings are
 * written as generic entries; System Settings modules, URL rules, custom
 * entries, the whole-file lock, INI import and the rendered-file preview
 * have their own panes.
 *
 * Same preview-vs-enable rule as the other tree editors: selecting a
 * setting loads it locally, editing or "Add to policy" writes it.
 */
export const KconfigPolicyEditor: React.FC<PolicyContentEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled = false,
}) => {
  // Load once; older typed content is upgraded to generic settings and,
  // for editable policies, written back so the stored shape is current.
  const [initial] = useState(() => parseKConfigContent(contentRaw));
  const content = useMemo(() => parseKConfigContent(contentRaw).content, [contentRaw]);

  useEffect(() => {
    if (initial.changed && !isDisabled) onChange(serializeKConfigContent(initial.content));
    // Only on mount: the upgrade must run exactly once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const configuredEntryIds = useMemo(() => {
    const ids: string[] = [];
    for (const e of KCONFIG_CATALOGUE) if (findEntrySetting(content, e)) ids.push(e.id);
    return ids;
  }, [content]);
  const custom = useMemo(() => customSettings(content), [content]);

  const configuredKeys = useMemo(() => {
    const keys = [...configuredEntryIds, ...custom.map(customItemKey)];
    if (content.kcmRestrictions.length > 0) keys.push(KEY_KCM);
    if (content.urlRestrictions.length > 0) keys.push(KEY_URL);
    if (content.fileImmutableKdeglobals) keys.push(KEY_FILE_LOCK);
    return keys;
  }, [configuredEntryIds, custom, content]);

  const [selectedKey, setSelectedKey] = useState<string | null>(() => {
    const first = configuredKeys[0];
    return first ?? null;
  });
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(() => {
    const groups = new Set<string>();
    for (const id of configuredEntryIds) {
      const e = findCatalogueEntry(id);
      if (e) groups.add(e.category);
    }
    if (content.kcmRestrictions.length > 0 || content.urlRestrictions.length > 0) groups.add(GROUP_RESTRICTIONS);
    if (custom.length > 0) groups.add(GROUP_CUSTOM);
    if (content.fileImmutableKdeglobals) groups.add(GROUP_ADVANCED);
    return groups;
  });
  const [filter, setFilter] = useState("");

  // Local (preview) state for the selected catalogue setting.
  const [value, setValue] = useState("");
  const [immutable, setImmutable] = useState(true);
  const [expand, setExpand] = useState(false);
  // Status line for presets, imports and the content upgrade.
  const [notice, setNotice] = useState<string>(() => {
    const parts: string[] = [];
    if (initial.convertedFields.length > 0) parts.push(`Converted ${initial.convertedFields.length} setting(s) from the previous format.`);
    if (initial.droppedFields.length > 0) {
      parts.push(`Removed ${initial.droppedFields.join(", ")}: KDE Plasma 5 and 6 do not read [KDE Resource Restrictions], so these settings never had an effect.`);
    }
    return parts.join(" ");
  });

  const write = (next: KConfigContent) => onChange(serializeKConfigContent(next));

  /* ── Tree ── */
  const tree = useMemo(() => {
    const t = new Map<string, TreeItem[]>();
    const f = filter.trim();
    for (const category of KCONFIG_CATEGORIES) {
      const items = KCONFIG_CATALOGUE.filter((e) => e.category === category && (!f || matchesFilter(e, f))).map((e) => ({ key: e.id, label: e.label }));
      if (items.length > 0) t.set(category, items);
    }
    const special: TreeItem[] = [
      { key: KEY_KCM, label: "System Settings modules" },
      { key: KEY_URL, label: "URL restrictions" },
    ].filter((i) => !f || i.label.toLowerCase().includes(f.toLowerCase()));
    if (special.length > 0) t.set(GROUP_RESTRICTIONS, special);
    const customItems: TreeItem[] = [
      ...custom.map((s) => ({ key: customItemKey(s), label: customItemLabel(s) })),
      { key: KEY_CUSTOM_NEW, label: "Add a custom entry" },
    ].filter((i) => !f || i.label.toLowerCase().includes(f.toLowerCase()));
    if (customItems.length > 0) t.set(GROUP_CUSTOM, customItems);
    const advanced: TreeItem[] = [
      { key: KEY_FILE_LOCK, label: "Lock the whole kdeglobals file" },
      { key: KEY_IMPORT, label: "Import INI" },
      { key: KEY_PREVIEW, label: "Preview rendered files" },
    ].filter((i) => !f || i.label.toLowerCase().includes(f.toLowerCase()));
    if (advanced.length > 0) t.set(GROUP_ADVANCED, advanced);
    return t;
  }, [filter, custom]);

  const effectiveExpanded = filter.trim() ? new Set(tree.keys()) : expandedGroups;

  const toggleGroup = (group: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  };

  const selectItem = (item: TreeItem) => {
    setSelectedKey(item.key);
    const entry = findCatalogueEntry(item.key);
    if (entry) {
      const existing = findEntrySetting(content, entry);
      if (existing) {
        setValue(existing.value ?? "");
        setImmutable(existing.immutable === true);
        setExpand(existing.expand === true);
      } else {
        setValue(defaultCatalogueValue(entry));
        setImmutable(entry.immutableDefault !== false);
        setExpand(false);
      }
    }
  };

  const removeItem = (key: string) => {
    const entry = findCatalogueEntry(key);
    if (entry) write(removeEntry(content, entry));
    else if (key.startsWith(CUSTOM_PREFIX)) write(removeSettingByKey(content, key.slice(CUSTOM_PREFIX.length)));
    else if (key === KEY_KCM) write({ ...content, kcmRestrictions: [], kcmMode: "KCM_HIDE_LISTED" });
    else if (key === KEY_URL) write({ ...content, urlRestrictions: [] });
    else if (key === KEY_FILE_LOCK) write({ ...content, fileImmutableKdeglobals: false });
    if (selectedKey === key && key.startsWith(CUSTOM_PREFIX)) setSelectedKey(null);
  };

  const applyPreset = (presetId: string) => {
    const preset = KCONFIG_PRESETS.find((p) => p.id === presetId);
    if (!preset) return;
    const { content: next, added } = applyKConfigPreset(content, preset);
    write(next);
    setNotice(added > 0 ? `${preset.label}: added ${added} setting(s). Existing values were kept.` : `${preset.label}: every setting is already configured.`);
    setExpandedGroups((prev) => {
      const n = new Set(prev);
      for (const s of preset.settings) {
        const e = findCatalogueEntry(s.id);
        if (e) n.add(e.category);
      }
      if (preset.kcmRestrictions) n.add(GROUP_RESTRICTIONS);
      return n;
    });
  };

  /* ── Catalogue setting pane ── */
  const renderEntryEditor = (entry: KConfigCatalogueEntry) => {
    const configured = findEntrySetting(content, entry) !== undefined;
    const commit = (v: string, imm: boolean, exp: boolean) => write(upsertEntry(content, entry, v, imm, exp));
    const updateValue = (v: string) => {
      setValue(v);
      if (configured) commit(v, immutable, expand);
    };
    const updateImmutable = (imm: boolean) => {
      setImmutable(imm);
      if (configured) commit(value, imm, expand);
    };
    const updateExpand = (exp: boolean) => {
      setExpand(exp);
      if (configured) commit(value, immutable, exp);
    };
    const numeric = entry.type === "Int" || entry.type === "UInt" || entry.type === "Double";
    const textual = entry.type === "String" || entry.type === "Path" || entry.type === "Url" || entry.type === "Font";
    const boolChecked = entry.inverted ? value === "false" : value === "true";

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{entry.label}</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>
          {entry.category} &nbsp;·&nbsp; <code>{targetLabel(entry.file, entry.groupPath, entry.key)}</code>
          {entry.plasmaMajor && entry.plasmaMajor.length > 0 && (
            <Label isCompact color="blue" style={{ marginLeft: "0.5rem" }}>Plasma {entry.plasmaMajor.join(", ")}</Label>
          )}
        </p>
        {entry.description && <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>{entry.description}</p>}
        {entry.windowsEquivalent && (
          <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>Windows Group Policy equivalent: {entry.windowsEquivalent}</p>
        )}
        <Form>
          {entry.type === "Bool" && (
            <FormGroup label="Value" fieldId="kc-value-bool">
              <Switch
                id="kc-value-bool"
                isChecked={boolChecked}
                onChange={(_ev, checked) => updateValue((entry.inverted ? !checked : checked) ? "true" : "false")}
                label={boolDisplay(entry, value)}
              />
            </FormGroup>
          )}
          {entry.type === "Enum" && (
            <FormGroup label="Value" fieldId="kc-value-enum">
              <FormSelect id="kc-value-enum" value={value} onChange={(_ev, v) => updateValue(v)}>
                {(entry.choices ?? []).map((c) => <FormSelectOption key={c} value={c} label={c} />)}
              </FormSelect>
            </FormGroup>
          )}
          {numeric && (
            <FormGroup label="Value" fieldId="kc-value-number">
              <TextInput
                id="kc-value-number"
                type="number"
                value={value}
                min={entry.rangeMin}
                max={entry.rangeMax}
                step={entry.type === "Double" ? "any" : 1}
                onChange={(_ev, v) => updateValue(v)}
              />
              {(entry.rangeMin || entry.rangeMax) && (
                <FieldHelp>
                  {entry.rangeMin && `Minimum ${entry.rangeMin}`}
                  {entry.rangeMin && entry.rangeMax && ", "}
                  {entry.rangeMax && `maximum ${entry.rangeMax}`}
                </FieldHelp>
              )}
            </FormGroup>
          )}
          {textual && (
            <FormGroup label="Value" fieldId="kc-value-text">
              <TextInput id="kc-value-text" value={value} onChange={(_ev, v) => updateValue(v)} />
            </FormGroup>
          )}
          {entry.type === "StringList" && (
            <FormGroup label="Values (one per line)" fieldId="kc-value-list">
              <TextArea
                id="kc-value-list"
                value={value.split(",").filter(Boolean).join("\n")}
                onChange={(_ev, v) => updateValue(v.split("\n").map((x) => x.trim()).filter(Boolean).join(","))}
                rows={5}
              />
              <FieldHelp>Stored comma-separated, as KDE expects</FieldHelp>
            </FormGroup>
          )}
          {entry.type === "Color" && (
            <FormGroup label="Value" fieldId="kc-value-color">
              <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
                <input
                  type="color"
                  id="kc-value-color"
                  value={rgbToHex(value || "0,0,0")}
                  onChange={(ev) => updateValue(hexToRgb(ev.target.value))}
                  style={{ width: "48px", height: "36px", padding: "2px", border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "4px", cursor: "pointer" }}
                />
                <TextInput id="kc-value-color-text" value={value} onChange={(_ev, v) => updateValue(v)} placeholder="R,G,B" style={{ maxWidth: "140px" }} aria-label="Colour as R,G,B" />
              </div>
            </FormGroup>
          )}
          {(textual || entry.type === "StringList") && (
            <FormGroup label="Expand $VARIABLES" fieldId="kc-value-expand">
              <Switch
                id="kc-value-expand"
                isChecked={expand}
                onChange={(_ev, checked) => updateExpand(checked)}
                label={expand ? "KDE expands $HOME and other variables ([$e])" : "Written literally"}
              />
            </FormGroup>
          )}
          <FormGroup label="Enforced (Immutable)" fieldId="kc-value-immutable">
            <Switch
              id="kc-value-immutable"
              isChecked={immutable}
              onChange={(_ev, checked) => updateImmutable(checked)}
              label={immutable ? "Enforced [$i]: users cannot change it" : "Default only: users may change it"}
            />
          </FormGroup>
          {!isDisabled && (
            <FormGroup fieldId="kc-value-actions">
              {configured ? (
                <Button variant="danger" size="sm" onClick={() => write(removeEntry(content, entry))}>Remove from policy</Button>
              ) : (
                <>
                  <div aria-live="polite" style={{ marginBottom: "0.5rem", ...subtle, fontSize: "0.85rem" }}>
                    Previewing this setting; it is not part of the policy yet. Edit its value or add it below.
                  </div>
                  <Button variant="primary" size="sm" onClick={() => commit(value, immutable, expand)}>Add to policy</Button>
                </>
              )}
            </FormGroup>
          )}
        </Form>
      </div>
    );
  };

  /* ── System Settings modules ── */
  const [kcmCustomInput, setKcmCustomInput] = useState("");
  const renderKcmEditor = () => {
    const modules = content.kcmRestrictions;
    const updateModules = (next: string[]) => write({ ...content, kcmRestrictions: next });
    const addModule = (id: string) => {
      if (!id || modules.includes(id)) return;
      updateModules([...modules, id]);
    };
    const addCustomModules = () => {
      const ids = kcmCustomInput.split(/[\n,]+/).map((s) => s.trim()).filter(Boolean).filter((id) => !modules.includes(id));
      if (ids.length > 0) updateModules([...modules, ...ids]);
      setKcmCustomInput("");
    };
    const available = KCM_MODULES.filter((m) => !modules.includes(m.id));
    const showOnly = content.kcmMode === "KCM_SHOW_ONLY_LISTED";

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>System Settings modules</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          File: <code>kdeglobals</code> &nbsp; Group: <code>[KDE Control Module Restrictions]</code>
          <br />
          Windows Group Policy equivalent: Settings Page Visibility (hide: / showonly:).
        </p>
        <FormGroup label="Mode" fieldId="kcm-mode" style={{ marginBottom: "1rem" }}>
          <FormSelect
            id="kcm-mode"
            value={content.kcmMode}
            isDisabled={isDisabled}
            onChange={(_ev, v) => write({ ...content, kcmMode: v === "KCM_SHOW_ONLY_LISTED" ? "KCM_SHOW_ONLY_LISTED" : "KCM_HIDE_LISTED" })}
          >
            <FormSelectOption value="KCM_HIDE_LISTED" label="Hide the listed modules" />
            <FormSelectOption value="KCM_SHOW_ONLY_LISTED" label="Show only the listed modules (hide every other installed module)" />
          </FormSelect>
          {showOnly && <FieldHelp>Each node hides the modules it has installed that are not listed here; the listed ones stay visible even if another policy hides them.</FieldHelp>}
        </FormGroup>
        <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem", alignItems: "flex-end" }}>
          <FormGroup label="Add module" fieldId="kcm-add-select" style={{ flex: 1 }}>
            <FormSelect id="kcm-add-select" value="" isDisabled={isDisabled} onChange={(_ev, v) => { if (v) addModule(v); }}>
              <FormSelectOption value="" label={available.length > 0 ? "Select a module..." : "(all known modules added)"} isDisabled />
              {available.map((m) => <FormSelectOption key={m.id} value={m.id} label={`${m.label} (${m.id})`} />)}
            </FormSelect>
          </FormGroup>
        </div>
        <details style={{ marginBottom: "1rem" }}>
          <summary style={{ cursor: "pointer", ...subtle, fontSize: "0.85rem" }}>Add custom module ids</summary>
          <div style={{ display: "flex", gap: "0.5rem", marginTop: "0.5rem", alignItems: "flex-end" }}>
            <FormGroup label="Module ids" fieldId="kcm-custom-input" style={{ flex: 1 }}>
              <TextArea id="kcm-custom-input" value={kcmCustomInput} onChange={(_ev, v) => setKcmCustomInput(v)} rows={2} placeholder="kcm_example, kcm_other" isDisabled={isDisabled} />
              <FieldHelp>One per line or comma-separated; see kcmshell6 --list on a node</FieldHelp>
            </FormGroup>
            <Button variant="secondary" size="sm" onClick={addCustomModules} isDisabled={isDisabled} style={{ marginBottom: "0.25rem" }}>Add</Button>
          </div>
        </details>
        {modules.length > 0 ? (
          <div>
            <Title headingLevel="h4" size="md" style={{ marginBottom: "0.5rem" }}>{showOnly ? "Visible" : "Hidden"} modules ({modules.length})</Title>
            {modules.map((id) => {
              const mod = KCM_MODULES.find((m) => m.id === id);
              return (
                <div key={id} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0.4rem 0.75rem", borderBottom: "1px solid var(--pf-t--global--border--color--default)", fontSize: "0.85rem" }}>
                  <span>
                    <strong>{mod ? mod.label : id}</strong>
                    {mod && <span style={{ ...subtle, marginLeft: "0.5rem" }}>({id})</span>}
                    {!mod && <Label isCompact color="orange" style={{ marginLeft: "0.5rem" }}>custom</Label>}
                  </span>
                  {!isDisabled && (
                    <Button variant="plain" size="sm" onClick={() => updateModules(modules.filter((m) => m !== id))} style={{ ...dangerText, padding: "0 0.25rem", minWidth: "auto" }} aria-label={`Remove ${id}`}>
                      Remove
                    </Button>
                  )}
                </div>
              );
            })}
          </div>
        ) : (
          <p style={{ ...subtle, fontStyle: "italic" }}>No modules listed. Use the drop-down above to add modules.</p>
        )}
      </div>
    );
  };

  /* ── URL restrictions ── */
  const [customProtocolIndices, setCustomProtocolIndices] = useState<Set<number>>(() => customProtocolIndicesOf(content.urlRestrictions));
  const renderUrlEditor = () => {
    const rules = content.urlRestrictions;
    const updateRules = (next: UrlRestrictionRule[]) => write({ ...content, urlRestrictions: next });
    const updateRule = (index: number, partial: Partial<UrlRestrictionRule>) => updateRules(rules.map((r, i) => (i === index ? { ...r, ...partial } : r)));
    const removeRule = (index: number) => {
      const next = new Set<number>();
      for (const ci of customProtocolIndices) {
        if (ci < index) next.add(ci);
        else if (ci > index) next.add(ci - 1);
      }
      setCustomProtocolIndices(next);
      updateRules(rules.filter((_, i) => i !== index));
    };
    const addPreset = (id: string) => {
      const preset = URL_RULE_PRESETS.find((p) => p.id === id);
      if (!preset) return;
      updateRules([...rules, ...preset.rules.map((r) => ({ ...r }))]);
      setNotice(`${preset.label}: added ${preset.rules.length} rule(s).`);
    };

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>URL restrictions</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          File: <code>kdeglobals</code> &nbsp; Group: <code>[KDE URL Restrictions]</code> &nbsp; Always enforced. Applies to KDE applications; <code>$HOME</code> and <code>$TMP</code> are expanded in paths.
          <br />
          Windows Group Policy equivalent: Prevent access to drives from My Computer; Remove Map Network Drive.
        </p>
        {!isDisabled && (
          <div style={{ display: "flex", gap: "0.5rem", marginBottom: "1rem", alignItems: "flex-end" }}>
            <Button variant="secondary" size="sm" onClick={() => updateRules([...rules, { ...DEFAULT_URL_RULE }])}>+ Add rule</Button>
            <FormGroup label="Add a rule set" fieldId="url-preset" style={{ flex: 1, maxWidth: "420px" }}>
              <FormSelect id="url-preset" value="" onChange={(_ev, v) => { if (v) addPreset(v); }}>
                <FormSelectOption value="" label="Select a rule set..." isDisabled />
                {URL_RULE_PRESETS.map((p) => <FormSelectOption key={p.id} value={p.id} label={p.label} />)}
              </FormSelect>
            </FormGroup>
          </div>
        )}
        {rules.map((rule, idx) => (
          <Card key={idx} isCompact style={{ marginBottom: "0.75rem" }}>
            <CardTitle>
              <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }}>
                <FlexItem><strong>Rule {idx + 1}</strong></FlexItem>
                {!isDisabled && (
                  <FlexItem>
                    <Button variant="plain" size="sm" onClick={() => removeRule(idx)} aria-label={`Remove rule ${idx + 1}`} style={dangerText}>Remove</Button>
                  </FlexItem>
                )}
              </Flex>
            </CardTitle>
            <CardBody>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem", marginBottom: "0.75rem" }}>
                <FormGroup label="Action" fieldId={`url-action-${idx}`}>
                  <FormSelect id={`url-action-${idx}`} value={rule.action} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { action: v as UrlRestrictionRule["action"] })}>
                    <FormSelectOption value="open" label="open" />
                    <FormSelectOption value="list" label="list" />
                    <FormSelectOption value="redirect" label="redirect" />
                  </FormSelect>
                </FormGroup>
                <FormGroup label="Protocol" fieldId={`url-protocol-${idx}`}>
                  <FormSelect
                    id={`url-protocol-${idx}`}
                    isDisabled={isDisabled}
                    value={customProtocolIndices.has(idx) ? "__custom__" : KIO_PROTOCOLS.includes(rule.protocol) ? rule.protocol : rule.protocol === "" ? "" : "__custom__"}
                    onChange={(_ev, v) => {
                      if (v === "__custom__") {
                        setCustomProtocolIndices((prev) => new Set(prev).add(idx));
                        updateRule(idx, { protocol: "" });
                      } else {
                        setCustomProtocolIndices((prev) => { const n = new Set(prev); n.delete(idx); return n; });
                        updateRule(idx, { protocol: v });
                      }
                    }}
                  >
                    <FormSelectOption value="" label="(any protocol)" />
                    {KIO_PROTOCOLS.map((p) => <FormSelectOption key={p} value={p} label={p} />)}
                    <FormSelectOption value="__custom__" label="Custom..." />
                  </FormSelect>
                  {customProtocolIndices.has(idx) && (
                    <TextInput id={`url-protocol-custom-${idx}`} value={rule.protocol} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { protocol: v })} placeholder="Enter custom protocol" style={{ marginTop: "0.5rem" }} />
                  )}
                  <FieldHelp>Without ! suffix = prefix match (http also matches https)</FieldHelp>
                </FormGroup>
                <FormGroup label="Access" fieldId={`url-enabled-${idx}`}>
                  <Switch id={`url-enabled-${idx}`} isChecked={rule.enabled} isDisabled={isDisabled} onChange={(_ev, checked) => updateRule(idx, { enabled: checked })} label={rule.enabled ? "Allow" : "Deny"} />
                </FormGroup>
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.75rem", marginBottom: "0.75rem" }}>
                <FormGroup label="Host" fieldId={`url-host-${idx}`}>
                  <TextInput id={`url-host-${idx}`} value={rule.host} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { host: v })} placeholder="blank = all" />
                  <FieldHelp>*.example.com, blank = all</FieldHelp>
                </FormGroup>
                <FormGroup label="Path" fieldId={`url-path-${idx}`}>
                  <TextInput id={`url-path-${idx}`} value={rule.path} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { path: v })} placeholder="blank = all" />
                  <FieldHelp>/path or $HOME, blank = all, ! = exact only</FieldHelp>
                </FormGroup>
              </div>
              <details style={{ marginTop: "0.25rem" }}>
                <summary style={{ cursor: "pointer", ...subtle, fontSize: "0.85rem" }}>Referrer matching (advanced)</summary>
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem", marginTop: "0.5rem" }}>
                  <FormGroup label="Referrer protocol" fieldId={`url-ref-proto-${idx}`}>
                    <TextInput id={`url-ref-proto-${idx}`} value={rule.referrerProtocol} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { referrerProtocol: v })} placeholder="blank = all" />
                  </FormGroup>
                  <FormGroup label="Referrer host" fieldId={`url-ref-host-${idx}`}>
                    <TextInput id={`url-ref-host-${idx}`} value={rule.referrerHost} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { referrerHost: v })} placeholder="blank = all" />
                  </FormGroup>
                  <FormGroup label="Referrer path" fieldId={`url-ref-path-${idx}`}>
                    <TextInput id={`url-ref-path-${idx}`} value={rule.referrerPath} isDisabled={isDisabled} onChange={(_ev, v) => updateRule(idx, { referrerPath: v })} placeholder="blank = all" />
                  </FormGroup>
                </div>
              </details>
            </CardBody>
          </Card>
        ))}
        {rules.length === 0 && <p style={{ ...subtle, fontStyle: "italic" }}>No rules configured. Add a rule or a rule set above.</p>}
      </div>
    );
  };

  /* ── Custom entry ── */
  const emptyCustom = (): KConfigSettingJson => ({ file: "kdeglobals", groupPath: ["General"], key: "", value: "", immutable: true });
  const [customDraft, setCustomDraft] = useState<KConfigSettingJson>(emptyCustom);
  const [customGroupText, setCustomGroupText] = useState("General");
  const [customEditingKey, setCustomEditingKey] = useState<string | null>(null);

  const loadCustom = (s: KConfigSettingJson | null) => {
    const draft = s ? { ...s, groupPath: [...s.groupPath] } : emptyCustom();
    setCustomDraft(draft);
    setCustomGroupText(groupPathLabel(draft.groupPath));
    setCustomEditingKey(s ? settingKey(s) : null);
  };

  const renderCustomEditor = () => {
    const groupPath = parseGroupPathInput(customGroupText);
    const valid = /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(customDraft.file) && groupPath.length > 0 && customDraft.key.trim() !== "" && !/[=[\]\r\n]/.test(customDraft.key) && !/[\r\n]/.test(customDraft.value ?? "");
    const known = catalogueEntryForSetting({ ...customDraft, groupPath });
    const save = () => {
      const s: KConfigSettingJson = { file: customDraft.file.trim(), groupPath, key: customDraft.key.trim() };
      if (customDraft.delete) s.delete = true;
      else s.value = customDraft.value ?? "";
      if (customDraft.immutable) s.immutable = true;
      if (customDraft.expand) s.expand = true;
      if (customDraft.locale) s.locale = customDraft.locale;
      if (known) s.catalogueId = known.id;
      write(upsertCustomSetting(content, s, customEditingKey ?? undefined));
      setSelectedKey(customItemKey(s));
      setCustomEditingKey(settingKey(s));
    };

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{customEditingKey ? "Custom entry" : "Add a custom entry"}</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          Any KDE configuration key: the file is a name inside the managed directory (<code>kdeglobals</code>, <code>dolphinrc</code>, ...), groups nest as <code>[Containments][1]</code>.
          {known && <> This key is in the catalogue as <strong>{known.label}</strong>.</>}
        </p>
        <Form>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: "0.75rem" }}>
            <FormGroup label="File" fieldId="kc-custom-file" isRequired>
              <TextInput id="kc-custom-file" value={customDraft.file} isDisabled={isDisabled} onChange={(_ev, v) => setCustomDraft({ ...customDraft, file: v })} placeholder="kdeglobals" />
            </FormGroup>
            <FormGroup label="Group" fieldId="kc-custom-group" isRequired>
              <TextInput id="kc-custom-group" value={customGroupText} isDisabled={isDisabled} onChange={(_ev, v) => setCustomGroupText(v)} placeholder="[General] or [Containments][1]" />
            </FormGroup>
            <FormGroup label="Key" fieldId="kc-custom-key" isRequired>
              <TextInput id="kc-custom-key" value={customDraft.key} isDisabled={isDisabled} onChange={(_ev, v) => setCustomDraft({ ...customDraft, key: v })} />
            </FormGroup>
          </div>
          <FormGroup label="Value" fieldId="kc-custom-value">
            <TextInput id="kc-custom-value" value={customDraft.value ?? ""} isDisabled={isDisabled || customDraft.delete === true} onChange={(_ev, v) => setCustomDraft({ ...customDraft, value: v })} />
          </FormGroup>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr 1fr", gap: "0.75rem" }}>
            <FormGroup label="Enforced [$i]" fieldId="kc-custom-immutable">
              <Switch id="kc-custom-immutable" isChecked={customDraft.immutable === true} isDisabled={isDisabled} onChange={(_ev, c) => setCustomDraft({ ...customDraft, immutable: c })} label={customDraft.immutable ? "Yes" : "No"} />
            </FormGroup>
            <FormGroup label="Expand variables [$e]" fieldId="kc-custom-expand">
              <Switch id="kc-custom-expand" isChecked={customDraft.expand === true} isDisabled={isDisabled} onChange={(_ev, c) => setCustomDraft({ ...customDraft, expand: c })} label={customDraft.expand ? "Yes" : "No"} />
            </FormGroup>
            <FormGroup label="Delete entry [$d]" fieldId="kc-custom-delete">
              <Switch id="kc-custom-delete" isChecked={customDraft.delete === true} isDisabled={isDisabled} onChange={(_ev, c) => setCustomDraft({ ...customDraft, delete: c })} label={customDraft.delete ? "Removes a default" : "No"} />
            </FormGroup>
            <FormGroup label="Locale" fieldId="kc-custom-locale">
              <TextInput id="kc-custom-locale" value={customDraft.locale ?? ""} isDisabled={isDisabled} onChange={(_ev, v) => setCustomDraft({ ...customDraft, locale: v })} placeholder="de_DE (optional)" />
            </FormGroup>
          </div>
          {!isDisabled && (
            <FormGroup fieldId="kc-custom-actions">
              <div style={{ display: "flex", gap: "0.5rem" }}>
                <Button variant="primary" size="sm" isDisabled={!valid} onClick={save}>{customEditingKey ? "Save entry" : "Add to policy"}</Button>
                {customEditingKey && (
                  <Button variant="danger" size="sm" onClick={() => { write(removeSettingByKey(content, customEditingKey)); setSelectedKey(null); }}>Remove from policy</Button>
                )}
              </div>
              {!valid && <FieldHelp>File, group and key are required; keys cannot contain =, [ or ].</FieldHelp>}
            </FormGroup>
          )}
        </Form>
      </div>
    );
  };

  /* ── Whole-file lock ── */
  const renderFileLock = () => (
    <div style={{ padding: "0.5rem 0" }}>
      <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Lock the whole kdeglobals file</Title>
      <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
        Writes a bare <code>[$i]</code> line at the top of <code>kdeglobals</code>. KDE then ignores every user's own kdeglobals, so no global preference (colours, fonts, icons, shortcuts) can be changed. Meant for kiosk desktops only.
      </p>
      <Form>
        <FormGroup label="Whole file immutable" fieldId="kc-file-lock">
          <Switch id="kc-file-lock" isChecked={content.fileImmutableKdeglobals} isDisabled={isDisabled} onChange={(_ev, c) => write({ ...content, fileImmutableKdeglobals: c })} label={content.fileImmutableKdeglobals ? "Locked" : "Not locked"} />
        </FormGroup>
      </Form>
    </div>
  );

  /* ── Preview ── */
  const renderPreview = () => {
    const files = renderKConfigPreview(content);
    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Preview rendered files</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          What the agent writes under <code>/etc/bor/xdg</code> for this policy on its own (other bound policies are merged by priority on the node; the managed-file header is omitted).
        </p>
        {files.length === 0 && <p style={{ ...subtle, fontStyle: "italic" }}>Nothing configured yet.</p>}
        {files.map((f) => (
          <div key={f.file} style={{ marginBottom: "1rem" }}>
            <Title headingLevel="h4" size="md" style={{ marginBottom: "0.25rem" }}><code>{f.file}</code></Title>
            {f.note && <p style={{ ...subtle, fontSize: "0.85rem" }}>{f.note}</p>}
            <pre style={{ ...mono, padding: "0.75rem", border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "4px", overflowX: "auto", margin: 0 }}>{f.content}</pre>
          </div>
        ))}
      </div>
    );
  };

  /* ── Import ── */
  const [importFile, setImportFile] = useState("kdeglobals");
  const [importText, setImportText] = useState("");
  const [importWarnings, setImportWarnings] = useState<string[]>([]);
  const renderImport = () => {
    const run = () => {
      const r = importKConfigIni(content, importFile.trim(), importText);
      write(r.content);
      setImportWarnings(r.warnings);
      setNotice(`Imported ${r.imported} entr${r.imported === 1 ? "y" : "ies"} into ${importFile.trim()}.`);
      setImportText("");
    };
    const fileValid = /^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(importFile.trim());
    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Import INI</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          Paste the content of a KDE configuration file (for example a <code>kdeglobals</code> or <code>dolphinrc</code> prepared on a reference desktop). Groups, <code>[$i]</code>, <code>[$e]</code>, <code>[$d]</code> and locale suffixes are understood; URL rules and module restrictions go to their own lists.
        </p>
        <Form>
          <FormGroup label="Target file" fieldId="kc-import-file" isRequired>
            <TextInput id="kc-import-file" value={importFile} isDisabled={isDisabled} onChange={(_ev, v) => setImportFile(v)} style={{ maxWidth: "320px" }} />
          </FormGroup>
          <FormGroup label="INI content" fieldId="kc-import-text">
            <TextArea id="kc-import-text" value={importText} isDisabled={isDisabled} onChange={(_ev, v) => setImportText(v)} rows={12} style={mono} placeholder={"[KDE Action Restrictions][$i]\nshell_access=false"} />
          </FormGroup>
          {!isDisabled && (
            <FormGroup fieldId="kc-import-actions">
              <Button variant="primary" size="sm" isDisabled={!fileValid || importText.trim() === ""} onClick={run}>Import</Button>
            </FormGroup>
          )}
          {importWarnings.length > 0 && (
            <ul aria-live="polite" style={{ ...subtle, fontSize: "0.85rem", paddingLeft: "1.25rem" }}>
              {importWarnings.map((w, i) => <li key={i}>{w}</li>)}
            </ul>
          )}
        </Form>
      </div>
    );
  };

  /* ── Right pane dispatch ── */
  const renderPane = () => {
    if (!selectedKey) {
      return (
        <div style={{ padding: "2rem", textAlign: "center", ...subtle }}>
          <Title headingLevel="h3" size="lg">Select a KDE setting</Title>
          <p style={{ marginTop: "0.5rem" }}>Choose a setting from the tree on the left, search it, or add a preset.</p>
        </div>
      );
    }
    if (selectedKey === KEY_KCM) return renderKcmEditor();
    if (selectedKey === KEY_URL) return renderUrlEditor();
    if (selectedKey === KEY_FILE_LOCK) return renderFileLock();
    if (selectedKey === KEY_PREVIEW) return renderPreview();
    if (selectedKey === KEY_IMPORT) return renderImport();
    if (selectedKey === KEY_CUSTOM_NEW || selectedKey.startsWith(CUSTOM_PREFIX)) return renderCustomEditor();
    const entry = findCatalogueEntry(selectedKey);
    return entry ? renderEntryEditor(entry) : null;
  };

  const onSelect = (item: TreeItem) => {
    if (item.key === KEY_CUSTOM_NEW) loadCustom(null);
    else if (item.key.startsWith(CUSTOM_PREFIX)) {
      const s = custom.find((c) => customItemKey(c) === item.key);
      loadCustom(s ?? null);
    }
    selectItem(item);
  };

  return (
    <div>
      <div style={{ display: "flex", gap: "0.75rem", alignItems: "flex-end", marginBottom: "0.75rem", flexWrap: "wrap" }}>
        <FormGroup label="Search settings" fieldId="kc-search" style={{ flex: "1 1 260px" }}>
          <TextInput id="kc-search" type="search" value={filter} onChange={(_ev, v) => setFilter(v)} placeholder="Label, key, file or Windows policy name" />
        </FormGroup>
        {!isDisabled && (
          <FormGroup label="Add a preset" fieldId="kc-preset" style={{ flex: "0 1 320px" }}>
            <FormSelect id="kc-preset" value="" onChange={(_ev, v) => { if (v) applyPreset(v); }}>
              <FormSelectOption value="" label="Select a preset..." isDisabled />
              {KCONFIG_PRESETS.map((p) => <FormSelectOption key={p.id} value={p.id} label={p.label} />)}
            </FormSelect>
          </FormGroup>
        )}
        <div style={{ ...subtle, fontSize: "0.85rem", alignSelf: "center" }}>
          {hasAnyKConfigContent(content) ? `${content.settings.length} setting(s)` : "Nothing configured yet"}
        </div>
      </div>
      {notice && (
        <div aria-live="polite" style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.75rem" }}>{notice}</div>
      )}
      <div style={{ display: "flex", minHeight: "400px" }}>
        <PolicyTreePanel
          ariaLabel="KDE settings"
          tree={tree}
          configuredKeys={configuredKeys}
          selectedKey={selectedKey}
          expandedGroups={effectiveExpanded}
          onToggleGroup={toggleGroup}
          onSelect={onSelect}
          onRemove={removeItem}
        />
        <div style={{ flex: 1, paddingLeft: "1.5rem", overflowY: "auto" }}>{renderPane()}</div>
      </div>
    </div>
  );
};
