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
  allCatalogueEntries,
  allCategories,
  boolDisplay,
  defaultCatalogueValue,
  findCatalogueEntry,
  groupPathLabel,
  registerCatalogueEntries,
  targetLabel,
  type KConfigCatalogueEntry,
} from "./kconfigCatalogue";
import { fetchKConfigCatalogue, fetchKConfigKcms, fetchKConfigThemes, type KcmModuleInfo, type LookAndFeelInfo } from "../../../apiClient/kconfigApi";
import { fetchNodesPaged, type Node } from "../../../apiClient/nodesApi";
import {
  DEFAULT_PANEL_SNIPPET,
  DEFAULT_URL_RULE,
  EMPTY_KONSOLE_PROFILE,
  EMPTY_LAYOUT,
  FILL_MODE_LABELS,
  HISTORY_MODE_LABELS,
  KCM_MODULES,
  KCONFIG_PRESETS,
  KDE_APPLICATION_IDS,
  KIO_PROTOCOLS,
  KNOWN_GLOBAL_THEMES,
  URL_RULE_PRESETS,
  applyKConfigPreset,
  hasKonsoleProfileContent,
  hasLayoutContent,
  isDesktopFileId,
  konsoleProfileFileName,
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
  type KonsoleProfileJson,
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
const KEY_LAYOUT = "__layout";
const KEY_SCOPE = "__scope";
const KEY_KONSOLE = "__konsole";
const KEY_HIDDEN_APPS = "__hidden_apps";
const CUSTOM_PREFIX = "custom:";

const GROUP_LAYOUT = "Desktop Layout";
const GROUP_SCOPE = "Scope";
const GROUP_APPS = "Applications";

/** Splits the hidden-applications textarea into valid desktop file ids
 *  (de-duplicated, order kept) and the entries the server would reject. */
function parseDesktopIdText(text: string): { ids: string[]; invalid: string[] } {
  const seen = new Set<string>();
  const ids: string[] = [];
  const invalid: string[] = [];
  for (const raw of text.split(/[\s,]+/)) {
    const id = raw.trim();
    if (!id) continue;
    if (!isDesktopFileId(id)) {
      invalid.push(id);
      continue;
    }
    if (seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return { ids, invalid };
}

/** Splits the Unix group textarea into valid names (de-duplicated, order
 *  kept) and the entries the server would reject. Same rules as the
 *  server: no path separators, colons or tabs, at most 128 characters. */
function parseGroupText(text: string): { groups: string[]; invalid: string[] } {
  const seen = new Set<string>();
  const groups: string[] = [];
  const invalid: string[] = [];
  for (const raw of text.split(/[\n,]+/)) {
    const g = raw.trim();
    if (!g) continue;
    if (/[/:\t]/.test(g) || g.length > 128) {
      invalid.push(g);
      continue;
    }
    const key = g.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    groups.push(g);
  }
  return { groups, invalid };
}
const GROUP_RESTRICTIONS = "System Settings & URL Rules";
const GROUP_CUSTOM = "Custom Entries";
const GROUP_ADVANCED = "Advanced";

/** Catalogue entry rendered as a theme picker instead of a text field. */
const GLOBAL_THEME_ENTRY = "kde.LookAndFeelPackage";
const THEME_CUSTOM = "__custom";

/** Built-in themes plus reported ones, by name; the current value stays selectable even when no node reports it. */
function themeOptions(reported: LookAndFeelInfo[], current: string): LookAndFeelInfo[] {
  const byId = new Map<string, LookAndFeelInfo>();
  for (const t of [...KNOWN_GLOBAL_THEMES, ...reported]) if (t.id && !byId.has(t.id)) byId.set(t.id, t);
  if (current && !byId.has(current)) byId.set(current, { id: current, name: current });
  return Array.from(byId.values()).sort((a, b) => a.name.localeCompare(b.name));
}

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
  policyName,
}) => {
  // Load once; older typed content is upgraded to generic settings and,
  // for editable policies, written back so the stored shape is current.
  const [initial] = useState(() => parseKConfigContent(contentRaw));
  const content = useMemo(() => parseKConfigContent(contentRaw).content, [contentRaw]);
  // Bumped when node-reported entries are registered, so derived lists recompute.
  const [catalogueVersion, setCatalogueVersion] = useState(0);

  useEffect(() => {
    if (initial.changed && !isDisabled) onChange(serializeKConfigContent(initial.content));
    // Only on mount: the upgrade must run exactly once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const configuredEntryIds = useMemo(() => {
    const ids: string[] = [];
    for (const e of allCatalogueEntries()) if (findEntrySetting(content, e)) ids.push(e.id);
    return ids;
    // catalogueVersion: registered node entries change the result.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [content, catalogueVersion]);
  const custom = useMemo(() => customSettings(content), [content]);

  const configuredKeys = useMemo(() => {
    const keys = [...configuredEntryIds, ...custom.map(customItemKey)];
    if (content.kcmRestrictions.length > 0) keys.push(KEY_KCM);
    if (content.urlRestrictions.length > 0) keys.push(KEY_URL);
    if (content.fileImmutableKdeglobals) keys.push(KEY_FILE_LOCK);
    if (hasLayoutContent(content.layout)) keys.push(KEY_LAYOUT);
    if (content.unixGroups.length > 0) keys.push(KEY_SCOPE);
    if (hasKonsoleProfileContent(content.konsoleProfile)) keys.push(KEY_KONSOLE);
    if (content.hiddenApplications.length > 0) keys.push(KEY_HIDDEN_APPS);
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
    if (hasLayoutContent(content.layout)) groups.add(GROUP_LAYOUT);
    if (content.unixGroups.length > 0) groups.add(GROUP_SCOPE);
    if (hasKonsoleProfileContent(content.konsoleProfile) || content.hiddenApplications.length > 0) groups.add(GROUP_APPS);
    return groups;
  });
  const [filter, setFilter] = useState("");

  // Node-reported catalogue: the KConfigXT schema entries and the System
  // Settings modules a chosen node reported.
  const [nodes, setNodes] = useState<Node[]>([]);
  const [nodeId, setNodeId] = useState("");
  const [nodeKcms, setNodeKcms] = useState<KcmModuleInfo[] | null>(null);
  const [nodeSummary, setNodeSummary] = useState("");
  // Global themes for the Appearance > Global theme picker: the ones every
  // Plasma ships plus what nodes reported (all nodes, or the chosen node).
  const [themes, setThemes] = useState<LookAndFeelInfo[]>([]);
  const [themeCustom, setThemeCustom] = useState(false);
  useEffect(() => {
    let cancelled = false;
    fetchKConfigThemes()
      .then((t) => { if (!cancelled) setThemes(t); })
      .catch(() => { /* the built-in list is still offered */ });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetchNodesPaged({ per_page: 200, sort_field: "name", sort_order: "asc" })
      .then((res) => {
        if (cancelled) return;
        setNodes(res.items.filter((n) => !n.desktop_env || /kde|plasma/i.test(n.desktop_env)));
      })
      .catch(() => { /* the picker stays hidden */ });
    return () => { cancelled = true; };
  }, []);

  // Settings tagged with ids the built-in catalogue does not know (added
  // from a node's schemas): load the server's catalogue once so their
  // labels resolve.
  useEffect(() => {
    if (!initial.content.settings.some((s) => s.catalogueId && !findCatalogueEntry(s.catalogueId))) return;
    let cancelled = false;
    fetchKConfigCatalogue()
      .then((entries) => {
        if (!cancelled && registerCatalogueEntries(entries) > 0) setCatalogueVersion((v) => v + 1);
      })
      .catch(() => { /* labels fall back to file, group and key */ });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selectNode = (id: string) => {
    setNodeId(id);
    if (!id) {
      setNodeKcms(null);
      setNodeSummary("");
      fetchKConfigThemes().then(setThemes).catch(() => { /* keep the current list */ });
      return;
    }
    const node = nodes.find((n) => n.id === id);
    const name = node?.name ?? "the node";
    Promise.all([fetchKConfigCatalogue(id), fetchKConfigKcms(id), fetchKConfigThemes(id)])
      .then(([entries, kcms, nodeThemes]) => {
        if (registerCatalogueEntries(entries) > 0) setCatalogueVersion((v) => v + 1);
        setNodeKcms(kcms.length > 0 ? kcms : null);
        if (nodeThemes.length > 0) setThemes(nodeThemes);
        setNodeSummary(`${name}${node?.desktop_env ? ` (${node.desktop_env})` : ""}: ${entries.length} settings from its KDE schemas, ${kcms.length} System Settings modules, ${nodeThemes.length} global themes.`);
      })
      .catch((err: unknown) => setNodeSummary(`Could not load the catalogue of ${name}: ${err instanceof Error ? err.message : String(err)}`));
  };

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
    const entries = allCatalogueEntries();
    for (const category of allCategories()) {
      const items = entries.filter((e) => e.category === category && (!f || matchesFilter(e, f))).map((e) => ({ key: e.id, label: e.label }));
      if (items.length > 0) t.set(category, items);
    }
    const layoutItems: TreeItem[] = [{ key: KEY_LAYOUT, label: "Wallpaper and panel layout" }].filter(
      (i) => !f || i.label.toLowerCase().includes(f.toLowerCase()) || "look and feel theme branding".includes(f.toLowerCase()),
    );
    if (layoutItems.length > 0) t.set(GROUP_LAYOUT, layoutItems);
    const appItems: TreeItem[] = [
      { key: KEY_KONSOLE, label: "Konsole profile" },
      { key: KEY_HIDDEN_APPS, label: "Hidden applications" },
    ].filter((i) => !f || i.label.toLowerCase().includes(f.toLowerCase()) || "terminal menu launcher desktop entry nodisplay".includes(f.toLowerCase()));
    if (appItems.length > 0) t.set(GROUP_APPS, appItems);
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
    const scopeItems: TreeItem[] = [{ key: KEY_SCOPE, label: "Apply only to Unix groups" }].filter(
      (i) => !f || i.label.toLowerCase().includes(f.toLowerCase()) || "scope group user".includes(f.toLowerCase()),
    );
    if (scopeItems.length > 0) t.set(GROUP_SCOPE, scopeItems);
    const advanced: TreeItem[] = [
      { key: KEY_FILE_LOCK, label: "Lock the whole kdeglobals file" },
      { key: KEY_IMPORT, label: "Import INI" },
      { key: KEY_PREVIEW, label: "Preview rendered files" },
    ].filter((i) => !f || i.label.toLowerCase().includes(f.toLowerCase()));
    if (advanced.length > 0) t.set(GROUP_ADVANCED, advanced);
    return t;
    // catalogueVersion: registered node entries add groups.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter, custom, catalogueVersion]);

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
    else if (key === KEY_LAYOUT) write({ ...content, layout: EMPTY_LAYOUT });
    else if (key === KEY_SCOPE) write({ ...content, unixGroups: [] });
    else if (key === KEY_KONSOLE) write({ ...content, konsoleProfile: EMPTY_KONSOLE_PROFILE });
    else if (key === KEY_HIDDEN_APPS) write({ ...content, hiddenApplications: [] });
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
      if (preset.hiddenApplications || preset.konsoleProfile) n.add(GROUP_APPS);
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
    const isThemePicker = entry.id === GLOBAL_THEME_ENTRY;
    const textual = !isThemePicker && (entry.type === "String" || entry.type === "Path" || entry.type === "Url" || entry.type === "Font");
    const boolChecked = entry.inverted ? value === "false" : value === "true";
    const themeList = isThemePicker ? themeOptions(themes, value) : [];

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>{entry.label}</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>
          {entry.category} &nbsp;·&nbsp; <code>{targetLabel(entry.file, entry.groupPath, entry.key)}</code>
          {entry.plasmaMajor && entry.plasmaMajor.length > 0 && (
            <Label isCompact color="blue" style={{ marginLeft: "0.5rem" }}>Plasma {entry.plasmaMajor.join(", ")}</Label>
          )}
          {entry.source === "kcfg" && (
            <Label isCompact color="green" style={{ marginLeft: "0.5rem" }}>reported by a node</Label>
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
          {isThemePicker && (
            <FormGroup label="Global theme" fieldId="kc-value-theme">
              {themeCustom ? (
                <TextInput id="kc-value-theme" value={value} onChange={(_ev, v) => updateValue(v)} placeholder="org.example.corporate.desktop" aria-label="Global theme package id" />
              ) : (
                <FormSelect
                  id="kc-value-theme"
                  value={value}
                  onChange={(_ev, v) => {
                    if (v === THEME_CUSTOM) setThemeCustom(true);
                    else updateValue(v);
                  }}
                >
                  {value === "" && <FormSelectOption value="" label="Select a theme..." isDisabled />}
                  {themeList.map((t) => <FormSelectOption key={t.id} value={t.id} label={t.name === t.id ? t.id : `${t.name} (${t.id})`} />)}
                  <FormSelectOption value={THEME_CUSTOM} label="Custom package id..." />
                </FormSelect>
              )}
              <FieldHelp>
                {themeCustom
                  ? "Package id of a theme installed on the nodes (the directory name under /usr/share/plasma/look-and-feel)."
                  : `Themes every Plasma installation ships plus the ones ${nodeId ? "the selected node" : "your nodes"} reported.`}
                {themeCustom && !isDisabled && (
                  <>
                    {" "}
                    <Button variant="link" isInline onClick={() => setThemeCustom(false)}>Back to the list</Button>
                  </>
                )}
                {hasLayoutContent(content.layout) && (
                  <> This policy also generates a look under Desktop Layout. An explicit theme here wins over it; to enforce the generated look instead, remove this setting and switch on &quot;Enforce as the global theme&quot; there.</>
                )}
              </FieldHelp>
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
    // With a node selected, its reported modules replace the built-in list.
    const moduleList: { id: string; label: string }[] = nodeKcms ? nodeKcms.map((m) => ({ id: m.id, label: m.name || m.id })) : KCM_MODULES;
    const available = moduleList.filter((m) => !modules.includes(m.id));
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
            {nodeKcms && <FieldHelp>Modules installed on the selected node</FieldHelp>}
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
              const mod = moduleList.find((m) => m.id === id);
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

  /* ── Desktop layout (look-and-feel package) ── */
  const renderLayoutEditor = () => {
    const layout = content.layout;
    const update = (partial: Partial<typeof layout>) => write({ ...content, layout: { ...layout, ...partial } });
    const configured = hasLayoutContent(layout);
    const imageOk = layout.wallpaperImage.trim() === "" || layout.wallpaperImage.trim().startsWith("/") || layout.wallpaperImage.trim().startsWith("file:///");

    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Wallpaper and panel layout</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>
          The agent generates a look-and-feel package named &quot;{policyName?.trim() || "this policy"} (managed)&quot; under the data directory and selects it as the global theme (<code>kdeglobals · [KDE] · LookAndFeelPackage</code>). Users who log in for the first time get the wallpaper and, if set, the panel layout. The rest of the theme (colours, icons, fonts) comes from the Appearance settings and falls back to Breeze. Do not combine this with an explicit Appearance &gt; Global theme in the same policy: the explicit theme would win.
        </p>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>Windows Group Policy equivalent: Desktop Wallpaper; Start Layout.</p>
        <Form>
          <FormGroup label="Wallpaper image" fieldId="kc-layout-image">
            <TextInput
              id="kc-layout-image"
              value={layout.wallpaperImage}
              isDisabled={isDisabled}
              validated={imageOk ? "default" : "error"}
              aria-invalid={!imageOk || undefined}
              onChange={(_ev, v) => update({ wallpaperImage: v })}
              placeholder="/usr/share/backgrounds/corporate.png"
            />
            <FieldHelp>Absolute path readable by every user on the nodes (deploy the file with a package or the Files policy). Empty keeps the theme's wallpaper.</FieldHelp>
          </FormGroup>
          <FormGroup label="Wallpaper fill mode" fieldId="kc-layout-fill">
            <FormSelect id="kc-layout-fill" value={layout.wallpaperFillMode || "2"} isDisabled={isDisabled} onChange={(_ev, v) => update({ wallpaperFillMode: v })}>
              {Object.entries(FILL_MODE_LABELS).map(([v, label]) => <FormSelectOption key={v} value={v} label={label} />)}
            </FormSelect>
          </FormGroup>
          <FormGroup label="Panel layout script (optional)" fieldId="kc-layout-script">
            <TextArea
              id="kc-layout-script"
              value={layout.layoutScript}
              isDisabled={isDisabled}
              onChange={(_ev, v) => update({ layoutScript: v })}
              rows={10}
              style={mono}
              placeholder={DEFAULT_PANEL_SNIPPET}
            />
            <FieldHelp>
              Plasma desktop scripting (JavaScript) that creates panels and widgets: new Panel, addWidget, loadTemplate, desktops(). Runs for users at their first login.
              {!isDisabled && (
                <>
                  {" "}
                  <Button variant="link" isInline onClick={() => update({ layoutScript: DEFAULT_PANEL_SNIPPET })}>Insert Plasma's default panel</Button>
                </>
              )}
            </FieldHelp>
          </FormGroup>
          <FormGroup label="Apply once to existing users" fieldId="kc-layout-once">
            <Switch
              id="kc-layout-once"
              isChecked={layout.applyOnce}
              isDisabled={isDisabled}
              onChange={(_ev, c) => update({ applyOnce: c })}
              label={layout.applyOnce ? "At their next login: wallpaper set on every desktop; with a layout script, their panels are replaced" : "Only users logging in for the first time"}
            />
          </FormGroup>
          <FormGroup label="Apply the wallpaper to logged-in sessions now" fieldId="kc-layout-live">
            <Switch id="kc-layout-live" isChecked={layout.applyLive} isDisabled={isDisabled} onChange={(_ev, c) => update({ applyLive: c })} label={layout.applyLive ? "Yes, through plasma-apply-wallpaperimage" : "No"} />
          </FormGroup>
          <FormGroup label="Enforce as the global theme" fieldId="kc-layout-lock">
            <Switch id="kc-layout-lock" isChecked={layout.lockPackage} isDisabled={isDisabled} onChange={(_ev, c) => update({ lockPackage: c })} label={layout.lockPackage ? "Enforced [$i]: users cannot pick another global theme" : "Default only: users may pick another global theme (settings enforced individually still apply)"} />
            <FieldHelp>No package id to type: the generated look is selected by the policy itself and appears in System Settings as &quot;{policyName?.trim() || "this policy"} (managed)&quot;.</FieldHelp>
          </FormGroup>
          {!isDisabled && configured && (
            <FormGroup fieldId="kc-layout-actions">
              <Button variant="danger" size="sm" onClick={() => write({ ...content, layout: EMPTY_LAYOUT })}>Remove from policy</Button>
            </FormGroup>
          )}
          {!configured && <p aria-live="polite" style={{ ...subtle, fontSize: "0.85rem" }}>Enter a wallpaper image or a layout script to add the desktop layout to the policy.</p>}
        </Form>
      </div>
    );
  };

  /* ── Scope: Unix groups ── */
  // The textarea keeps its own text so that line breaks survive typing;
  // the content only holds the valid, de-duplicated names. When the scope
  // changes elsewhere (removed from the tree, policy reloaded) the text
  // follows.
  const [scopeText, setScopeText] = useState(() => content.unixGroups.join("\n"));
  useEffect(() => {
    if (parseGroupText(scopeText).groups.join("\n") !== content.unixGroups.join("\n")) {
      setScopeText(content.unixGroups.join("\n"));
    }
  }, [content.unixGroups, scopeText]);
  const renderScopeEditor = () => {
    const { invalid } = parseGroupText(scopeText);
    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Apply only to Unix groups</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>
          Limits every setting of this policy to members of the listed groups. The agent writes the policy's files into <code>groups/&lt;group&gt;/</code> inside the managed directory and the login hook adds that directory for members only, matched by numeric group id. Group names are resolved on each node (local, LDAP, SSSD or winbind), so use the names <code>id -Gn</code> prints there, for example <code>students</code>, <code>Domain Users</code> or <code>teachers@ad.example.com</code>.
        </p>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          Windows Group Policy equivalent: User Configuration with security filtering. KDE applies group directories on top of the node-wide ones, so a group policy can override node-wide defaults but cannot relax a node-wide enforced setting; keep group differences in group-scoped policies. The desktop layout cannot be scoped.
        </p>
        <Form>
          <FormGroup label="Unix groups (one per line)" fieldId="kc-scope-groups">
            <TextArea
              id="kc-scope-groups"
              value={scopeText}
              isDisabled={isDisabled}
              rows={5}
              validated={invalid.length > 0 ? "error" : "default"}
              aria-invalid={invalid.length > 0 || undefined}
              onChange={(_ev, v) => {
                setScopeText(v);
                write({ ...content, unixGroups: parseGroupText(v).groups });
              }}
              placeholder={"students\nDomain Users"}
            />
            <FieldHelp>{invalid.length > 0 ? `Not allowed: ${invalid.join(", ")} (no /, : or tabs; at most 128 characters)` : "Empty list: the policy applies to every user on the node."}</FieldHelp>
          </FormGroup>
          {!isDisabled && content.unixGroups.length > 0 && (
            <FormGroup fieldId="kc-scope-actions">
              <Button variant="danger" size="sm" onClick={() => { setScopeText(""); write({ ...content, unixGroups: [] }); }}>Apply to every user</Button>
            </FormGroup>
          )}
        </Form>
      </div>
    );
  };

  /* ── Konsole profile ── */
  const renderKonsoleEditor = () => {
    const k = content.konsoleProfile;
    const update = (partial: Partial<KonsoleProfileJson>) => write({ ...content, konsoleProfile: { ...k, ...partial } });
    const configured = hasKonsoleProfileContent(k);
    const nameOk = k.name.trim() === "" || /^[A-Za-z0-9][A-Za-z0-9 ._-]*$/.test(k.name.trim());
    const isDefault = k.setDefault || k.lock;
    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Konsole profile</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>
          The agent writes <code>konsole/{konsoleProfileFileName(k)}</code> into the data directory, where Konsole finds it next to the distribution's profiles. <strong>Use as default profile</strong> makes it the profile of new windows and tabs (<code>konsolerc · [Desktop Entry] · DefaultProfile</code>); <strong>Lock</strong> enforces that and hides Konsole's profile management, so users cannot create, edit or switch profiles. Everything the profile does not set is inherited from Konsole's built-in defaults.
        </p>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>Windows equivalent: Windows Terminal defaults deployed through its settings file; a locked profile has no direct counterpart.</p>
        <Form>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.75rem" }}>
            <FormGroup label="Profile name" fieldId="kc-konsole-name">
              <TextInput id="kc-konsole-name" value={k.name} isDisabled={isDisabled} validated={nameOk ? "default" : "error"} aria-invalid={!nameOk || undefined} onChange={(_ev, v) => update({ name: v })} placeholder="Managed" />
              <FieldHelp>Letters, digits, spaces, dots, underscores and dashes. File name: {konsoleProfileFileName(k)}</FieldHelp>
            </FormGroup>
            <FormGroup label="Command" fieldId="kc-konsole-command">
              <TextInput id="kc-konsole-command" value={k.command} isDisabled={isDisabled} onChange={(_ev, v) => update({ command: v })} placeholder="/bin/bash" />
              <FieldHelp>Program the terminal starts; empty keeps the user's login shell.</FieldHelp>
            </FormGroup>
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.75rem" }}>
            <FormGroup label="Scrollback" fieldId="kc-konsole-history">
              <FormSelect id="kc-konsole-history" value={k.historyMode} isDisabled={isDisabled} onChange={(_ev, v) => update({ historyMode: v })}>
                {Object.entries(HISTORY_MODE_LABELS).map(([v, label]) => <FormSelectOption key={v} value={v} label={label} />)}
              </FormSelect>
            </FormGroup>
            {k.historyMode === "1" && (
              <FormGroup label="Scrollback lines" fieldId="kc-konsole-history-size">
                <TextInput id="kc-konsole-history-size" type="number" min={1} max={1000000} value={k.historySize > 0 ? String(k.historySize) : ""} isDisabled={isDisabled} onChange={(_ev, v) => update({ historySize: Number(v) || 0 })} placeholder="1000" />
              </FormGroup>
            )}
          </div>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "0.75rem" }}>
            <FormGroup label="Colour scheme" fieldId="kc-konsole-scheme">
              <TextInput id="kc-konsole-scheme" value={k.colorScheme} isDisabled={isDisabled} onChange={(_ev, v) => update({ colorScheme: v })} placeholder="Breeze" />
              <FieldHelp>A .colorscheme Konsole ships or one deployed to the nodes: Breeze, BlackOnWhite, WhiteOnBlack, Solarized, ...</FieldHelp>
            </FormGroup>
            <FormGroup label="Font" fieldId="kc-konsole-font">
              <TextInput id="kc-konsole-font" value={k.font} isDisabled={isDisabled} onChange={(_ev, v) => update({ font: v })} placeholder="Hack,10,-1,5,50,0,0,0,0,0" />
              <FieldHelp>Qt font string (family, point size, ...) as Konsole writes it.</FieldHelp>
            </FormGroup>
          </div>
          <FormGroup label="Additional profile entries (INI)" fieldId="kc-konsole-extra">
            <TextArea
              id="kc-konsole-extra"
              value={k.extraIni}
              isDisabled={isDisabled}
              rows={6}
              style={mono}
              onChange={(_ev, v) => update({ extraIni: v })}
              placeholder={"[Terminal Features]\nBlinkingCursorEnabled=false\n\n[Interaction Options]\nOpenLinksByDirectClickEnabled=false"}
            />
            <FieldHelp>Any other key of a Konsole profile: [General], [Appearance], [Scrolling], [Terminal Features], [Cursor Options], [Interaction Options], [Encoding Options], [Keyboard].</FieldHelp>
          </FormGroup>
          <FormGroup label="Use as default profile" fieldId="kc-konsole-default">
            <Switch id="kc-konsole-default" isChecked={isDefault} isDisabled={isDisabled || k.lock} onChange={(_ev, c) => update({ setDefault: c })} label={isDefault ? "New windows and tabs use this profile" : "Listed in Konsole's profiles only"} />
          </FormGroup>
          <FormGroup label="Lock" fieldId="kc-konsole-lock">
            <Switch id="kc-konsole-lock" isChecked={k.lock} isDisabled={isDisabled} onChange={(_ev, c) => update({ lock: c, setDefault: c ? true : k.setDefault })} label={k.lock ? "Enforced default; managing, editing and switching profiles is hidden" : "Users may switch to or create other profiles"} />
          </FormGroup>
          {!isDisabled && configured && (
            <FormGroup fieldId="kc-konsole-actions">
              <Button variant="danger" size="sm" onClick={() => write({ ...content, konsoleProfile: EMPTY_KONSOLE_PROFILE })}>Remove from policy</Button>
            </FormGroup>
          )}
          {!configured && <p aria-live="polite" style={{ ...subtle, fontSize: "0.85rem" }}>Set a command, scrollback mode, colour scheme, font or additional entries, or switch on the default or the lock, to add the profile to the policy.</p>}
        </Form>
      </div>
    );
  };

  /* ── Hidden applications ── */
  const [hiddenText, setHiddenText] = useState(() => content.hiddenApplications.join("\n"));
  useEffect(() => {
    if (parseDesktopIdText(hiddenText).ids.join("\n") !== content.hiddenApplications.join("\n")) {
      setHiddenText(content.hiddenApplications.join("\n"));
    }
  }, [content.hiddenApplications, hiddenText]);
  const renderHiddenAppsEditor = () => {
    const { invalid } = parseDesktopIdText(hiddenText);
    const addId = (id: string) => {
      if (!id || content.hiddenApplications.includes(id)) return;
      const next = [...content.hiddenApplications, id];
      setHiddenText(next.join("\n"));
      write({ ...content, hiddenApplications: next });
    };
    return (
      <div style={{ padding: "0.5rem 0" }}>
        <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.25rem" }}>Hidden applications</Title>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.25rem" }}>
          Removes the listed applications from the application menu, KRunner and other launchers. The agent copies each installed desktop entry into <code>applications/</code> in the data directory with <code>NoDisplay=true</code>; that directory comes first in <code>XDG_DATA_DIRS</code>, so the copy shadows the original. File associations keep working and the program can still be started from a terminal or a file manager: hiding is a menu clean-up, not a block. Use Kiosk restrictions (Session &amp; Shell), System Settings module restrictions or package removal to prevent use.
        </p>
        <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "1rem" }}>
          Windows Group Policy equivalent: Remove programs from the Start menu (hiding only). Ids are the desktop file names under <code>/usr/share/applications</code> on the nodes, for example <code>org.kde.konsole.desktop</code> or <code>systemsettings.desktop</code>. Takes effect at the next login; the agent refreshes the application cache of running sessions.
        </p>
        <Form>
          {!isDisabled && (
            <FormGroup label="Add a common application" fieldId="kc-hidden-add">
              <FormSelect id="kc-hidden-add" value="" onChange={(_ev, v) => addId(v)}>
                <FormSelectOption value="" label="Select an application..." isDisabled />
                {KDE_APPLICATION_IDS.filter((a) => !content.hiddenApplications.includes(a.id)).map((a) => <FormSelectOption key={a.id} value={a.id} label={`${a.label} (${a.id})`} />)}
              </FormSelect>
            </FormGroup>
          )}
          <FormGroup label="Desktop file ids (one per line)" fieldId="kc-hidden-ids">
            <TextArea
              id="kc-hidden-ids"
              value={hiddenText}
              isDisabled={isDisabled}
              rows={8}
              style={mono}
              validated={invalid.length > 0 ? "error" : "default"}
              aria-invalid={invalid.length > 0 || undefined}
              onChange={(_ev, v) => {
                setHiddenText(v);
                write({ ...content, hiddenApplications: parseDesktopIdText(v).ids });
              }}
              placeholder={"org.kde.konsole.desktop\nsystemsettings.desktop"}
            />
            <FieldHelp>{invalid.length > 0 ? `Not a desktop file id: ${invalid.join(", ")}` : `${content.hiddenApplications.length} application(s) hidden.`}</FieldHelp>
          </FormGroup>
          {!isDisabled && content.hiddenApplications.length > 0 && (
            <FormGroup fieldId="kc-hidden-actions">
              <Button variant="danger" size="sm" onClick={() => { setHiddenText(""); write({ ...content, hiddenApplications: [] }); }}>Remove from policy</Button>
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
          {content.unixGroups.length > 0 && (
            <> This policy is limited to {content.unixGroups.join(", ")}, so its files go to <code>groups/&lt;group&gt;/</code> under the config and data directories.</>
          )}
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
    if (selectedKey === KEY_LAYOUT) return renderLayoutEditor();
    if (selectedKey === KEY_SCOPE) return renderScopeEditor();
    if (selectedKey === KEY_KONSOLE) return renderKonsoleEditor();
    if (selectedKey === KEY_HIDDEN_APPS) return renderHiddenAppsEditor();
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
        {nodes.length > 0 && (
          <FormGroup label="Settings reported by node" fieldId="kc-node" style={{ flex: "0 1 300px" }}>
            <FormSelect id="kc-node" value={nodeId} onChange={(_ev, v) => selectNode(v)}>
              <FormSelectOption value="" label="Built-in catalogue only" />
              {nodes.map((n) => <FormSelectOption key={n.id} value={n.id} label={n.name} />)}
            </FormSelect>
          </FormGroup>
        )}
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
      {(notice || nodeSummary) && (
        <div aria-live="polite" style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.75rem" }}>
          {notice}{notice && nodeSummary ? " " : ""}{nodeSummary}
        </div>
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
