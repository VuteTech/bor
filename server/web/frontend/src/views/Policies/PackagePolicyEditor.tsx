// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useCallback, useId } from "react";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  Form,
  FormGroup,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectList,
  SelectOption,
  Switch,
  Tab,
  Tabs,
  TabTitleText,
  TextInput,
  Title,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";

import { LiveAlert } from "../../components/LiveAlert";

/* ── types ── */

type RepoType =
  | "REPOSITORY_TYPE_UNSPECIFIED"
  | "REPOSITORY_TYPE_APT_DEB822"
  | "REPOSITORY_TYPE_DNF"
  | "REPOSITORY_TYPE_ZYPPER";

type PkgState =
  | "PACKAGE_STATE_UNSPECIFIED"
  | "PACKAGE_STATE_PRESENT"
  | "PACKAGE_STATE_ABSENT"
  | "PACKAGE_STATE_LATEST";

interface RepoEntry {
  id: string;
  name: string;
  type: RepoType;
  enabled: boolean;
  aptUri?: string;
  aptSuites?: string;
  aptComponents?: string;
  aptArchitectures?: string;
  baseurl?: string;
  mirrorlist?: string;
  metadataExpire?: number;
  zypperPriority?: number;
  gpgCheck: boolean;
  gpgKeyData?: string; // base64-encoded bytes
}

interface PkgEntry {
  name: string;
  state: PkgState;
  version?: string;
  optional?: boolean;
}

interface PackagePolicyContent {
  repositories?: RepoEntry[];
  packages?: PkgEntry[];
  updateCache?: boolean;
  allowDowngrade?: boolean;
}

const DEFAULT_REPO: RepoEntry = {
  id: "",
  name: "",
  type: "REPOSITORY_TYPE_APT_DEB822",
  enabled: true,
  gpgCheck: true,
};

const DEFAULT_PKG: PkgEntry = {
  name: "",
  state: "PACKAGE_STATE_PRESENT",
};

const REPO_TYPE_LABELS: { value: RepoType; label: string }[] = [
  { value: "REPOSITORY_TYPE_APT_DEB822", label: "APT (deb822)" },
  { value: "REPOSITORY_TYPE_DNF",        label: "DNF / YUM" },
  { value: "REPOSITORY_TYPE_ZYPPER",     label: "Zypper" },
];

const PKG_STATE_LABELS: { value: PkgState; label: string; description: string }[] = [
  { value: "PACKAGE_STATE_PRESENT", label: "Present", description: "Install if not present" },
  { value: "PACKAGE_STATE_ABSENT",  label: "Absent",  description: "Remove if installed" },
  { value: "PACKAGE_STATE_LATEST",  label: "Latest",  description: "Install or upgrade to latest" },
];

/* ── helpers ── */

function parseContent(raw: string): PackagePolicyContent {
  try {
    const parsed = JSON.parse(raw || "{}") as Partial<PackagePolicyContent>;
    return {
      repositories: Array.isArray(parsed.repositories) ? parsed.repositories : [],
      packages: Array.isArray(parsed.packages) ? parsed.packages : [],
      updateCache: parsed.updateCache ?? true,
      allowDowngrade: parsed.allowDowngrade ?? false,
    };
  } catch {
    return { repositories: [], packages: [], updateCache: true, allowDowngrade: false };
  }
}

function serializeContent(content: PackagePolicyContent): string {
  return JSON.stringify(content, null, 2);
}

function gpgKeySizeKiB(b64: string): number {
  try {
    return Math.round(atob(b64).length / 1024);
  } catch {
    return 0;
  }
}

/* ── RepoCard sub-component ── */

interface RepoCardProps {
  repo: RepoEntry;
  idx: number;
  onUpdate: (patch: Partial<RepoEntry>) => void;
  onRemove: () => void;
  isDisabled?: boolean;
  idPrefix: string;
}

const RepoCard: React.FC<RepoCardProps> = ({ repo, idx, onUpdate, onRemove, isDisabled, idPrefix }) => {
  const [expanded, setExpanded] = useState(repo.id === "");
  const [typeOpen, setTypeOpen] = useState(false);

  const isAPT = repo.type === "REPOSITORY_TYPE_APT_DEB822";
  const isZypper = repo.type === "REPOSITORY_TYPE_ZYPPER";
  const currentTypeLabel = REPO_TYPE_LABELS.find(r => r.value === repo.type)?.label ?? repo.type;

  const handleGPGKeyUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (ev) => {
      const result = ev.target?.result;
      if (typeof result === "string") {
        // FileReader produces "data:<mime>;base64,<data>" — extract the payload
        const b64 = result.split(",")[1] ?? "";
        onUpdate({ gpgKeyData: b64 });
      }
    };
    reader.readAsDataURL(file);
  };

  return (
    <Card style={{ marginBottom: "1rem" }}>
      <CardTitle>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <button
            type="button"
            onClick={() => setExpanded(v => !v)}
            style={{
              background: "none",
              border: "none",
              cursor: "pointer",
              padding: 0,
              font: "inherit",
              textAlign: "left",
              flex: 1,
            }}
            aria-expanded={expanded}
            aria-controls={`${idPrefix}-body`}
          >
            <Title headingLevel="h4" size="md">
              {expanded ? "▾" : "▸"}{" "}
              {repo.id ? `${repo.id} — ${currentTypeLabel}` : `Repository ${idx + 1} (new)`}
            </Title>
          </button>
          <Button
            variant="plain"
            onClick={onRemove}
            isDisabled={isDisabled}
            aria-label={`Remove repository ${idx + 1}`}
            style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
          >
            <TrashIcon />
          </Button>
        </div>
      </CardTitle>

      {expanded && (
        <CardBody id={`${idPrefix}-body`}>
          <Form isHorizontal>
            <FormGroup label="ID" fieldId={`${idPrefix}-id`} isRequired>
              <TextInput
                id={`${idPrefix}-id`}
                value={repo.id}
                onChange={(_ev, v) => onUpdate({ id: v })}
                placeholder="e.g. google-chrome"
                isDisabled={isDisabled}
                aria-label="Repository ID"
              />
            </FormGroup>

            <FormGroup label="Name" fieldId={`${idPrefix}-name`}>
              <TextInput
                id={`${idPrefix}-name`}
                value={repo.name}
                onChange={(_ev, v) => onUpdate({ name: v })}
                placeholder="e.g. Google Chrome"
                isDisabled={isDisabled}
                aria-label="Repository display name"
              />
            </FormGroup>

            <FormGroup label="Type" fieldId={`${idPrefix}-type`} isRequired>
              <Select
                id={`${idPrefix}-type`}
                isOpen={typeOpen}
                onOpenChange={setTypeOpen}
                selected={repo.type}
                onSelect={(_ev, val) => { onUpdate({ type: val as RepoType }); setTypeOpen(false); }}
                toggle={(ref: React.Ref<MenuToggleElement>) => (
                  <MenuToggle
                    ref={ref}
                    onClick={() => setTypeOpen(v => !v)}
                    isExpanded={typeOpen}
                    isDisabled={isDisabled}
                    aria-label="Select repository type"
                  >
                    {currentTypeLabel}
                  </MenuToggle>
                )}
              >
                <SelectList>
                  {REPO_TYPE_LABELS.map(r => (
                    <SelectOption key={r.value} value={r.value}>{r.label}</SelectOption>
                  ))}
                </SelectList>
              </Select>
            </FormGroup>

            <FormGroup label="Enabled" fieldId={`${idPrefix}-enabled`}>
              <Switch
                id={`${idPrefix}-enabled`}
                isChecked={repo.enabled}
                onChange={(_ev, checked) => onUpdate({ enabled: checked })}
                isDisabled={isDisabled}
                aria-label="Repository enabled"
              />
            </FormGroup>

            {/* APT-specific */}
            {isAPT && (
              <>
                <FormGroup label="URI" fieldId={`${idPrefix}-apt-uri`} isRequired>
                  <TextInput
                    id={`${idPrefix}-apt-uri`}
                    value={repo.aptUri ?? ""}
                    onChange={(_ev, v) => onUpdate({ aptUri: v })}
                    placeholder="https://dl.google.com/linux/chrome/deb/"
                    isDisabled={isDisabled}
                    aria-label="APT repository URI"
                  />
                </FormGroup>
                <FormGroup label="Suites" fieldId={`${idPrefix}-apt-suites`} isRequired>
                  <TextInput
                    id={`${idPrefix}-apt-suites`}
                    value={repo.aptSuites ?? ""}
                    onChange={(_ev, v) => onUpdate({ aptSuites: v })}
                    placeholder="stable"
                    isDisabled={isDisabled}
                    aria-label="APT suites (distribution)"
                  />
                </FormGroup>
                <FormGroup label="Components" fieldId={`${idPrefix}-apt-components`} isRequired>
                  <TextInput
                    id={`${idPrefix}-apt-components`}
                    value={repo.aptComponents ?? ""}
                    onChange={(_ev, v) => onUpdate({ aptComponents: v })}
                    placeholder="main"
                    isDisabled={isDisabled}
                    aria-label="APT components"
                  />
                </FormGroup>
                <FormGroup label="Architectures" fieldId={`${idPrefix}-apt-arch`}>
                  <TextInput
                    id={`${idPrefix}-apt-arch`}
                    value={repo.aptArchitectures ?? ""}
                    onChange={(_ev, v) => onUpdate({ aptArchitectures: v || undefined })}
                    placeholder="amd64 arm64 (leave empty for all)"
                    isDisabled={isDisabled}
                    aria-label="APT architectures (space-separated)"
                  />
                </FormGroup>
              </>
            )}

            {/* DNF / Zypper shared fields */}
            {!isAPT && (
              <>
                <FormGroup label="Base URL" fieldId={`${idPrefix}-baseurl`}>
                  <TextInput
                    id={`${idPrefix}-baseurl`}
                    value={repo.baseurl ?? ""}
                    onChange={(_ev, v) => onUpdate({ baseurl: v || undefined })}
                    placeholder="https://repo.example.com/$releasever/$basearch"
                    isDisabled={isDisabled}
                    aria-label="Repository base URL"
                  />
                </FormGroup>
                <FormGroup label="Mirror list" fieldId={`${idPrefix}-mirrorlist`}>
                  <TextInput
                    id={`${idPrefix}-mirrorlist`}
                    value={repo.mirrorlist ?? ""}
                    onChange={(_ev, v) => onUpdate({ mirrorlist: v || undefined })}
                    placeholder="https://mirrors.example.com/list (optional)"
                    isDisabled={isDisabled}
                    aria-label="Mirror list URL"
                  />
                </FormGroup>
                <FormGroup label="Metadata expire (s)" fieldId={`${idPrefix}-meta-expire`}>
                  <TextInput
                    id={`${idPrefix}-meta-expire`}
                    type="number"
                    value={repo.metadataExpire !== undefined ? String(repo.metadataExpire) : ""}
                    onChange={(_ev, v) => onUpdate({ metadataExpire: v ? parseInt(v, 10) : undefined })}
                    placeholder="21600"
                    isDisabled={isDisabled}
                    aria-label="Metadata expiry in seconds"
                  />
                </FormGroup>
              </>
            )}

            {/* Zypper-only */}
            {isZypper && (
              <FormGroup label="Priority" fieldId={`${idPrefix}-priority`}>
                <TextInput
                  id={`${idPrefix}-priority`}
                  type="number"
                  value={repo.zypperPriority !== undefined ? String(repo.zypperPriority) : ""}
                  onChange={(_ev, v) => onUpdate({ zypperPriority: v ? parseInt(v, 10) : undefined })}
                  placeholder="99 (lower number = higher priority)"
                  isDisabled={isDisabled}
                  aria-label="Zypper repository priority"
                />
              </FormGroup>
            )}

            {/* GPG */}
            <FormGroup label="GPG check" fieldId={`${idPrefix}-gpgcheck`}>
              <div style={{ display: "flex", alignItems: "center", gap: "0.75rem" }}>
                <Switch
                  id={`${idPrefix}-gpgcheck`}
                  isChecked={repo.gpgCheck}
                  onChange={(_ev, checked) => onUpdate({ gpgCheck: checked })}
                  isDisabled={isDisabled}
                  aria-label="Enable GPG signature verification"
                />
                {!repo.gpgCheck && (
                  <span style={{ color: "var(--pf-t--global--color--status--warning--100)", fontSize: "0.875rem", display: "flex", alignItems: "center", gap: "0.25rem" }}>
                    <ExclamationTriangleIcon /> GPG verification disabled — security risk
                  </span>
                )}
              </div>
            </FormGroup>

            {repo.gpgCheck && (
              <FormGroup label="GPG key file" fieldId={`${idPrefix}-gpgkey`}>
                <div>
                  {repo.gpgKeyData ? (
                    <div style={{ marginBottom: "0.4rem", fontSize: "0.875rem" }}>
                      Key uploaded ({gpgKeySizeKiB(repo.gpgKeyData)} KiB){" "}
                      <Button
                        variant="link"
                        isInline
                        onClick={() => onUpdate({ gpgKeyData: undefined })}
                        isDisabled={isDisabled}
                      >
                        Remove
                      </Button>
                    </div>
                  ) : (
                    <p style={{ fontSize: "0.875rem", color: "var(--pf-t--global--text--color--subtle)", marginBottom: "0.4rem" }}>
                      No key uploaded. Without a key file the node must already trust the signing key.
                    </p>
                  )}
                  {!isDisabled && (
                    <input
                      id={`${idPrefix}-gpgkey-upload`}
                      type="file"
                      accept=".gpg,.asc,.key"
                      onChange={handleGPGKeyUpload}
                      aria-label="Upload GPG key file (.gpg, .asc, or .key)"
                    />
                  )}
                </div>
              </FormGroup>
            )}
          </Form>
        </CardBody>
      )}
    </Card>
  );
};

/* ── PkgRow sub-component ── */

interface PkgRowProps {
  pkg: PkgEntry;
  idx: number;
  onUpdate: (patch: Partial<PkgEntry>) => void;
  onRemove: () => void;
  isDisabled?: boolean;
  idPrefix: string;
}

const PkgRow: React.FC<PkgRowProps> = ({ pkg, idx, onUpdate, onRemove, isDisabled, idPrefix }) => {
  const [stateOpen, setStateOpen] = useState(false);
  const currentStateLabel = PKG_STATE_LABELS.find(s => s.value === pkg.state)?.label ?? pkg.state;
  const versionEnabled = pkg.state === "PACKAGE_STATE_PRESENT";

  return (
    <tr>
      <td style={{ padding: "0.3rem 0.5rem" }}>
        <TextInput
          id={`${idPrefix}-name-${idx}`}
          value={pkg.name}
          onChange={(_ev, v) => onUpdate({ name: v })}
          placeholder="package-name"
          isDisabled={isDisabled}
          aria-label={`Package name ${idx + 1}`}
          style={{ minWidth: "12rem" }}
        />
      </td>
      <td style={{ padding: "0.3rem 0.5rem" }}>
        <Select
          id={`${idPrefix}-state-${idx}`}
          isOpen={stateOpen}
          onOpenChange={setStateOpen}
          selected={pkg.state}
          onSelect={(_ev, val) => {
            const newState = val as PkgState;
            const patch: Partial<PkgEntry> = { state: newState };
            if (newState !== "PACKAGE_STATE_PRESENT") patch.version = undefined;
            onUpdate(patch);
            setStateOpen(false);
          }}
          toggle={(ref: React.Ref<MenuToggleElement>) => (
            <MenuToggle
              ref={ref}
              onClick={() => setStateOpen(v => !v)}
              isExpanded={stateOpen}
              isDisabled={isDisabled}
              aria-label={`Package ${idx + 1} desired state`}
            >
              {currentStateLabel}
            </MenuToggle>
          )}
        >
          <SelectList>
            {PKG_STATE_LABELS.map(s => (
              <SelectOption key={s.value} value={s.value} description={s.description}>
                {s.label}
              </SelectOption>
            ))}
          </SelectList>
        </Select>
      </td>
      <td style={{ padding: "0.3rem 0.5rem" }}>
        <TextInput
          id={`${idPrefix}-version-${idx}`}
          value={pkg.version ?? ""}
          onChange={(_ev, v) => onUpdate({ version: v || undefined })}
          placeholder="any"
          isDisabled={isDisabled || !versionEnabled}
          aria-label={`Package ${idx + 1} version pin`}
          style={{ minWidth: "8rem" }}
        />
      </td>
      <td style={{ padding: "0.3rem 0.5rem", textAlign: "center" }}>
        <Checkbox
          id={`${idPrefix}-optional-${idx}`}
          isChecked={!!pkg.optional}
          onChange={(_ev, checked) => onUpdate({ optional: checked || undefined })}
          isDisabled={isDisabled}
          aria-label={`Package ${idx + 1} optional`}
        />
      </td>
      <td style={{ padding: "0.3rem 0.5rem" }}>
        <Button
          variant="plain"
          onClick={onRemove}
          isDisabled={isDisabled}
          aria-label={`Remove package ${idx + 1}`}
          style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
        >
          <TrashIcon />
        </Button>
      </td>
    </tr>
  );
};

/* ── main component ── */

export interface PackagePolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
}

export const PackagePolicyEditor: React.FC<PackagePolicyEditorProps> = ({
  contentRaw,
  onChange,
  isDisabled,
}) => {
  const idPrefix = useId();
  const [activeTab, setActiveTab] = useState<string | number>(0);
  const [parseError, setParseError] = useState<string | null>(null);

  const content = (() => {
    try {
      const parsed = parseContent(contentRaw);
      if (parseError) setParseError(null);
      return parsed;
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Failed to parse policy content";
      setParseError(msg);
      return { repositories: [], packages: [], updateCache: true, allowDowngrade: false };
    }
  })();

  const pushChange = useCallback(
    (updated: PackagePolicyContent) => { onChange(serializeContent(updated)); },
    [onChange],
  );

  const repos = content.repositories ?? [];
  const pkgs = content.packages ?? [];

  /* ── repo handlers ── */

  const addRepo = () => pushChange({ ...content, repositories: [...repos, { ...DEFAULT_REPO }] });
  const removeRepo = (idx: number) => pushChange({ ...content, repositories: repos.filter((_, i) => i !== idx) });
  const updateRepo = (idx: number, patch: Partial<RepoEntry>) =>
    pushChange({ ...content, repositories: repos.map((r, i) => (i === idx ? { ...r, ...patch } : r)) });

  /* ── pkg handlers ── */

  const addPkg = () => pushChange({ ...content, packages: [...pkgs, { ...DEFAULT_PKG }] });
  const removePkg = (idx: number) => pushChange({ ...content, packages: pkgs.filter((_, i) => i !== idx) });
  const updatePkg = (idx: number, patch: Partial<PkgEntry>) =>
    pushChange({ ...content, packages: pkgs.map((p, i) => (i === idx ? { ...p, ...patch } : p)) });

  return (
    <div>
      <LiveAlert message={parseError} variant="danger" style={{ marginBottom: "0.75rem" }} />

      <Tabs
        activeKey={activeTab}
        onSelect={(_ev, key) => setActiveTab(key)}
        aria-label="Package policy sections"
        style={{ marginBottom: "1rem" }}
      >
        {/* ── Repositories tab ── */}
        <Tab eventKey={0} title={<TabTitleText>Repositories ({repos.length})</TabTitleText>}>
          <div style={{ paddingTop: "1rem" }}>
            {repos.length === 0 && (
              <p style={{ color: "var(--pf-t--global--text--color--subtle)", marginBottom: "1rem" }}>
                No repositories defined. Packages will be installed from the node&apos;s existing package sources.
              </p>
            )}
            {repos.map((repo, idx) => (
              <RepoCard
                key={idx}
                repo={repo}
                idx={idx}
                onUpdate={(patch) => updateRepo(idx, patch)}
                onRemove={() => removeRepo(idx)}
                isDisabled={isDisabled}
                idPrefix={`${idPrefix}-repo-${idx}`}
              />
            ))}
            <Button
              variant="secondary"
              icon={<PlusCircleIcon />}
              onClick={addRepo}
              isDisabled={isDisabled}
            >
              Add Repository
            </Button>
          </div>
        </Tab>

        {/* ── Packages tab ── */}
        <Tab eventKey={1} title={<TabTitleText>Packages ({pkgs.length})</TabTitleText>}>
          <div style={{ paddingTop: "1rem" }}>
            {pkgs.length === 0 ? (
              <p style={{ color: "var(--pf-t--global--text--color--subtle)", marginBottom: "1rem" }}>
                No packages defined yet. Click &quot;Add Package&quot; to begin.
              </p>
            ) : (
              <div style={{ overflowX: "auto" }}>
                <table style={{ width: "100%", borderCollapse: "collapse" }}>
                  <thead>
                    <tr style={{ borderBottom: "2px solid var(--pf-t--global--border--color--default)" }}>
                      <th style={{ textAlign: "left", padding: "0.4rem 0.5rem", fontWeight: 600 }}>Package name</th>
                      <th style={{ textAlign: "left", padding: "0.4rem 0.5rem", fontWeight: 600 }}>Desired state</th>
                      <th style={{ textAlign: "left", padding: "0.4rem 0.5rem", fontWeight: 600 }}>Version pin</th>
                      <th style={{ textAlign: "center", padding: "0.4rem 0.5rem", fontWeight: 600 }}>Optional</th>
                      <th style={{ padding: "0.4rem 0.5rem" }}></th>
                    </tr>
                  </thead>
                  <tbody>
                    {pkgs.map((pkg, idx) => (
                      <PkgRow
                        key={idx}
                        pkg={pkg}
                        idx={idx}
                        onUpdate={(patch) => updatePkg(idx, patch)}
                        onRemove={() => removePkg(idx)}
                        isDisabled={isDisabled}
                        idPrefix={`${idPrefix}-pkg`}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div style={{ marginTop: "1rem" }}>
              <Button
                variant="secondary"
                icon={<PlusCircleIcon />}
                onClick={addPkg}
                isDisabled={isDisabled}
              >
                Add Package
              </Button>
            </div>
          </div>
        </Tab>

        {/* ── Options tab ── */}
        <Tab eventKey={2} title={<TabTitleText>Options</TabTitleText>}>
          <div style={{ paddingTop: "1rem" }}>
            <Form>
              <FormGroup label="Refresh package cache" fieldId={`${idPrefix}-update-cache`}>
                <Switch
                  id={`${idPrefix}-update-cache`}
                  label="Refresh repository metadata after writing repo files (equivalent to apt-get update)"
                  isChecked={content.updateCache ?? true}
                  onChange={(_ev, checked) => pushChange({ ...content, updateCache: checked })}
                  isDisabled={isDisabled}
                />
              </FormGroup>
              <FormGroup label="Allow downgrade" fieldId={`${idPrefix}-allow-downgrade`}>
                <Switch
                  id={`${idPrefix}-allow-downgrade`}
                  label="Allow downgrading packages when installing a pinned version"
                  isChecked={content.allowDowngrade ?? false}
                  onChange={(_ev, checked) => pushChange({ ...content, allowDowngrade: checked })}
                  isDisabled={isDisabled}
                />
              </FormGroup>
            </Form>
          </div>
        </Tab>
      </Tabs>
    </div>
  );
};
