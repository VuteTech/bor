// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useCallback, useId, useRef } from "react";
import { authHeaders } from "../../apiClient/authApi";
import {
  Button,
  Card,
  CardBody,
  CardTitle,
  Checkbox,
  ExpandableSection,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  MenuToggle,
  MenuToggleElement,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Radio,
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
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { SearchableSelect } from "../../components/SearchableSelect";

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

/* ── Ubuntu PPA constants ── */

const UBUNTU_CODENAMES: { value: string; label: string }[] = [
  { value: "plucky",   label: "plucky   (25.04)" },
  { value: "noble",    label: "noble    (24.04 LTS)" },
  { value: "oracular", label: "oracular (24.10)" },
  { value: "jammy",    label: "jammy    (22.04 LTS)" },
  { value: "focal",    label: "focal    (20.04 LTS)" },
  { value: "bionic",   label: "bionic   (18.04 LTS)" },
];

/* ── Fedora COPR constants and helpers ── */

const FEDORA_CHROOTS: { value: string; label: string }[] = [
  { value: "fedora-44-x86_64",      label: "fedora-44      (x86_64)" },
  { value: "fedora-43-x86_64",      label: "fedora-43      (x86_64)" },
  { value: "fedora-42-x86_64",      label: "fedora-42      (x86_64)" },
  { value: "fedora-41-x86_64",      label: "fedora-41      (x86_64)" },
  { value: "fedora-rawhide-x86_64", label: "fedora-rawhide (x86_64)" },
  { value: "epel-10-x86_64",        label: "epel-10 / RHEL 10 (x86_64)" },
  { value: "epel-9-x86_64",         label: "epel-9  / RHEL 9  (x86_64)" },
  { value: "epel-8-x86_64",         label: "epel-8  / RHEL 8  (x86_64)" },
  { value: "fedora-44-aarch64",     label: "fedora-44      (aarch64)" },
  { value: "fedora-43-aarch64",     label: "fedora-43      (aarch64)" },
  { value: "fedora-42-aarch64",     label: "fedora-42      (aarch64)" },
  { value: "epel-9-aarch64",        label: "epel-9  / RHEL 9  (aarch64)" },
];

interface ParsedCOPR {
  owner: string;
  project: string;
}

function parseCOPRAddress(input: string): ParsedCOPR | null {
  const cleaned = input.trim().replace(/^copr:/i, "");
  const parts = cleaned.split("/");
  if (parts.length !== 2) return null;
  const [owner, project] = parts.map(s => s.trim());
  if (!owner || !project) return null;
  return { owner, project };
}

/* ── YMP import types ── */

interface YMPImportRepo {
  id: string;
  name: string;
  type: string;
  enabled: boolean;
  baseurl: string;
  gpgCheck: boolean;
}

interface YMPImportPackage {
  name: string;
  state: string;
}

interface YMPImportGroup {
  distVersion: string;
  repositories: YMPImportRepo[];
  packages: YMPImportPackage[];
  warnings?: string[];
}

/* ── PPA helpers ── */

interface ParsedPPA {
  owner: string;
  ppaname: string;
}

function parsePPAAddress(input: string): ParsedPPA | null {
  const cleaned = input.trim().replace(/^ppa:/, "");
  const parts = cleaned.split("/");
  if (parts.length !== 2) return null;
  const [owner, ppaname] = parts.map(s => s.trim());
  if (!owner || !ppaname) return null;
  return { owner, ppaname };
}

function ppaToDep822ID(owner: string, ppaname: string): string {
  // Produce a valid repo ID: lowercase, hyphens only.
  return `ppa-${owner}-${ppaname}`.toLowerCase().replace(/[^a-z0-9-]/g, "-");
}


interface PPAFetchResult {
  repo: RepoEntry;
  warning?: string;
}

async function fetchPPAInfo(owner: string, ppaname: string, suite: string): Promise<PPAFetchResult> {
  const id = ppaToDep822ID(owner, ppaname);

  // Fetch PPA info through the server-side proxy (/api/v1/ppa-info) so that
  // requests to Launchpad and the Ubuntu keyserver are not subject to browser CORS.
  let proxyData: { uri?: string; fingerprint?: string; gpgKeyData?: string; warning?: string } = {};
  try {
    const resp = await fetch(
      `/api/v1/ppa-info?owner=${encodeURIComponent(owner)}&ppa=${encodeURIComponent(ppaname)}`,
      { headers: authHeaders() },
    );
    if (resp.ok) {
      proxyData = (await resp.json()) as typeof proxyData;
    }
  } catch {
    // Server unreachable — fall through with empty data.
  }

  const uri = proxyData.uri ?? `https://ppa.launchpadcontent.net/${owner}/${ppaname}/ubuntu`;

  const base: RepoEntry = {
    id,
    name: `PPA ${owner}/${ppaname}`,
    type: "REPOSITORY_TYPE_APT_DEB822",
    enabled: true,
    aptUri: uri,
    aptSuites: suite,
    aptComponents: "main",
    gpgCheck: !!proxyData.gpgKeyData,
    gpgKeyData: proxyData.gpgKeyData,
  };

  return { repo: base, warning: proxyData.warning };
}

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

/* ── AddPPAModal sub-component ── */

interface AddPPAModalProps {
  isOpen: boolean;
  onClose: () => void;
  onAdd: (repo: RepoEntry, warning?: string) => void;
  triggerRef: React.RefObject<HTMLButtonElement | null>;
}

type FetchState = "idle" | "fetching" | "done";

const AddPPAModal: React.FC<AddPPAModalProps> = ({ isOpen, onClose, onAdd, triggerRef }) => {
  const idPrefix = useId();
  const [ppaAddress, setPPAAddress] = useState("");
  const [suiteOpen, setSuiteOpen] = useState(false);
  const [suite, setSuite] = useState("noble");
  const [customSuite, setCustomSuite] = useState("");
  const [fetchState, setFetchState] = useState<FetchState>("idle");
  const [fetchError, setFetchError] = useState<string | null>(null);

  const parsed = parsePPAAddress(ppaAddress);
  const effectiveSuite = suite === "_custom" ? customSuite.trim() : suite;
  const canSubmit = parsed !== null && effectiveSuite !== "" && fetchState !== "fetching";

  const currentSuiteLabel =
    suite === "_custom"
      ? `Custom: ${customSuite || "…"}`
      : (UBUNTU_CODENAMES.find(c => c.value === suite)?.label ?? suite);

  const handleClose = () => {
    setPPAAddress("");
    setSuite("noble");
    setCustomSuite("");
    setFetchState("idle");
    setFetchError(null);
    onClose();
    triggerRef.current?.focus();
  };

  const handleSubmit = async () => {
    if (!parsed || !effectiveSuite) return;
    setFetchState("fetching");
    setFetchError(null);
    try {
      const result = await fetchPPAInfo(parsed.owner, parsed.ppaname, effectiveSuite);
      setFetchState("done");
      onAdd(result.repo, result.warning);
      handleClose();
    } catch (e) {
      setFetchState("idle");
      setFetchError(e instanceof Error ? e.message : "Unexpected error");
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      aria-labelledby={`${idPrefix}-ppa-title`}
      variant="small"
    >
      <ModalHeader title="Add Ubuntu PPA" labelId={`${idPrefix}-ppa-title`} />
      <ModalBody>
        <Form>
          <FormGroup label="PPA address" fieldId={`${idPrefix}-ppa-addr`} isRequired>
            <TextInput
              id={`${idPrefix}-ppa-addr`}
              value={ppaAddress}
              onChange={(_ev, v) => { setPPAAddress(v); setFetchState("idle"); setFetchError(null); }}
              placeholder="ppa:owner/name"
              aria-label="PPA address"
              aria-invalid={ppaAddress !== "" && parsed === null ? true : undefined}
              aria-describedby={ppaAddress !== "" && parsed === null ? `${idPrefix}-ppa-addr-err` : undefined}
              autoFocus
            />
            <FormHelperText>
              <HelperText>
                {ppaAddress !== "" && parsed === null ? (
                  <HelperTextItem
                    id={`${idPrefix}-ppa-addr-err`}
                    variant="error"
                  >
                    Enter a PPA address in the format <code>ppa:owner/name</code>
                  </HelperTextItem>
                ) : (
                  <HelperTextItem>
                    E.g. <code>ppa:graphics-drivers/ppa</code> or <code>ppa:ondrej/php</code>
                  </HelperTextItem>
                )}
              </HelperText>
            </FormHelperText>
          </FormGroup>

          <FormGroup label="Ubuntu codename" fieldId={`${idPrefix}-ppa-suite`} isRequired>
            <Select
              id={`${idPrefix}-ppa-suite`}
              isOpen={suiteOpen}
              onOpenChange={setSuiteOpen}
              selected={suite}
              onSelect={(_ev, val) => { setSuite(val as string); setSuiteOpen(false); }}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle
                  ref={ref}
                  onClick={() => setSuiteOpen(v => !v)}
                  isExpanded={suiteOpen}
                  aria-label="Select Ubuntu codename"
                  style={{ width: "100%" }}
                >
                  {currentSuiteLabel}
                </MenuToggle>
              )}
            >
              <SelectList>
                {UBUNTU_CODENAMES.map(c => (
                  <SelectOption key={c.value} value={c.value}>{c.label}</SelectOption>
                ))}
                <SelectOption key="_custom" value="_custom">Custom…</SelectOption>
              </SelectList>
            </Select>
            {suite === "_custom" && (
              <TextInput
                id={`${idPrefix}-ppa-suite-custom`}
                value={customSuite}
                onChange={(_ev, v) => setCustomSuite(v)}
                placeholder="e.g. mantic"
                aria-label="Custom Ubuntu codename"
                style={{ marginTop: "0.4rem" }}
              />
            )}
            <FormHelperText>
              <HelperText>
                <HelperTextItem>
                  The Ubuntu release codename installed on target nodes.
                </HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>

          <LiveAlert
            message={fetchError}
            variant="danger"
            style={{ marginTop: "0.25rem" }}
          />
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button
          variant="primary"
          onClick={handleSubmit}
          isDisabled={!canSubmit}
          isLoading={fetchState === "fetching"}
          icon={fetchState === "fetching" ? <Spinner size="sm" aria-label="Fetching PPA info" /> : undefined}
        >
          {fetchState === "fetching" ? "Fetching…" : "Add PPA"}
        </Button>
        <Button variant="link" onClick={handleClose} isDisabled={fetchState === "fetching"}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};

/* ── AddCOPRModal sub-component ── */

interface AddCOPRModalProps {
  isOpen: boolean;
  onClose: () => void;
  onAdd: (repo: RepoEntry, warning?: string) => void;
  triggerRef: React.RefObject<HTMLButtonElement | null>;
}

const AddCOPRModal: React.FC<AddCOPRModalProps> = ({ isOpen, onClose, onAdd, triggerRef }) => {
  const idPrefix = useId();
  const [coprAddress, setCOPRAddress] = useState("");
  const [chroot, setChroot] = useState("fedora-42-x86_64");
  const [customChroot, setCustomChroot] = useState("");
  const [fetchState, setFetchState] = useState<FetchState>("idle");
  const [fetchError, setFetchError] = useState<string | null>(null);

  const parsed = parseCOPRAddress(coprAddress);
  const effectiveChroot = chroot === "_custom" ? customChroot.trim() : chroot;
  const canSubmit = parsed !== null && effectiveChroot !== "" && fetchState !== "fetching";

  const handleClose = () => {
    setCOPRAddress("");
    setChroot("fedora-42-x86_64");
    setCustomChroot("");
    setFetchState("idle");
    setFetchError(null);
    onClose();
    triggerRef.current?.focus();
  };

  const handleSubmit = async () => {
    if (!parsed || !effectiveChroot) return;
    setFetchState("fetching");
    setFetchError(null);
    try {
      const resp = await fetch(
        `/api/v1/copr-info?owner=${encodeURIComponent(parsed.owner)}&project=${encodeURIComponent(parsed.project)}&chroot=${encodeURIComponent(effectiveChroot)}`,
        { headers: authHeaders() },
      );
      if (!resp.ok) {
        const text = await resp.text();
        setFetchState("idle");
        setFetchError(text.trim() || "Failed to fetch COPR info.");
        return;
      }
      const data = (await resp.json()) as { id?: string; baseurl?: string; gpgKeyData?: string; warning?: string };
      setFetchState("done");

      const repo: RepoEntry = {
        id: data.id ?? `copr-${parsed.owner.replace(/^@/, "")}-${parsed.project}`.toLowerCase().replace(/[^a-z0-9_-]/g, "-"),
        name: `COPR ${parsed.owner}/${parsed.project}`,
        type: "REPOSITORY_TYPE_DNF",
        enabled: true,
        baseurl: data.baseurl ?? "",
        gpgCheck: !!data.gpgKeyData,
        gpgKeyData: data.gpgKeyData,
      };
      onAdd(repo, data.warning);
      handleClose();
    } catch {
      setFetchState("idle");
      setFetchError("Network error. Could not reach the server.");
    }
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      aria-labelledby={`${idPrefix}-copr-title`}
      variant="small"
    >
      <ModalHeader title="Add Fedora COPR" labelId={`${idPrefix}-copr-title`} />
      <ModalBody>
        <Form>
          <FormGroup label="COPR address" fieldId={`${idPrefix}-copr-addr`} isRequired>
            <TextInput
              id={`${idPrefix}-copr-addr`}
              value={coprAddress}
              onChange={(_ev, v) => { setCOPRAddress(v); setFetchState("idle"); setFetchError(null); }}
              placeholder="copr:owner/project  or  @group/project"
              aria-label="COPR address"
              aria-invalid={coprAddress !== "" && parsed === null ? true : undefined}
              aria-describedby={coprAddress !== "" && parsed === null ? `${idPrefix}-copr-addr-err` : undefined}
              autoFocus
            />
            {coprAddress !== "" && parsed === null && (
              <FormHelperText>
                <HelperText>
                  <HelperTextItem variant="error" id={`${idPrefix}-copr-addr-err`}>
                    Enter a COPR address like <code>copr:user/project</code> or <code>@group/project</code>
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            )}
          </FormGroup>

          <FormGroup label="Chroot" fieldId={`${idPrefix}-chroot`} isRequired>
            <SearchableSelect
              id={`${idPrefix}-chroot`}
              ariaLabel="Select chroot"
              placeholder="Select a chroot"
              selected={chroot}
              onSelect={setChroot}
              options={[
                ...FEDORA_CHROOTS.map(c => ({ value: c.value, label: c.label })),
                { value: "_custom", label: "Custom…" },
              ]}
            />
            {chroot === "_custom" && (
              <TextInput
                style={{ marginTop: "0.5rem" }}
                value={customChroot}
                onChange={(_ev, v) => setCustomChroot(v)}
                placeholder="e.g. fedora-43-aarch64"
                aria-label="Custom chroot name"
              />
            )}
          </FormGroup>

          {fetchError && (
            <LiveAlert
              id={`${idPrefix}-copr-err`}
              message={fetchError}
              variant="danger"
            />
          )}
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button
          onClick={handleSubmit}
          isDisabled={!canSubmit}
          icon={fetchState === "fetching" ? <Spinner size="sm" aria-label="Loading" /> : undefined}
        >
          {fetchState === "fetching" ? "Adding…" : "Add"}
        </Button>
        <Button variant="link" onClick={handleClose} isDisabled={fetchState === "fetching"}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};

/* ── YMPDistVersionModal sub-component ── */

interface YMPDistVersionModalProps {
  isOpen: boolean;
  groups: YMPImportGroup[];
  selectedIdx: number;
  onSelect: (idx: number) => void;
  onImport: () => void;
  onClose: () => void;
  triggerRef: React.RefObject<HTMLButtonElement | null>;
}

const YMPDistVersionModal: React.FC<YMPDistVersionModalProps> = ({
  isOpen, groups, selectedIdx, onSelect, onImport, onClose, triggerRef,
}) => {
  const idPrefix = useId();

  const handleClose = () => {
    onClose();
    triggerRef.current?.focus();
  };

  return (
    <Modal
      isOpen={isOpen}
      onClose={handleClose}
      aria-labelledby={`${idPrefix}-ymp-title`}
      variant="small"
    >
      <ModalHeader title="Import from .ymp" labelId={`${idPrefix}-ymp-title`} />
      <ModalBody>
        <p style={{ marginBottom: "1rem" }}>
          This file contains entries for multiple distribution versions.
          Select which one to import:
        </p>
        {groups.map((g, i) => (
          <div key={i} style={{ marginBottom: "0.75rem" }}>
            <Radio
              id={`${idPrefix}-ymp-group-${i}`}
              name={`${idPrefix}-ymp-group`}
              label={g.distVersion || `Group ${i + 1}`}
              description={`${g.repositories.length} repositor${g.repositories.length === 1 ? "y" : "ies"}, ${g.packages.length} package${g.packages.length === 1 ? "" : "s"}`}
              isChecked={selectedIdx === i}
              onChange={() => onSelect(i)}
            />
            {g.warnings && g.warnings.length > 0 && (
              <div style={{ marginLeft: "1.5rem", marginTop: "0.25rem", fontSize: "0.875rem", color: "var(--pf-t--global--color--status--warning--100)" }}>
                <ExclamationTriangleIcon aria-hidden /> {g.warnings.join("; ")}
              </div>
            )}
          </div>
        ))}
      </ModalBody>
      <ModalFooter>
        <Button onClick={onImport}>Import selected</Button>
        <Button variant="link" onClick={handleClose}>Cancel</Button>
      </ModalFooter>
    </Modal>
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
  // Each configuration section is an independently collapsible disclosure
  // (flattened from the former nested Repositories/Packages/Options tabs).
  // All start expanded so nothing is hidden on open.
  const [reposExpanded, setReposExpanded] = useState(true);
  const [pkgsExpanded, setPkgsExpanded] = useState(true);
  const [optionsExpanded, setOptionsExpanded] = useState(true);
  const [parseError, setParseError] = useState<string | null>(null);
  const [ppaModalOpen, setPPAModalOpen] = useState(false);
  const [ppaWarning, setPPAWarning] = useState<string | null>(null);
  const ppaButtonRef = useRef<HTMLButtonElement | null>(null);

  const [coprModalOpen, setCOPRModalOpen] = useState(false);
  const [coprWarning, setCOPRWarning] = useState<string | null>(null);
  const coprButtonRef = useRef<HTMLButtonElement | null>(null);

  // YMP import state
  const ympFileInputRef = useRef<HTMLInputElement | null>(null);
  const ympButtonRef = useRef<HTMLButtonElement | null>(null);
  const [ympGroups, setYMPGroups] = useState<YMPImportGroup[]>([]);
  const [ympSelectedIdx, setYMPSelectedIdx] = useState(0);
  const [ympModalOpen, setYMPModalOpen] = useState(false);
  const [ympAlert, setYMPAlert] = useState<{ variant: "success" | "warning" | "danger"; msg: string } | null>(null);

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

  const handlePPAAdd = (repo: RepoEntry, warning?: string) => {
    pushChange({ ...content, repositories: [...repos, repo] });
    setPPAWarning(warning ?? null);
  };

  const handleCOPRAdd = (repo: RepoEntry, warning?: string) => {
    pushChange({ ...content, repositories: [...repos, repo] });
    setCOPRWarning(warning ?? null);
  };

  /* ── ymp handlers ── */

  const applyYMPGroup = useCallback((group: YMPImportGroup) => {
    const existingIDs = new Set(repos.map(r => r.id));
    const newRepos: RepoEntry[] = [];
    for (const r of group.repositories) {
      let id = r.id;
      let counter = 2;
      while (existingIDs.has(id)) { id = `${r.id}-${counter++}`; }
      existingIDs.add(id);
      newRepos.push({ id, name: r.name, type: "REPOSITORY_TYPE_ZYPPER", enabled: r.enabled, baseurl: r.baseurl, gpgCheck: false });
    }

    const existingPkgNames = new Set(pkgs.map(p => p.name));
    const newPkgs: PkgEntry[] = [];
    let skipped = 0;
    for (const p of group.packages) {
      if (existingPkgNames.has(p.name)) { skipped++; continue; }
      existingPkgNames.add(p.name);
      newPkgs.push({ name: p.name, state: p.state as PkgState });
    }

    pushChange({ ...content, repositories: [...repos, ...newRepos], packages: [...pkgs, ...newPkgs] });

    const parts: string[] = [];
    if (newRepos.length > 0) parts.push(`${newRepos.length} repositor${newRepos.length === 1 ? "y" : "ies"}`);
    if (newPkgs.length > 0) parts.push(`${newPkgs.length} package${newPkgs.length === 1 ? "" : "s"}`);
    const skipNote = skipped > 0 ? ` (${skipped} package${skipped === 1 ? "" : "s"} already existed, skipped)` : "";
    const warnings = group.warnings ?? [];
    const msgBase = parts.length > 0
      ? `Imported ${parts.join(" and ")} from .ymp file${skipNote}.`
      : `Nothing new to import${skipNote}.`;
    setYMPAlert({
      variant: warnings.length > 0 ? "warning" : "success",
      msg: warnings.length > 0 ? `${msgBase} Note: ${warnings.join("; ")}` : msgBase,
    });
  }, [content, repos, pkgs, pushChange]);

  const handleYMPFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = ""; // allow re-uploading the same file
    setYMPAlert(null);
    const formData = new FormData();
    formData.append("file", file);
    try {
      // Drop Content-Type from authHeaders — the browser sets it automatically
      // with the correct multipart boundary when body is FormData.
      const { "Content-Type": _, ...csrfHeader } = authHeaders();
      const resp = await fetch("/api/v1/ymp-import", { method: "POST", headers: csrfHeader, body: formData });
      if (!resp.ok) {
        const text = await resp.text();
        setYMPAlert({ variant: "danger", msg: `Import failed: ${text.trim()}` });
        return;
      }
      const data = (await resp.json()) as { groups: YMPImportGroup[] };
      if (!data.groups || data.groups.length === 0) {
        setYMPAlert({ variant: "danger", msg: "No groups found in .ymp file." });
        return;
      }
      if (data.groups.length === 1) {
        applyYMPGroup(data.groups[0]);
      } else {
        setYMPGroups(data.groups);
        setYMPSelectedIdx(0);
        setYMPModalOpen(true);
      }
    } catch {
      setYMPAlert({ variant: "danger", msg: "Could not upload the .ymp file." });
    }
  };

  const handleYMPImportSelected = () => {
    applyYMPGroup(ympGroups[ympSelectedIdx]);
    setYMPModalOpen(false);
    setYMPGroups([]);
  };

  /* ── pkg handlers ── */

  const addPkg = () => pushChange({ ...content, packages: [...pkgs, { ...DEFAULT_PKG }] });
  const removePkg = (idx: number) => pushChange({ ...content, packages: pkgs.filter((_, i) => i !== idx) });
  const updatePkg = (idx: number, patch: Partial<PkgEntry>) =>
    pushChange({ ...content, packages: pkgs.map((p, i) => (i === idx ? { ...p, ...patch } : p)) });

  return (
    <div>
      <LiveAlert message={parseError} variant="danger" style={{ marginBottom: "0.75rem" }} />

      {/* ── Repositories section ── */}
      <ExpandableSection
        displaySize="lg"
        toggleText={`Repositories (${repos.length})`}
        isExpanded={reposExpanded}
        onToggle={(_ev, expanded) => setReposExpanded(expanded)}
      >
          <div style={{ paddingTop: "1rem" }}>
            <LiveAlert
              message={ppaWarning}
              variant="warning"
              style={{ marginBottom: "1rem" }}
            />
            <LiveAlert
              message={coprWarning}
              variant="warning"
              style={{ marginBottom: "1rem" }}
            />
            <LiveAlert
              message={ympAlert?.msg ?? null}
              variant={ympAlert?.variant ?? "info"}
              style={{ marginBottom: "1rem" }}
            />
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
            <div style={{ display: "flex", gap: "0.75rem", flexWrap: "wrap" }}>
              <Button
                variant="secondary"
                icon={<PlusCircleIcon />}
                onClick={addRepo}
                isDisabled={isDisabled}
              >
                Add Repository
              </Button>
              <Button
                variant="secondary"
                onClick={() => { setPPAWarning(null); setPPAModalOpen(true); }}
                isDisabled={isDisabled}
                ref={ppaButtonRef}
                aria-haspopup="dialog"
              >
                Add Ubuntu PPA…
              </Button>
              <Button
                variant="secondary"
                onClick={() => { setYMPAlert(null); ympFileInputRef.current?.click(); }}
                isDisabled={isDisabled}
                ref={ympButtonRef}
                aria-label="Add OpenSUSE 1-Click .ymp repository"
              >
                Add openSUSE 1-Click .ymp…
              </Button>
              <Button
                variant="secondary"
                onClick={() => { setCOPRWarning(null); setCOPRModalOpen(true); }}
                isDisabled={isDisabled}
                ref={coprButtonRef}
                aria-haspopup="dialog"
              >
                Add Fedora COPR…
              </Button>
              {/* Hidden file input for .ymp upload */}
              <input
                ref={ympFileInputRef}
                type="file"
                accept=".ymp,text/x-suse-ymp"
                style={{ display: "none" }}
                aria-hidden="true"
                onChange={handleYMPFileChange}
              />
            </div>

            <AddPPAModal
              isOpen={ppaModalOpen}
              onClose={() => setPPAModalOpen(false)}
              onAdd={handlePPAAdd}
              triggerRef={ppaButtonRef}
            />
            <AddCOPRModal
              isOpen={coprModalOpen}
              onClose={() => setCOPRModalOpen(false)}
              onAdd={handleCOPRAdd}
              triggerRef={coprButtonRef}
            />
            <YMPDistVersionModal
              isOpen={ympModalOpen}
              groups={ympGroups}
              selectedIdx={ympSelectedIdx}
              onSelect={setYMPSelectedIdx}
              onImport={handleYMPImportSelected}
              onClose={() => { setYMPModalOpen(false); setYMPGroups([]); }}
              triggerRef={ympButtonRef}
            />
          </div>
      </ExpandableSection>

      {/* ── Packages section ── */}
      <ExpandableSection
        displaySize="lg"
        toggleText={`Packages (${pkgs.length})`}
        isExpanded={pkgsExpanded}
        onToggle={(_ev, expanded) => setPkgsExpanded(expanded)}
      >
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
      </ExpandableSection>

      {/* ── Options section ── */}
      <ExpandableSection
        displaySize="lg"
        toggleText="Options"
        isExpanded={optionsExpanded}
        onToggle={(_ev, expanded) => setOptionsExpanded(expanded)}
      >
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
      </ExpandableSection>
    </div>
  );
};
