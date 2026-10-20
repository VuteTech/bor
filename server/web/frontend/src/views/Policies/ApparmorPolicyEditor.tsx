// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ApparmorPolicyEditor - an `Apparmor` policy: whether AppArmor must be
 * enabled and its service running, the user namespace restriction, the
 * mode of existing profiles (by name or pattern), Bor-deployed profiles
 * from templates, local overrides of vendor profiles and extra home
 * directories. Content is protojson (camelCase, enum names); see
 * apparmorModel.ts.
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
import { ConfirmModal } from "../../components/ConfirmModal";
import { ListEditor } from "./ListEditor";
import { MacCatalogueSearch, MacNodePicker, subtle } from "./macPickers";
import { type MacCatalogueItem } from "../../apiClient/macApi";
import {
  APPARMOR_MODE_OPTIONS,
  APPARMOR_PROFILE_TEMPLATES,
  APPARMOR_SWITCH_OPTIONS,
  ApparmorContent,
  ApparmorMode,
  ApparmorSwitch,
  PROTECTED_APPARMOR_PROFILES,
  bracesProblem,
  customNameProblem,
  homeDirProblem,
  isGlob,
  parseApparmorContent,
  profileFileProblem,
  profileMatchProblem,
  profileTextProblem,
  serializeApparmorContent,
  validateApparmorContent,
} from "./apparmorModel";

export interface ApparmorPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
  onValidityChange?: (ok: boolean) => void;
}

const mono: React.CSSProperties = { fontFamily: "monospace", fontSize: "0.85rem" };

export const ApparmorPolicyEditor: React.FC<ApparmorPolicyEditorProps> = ({ contentRaw, onChange, isDisabled, onValidityChange }) => {
  const idp = useId();
  const [model, setModel] = useState<ApparmorContent>(() => parseApparmorContent(contentRaw));
  const lastRaw = useRef(contentRaw);
  const [tab, setTab] = useState<string | number>("status");
  const [nodeId, setNodeId] = useState("");
  const [pendingDisable, setPendingDisable] = useState<number | null>(null);
  const [pendingUsernsOff, setPendingUsernsOff] = useState(false);
  const [templateOpen, setTemplateOpen] = useState<number | null>(null);
  const [binary, setBinary] = useState<Record<number, string>>({});

  useEffect(() => {
    if (contentRaw !== lastRaw.current) {
      lastRaw.current = contentRaw;
      setModel(parseApparmorContent(contentRaw));
    }
  }, [contentRaw]);

  const update = useCallback((fn: (m: ApparmorContent) => ApparmorContent) => {
    setModel((prev) => {
      const next = fn(prev);
      const raw = serializeApparmorContent(next);
      lastRaw.current = raw;
      onChange(raw);
      return next;
    });
  }, [onChange]);

  const problem = useMemo(() => validateApparmorContent(model), [model]);
  useEffect(() => { onValidityChange?.(problem === null); }, [problem, onValidityChange]);

  const profileMatches = useMemo(() => new Set(model.profiles.map((p) => p.match)), [model.profiles]);
  const overrideFiles = useMemo(() => new Set(model.localOverrides.map((o) => o.profileFile)), [model.localOverrides]);

  const setProfileMode = (i: number, mode: ApparmorMode) => {
    if (mode === "APPARMOR_MODE_DISABLED" && model.profiles[i]?.mode !== mode) { setPendingDisable(i); return; }
    update((m) => ({ ...m, profiles: m.profiles.map((x, j) => (j === i ? { ...x, mode, editVendorFlags: mode === "APPARMOR_MODE_DISABLED" ? false : x.editVendorFlags } : x)) }));
  };

  const setUserns = (v: ApparmorSwitch) => {
    if (v === "APPARMOR_SWITCH_OFF" && model.restrictUnprivilegedUserns !== v) { setPendingUsernsOff(true); return; }
    update((m) => ({ ...m, restrictUnprivilegedUserns: v }));
  };

  const describeProfile = (it: MacCatalogueItem): string => {
    const parts: string[] = [];
    if (it.description) parts.push(it.description);
    if (it.info.file && it.info.file !== it.name) parts.push(`file ${it.info.file}`);
    if (nodeId) {
      parts.push(it.info.loadedMode ? `loaded as ${it.info.loadedMode}` : "not loaded");
      if (it.info.fileMode) parts.push(`file sets ${it.info.fileMode}`);
      if (it.info.unconfinedWithProfile) parts.push("a process still runs unconfined (restart it)");
      if (it.info.attachmentConflict) parts.push("attachment shared with another profile");
    } else if (it.info.fileMode) {
      parts.push(`file sets ${it.info.fileMode}`);
    }
    return parts.join(" · ") || "Vendor profile";
  };

  const profileFileOf = (it: MacCatalogueItem): string => it.info.file || it.name;

  const addProfile = (it: MacCatalogueItem) => {
    const file = profileFileOf(it);
    update((m) => (m.profiles.some((p) => p.match === file) ? m : { ...m, profiles: [...m.profiles, { match: file, mode: "APPARMOR_MODE_ENFORCE", editVendorFlags: false }] }));
  };

  const excludeProfile = (it: MacCatalogueItem): string | null => {
    if (it.dynamic) return "Managed by " + (it.info.managedBy || "other software");
    if (it.name.includes("/") && !it.info.file) return "No profile file";
    if (profileFileOf(it).startsWith("bor-")) return "Bor profile";
    return null;
  };

  const usedMatches = useMemo(() => new Set(Array.from(profileMatches)), [profileMatches]);

  return (
    <div>
      <LiveAlert id={`${idp}-problem`} variant="warning" isInline message={problem} style={{ marginBottom: "0.75rem" }} />
      <div style={{ marginBottom: "0.75rem" }}>
        <MacNodePicker lsm="apparmor" value={nodeId} onChange={setNodeId} />
      </div>
      <Tabs activeKey={tab} onSelect={(_ev, k) => setTab(k)} aria-label="AppArmor policy sections" isBox>
        <Tab eventKey="status" title={<TabTitleText>Status</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <Checkbox
              id={`${idp}-require`}
              label="Treat nodes without AppArmor as non-compliant"
              description="Otherwise a node that does not run AppArmor (Fedora, RHEL, SUSE 16, a node booted with apparmor=0) reports the policy as inapplicable."
              isChecked={model.requireEnabled}
              onChange={(_ev, v) => update((m) => ({ ...m, requireEnabled: v }))}
              isDisabled={isDisabled}
            />
            <Checkbox
              id={`${idp}-service`}
              label="Keep apparmor.service enabled and running"
              description="The service loads the profiles at boot. The agent enables and starts it when it is installed but inactive."
              isChecked={model.ensureService}
              onChange={(_ev, v) => update((m) => ({ ...m, ensureService: v }))}
              isDisabled={isDisabled}
              style={{ marginTop: "0.5rem" }}
            />
            <FormGroup label="Restrict unprivileged user namespaces" isStack fieldId={`${idp}-userns`} role="radiogroup" style={{ marginTop: "1rem" }}>
              <FormHelperText><HelperText><HelperTextItem>
                Ubuntu 24.04 and later turn this on: a program without a profile that creates a user namespace (a sandbox) lands in the restricted <code>unprivileged_userns</code> profile and cannot use capabilities inside it. Applications that need sandboxes get a stub profile on the Custom profiles tab; the node page lists the ones that were denied. Kernels without the switch report the item as inapplicable.
              </HelperTextItem></HelperText></FormHelperText>
              {APPARMOR_SWITCH_OPTIONS.map((o) => (
                <Radio key={o.value} id={`${idp}-userns-${o.value}`} name={`${idp}-userns`} label={o.label} isChecked={model.restrictUnprivilegedUserns === o.value} onChange={() => setUserns(o.value)} isDisabled={isDisabled} />
              ))}
            </FormGroup>
            <FormGroup label="Restrict unprivileged unconfined processes" isStack fieldId={`${idp}-unconfined`} role="radiogroup" style={{ marginTop: "1rem" }}>
              <FormHelperText><HelperText><HelperTextItem>
                Stops an unconfined process from switching itself into a friendlier profile (<code>aa-exec</code>). Ubuntu enables it with the restriction above.
              </HelperTextItem></HelperText></FormHelperText>
              {APPARMOR_SWITCH_OPTIONS.map((o) => (
                <Radio key={o.value} id={`${idp}-unconfined-${o.value}`} name={`${idp}-unconfined`} label={o.label} isChecked={model.restrictUnprivilegedUnconfined === o.value} onChange={() => update((m) => ({ ...m, restrictUnprivilegedUnconfined: o.value }))} isDisabled={isDisabled} />
              ))}
            </FormGroup>
          </div>
        </Tab>

        <Tab eventKey="profiles" title={<TabTitleText>Profiles{model.profiles.length ? ` (${model.profiles.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              The mode of profile files under <code>/etc/apparmor.d</code>, set through the <code>force-complain/</code> and <code>disable/</code> links the parser honours, so vendor files stay untouched. A name beats a pattern; stub profiles (<code>unconfined</code>) are left alone by patterns.
            </p>
            <div style={{ display: "flex", gap: "0.5rem", alignItems: "flex-start", flexWrap: "wrap" }}>
              <div style={{ flex: "1 1 24rem" }}>
                <MacCatalogueSearch lsm="apparmor" kind="profile" nodeId={nodeId} taken={usedMatches} onPick={addProfile} describe={describeProfile} placeholder="Search profiles" isDisabled={isDisabled} exclude={excludeProfile} />
              </div>
              <Button variant="secondary" onClick={() => update((m) => (m.profiles.some((p) => p.match === "*") ? m : { ...m, profiles: [...m.profiles, { match: "*", mode: "APPARMOR_MODE_ENFORCE", editVendorFlags: false }] }))} isDisabled={isDisabled || profileMatches.has("*")}>
                All vendor profiles: enforce
              </Button>
            </div>
            <ListEditor
              items={model.profiles}
              addLabel="Add a profile or pattern by name"
              emptyText="No profile modes yet."
              onAdd={() => update((m) => ({ ...m, profiles: [...m.profiles, { match: "", mode: "APPARMOR_MODE_ENFORCE", editVendorFlags: false }] }))}
              onRemove={(i) => update((m) => ({ ...m, profiles: m.profiles.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove profile entry ${model.profiles[i]?.match || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(p, i) => {
                const matchProblem = profileMatchProblem(p.match);
                const glob = isGlob(p.match);
                return (
                  <div style={{ flex: 1 }}>
                    <div style={{ display: "grid", gridTemplateColumns: "minmax(12rem, 2fr) minmax(8rem, 1fr) auto", gap: "0.5rem", alignItems: "center" }}>
                      <TextInput id={`${idp}-prof-${i}`} aria-label="Profile file name or pattern" placeholder="usr.sbin.cupsd or usr.sbin.*" value={p.match} onChange={(_ev, v) => update((m) => ({ ...m, profiles: m.profiles.map((x, j) => (j === i ? { ...x, match: v.trim() } : x)) }))} isDisabled={isDisabled} validated={matchProblem ? "error" : "default"} style={mono} />
                      <FormSelect id={`${idp}-prof-${i}-mode`} aria-label="Profile mode" value={p.mode} onChange={(_ev, v) => setProfileMode(i, v as ApparmorMode)} isDisabled={isDisabled}>
                        {APPARMOR_MODE_OPTIONS.filter((o) => !(glob && o.value === "APPARMOR_MODE_DISABLED")).map((o) => <FormSelectOption key={o.value} value={o.value} label={o.label} />)}
                      </FormSelect>
                      <Tooltip content="For vendor files shipped with flags=(complain): strip the flag inside the file so it can be enforced. Needs apparmor.allow_vendor_flag_edits on the node; a backup is kept and restored.">
                        <Checkbox id={`${idp}-prof-${i}-flags`} label="edit vendor flags" isChecked={p.editVendorFlags} onChange={(_ev, v) => update((m) => ({ ...m, profiles: m.profiles.map((x, j) => (j === i ? { ...x, editVendorFlags: v } : x)) }))} isDisabled={isDisabled || p.mode === "APPARMOR_MODE_DISABLED"} />
                      </Tooltip>
                    </div>
                    {matchProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{matchProblem}</HelperTextItem></HelperText></FormHelperText>}
                    {!matchProblem && p.mode === "APPARMOR_MODE_DISABLED" && PROTECTED_APPARMOR_PROFILES.has(p.match) && <FormHelperText><HelperText><HelperTextItem variant="error">This profile cannot be disabled: the user namespace restriction depends on it.</HelperTextItem></HelperText></FormHelperText>}
                    {!matchProblem && glob && <FormHelperText><HelperText><HelperTextItem>{APPARMOR_MODE_OPTIONS.find((o) => o.value === p.mode)?.help} Applies to every matching vendor profile file; exact entries win.</HelperTextItem></HelperText></FormHelperText>}
                  </div>
                );
              }}
            />
          </div>
        </Tab>

        <Tab eventKey="custom" title={<TabTitleText>Custom profiles{model.customProfiles.length ? ` (${model.customProfiles.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              Profiles Bor writes to <code>/etc/apparmor.d/bor-&lt;name&gt;</code>, checked with <code>apparmor_parser</code> before they are loaded. The templates use ABI 4.0, which loads on AppArmor 4.0, 4.1 and 5.0. Saving or releasing a policy with custom profiles is audited.
            </p>
            {model.customProfiles.map((c, i) => {
              const nameProblem = customNameProblem(c.name);
              const textProblem = c.text.trim() ? profileTextProblem(c.name || "name", c.text) : "Enter the profile text or insert a template";
              return (
                <div key={i} style={{ border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "var(--pf-t--global--border--radius--small)", padding: "0.75rem", marginBottom: "0.75rem" }}>
                  <div style={{ display: "grid", gridTemplateColumns: "minmax(10rem, 1fr) minmax(10rem, 2fr) auto", gap: "0.5rem", alignItems: "start" }}>
                    <div>
                      <TextInput id={`${idp}-custom-${i}-name`} aria-label="Custom profile name" placeholder="name (file bor-name)" value={c.name} onChange={(_ev, v) => update((m) => ({ ...m, customProfiles: m.customProfiles.map((x, j) => (j === i ? { ...x, name: v.trim() } : x)) }))} isDisabled={isDisabled} validated={nameProblem ? "error" : "default"} style={mono} />
                      {nameProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{nameProblem}</HelperTextItem></HelperText></FormHelperText>}
                    </div>
                    <TextInput id={`${idp}-custom-${i}-desc`} aria-label="Custom profile description" placeholder="What it confines or allows (optional)" value={c.description} onChange={(_ev, v) => update((m) => ({ ...m, customProfiles: m.customProfiles.map((x, j) => (j === i ? { ...x, description: v } : x)) }))} isDisabled={isDisabled} />
                    <Button variant="plain" aria-label={`Remove custom profile ${c.name || i + 1}`} onClick={() => update((m) => ({ ...m, customProfiles: m.customProfiles.filter((_, j) => j !== i) }))} isDisabled={isDisabled} icon={<TrashIcon />} />
                  </div>
                  <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", flexWrap: "wrap", marginTop: "0.5rem" }}>
                    <TextInput id={`${idp}-custom-${i}-binary`} aria-label="Executable path for the template" placeholder="/opt/application/bin/application" value={binary[i] ?? ""} onChange={(_ev, v) => setBinary((b) => ({ ...b, [i]: v.trim() }))} isDisabled={isDisabled} style={{ ...mono, flex: "1 1 18rem" }} />
                    <Dropdown
                      isOpen={templateOpen === i}
                      onOpenChange={(o) => setTemplateOpen(o ? i : null)}
                      onSelect={(_ev, v) => {
                        setTemplateOpen(null);
                        const t = APPARMOR_PROFILE_TEMPLATES.find((x) => x.id === v);
                        if (t) update((m) => ({ ...m, customProfiles: m.customProfiles.map((x, j) => (j === i ? { ...x, text: t.render(x.name, binary[i] ?? "") } : x)) }));
                      }}
                      toggle={(ref: React.Ref<MenuToggleElement>) => <MenuToggle ref={ref} variant="secondary" size="sm" onClick={() => setTemplateOpen((cur) => (cur === i ? null : i))} isExpanded={templateOpen === i} isDisabled={isDisabled}>Insert a template</MenuToggle>}
                    >
                      <DropdownList>
                        {APPARMOR_PROFILE_TEMPLATES.map((t) => <DropdownItem key={t.id} value={t.id} description={t.description}>{t.label}</DropdownItem>)}
                      </DropdownList>
                    </Dropdown>
                  </div>
                  <TextArea id={`${idp}-custom-${i}-text`} aria-label="Profile text" value={c.text} onChange={(_ev, v) => update((m) => ({ ...m, customProfiles: m.customProfiles.map((x, j) => (j === i ? { ...x, text: v } : x)) }))} isDisabled={isDisabled} rows={12} resizeOrientation="vertical" style={{ ...mono, marginTop: "0.5rem" }} validated={textProblem ? "error" : "default"} />
                  {textProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{textProblem}</HelperTextItem></HelperText></FormHelperText>}
                </div>
              );
            })}
            <Button variant="link" icon={<PlusCircleIcon />} onClick={() => update((m) => ({ ...m, customProfiles: [...m.customProfiles, { name: "", text: "", description: "" }] }))} isDisabled={isDisabled} style={{ paddingLeft: 0 }}>Add a custom profile</Button>
          </div>
        </Tab>

        <Tab eventKey="local" title={<TabTitleText>Local overrides{model.localOverrides.length ? ` (${model.localOverrides.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              Rules added to a vendor profile through its <code>local/</code> include, in a block Bor owns; the rest of the local file and the vendor profile stay as they are. Use <code>priority=1</code> on a rule to override a vendor <code>deny</code> (AppArmor 4.1 and later).
            </p>
            <MacCatalogueSearch lsm="apparmor" kind="profile" nodeId={nodeId} taken={overrideFiles} onPick={(it) => update((m) => ({ ...m, localOverrides: [...m.localOverrides, { profileFile: profileFileOf(it), text: "" }] }))} describe={(it) => (it.info.hasLocalInclude === false ? "No local include; use a custom profile" : describeProfile(it))} placeholder="Search vendor profiles" isDisabled={isDisabled} exclude={(it) => excludeProfile(it) ?? (it.info.hasLocalInclude === false ? "No local include" : null)} />
            {model.localOverrides.map((o, i) => {
              const fileProblem = profileFileProblem(o.profileFile);
              const textProblem = bracesProblem(o.text, "override");
              return (
                <div key={i} style={{ border: "1px solid var(--pf-t--global--border--color--default)", borderRadius: "var(--pf-t--global--border--radius--small)", padding: "0.75rem", margin: "0.75rem 0" }}>
                  <div style={{ display: "grid", gridTemplateColumns: "minmax(12rem, 1fr) auto", gap: "0.5rem", alignItems: "start" }}>
                    <div>
                      <TextInput id={`${idp}-local-${i}-file`} aria-label="Vendor profile file" placeholder="usr.sbin.cupsd" value={o.profileFile} onChange={(_ev, v) => update((m) => ({ ...m, localOverrides: m.localOverrides.map((x, j) => (j === i ? { ...x, profileFile: v.trim() } : x)) }))} isDisabled={isDisabled} validated={fileProblem ? "error" : "default"} style={mono} />
                      {fileProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{fileProblem}</HelperTextItem></HelperText></FormHelperText>}
                    </div>
                    <Button variant="plain" aria-label={`Remove local override ${o.profileFile || i + 1}`} onClick={() => update((m) => ({ ...m, localOverrides: m.localOverrides.filter((_, j) => j !== i) }))} isDisabled={isDisabled} icon={<TrashIcon />} />
                  </div>
                  <TextArea id={`${idp}-local-${i}-text`} aria-label="Override rules" placeholder={"  /nfs/home/** rwk,\n  priority=1 /etc/hostname r,"} value={o.text} onChange={(_ev, v) => update((m) => ({ ...m, localOverrides: m.localOverrides.map((x, j) => (j === i ? { ...x, text: v } : x)) }))} isDisabled={isDisabled} rows={5} resizeOrientation="vertical" style={{ ...mono, marginTop: "0.5rem" }} validated={textProblem ? "error" : "default"} />
                  {textProblem && <FormHelperText><HelperText><HelperTextItem variant="error">{textProblem}</HelperTextItem></HelperText></FormHelperText>}
                </div>
              );
            })}
            <Button variant="link" icon={<PlusCircleIcon />} onClick={() => update((m) => ({ ...m, localOverrides: [...m.localOverrides, { profileFile: "", text: "" }] }))} isDisabled={isDisabled} style={{ paddingLeft: 0 }}>Add a local override</Button>
          </div>
        </Tab>

        <Tab eventKey="homes" title={<TabTitleText>Home directories{model.extraHomeDirs.length ? ` (${model.extraHomeDirs.length})` : ""}</TabTitleText>}>
          <div style={{ padding: "1rem 0" }}>
            <p style={{ ...subtle, fontSize: "0.85rem", marginBottom: "0.5rem" }}>
              Extra roots for <code>@{"{HOMEDIRS}"}</code> (NFS or autofs homes), written to <code>tunables/home.d/bor</code>; every profile is reloaded after a change.
            </p>
            <ListEditor
              items={model.extraHomeDirs}
              addLabel="Add a home directory root"
              emptyText="Only /home/ (the default)."
              onAdd={() => update((m) => ({ ...m, extraHomeDirs: [...m.extraHomeDirs, ""] }))}
              onRemove={(i) => update((m) => ({ ...m, extraHomeDirs: m.extraHomeDirs.filter((_, j) => j !== i) }))}
              removeAriaLabel={(i) => `Remove home directory ${model.extraHomeDirs[i] || i + 1}`}
              isDisabled={isDisabled}
              renderItem={(d, i) => {
                const p = homeDirProblem(d);
                return (
                  <div style={{ flex: 1 }}>
                    <TextInput id={`${idp}-home-${i}`} aria-label="Home directory root" placeholder="/nfs/home/" value={d} onChange={(_ev, v) => update((m) => ({ ...m, extraHomeDirs: m.extraHomeDirs.map((x, j) => (j === i ? v.trim() : x)) }))} isDisabled={isDisabled} validated={p ? "error" : "default"} style={mono} />
                    {p && <FormHelperText><HelperText><HelperTextItem variant="error">{p}</HelperTextItem></HelperText></FormHelperText>}
                  </div>
                );
              }}
            />
          </div>
        </Tab>
      </Tabs>
      <div style={{ marginTop: "0.75rem", display: "flex", gap: "0.5rem", flexWrap: "wrap" }}>
        {model.restrictUnprivilegedUserns === "APPARMOR_SWITCH_OFF" && <Tooltip content="Every unprivileged program may create user namespaces"><Label color="orange">user namespaces unrestricted</Label></Tooltip>}
        {(model.customProfiles.length > 0 || model.localOverrides.length > 0) && <Tooltip content="Saving or releasing a policy with custom profiles or overrides is audited with a hash of each text"><Label color="blue">audited: custom policy text</Label></Tooltip>}
      </div>
      <ConfirmModal
        isOpen={pendingDisable !== null}
        title={pendingDisable !== null ? `Disable the profile ${model.profiles[pendingDisable]?.match || ""}?` : ""}
        confirmLabel="Disable"
        isDanger
        onCancel={() => setPendingDisable(null)}
        onConfirm={() => {
          const i = pendingDisable;
          setPendingDisable(null);
          if (i !== null) update((m) => ({ ...m, profiles: m.profiles.map((x, j) => (j === i ? { ...x, mode: "APPARMOR_MODE_DISABLED", editVendorFlags: false } : x)) }));
        }}
      >
        A disabled profile is unloaded on every bound node and skipped at boot: the application runs unconfined. Prefer complain mode while you investigate.
      </ConfirmModal>
      <ConfirmModal
        isOpen={pendingUsernsOff}
        title="Turn the user namespace restriction off?"
        confirmLabel="Turn off"
        isDanger
        onCancel={() => setPendingUsernsOff(false)}
        onConfirm={() => { setPendingUsernsOff(false); update((m) => ({ ...m, restrictUnprivilegedUserns: "APPARMOR_SWITCH_OFF" })); }}
      >
        With the restriction off, every unprivileged program may create user namespaces, which several kernel exploit chains rely on. Exempt the applications that need sandboxes with a stub profile instead.
      </ConfirmModal>
    </div>
  );
};
