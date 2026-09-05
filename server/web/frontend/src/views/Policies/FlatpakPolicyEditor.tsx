// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * FlatpakPolicyEditor — remotes, applications and installation options of a
 * `Flatpak` policy. Content is protojson (camelCase, enum names); see
 * flatpakModel.ts. Apps are picked from the server-indexed catalog
 * (FlatpakAppPicker) or added by ID; remotes are copied from the server's
 * repositories, probed from a .flatpakrepo URL, or typed by hand.
 */

import React, { useCallback, useEffect, useId, useMemo, useState } from "react";
import {
  Button,
  Checkbox,
  Dropdown,
  DropdownItem,
  DropdownList,
  ExpandableSection,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Label,
  MenuToggle,
  MenuToggleElement,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  NumberInput,
  Select,
  SelectList,
  SelectOption,
  Switch,
  TextInput,
  Title,
  Tooltip,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td, ActionsColumn, IAction } from "@patternfly/react-table";
import EllipsisVIcon from "@patternfly/react-icons/dist/esm/icons/ellipsis-v-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { SearchableSelect } from "../../components/SearchableSelect";
import {
  FlatpakCatalogApp,
  FlatpakCatalogRepo,
  fetchFlatpakCatalogRepos,
} from "../../apiClient/flatpakApi";
import { FlatpakRemoteModal } from "./FlatpakRemoteModal";
import { FlatpakAppPicker } from "./FlatpakAppPicker";
import {
  APP_STATE_OPTIONS,
  DEFAULT_AUTO_UPDATE_HOURS,
  DEFAULT_OPERATION_TIMEOUT_MIN,
  FlatpakAppEntry,
  FlatpakAppState,
  FlatpakContent,
  FlatpakRemoteEntry,
  FlatpakScope,
  filterModeLabel,
  newAppEntry,
  newRemoteEntry,
  parseFlatpakContent,
  serializeFlatpakContent,
  validateFlatpakContent,
} from "./flatpakModel";

export interface FlatpakPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
  onValidityChange?: (ok: boolean) => void;
}

type RemoteModalState =
  | { kind: "closed" }
  | { kind: "add"; startWithImport: boolean }
  | { kind: "edit"; index: number };

/* ── "From server repository" modal ── */

const AddFromServerModal: React.FC<{
  isOpen: boolean;
  repos: FlatpakCatalogRepo[];
  takenNames: string[];
  onAdd: (entry: FlatpakRemoteEntry) => void;
  onClose: () => void;
}> = ({ isOpen, repos, takenNames, onAdd, onClose }) => {
  const idp = useId();
  const [selected, setSelected] = useState("");
  useEffect(() => {
    if (isOpen) setSelected(repos.find((r) => !takenNames.includes(r.name))?.id ?? "");
  }, [isOpen, repos, takenNames]);
  const repo = repos.find((r) => r.id === selected);
  const taken = repo ? takenNames.includes(repo.name) : false;
  return (
    <Modal variant={ModalVariant.small} isOpen={isOpen} onClose={onClose} aria-labelledby={`${idp}-title`}>
      <ModalHeader title="Add remote from server repository" labelId={`${idp}-title`} />
      <ModalBody>
        {repos.length === 0 ? (
          <LiveAlert message="No server repositories are configured" variant="info" isInline>
            Add repositories under Settings → Flatpak repositories, or add a custom remote instead.
          </LiveAlert>
        ) : (
          <Form onSubmit={(e) => e.preventDefault()}>
            <FormGroup label="Repository" fieldId={`${idp}-repo`}>
              <SearchableSelect
                id={`${idp}-repo`}
                options={repos.map((r) => ({ value: r.id, label: r.title || r.name, description: r.url }))}
                selected={selected}
                onSelect={setSelected}
                ariaLabel="Select a server repository"
              />
            </FormGroup>
            {repo && (
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Copies the name <code>{repo.name}</code>, URL, signing key{repo.subset ? `, subset ${repo.subset}` : ""} and collection ID into this policy. You can edit the remote afterwards.
                  </HelperTextItem>
                  {!repo.gpg_key_data && (
                    <HelperTextItem variant="warning">This repository has no signing key on the server; verification will be disabled until you add one.</HelperTextItem>
                  )}
                  {taken && <HelperTextItem variant="error">A remote named {repo.name} is already in this policy.</HelperTextItem>}
                </HelperText>
              </FormHelperText>
            )}
          </Form>
        )}
      </ModalBody>
      <ModalFooter>
        <Button
          variant="primary"
          isDisabled={!repo || taken}
          onClick={() => {
            if (!repo) return;
            onAdd(newRemoteEntry({
              name: repo.name,
              url: repo.url,
              title: repo.title,
              enabled: true,
              gpgVerify: !!repo.gpg_key_data,
              gpgKeyData: repo.gpg_key_data || "",
              subset: repo.subset || "",
              defaultBranch: repo.default_branch || "",
              collectionId: repo.collection_id || "",
              homepage: repo.homepage || "",
              comment: repo.comment || "",
            }));
          }}
        >
          Add remote
        </Button>
        <Button variant="link" onClick={onClose}>Cancel</Button>
      </ModalFooter>
    </Modal>
  );
};

/* ── small selects used per app row ── */

const StateSelect: React.FC<{ id: string; value: FlatpakAppState; onChange: (v: FlatpakAppState) => void; isDisabled?: boolean; ariaLabel: string }> =
  ({ id, value, onChange, isDisabled, ariaLabel }) => {
    const [open, setOpen] = useState(false);
    return (
      <Select
        id={id}
        isOpen={open}
        onOpenChange={setOpen}
        selected={value}
        onSelect={(_ev, val) => { onChange(val as FlatpakAppState); setOpen(false); }}
        toggle={(ref: React.Ref<MenuToggleElement>) => (
          <MenuToggle ref={ref} onClick={() => setOpen((v) => !v)} isExpanded={open} isDisabled={isDisabled} aria-label={ariaLabel} size="sm">
            {APP_STATE_OPTIONS.find((o) => o.value === value)?.label ?? value}
          </MenuToggle>
        )}
      >
        <SelectList>
          {APP_STATE_OPTIONS.map((o) => (
            <SelectOption key={o.value} value={o.value} description={o.description}>{o.label}</SelectOption>
          ))}
        </SelectList>
      </Select>
    );
  };

const ANY_REMOTE = "__any__";

const RemoteSelect: React.FC<{ id: string; value: string; options: string[]; onChange: (v: string) => void; isDisabled?: boolean; ariaLabel: string }> =
  ({ id, value, options, onChange, isDisabled, ariaLabel }) => {
    const [open, setOpen] = useState(false);
    const all = value && !options.includes(value) ? [value, ...options] : options;
    return (
      <Select
        id={id}
        isOpen={open}
        onOpenChange={setOpen}
        selected={value || ANY_REMOTE}
        onSelect={(_ev, val) => { onChange(val === ANY_REMOTE ? "" : String(val ?? "")); setOpen(false); }}
        toggle={(ref: React.Ref<MenuToggleElement>) => (
          <MenuToggle ref={ref} onClick={() => setOpen((v) => !v)} isExpanded={open} isDisabled={isDisabled} aria-label={ariaLabel} size="sm">
            {value || "any"}
          </MenuToggle>
        )}
      >
        <SelectList>
          <SelectOption value={ANY_REMOTE} description="Let flatpak pick by remote priority">any</SelectOption>
          {all.map((r) => (
            <SelectOption key={r} value={r}>{r}</SelectOption>
          ))}
        </SelectList>
      </Select>
    );
  };

const ScopeSelect: React.FC<{ id: string; value: FlatpakScope; onChange: (v: FlatpakScope) => void; isDisabled?: boolean; ariaLabel: string }> =
  ({ id, value, onChange, isDisabled, ariaLabel }) => {
    const [open, setOpen] = useState(false);
    return (
      <Select
        id={id}
        isOpen={open}
        onOpenChange={setOpen}
        selected={value}
        onSelect={(_ev, val) => { onChange(val as FlatpakScope); setOpen(false); }}
        toggle={(ref: React.Ref<MenuToggleElement>) => (
          <MenuToggle ref={ref} onClick={() => setOpen((v) => !v)} isExpanded={open} isDisabled={isDisabled} aria-label={ariaLabel} size="sm">
            {value === "FLATPAK_SCOPE_USER" ? "User" : "System"}
          </MenuToggle>
        )}
      >
        <SelectList>
          <SelectOption value="FLATPAK_SCOPE_SYSTEM" description="System-wide installation (/var/lib/flatpak)">System</SelectOption>
          <SelectOption value="FLATPAK_SCOPE_USER" isDisabled description="Per-user installs are accepted by the server but not yet enforced by agents">
            User (not yet enforced)
          </SelectOption>
        </SelectList>
      </Select>
    );
  };

/* ── main component ── */

export const FlatpakPolicyEditor: React.FC<FlatpakPolicyEditorProps> = ({ contentRaw, onChange, isDisabled, onValidityChange }) => {
  const idp = useId();
  const content = useMemo(() => parseFlatpakContent(contentRaw), [contentRaw]);
  const push = useCallback((next: FlatpakContent) => onChange(serializeFlatpakContent(next)), [onChange]);

  const [repos, setRepos] = useState<FlatpakCatalogRepo[]>([]);
  const [reposError, setReposError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchFlatpakCatalogRepos()
      .then((r) => { if (!cancelled) setRepos(r); })
      .catch((e: unknown) => { if (!cancelled) setReposError(e instanceof Error ? `Catalog unavailable: ${e.message}` : "Catalog unavailable"); });
    return () => { cancelled = true; };
  }, []);

  const [addMenuOpen, setAddMenuOpen] = useState(false);
  const [remoteModal, setRemoteModal] = useState<RemoteModalState>({ kind: "closed" });
  const [fromServerOpen, setFromServerOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(!!content.installation || !!content.operationTimeoutMinutes);

  const problem = useMemo(() => validateFlatpakContent(content), [content]);
  useEffect(() => {
    onValidityChange?.(problem === null);
  }, [problem, onValidityChange]);
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);

  const remoteNames = content.remotes.map((r) => r.name);
  const listedIds = useMemo(() => new Set(content.apps.map((a) => a.appId)), [content.apps]);

  /* remotes */
  const saveRemote = (entry: FlatpakRemoteEntry) => {
    if (remoteModal.kind === "edit") {
      push({ ...content, remotes: content.remotes.map((r, i) => (i === remoteModal.index ? entry : r)) });
    } else {
      push({ ...content, remotes: [...content.remotes, entry] });
    }
    setRemoteModal({ kind: "closed" });
  };
  const removeRemote = (index: number) => push({ ...content, remotes: content.remotes.filter((_, i) => i !== index) });

  const remoteActions = (r: FlatpakRemoteEntry, index: number): IAction[] => [
    { title: "Edit", onClick: () => setRemoteModal({ kind: "edit", index }) },
    { isSeparator: true },
    { title: "Remove", isDanger: true, onClick: () => removeRemote(index) },
    ...(r.name === "flathub" ? [{ title: "Open Flathub", onClick: () => window.open("https://flathub.org/", "_blank", "noopener") }] : []),
  ];

  /* apps */
  const addFromCatalog = (app: FlatpakCatalogApp) => {
    if (listedIds.has(app.app_id)) return;
    const remoteInPolicy = remoteNames.includes(app.repo_name) ? app.repo_name : "";
    push({
      ...content,
      apps: [
        ...content.apps,
        newAppEntry({
          appId: app.app_id,
          remote: remoteInPolicy || app.repo_name,
          branch: app.branch && app.branch !== "stable" ? app.branch : "",
          displayName: app.name || "",
        }),
      ],
    });
  };
  const addById = (appId: string) => {
    if (listedIds.has(appId)) return;
    push({ ...content, apps: [...content.apps, newAppEntry({ appId })] });
  };
  const patchApp = (index: number, p: Partial<FlatpakAppEntry>) =>
    push({ ...content, apps: content.apps.map((a, i) => (i === index ? { ...a, ...p } : a)) });
  const removeApp = (index: number) => push({ ...content, apps: content.apps.filter((_, i) => i !== index) });

  const appActions = (a: FlatpakAppEntry, index: number): IAction[] => {
    const items: IAction[] = [];
    if (a.remote === "flathub" || (a.remote === "" && remoteNames.includes("flathub"))) {
      items.push({
        title: "Open on Flathub",
        onClick: () => window.open(`https://flathub.org/apps/${encodeURIComponent(a.appId)}`, "_blank", "noopener"),
      });
      items.push({ isSeparator: true });
    }
    items.push({ title: "Remove", isDanger: true, onClick: () => removeApp(index) });
    return items;
  };

  const intervalHours = content.autoUpdateIntervalHours || DEFAULT_AUTO_UPDATE_HOURS;
  const timeoutMin = content.operationTimeoutMinutes || DEFAULT_OPERATION_TIMEOUT_MIN;
  const isEmpty = content.remotes.length === 0 && content.apps.length === 0;

  return (
    <div>
      <LiveAlert message={problem} variant="danger" isInline style={{ marginBottom: 16 }} />

      {/* ── Remotes ── */}
      <Title headingLevel="h4" size="md">Remotes</Title>
      <p className="bor-text-secondary" style={{ margin: "0.25rem 0 0.75rem" }}>
        Remotes configured on every node this policy applies to. Copy one from the server&apos;s indexed repositories, import a <code>.flatpakrepo</code> file or define it by hand.
      </p>

      {content.remotes.length > 0 && (
        <Table aria-label="Flatpak remotes" variant="compact" style={{ marginBottom: "0.5rem" }}>
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>URL</Th>
              <Th>Subset</Th>
              <Th>Filter</Th>
              <Th><abbr title="GNU Privacy Guard">GPG</abbr></Th>
              <Th>Enabled</Th>
              {!isDisabled && <Th screenReaderText="Actions" />}
            </Tr>
          </Thead>
          <Tbody>
            {content.remotes.map((r, index) => (
              <Tr key={`${r.name}-${index}`}>
                <Td dataLabel="Name">
                  <div>{r.title || r.name}</div>
                  {r.title && <small className="bor-text-secondary" style={{ fontFamily: "monospace" }}>{r.name}</small>}
                </Td>
                <Td dataLabel="URL" style={{ wordBreak: "break-all" }}>{r.url}</Td>
                <Td dataLabel="Subset">{r.subset || "—"}</Td>
                <Td dataLabel="Filter">
                  {r.filterMode === "FLATPAK_FILTER_MODE_NONE" ? "—" : `${filterModeLabel(r.filterMode)} (${r.filterRefs.filter((x) => x.trim()).length})`}
                </Td>
                <Td dataLabel="GPG">
                  {r.gpgVerify ? (
                    <Label color="green" isCompact icon={<CheckCircleIcon />}>Verified</Label>
                  ) : (
                    <Label color="orange" isCompact icon={<ExclamationTriangleIcon />}>Verification off</Label>
                  )}
                </Td>
                <Td dataLabel="Enabled">{r.enabled ? "Yes" : "No"}</Td>
                {!isDisabled && (
                  <Td dataLabel="Actions" isActionCell>
                    <ActionsColumn
                      items={remoteActions(r, index)}
                      actionsToggle={({ onToggle, isOpen, isDisabled: dis, toggleRef }) => (
                        <MenuToggle
                          ref={toggleRef}
                          aria-label={`Actions for remote ${r.name}`}
                          variant="plain"
                          onClick={onToggle}
                          isExpanded={isOpen}
                          isDisabled={dis}
                          icon={<EllipsisVIcon />}
                        />
                      )}
                    />
                  </Td>
                )}
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}

      {!isDisabled && (
        <Dropdown
          isOpen={addMenuOpen}
          onSelect={() => setAddMenuOpen(false)}
          onOpenChange={setAddMenuOpen}
          toggle={(ref: React.Ref<MenuToggleElement>) => (
            <MenuToggle ref={ref} onClick={() => setAddMenuOpen((v) => !v)} isExpanded={addMenuOpen} variant="secondary" icon={<PlusCircleIcon />}>
              Add remote
            </MenuToggle>
          )}
        >
          <DropdownList>
            <DropdownItem key="server" onClick={() => setFromServerOpen(true)} description="Copy a repository indexed by this server">
              From server repository…
            </DropdownItem>
            <DropdownItem key="import" onClick={() => setRemoteModal({ kind: "add", startWithImport: true })} description="Fetch name, URL and signing key from a .flatpakrepo file">
              From .flatpakrepo URL…
            </DropdownItem>
            <DropdownItem key="custom" onClick={() => setRemoteModal({ kind: "add", startWithImport: false })} description="Enter every field yourself">
              Custom…
            </DropdownItem>
          </DropdownList>
        </Dropdown>
      )}

      {/* ── Applications ── */}
      <Title headingLevel="h4" size="md" style={{ marginTop: "1.5rem" }}>Applications</Title>
      <p className="bor-text-secondary" style={{ margin: "0.25rem 0 0.75rem" }}>
        Desired state of each application. <strong>Present</strong> installs once, <strong>Latest</strong> also updates on every sync, <strong>Absent</strong> uninstalls.
      </p>

      {!isDisabled && (
        <FlatpakAppPicker
          repos={repos}
          reposError={reposError}
          listedIds={listedIds}
          onAddFromCatalog={addFromCatalog}
          onAddById={addById}
          isDisabled={isDisabled}
        />
      )}

      {content.apps.length > 0 && (
        <Table aria-label="Applications in this policy" variant="compact" style={{ marginTop: "1rem" }}>
          <Thead>
            <Tr>
              <Th>Application</Th>
              <Th>State</Th>
              <Th>Remote</Th>
              <Th>Branch</Th>
              <Th>Scope</Th>
              <Th>Optional</Th>
              {!isDisabled && <Th screenReaderText="Actions" />}
            </Tr>
          </Thead>
          <Tbody>
            {content.apps.map((a, index) => {
              const remoteMissing = a.remote !== "" && !remoteNames.includes(a.remote);
              return (
                <Tr key={`${a.appId}-${a.scope}`}>
                  <Td dataLabel="Application">
                    <div>{a.displayName || a.appId}</div>
                    {a.displayName && <small className="bor-text-secondary" style={{ fontFamily: "monospace" }}>{a.appId}</small>}
                  </Td>
                  <Td dataLabel="State">
                    <StateSelect id={`${idp}-state-${index}`} value={a.state} onChange={(v) => patchApp(index, { state: v, deleteData: v === "FLATPAK_APP_STATE_ABSENT" ? a.deleteData : false })} isDisabled={isDisabled} ariaLabel={`Desired state for ${a.appId}`} />
                    {a.state === "FLATPAK_APP_STATE_ABSENT" && (
                      <div style={{ marginTop: "0.25rem" }}>
                        <Checkbox
                          id={`${idp}-deldata-${index}`}
                          label="Also delete app data"
                          isChecked={a.deleteData}
                          onChange={(_ev, c) => patchApp(index, { deleteData: c })}
                          isDisabled={isDisabled}
                        />
                      </div>
                    )}
                  </Td>
                  <Td dataLabel="Remote">
                    <RemoteSelect id={`${idp}-remote-${index}`} value={a.remote} options={remoteNames} onChange={(v) => patchApp(index, { remote: v })} isDisabled={isDisabled} ariaLabel={`Remote for ${a.appId}`} />
                    {remoteMissing && (
                      <div style={{ marginTop: "0.25rem" }}>
                        <Tooltip content="This remote is not defined in the policy, so it must already exist on the node (for example a distribution-provided remote).">
                          <Label color="orange" isCompact icon={<ExclamationTriangleIcon />}>Remote not in policy</Label>
                        </Tooltip>
                      </div>
                    )}
                  </Td>
                  <Td dataLabel="Branch">
                    <TextInput
                      id={`${idp}-branch-${index}`}
                      value={a.branch}
                      onChange={(_ev, v) => patchApp(index, { branch: v })}
                      placeholder="default"
                      isDisabled={isDisabled}
                      aria-label={`Branch for ${a.appId}`}
                      style={{ maxWidth: "8rem" }}
                    />
                  </Td>
                  <Td dataLabel="Scope">
                    <ScopeSelect id={`${idp}-scope-${index}`} value={a.scope} onChange={(v) => patchApp(index, { scope: v })} isDisabled={isDisabled} ariaLabel={`Installation scope for ${a.appId}`} />
                  </Td>
                  <Td dataLabel="Optional">
                    <Switch
                      id={`${idp}-optional-${index}`}
                      aria-label={`Optional: ${a.appId}`}
                      isChecked={a.optional}
                      onChange={(_ev, c) => patchApp(index, { optional: c })}
                      isDisabled={isDisabled}
                    />
                  </Td>
                  {!isDisabled && (
                    <Td dataLabel="Actions" isActionCell>
                      <ActionsColumn
                        items={appActions(a, index)}
                        actionsToggle={({ onToggle, isOpen, isDisabled: dis, toggleRef }) => (
                          <MenuToggle
                            ref={toggleRef}
                            aria-label={`Actions for ${a.appId}`}
                            variant="plain"
                            onClick={onToggle}
                            isExpanded={isOpen}
                            isDisabled={dis}
                            icon={<EllipsisVIcon />}
                          />
                        )}
                      />
                    </Td>
                  )}
                </Tr>
              );
            })}
          </Tbody>
        </Table>
      )}
      <FormHelperText style={{ marginTop: "0.25rem" }}>
        <HelperText>
          <HelperTextItem>Optional apps that a remote does not offer are reported as inapplicable instead of non-compliant.</HelperTextItem>
        </HelperText>
      </FormHelperText>

      {/* ── Options ── */}
      <Title headingLevel="h4" size="md" style={{ marginTop: "1.5rem" }}>Options</Title>
      <Form style={{ marginTop: "0.5rem" }}>
        <FormGroup fieldId={`${idp}-autoupdate`}>
          <Switch
            id={`${idp}-autoupdate`}
            label="Update installed Flatpak apps automatically"
            isChecked={content.autoUpdate}
            onChange={(_ev, c) => push({ ...content, autoUpdate: c })}
            isDisabled={isDisabled}
          />
        </FormGroup>
        {content.autoUpdate && (
          <FormGroup label="Update interval (hours)" fieldId={`${idp}-interval`}>
            <NumberInput
              id={`${idp}-interval`}
              value={intervalHours}
              min={1}
              max={168}
              onMinus={() => push({ ...content, autoUpdateIntervalHours: Math.max(1, intervalHours - 1) })}
              onPlus={() => push({ ...content, autoUpdateIntervalHours: Math.min(168, intervalHours + 1) })}
              onChange={(ev) => {
                const n = Number((ev.target as HTMLInputElement).value);
                push({ ...content, autoUpdateIntervalHours: Number.isFinite(n) && n > 0 ? Math.min(168, Math.max(1, Math.round(n))) : DEFAULT_AUTO_UPDATE_HOURS });
              }}
              inputAriaLabel="Auto-update interval in hours"
              minusBtnAriaLabel="Decrease interval"
              plusBtnAriaLabel="Increase interval"
              isDisabled={isDisabled}
              widthChars={5}
            />
            <FormHelperText>
              <HelperText><HelperTextItem>The agent runs <code>flatpak update</code> on this schedule. 1–168 hours; default 24.</HelperTextItem></HelperText>
            </FormHelperText>
          </FormGroup>
        )}
        <FormGroup fieldId={`${idp}-unused`}>
          <Switch
            id={`${idp}-unused`}
            label="Remove unused runtimes after changes and updates"
            isChecked={content.uninstallUnused}
            onChange={(_ev, c) => push({ ...content, uninstallUnused: c })}
            isDisabled={isDisabled}
          />
        </FormGroup>

        <ExpandableSection toggleText="Advanced" isExpanded={advancedOpen} onToggle={(_ev, ex) => setAdvancedOpen(ex)}>
          <FormGroup label="Installation name" fieldId={`${idp}-installation`}>
            <TextInput
              id={`${idp}-installation`}
              value={content.installation}
              onChange={(_ev, v) => push({ ...content, installation: v })}
              placeholder="leave empty for the default system installation"
              isDisabled={isDisabled}
            />
            <FormHelperText>
              <HelperText>
                <HelperTextItem>A named system installation from <code>/etc/flatpak/installations.d</code>. Nodes without it report the policy as inapplicable.</HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>
          <FormGroup label="Operation timeout (minutes)" fieldId={`${idp}-timeout`}>
            <NumberInput
              id={`${idp}-timeout`}
              value={timeoutMin}
              min={5}
              max={240}
              onMinus={() => push({ ...content, operationTimeoutMinutes: Math.max(5, timeoutMin - 5) })}
              onPlus={() => push({ ...content, operationTimeoutMinutes: Math.min(240, timeoutMin + 5) })}
              onChange={(ev) => {
                const n = Number((ev.target as HTMLInputElement).value);
                push({ ...content, operationTimeoutMinutes: Number.isFinite(n) && n > 0 ? Math.min(240, Math.max(5, Math.round(n))) : DEFAULT_OPERATION_TIMEOUT_MIN });
              }}
              inputAriaLabel="Per-operation timeout in minutes"
              minusBtnAriaLabel="Decrease timeout"
              plusBtnAriaLabel="Increase timeout"
              isDisabled={isDisabled}
              widthChars={5}
            />
            <FormHelperText>
              <HelperText><HelperTextItem>Deadline for each install, update or uninstall. 5–240 minutes; default 30.</HelperTextItem></HelperText>
            </FormHelperText>
          </FormGroup>
        </ExpandableSection>
      </Form>

      {isEmpty && (
        <LiveAlert
          message="This Flatpak policy is currently empty. Add at least one remote or application."
          variant="info"
          isInline
          style={{ marginTop: 16 }}
        />
      )}

      <FlatpakRemoteModal
        isOpen={remoteModal.kind !== "closed"}
        initial={remoteModal.kind === "edit" ? content.remotes[remoteModal.index] : undefined}
        takenNames={remoteNames}
        startWithImport={remoteModal.kind === "add" && remoteModal.startWithImport}
        onSave={saveRemote}
        onClose={() => setRemoteModal({ kind: "closed" })}
      />
      <AddFromServerModal
        isOpen={fromServerOpen}
        repos={repos}
        takenNames={remoteNames}
        onAdd={(entry) => { push({ ...content, remotes: [...content.remotes, entry] }); setFromServerOpen(false); }}
        onClose={() => setFromServerOpen(false)}
      />
    </div>
  );
};
