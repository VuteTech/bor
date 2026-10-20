// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * SelinuxPolicyEditor - a `Selinux` policy: the enforcement mode, booleans
 * (picked from what nodes report plus the curated list), per-domain
 * permissive exceptions, existing modules to enable or disable, custom
 * CIL modules, file contexts and login mappings. Content is protojson
 * (camelCase, enum names); see selinuxModel.ts.
 */

import React, { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import {
  Button,
  Checkbox,
  Dropdown,
  DropdownItem,
  DropdownList,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Label,
  MenuToggle,
  MenuToggleElement,
  Radio,
  Switch,
  Tab,
  TabTitleText,
  Tabs,
  TextArea,
  TextInput,
  Title,
  Tooltip,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { hasPermission } from "../../apiClient/permissions";
import { ConfirmModal } from "../../components/ConfirmModal";
import { ListEditor } from "./ListEditor";
import { MacCatalogueSearch, MacNodePicker, subtle } from "./macPickers";
import { fetchNodeMac, type MacCatalogueItem, type MacDenialGroup } from "../../apiClient/macApi";
import {
  RISKY_SELINUX_MODULES,
  RISKY_SELINUX_PERMISSIVE,
  SELINUX_BOOLEAN_TEMPLATES,
  SELINUX_CIL_TEMPLATES,
  SELINUX_FILE_KIND_OPTIONS,
  SELINUX_MODE_OPTIONS,
  SelinuxContent,
  SelinuxMode,
  booleanNameProblem,
  cilProblem,
  customNameProblem,
  domainProblem,
  fileContextProblem,
  isRiskySelinuxBoolean,
  loginProblem,
  moduleNameProblem,
  parseSelinuxContent,
  serializeSelinuxContent,
  validateSelinuxContent,
} from "./selinuxModel";

export interface SelinuxPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
  onValidityChange?: (ok: boolean) => void;
}

const SEUSER_SUGGESTIONS = ["user_u", "staff_u", "sysadm_u", "guest_u", "xguest_u", "unconfined_u"];

const mono: React.CSSProperties = { fontFamily: "monospace", fontSize: "0.85rem" };

export const SelinuxPolicyEditor: React.FC<SelinuxPolicyEditorProps> = ({ contentRaw, onChange, isDisabled, onValidityChange }) => {
  const idp = useId();
  const [model, setModel] = useState<SelinuxContent>(() => parseSelinuxContent(contentRaw));
  const lastRaw = useRef(contentRaw);
  const [tab, setTab] = useState<string | number>("mode");
  const [nodeId, setNodeId] = useState("");
  const [pendingPermissive, setPendingPermissive] = useState(false);
  const [denials, setDenials] = useState<MacDenialGroup[]>([]);
  const [templateOpen, setTemplateOpen] = useState(false);
  const [cilTemplateOpen, setCilTemplateOpen] = useState<number | null>(null);

  // Adopt external changes (reset, reload) without echoing our own.
  useEffect(() => {
    if (contentRaw !== lastRaw.current) {
      lastRaw.current = contentRaw;
      setModel(parseSelinuxContent(contentRaw));
    }
  }, [contentRaw]);

  const update = useCallback((fn: (m: SelinuxContent) => SelinuxContent) => {
    setModel((prev) => {
      const next = fn(prev);
      const raw = serializeSelinuxContent(next);
      lastRaw.current = raw;
      onChange(raw);
      return next;
    });
  }, [onChange]);

  const problem = useMemo(() => validateSelinuxContent(model), [model]);
  // Custom policy text needs mac:author on the server; say so before the save fails.
  const needsAuthor = model.customModules.length > 0 || model.fileContexts.length > 0 || model.logins.length > 0 || model.permissiveDomains.length > 0;
  const authorBlocked = needsAuthor && !hasPermission("mac:author");
  useEffect(() => { onValidityChange?.(problem === null && !authorBlocked); }, [problem, authorBlocked, onValidityChange]);

  // Denials of the picked node feed the exceptions picker.
  useEffect(() => {
    if (!nodeId) { setDenials([]); return; }
    let cancelled = false;
    fetchNodeMac(nodeId).then((st) => { if (!cancelled) setDenials(st.denials.filter((d) => d.subject.endsWith("_t"))); }).catch(() => { if (!cancelled) setDenials([]); });
    return () => { cancelled = true; };
  }, [nodeId]);

  const booleanNames = useMemo(() => new Set(model.booleans.map((b) => b.name)), [model.booleans]);
  const moduleNames = useMemo(() => new Set(model.modules.map((m) => m.name)), [model.modules]);

  const setMode = (mode: SelinuxMode) => {
    if (mode === "SELINUX_MODE_PERMISSIVE" && model.mode !== mode) { setPendingPermissive(true); return; }
    update((m) => ({ ...m, mode, prepareDisabledNodes: mode === "SELINUX_MODE_UNSPECIFIED" ? false : m.prepareDisabledNodes }));
  };

  const addBoolean = (item: MacCatalogueItem) => {
    const value = item.restrictive ?? !(item.info.current ?? false);
    update((m) => booleanNames.has(item.name) ? m : { ...m, booleans: [...m.booleans, { name: item.name, value, description: item.description ? "" : "" }] });
  };

  const addTemplate = (id: string) => {
    const t = SELINUX_BOOLEAN_TEMPLATES.find((x) => x.id === id);
    if (!t) return;
    update((m) => {
      const have = new Set(m.booleans.map((b) => b.name));
      return { ...m, booleans: [...m.booleans, ...t.booleans.filter((b) => !have.has(b.name))] };
    });
  };

  const describeBoolean = (it: MacCatalogueItem): string => {
    const parts: string[] = [];
    if (it.description) parts.push(it.description);
    if (nodeId && typeof it.info.current === "boolean") parts.push(`on this node: ${it.info.current ? "on" : "off"}`);
    return parts.join(" · ") || "No description";
  };

  const describeModule = (it: MacCatalogueItem): string => {
    const parts: string[] = [];
    if (typeof it.info.priority === "number") parts.push(`priority ${it.info.priority}`);
    if (typeof it.info.enabled === "boolean") parts.push(it.info.enabled ? "enabled" : "disabled");
    if (it.risky) parts.push("disabling it can lock users out");
    return parts.join(" · ");
  };

  return (
    <div>
      <LiveAlert id={`${idp}-problem`} variant="warning" isInline message={problem} style={{ marginBottom: "0.75rem" }} />
      <LiveAlert id={`${idp}-author`} variant="danger" isInline message={authorBlocked ? "This policy carries custom policy text (custom modules, exceptions, file contexts or login mappings). Saving or releasing it needs the mac:author permission, which your account does not hold." : null} style={{ marginBottom: "0.75rem" }} />
      <div style={{ marginBottom: "0.75rem" }}>
        <MacNodePicker lsm="selinux" value={nodeId} onChange={setNodeId} />
      </div>
      <Tabs activeKey={tab} onSelect={(_ev, k) => setTab(k)} aria-label="SELinux policy sections" isBox>
        <Tab eventKey="mode" title={<TabTitleText>Mode</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <FormGroup label="Enforcement mode" isStack fieldId={`${idp}-mode`} role="radiogroup">
              {SELINUX_MODE_OPTIONS.map((o) => (
                <Radio
                  key={o.value}
                  id={`${idp}-mode-${o.value}`}
                  name={`${idp}-mode`}
                  label={o.label}
                  description={o.help}
                  isChecked={model.mode === o.value}
                  onChange={() => setMode(o.value)}
                  isDisabled={isDisabled}
                />
              ))}
            </FormGroup>
            <Checkbox
              id={`${idp}-require`}
              label="Treat nodes without SELinux as non-compliant"
              description="Otherwise a node that does not run SELinux (Debian, Ubuntu, a node booted with selinux=0) reports the policy as inapplicable."
              isChecked={model.requireActive}
              onChange={(_ev, v) => update((m) => ({ ...m, requireActive: v }))}
              isDisabled={isDisabled}
              style={{ marginTop: "1rem" }}
            />
            <Checkbox
              id={`${idp}-prepare`}
              label="Prepare nodes where SELinux is disabled"
              description="On such a node the agent sets /etc/selinux/config to permissive and requests a relabel at the next boot. Nothing reboots; after the administrator reboots, the mode above applies. Needs a mode."
              isChecked={model.prepareDisabledNodes}
              onChange={(_ev, v) => update((m) => ({ ...m, prepareDisabledNodes: v }))}
              isDisabled={isDisabled || model.mode === "SELINUX_MODE_UNSPECIFIED"}
              style={{ marginTop: "0.5rem" }}
            />
          </div>
        </Tab>

        <Tab eventKey="booleans" title={<TabTitleText>Booleans{model.booleans.length ? ` (${model.booleans.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <div style={{ display: "flex", gap: "0.5rem", alignItems: "flex-start", flexWrap: "wrap" }}>
              <div style={{ flex: "1 1 24rem" }}>
                <MacCatalogueSearch lsm="selinux" kind="boolean" nodeId={nodeId} taken={booleanNames} onPick={addBoolean} describe={describeBoolean} placeholder="Search booleans" isDisabled={isDisabled} />
              </div>
              <Dropdown
                isOpen={templateOpen}
                onOpenChange={setTemplateOpen}
                onSelect={(_ev, v) => { setTemplateOpen(false); addTemplate(String(v)); }}
                toggle={(ref: React.Ref<MenuToggleElement>) => <MenuToggle ref={ref} onClick={() => setTemplateOpen((o) => !o)} isExpanded={templateOpen} isDisabled={isDisabled}>Add a template</MenuToggle>}
              >
                <DropdownList>
                  {SELINUX_BOOLEAN_TEMPLATES.map((t) => <DropdownItem key={t.id} value={t.id} description={t.description}>{t.label}</DropdownItem>)}
                </DropdownList>
              </Dropdown>
            </div>
            <ListEditor
              items={model.booleans}
              addLabel="Add a boolean by name"
              emptyText="No booleans yet. Pick them from the search above or add a template."
              onAdd={() => update((m) => ({ ...m, booleans: [...m.booleans, { name: "", value: true, description: "" }] }))}
              onRemove={(i) => update((m) => ({ ...m, booleans: m.booleans.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove boolean ${model.booleans[i]?.name || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(b, i) => {
                const nameProblem = booleanNameProblem(b.name);
                const risky = !nameProblem && isRiskySelinuxBoolean(b.name, b.value);
                return (
                  <div style={{ flex: 1, display: "grid", gridTemplateColumns: "minmax(12rem, 2fr) auto minmax(10rem, 3fr)", gap: "0.5rem", alignItems: "center" }}>
                    <div>
                      <TextInput id={`${idp}-bool-${i}`} aria-label="Boolean name" value={b.name} onChange={(_ev, v) => update((m) => ({ ...m, booleans: m.booleans.map((x, j) => (j === i ? { ...x, name: v.trim() } : x)) }))} isDisabled={isDisabled} validated={nameProblem ? "error" : "default"} style={mono} />
                      {nameProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{nameProblem}</HelperTextItem></HelperText></FormHelperText>}
                    </div>
                    <Switch id={`${idp}-bool-${i}-value`} aria-label={`${b.name || "boolean"} value`} label={b.value ? "on" : "off"} isChecked={b.value} onChange={(_ev, v) => update((m) => ({ ...m, booleans: m.booleans.map((x, j) => (j === i ? { ...x, value: v } : x)) }))} isDisabled={isDisabled} />
                    <div>
                      <TextInput id={`${idp}-bool-${i}-desc`} aria-label="Boolean description" placeholder="Why (optional)" value={b.description} onChange={(_ev, v) => update((m) => ({ ...m, booleans: m.booleans.map((x, j) => (j === i ? { ...x, description: v } : x)) }))} isDisabled={isDisabled} />
                      {risky && <FormHelperText><HelperText><HelperTextItem variant="warning">This value can lock users out or break desktop applications; try it on a pilot group first.</HelperTextItem></HelperText></FormHelperText>}
                    </div>
                  </div>
                );
              }}
            />
          </div>
        </Tab>

        <Tab eventKey="exceptions" title={<TabTitleText>Exceptions{model.permissiveDomains.length ? ` (${model.permissiveDomains.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              Domains listed here run permissive while the rest of the system stays enforcing: the safe alternative to switching a whole group to permissive when one application misbehaves. The agent installs them as the module <code>bor-permissive</code>.
            </p>
            {denials.length > 0 && (
              <div style={{ marginBottom: "0.5rem" }}>
                <span style={{ fontSize: "0.85rem" }}>Domains denied on the selected node recently: </span>
                {Array.from(new Set(denials.map((d) => d.subject))).slice(0, 12).map((s) => (
                  <Button key={s} variant="link" size="sm" isInline onClick={() => update((m) => m.permissiveDomains.includes(s) ? m : { ...m, permissiveDomains: [...m.permissiveDomains, s] })} isDisabled={isDisabled || model.permissiveDomains.includes(s)} style={{ marginRight: "0.5rem" }}>
                    <code>{s}</code>
                  </Button>
                ))}
              </div>
            )}
            <ListEditor
              items={model.permissiveDomains}
              addLabel="Add a domain"
              emptyText="No permissive domains."
              onAdd={() => update((m) => ({ ...m, permissiveDomains: [...m.permissiveDomains, ""] }))}
              onRemove={(i) => update((m) => ({ ...m, permissiveDomains: m.permissiveDomains.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove domain ${model.permissiveDomains[i] || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(d, i) => {
                const p = domainProblem(d);
                return (
                  <div style={{ flex: 1 }}>
                    <TextInput id={`${idp}-perm-${i}`} aria-label="Permissive domain" placeholder="corpapp_t" value={d} onChange={(_ev, v) => update((m) => ({ ...m, permissiveDomains: m.permissiveDomains.map((x, j) => (j === i ? v.trim() : x)) }))} isDisabled={isDisabled} validated={p ? "error" : "default"} style={mono} />
                    {p && <FormHelperText><HelperText><HelperTextItem variant="error">{p}</HelperTextItem></HelperText></FormHelperText>}
                    {!p && RISKY_SELINUX_PERMISSIVE.has(d) && <FormHelperText><HelperText><HelperTextItem variant="warning">Making this domain permissive removes the protection of the login path.</HelperTextItem></HelperText></FormHelperText>}
                  </div>
                );
              }}
            />
          </div>
        </Tab>

        <Tab eventKey="modules" title={<TabTitleText>Modules{model.modules.length + model.customModules.length ? ` (${model.modules.length + model.customModules.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <Title headingLevel="h4" size="md">Existing modules</Title>
            <p style={{ ...subtle, fontSize: "0.85rem", margin: "0.25rem 0 0.5rem" }}>Enable or disable modules of the node's policy store (semodule -e / -d).</p>
            <MacCatalogueSearch lsm="selinux" kind="module" nodeId={nodeId} taken={moduleNames} onPick={(it) => update((m) => ({ ...m, modules: [...m.modules, { name: it.name, enabled: !(it.info.enabled ?? true) }] }))} describe={describeModule} placeholder="Search modules" isDisabled={isDisabled} />
            <ListEditor
              items={model.modules}
              addLabel="Add a module by name"
              emptyText="No module switches."
              onAdd={() => update((m) => ({ ...m, modules: [...m.modules, { name: "", enabled: false }] }))}
              onRemove={(i) => update((m) => ({ ...m, modules: m.modules.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove module ${model.modules[i]?.name || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(mod, i) => {
                const p = moduleNameProblem(mod.name);
                return (
                  <div style={{ flex: 1, display: "grid", gridTemplateColumns: "minmax(12rem, 1fr) auto", gap: "0.5rem", alignItems: "center" }}>
                    <div>
                      <TextInput id={`${idp}-mod-${i}`} aria-label="Module name" value={mod.name} onChange={(_ev, v) => update((m) => ({ ...m, modules: m.modules.map((x, j) => (j === i ? { ...x, name: v.trim() } : x)) }))} isDisabled={isDisabled} validated={p ? "error" : "default"} style={mono} />
                      {p && <FormHelperText><HelperText><HelperTextItem variant="error">{p}</HelperTextItem></HelperText></FormHelperText>}
                      {!p && !mod.enabled && RISKY_SELINUX_MODULES.has(mod.name) && <FormHelperText><HelperText><HelperTextItem variant="warning">Disabling this module can lock users or the agent out.</HelperTextItem></HelperText></FormHelperText>}
                    </div>
                    <Switch id={`${idp}-mod-${i}-enabled`} aria-label={`${mod.name || "module"} enabled`} label={mod.enabled ? "enabled" : "disabled"} isChecked={mod.enabled} onChange={(_ev, v) => update((m) => ({ ...m, modules: m.modules.map((x, j) => (j === i ? { ...x, enabled: v } : x)) }))} isDisabled={isDisabled} />
                  </div>
                );
              }}
            />
            <Title headingLevel="h4" size="md" style={{ marginTop: "1.5rem" }}>Custom modules</Title>
            <p style={{ ...subtle, fontSize: "0.85rem", margin: "0.25rem 0 0.5rem" }}>
              Policy written in CIL, installed as <code>bor-&lt;name&gt;</code>. A Bor scheduled task can run under a type declared here: set SELinuxContext on the task in a Services &amp; timers policy. The node page can propose one from an application's denials. Saving or releasing a policy with custom modules is audited.
            </p>
            {model.customModules.map((c, i) => {
              const nameProblem = customNameProblem(c.name);
              const textProblem = cilProblem(c.cil);
              return (
                <div key={i} style={{ border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "var(--pf-t--global--border--radius--small)", padding: "0.75rem", marginBottom: "0.75rem" }}>
                  <div style={{ display: "grid", gridTemplateColumns: "minmax(10rem, 1fr) minmax(10rem, 2fr) auto auto", gap: "0.5rem", alignItems: "start" }}>
                    <div>
                      <TextInput id={`${idp}-custom-${i}-name`} aria-label="Custom module name" placeholder="name (installed as bor-name)" value={c.name} onChange={(_ev, v) => update((m) => ({ ...m, customModules: m.customModules.map((x, j) => (j === i ? { ...x, name: v.trim() } : x)) }))} isDisabled={isDisabled} validated={nameProblem ? "error" : "default"} style={mono} />
                      {nameProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{nameProblem}</HelperTextItem></HelperText></FormHelperText>}
                    </div>
                    <TextInput id={`${idp}-custom-${i}-desc`} aria-label="Custom module description" placeholder="What it allows and why (optional)" value={c.description} onChange={(_ev, v) => update((m) => ({ ...m, customModules: m.customModules.map((x, j) => (j === i ? { ...x, description: v } : x)) }))} isDisabled={isDisabled} />
                    <Dropdown
                      isOpen={cilTemplateOpen === i}
                      onOpenChange={(o) => setCilTemplateOpen(o ? i : null)}
                      onSelect={(_ev, v) => {
                        setCilTemplateOpen(null);
                        const t = SELINUX_CIL_TEMPLATES.find((x) => x.id === v);
                        if (t) update((m) => ({ ...m, customModules: m.customModules.map((x, j) => (j === i ? { ...x, cil: x.cil.trim() ? `${x.cil.trimEnd()}\n${t.cil}` : t.cil } : x)) }));
                      }}
                      toggle={(ref: React.Ref<MenuToggleElement>) => <MenuToggle ref={ref} variant="secondary" size="sm" onClick={() => setCilTemplateOpen((cur) => (cur === i ? null : i))} isExpanded={cilTemplateOpen === i} isDisabled={isDisabled}>Insert a template</MenuToggle>}
                    >
                      <DropdownList>
                        {SELINUX_CIL_TEMPLATES.map((t) => <DropdownItem key={t.id} value={t.id} description={t.description}>{t.label}</DropdownItem>)}
                      </DropdownList>
                    </Dropdown>
                    <Button variant="plain" aria-label={`Remove custom module ${c.name || i + 1}`} onClick={() => update((m) => ({ ...m, customModules: m.customModules.filter((_, j) => j !== i) }))} isDisabled={isDisabled} icon={<TrashIcon />} />
                  </div>
                  <TextArea id={`${idp}-custom-${i}-cil`} aria-label="CIL source" value={c.cil} onChange={(_ev, v) => update((m) => ({ ...m, customModules: m.customModules.map((x, j) => (j === i ? { ...x, cil: v } : x)) }))} isDisabled={isDisabled} rows={8} resizeOrientation="vertical" style={{ ...mono, marginTop: "0.5rem" }} validated={textProblem ? "error" : "default"} />
                  {textProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{textProblem}</HelperTextItem></HelperText></FormHelperText>}
                </div>
              );
            })}
            <Button variant="link" icon={<PlusCircleIcon />} onClick={() => update((m) => ({ ...m, customModules: [...m.customModules, { name: "", cil: "", description: "" }] }))} isDisabled={isDisabled} style={{ paddingLeft: 0 }}>Add a custom module</Button>
          </div>
        </Tab>

        <Tab eventKey="fcontexts" title={<TabTitleText>File contexts{model.fileContexts.length ? ` (${model.fileContexts.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              Label files that match a path expression, the way <code>semanage fcontext</code> does; the agent installs the rules as the module <code>bor-filecontexts</code> and runs <code>restorecon</code> on the literal prefix when asked.
            </p>
            <ListEditor
              items={model.fileContexts}
              addLabel="Add a file context"
              emptyText="No file contexts."
              onAdd={() => update((m) => ({ ...m, fileContexts: [...m.fileContexts, { pathRegex: "", type: "", kind: "SELINUX_FILE_KIND_UNSPECIFIED", restorecon: true }] }))}
              onRemove={(i) => update((m) => ({ ...m, fileContexts: m.fileContexts.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove file context ${model.fileContexts[i]?.pathRegex || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(f, i) => {
                const p = fileContextProblem(f);
                const set = (patch: Partial<typeof f>) => update((m) => ({ ...m, fileContexts: m.fileContexts.map((x, j) => (j === i ? { ...x, ...patch } : x)) }));
                return (
                  <div style={{ flex: 1 }}>
                    <div style={{ display: "grid", gridTemplateColumns: "minmax(12rem, 3fr) minmax(8rem, 2fr) minmax(8rem, 1fr) auto", gap: "0.5rem", alignItems: "center" }}>
                      <TextInput id={`${idp}-fc-${i}-path`} aria-label="Path expression" placeholder="/opt/corpapp(/.*)?" value={f.pathRegex} onChange={(_ev, v) => set({ pathRegex: v.trim() })} isDisabled={isDisabled} style={mono} />
                      <TextInput id={`${idp}-fc-${i}-type`} aria-label="SELinux type" placeholder="bin_t" value={f.type} onChange={(_ev, v) => set({ type: v.trim() })} isDisabled={isDisabled} style={mono} />
                      <FormSelect id={`${idp}-fc-${i}-kind`} aria-label="File kind" value={f.kind} onChange={(_ev, v) => set({ kind: v as typeof f.kind })} isDisabled={isDisabled}>
                        {SELINUX_FILE_KIND_OPTIONS.map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
                      </FormSelect>
                      <Checkbox id={`${idp}-fc-${i}-restorecon`} label="relabel now" isChecked={f.restorecon} onChange={(_ev, v) => set({ restorecon: v })} isDisabled={isDisabled} />
                    </div>
                    {p && <FormHelperText><HelperText><HelperTextItem variant="error">{p}</HelperTextItem></HelperText></FormHelperText>}
                  </div>
                );
              }}
            />
          </div>
        </Tab>

        <Tab eventKey="logins" title={<TabTitleText>Logins{model.logins.length ? ` (${model.logins.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              Map named accounts or groups (<code>%group</code>) to a confined SELinux user, for kiosks and shared desktops. Needs <code>semanage</code> on the node. A <code>user_u</code> session cannot use sudo; the default mapping of every other account is never changed.
            </p>
            <ListEditor
              items={model.logins}
              addLabel="Add a login mapping"
              emptyText="No login mappings."
              onAdd={() => update((m) => ({ ...m, logins: [...m.logins, { login: "", seuser: "user_u", range: "" }] }))}
              onRemove={(i) => update((m) => ({ ...m, logins: m.logins.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove login ${model.logins[i]?.login || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(l, i) => {
                const p = loginProblem(l);
                const set = (patch: Partial<typeof l>) => update((m) => ({ ...m, logins: m.logins.map((x, j) => (j === i ? { ...x, ...patch } : x)) }));
                return (
                  <div style={{ flex: 1 }}>
                    <div style={{ display: "grid", gridTemplateColumns: "minmax(10rem, 2fr) minmax(8rem, 2fr) minmax(8rem, 1fr)", gap: "0.5rem", alignItems: "center" }}>
                      <TextInput id={`${idp}-login-${i}`} aria-label="Login or %group" placeholder="kiosk or %students" value={l.login} onChange={(_ev, v) => set({ login: v.trim() })} isDisabled={isDisabled} style={mono} />
                      <TextInput id={`${idp}-login-${i}-seuser`} aria-label="SELinux user" list={`${idp}-seusers`} value={l.seuser} onChange={(_ev, v) => set({ seuser: v.trim() })} isDisabled={isDisabled} style={mono} />
                      <TextInput id={`${idp}-login-${i}-range`} aria-label="MLS/MCS range" placeholder="s0 (optional)" value={l.range} onChange={(_ev, v) => set({ range: v.trim() })} isDisabled={isDisabled} style={mono} />
                    </div>
                    {p && <FormHelperText><HelperText><HelperTextItem variant="error">{p}</HelperTextItem></HelperText></FormHelperText>}
                  </div>
                );
              }}
            />
            <datalist id={`${idp}-seusers`}>{SEUSER_SUGGESTIONS.map((u) => <option key={u} value={u} />)}</datalist>
          </div>
        </Tab>
      </Tabs>
      <div style={{ marginTop: "0.75rem", display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
        {model.mode === "SELINUX_MODE_PERMISSIVE" && <Tooltip content="Every denial on the bound nodes is only logged"><Label color="orange">permissive</Label></Tooltip>}
        {model.customModules.length > 0 && <Tooltip content="Saving or releasing a policy with custom modules is audited with a hash of each module"><Label color="blue">audited: custom policy text</Label></Tooltip>}
      </div>
      <ConfirmModal
        isOpen={pendingPermissive}
        title="Switch the bound nodes to permissive mode?"
        confirmLabel="Use permissive"
        isDanger
        onCancel={() => setPendingPermissive(false)}
        onConfirm={() => { setPendingPermissive(false); update((m) => ({ ...m, mode: "SELINUX_MODE_PERMISSIVE" })); }}
      >
        In permissive mode SELinux logs denials but enforces nothing on every node this policy reaches. Prefer a per-domain exception on the Exceptions tab, or bind the policy to a pilot group.
      </ConfirmModal>
    </div>
  );
};
