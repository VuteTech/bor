// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * FlatpakRepositoryModal — add or edit a server-side Flatpak repository
 * (Settings → Flatpak repositories). Three ways in: a preset, a .flatpakrepo
 * URL (probed server-side and prefilled), or manual fields.
 */

import React, { useEffect, useId, useState } from "react";
import {
  Button,
  Checkbox,
  ExpandableSection,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  InputGroup,
  InputGroupItem,
  Label,
  NumberInput,
  Switch,
  TextArea,
  TextInput,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
} from "@patternfly/react-core";
import { LiveAlert } from "../../components/LiveAlert";
import {
  FlatpakRepository,
  FlatpakRepositoryRequest,
  createFlatpakRepository,
  fileToBase64,
  isValidFlatpakRemoteName,
  isValidFlatpakRemoteUrl,
  probeFlatpakRemote,
  updateFlatpakRepository,
} from "../../apiClient/flatpakApi";
import { SUBSET_OPTIONS } from "../Policies/flatpakModel";

export interface FlatpakRepositoryModalProps {
  isOpen: boolean;
  /** Existing repository to edit; null/undefined = add. */
  initial?: FlatpakRepository | null;
  onSaved: (repo: FlatpakRepository) => void;
  onClose: () => void;
}

const ARCHES = ["x86_64", "aarch64", "i686"];
const PRESETS: { label: string; url: string }[] = [
  { label: "Flathub Beta", url: "https://dl.flathub.org/beta-repo/flathub-beta.flatpakrepo" },
];
const CUSTOM_SUBSET = "__custom__";

interface FormState {
  name: string;
  title: string;
  url: string;
  flatpakrepoUrl: string;
  homepage: string;
  comment: string;
  description: string;
  iconUrl: string;
  gpgKeyData: string; // base64 of a newly chosen key; "" = keep/none
  clearGpgKey: boolean;
  gpgKeyId: string;
  collectionId: string;
  defaultBranch: string;
  subset: string;
  appstreamUrl: string;
  arches: string[];
  catalogEnabled: boolean;
  refreshHours: number;
}

function fromRepo(r: FlatpakRepository | null | undefined): FormState {
  return {
    name: r?.name ?? "",
    title: r?.title ?? "",
    url: r?.url ?? "",
    flatpakrepoUrl: r?.flatpakrepo_url ?? "",
    homepage: r?.homepage ?? "",
    comment: r?.comment ?? "",
    description: r?.description ?? "",
    iconUrl: r?.icon_url ?? "",
    gpgKeyData: "",
    clearGpgKey: false,
    gpgKeyId: r?.gpg_key_id ?? "",
    collectionId: r?.collection_id ?? "",
    defaultBranch: r?.default_branch ?? "",
    subset: r?.subset ?? "",
    appstreamUrl: r?.appstream_url ?? "",
    arches: r?.arches?.length ? [...r.arches] : ["x86_64"],
    catalogEnabled: r ? r.catalog_enabled : true,
    refreshHours: r ? Math.max(1, Math.round(r.refresh_interval_s / 3600)) : 24,
  };
}

function validate(f: FormState): string | null {
  if (!isValidFlatpakRemoteName(f.name.trim())) {
    return "Name must start with a letter or digit and contain only letters, digits, dots, underscores or hyphens (max 64).";
  }
  if (!isValidFlatpakRemoteUrl(f.url.trim())) return "Repository URL must start with https:// or oci+https://.";
  if (f.flatpakrepoUrl.trim() && !/^https:\/\//i.test(f.flatpakrepoUrl.trim())) return ".flatpakrepo URL must start with https://.";
  if (f.appstreamUrl.trim() && !/^https:\/\//i.test(f.appstreamUrl.trim())) return "AppStream URL must start with https://.";
  if (f.arches.length === 0) return "Select at least one architecture.";
  if (!Number.isFinite(f.refreshHours) || f.refreshHours < 1 || f.refreshHours > 720) return "Refresh interval must be between 1 and 720 hours.";
  if (f.gpgKeyId.trim() && !/^[0-9A-Fa-f]{40}$/.test(f.gpgKeyId.trim())) return "GPG key fingerprint must be 40 hexadecimal characters.";
  return null;
}

export const FlatpakRepositoryModal: React.FC<FlatpakRepositoryModalProps> = ({ isOpen, initial, onSaved, onClose }) => {
  const idp = useId();
  const isEdit = !!initial;
  const [form, setForm] = useState<FormState>(fromRepo(initial));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [importBusy, setImportBusy] = useState(false);
  const [importMsg, setImportMsg] = useState<{ variant: "success" | "warning" | "danger"; text: string } | null>(null);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [customSubset, setCustomSubset] = useState(false);
  const [keyLabel, setKeyLabel] = useState<string>("");

  useEffect(() => {
    if (isOpen) {
      const f = fromRepo(initial);
      setForm(f);
      setError(null);
      setImportMsg(null);
      setSaving(false);
      setAdvancedOpen(false);
      setCustomSubset(!SUBSET_OPTIONS.some((o) => o.value === f.subset));
      setKeyLabel("");
    }
  }, [isOpen, initial]);

  const patch = (p: Partial<FormState>) => setForm((f) => ({ ...f, ...p }));

  const runImport = async (url: string) => {
    const trimmed = url.trim();
    if (!trimmed) return;
    setImportBusy(true);
    setImportMsg(null);
    try {
      const info = await probeFlatpakRemote(trimmed);
      patch({
        flatpakrepoUrl: trimmed,
        name: form.name || info.name,
        url: info.url || form.url,
        title: info.title || form.title,
        homepage: info.homepage || form.homepage,
        comment: info.comment || form.comment,
        description: info.description || form.description,
        iconUrl: info.icon_url || form.iconUrl,
        collectionId: info.collection_id || form.collectionId,
        defaultBranch: info.default_branch || form.defaultBranch,
        subset: info.subset || form.subset,
        gpgKeyData: info.gpg_key_data || form.gpgKeyData,
        clearGpgKey: false,
      });
      if (info.gpg_key_data) setKeyLabel("key imported from .flatpakrepo");
      if (info.warning) setImportMsg({ variant: "warning", text: info.warning });
      else setImportMsg({ variant: "success", text: `Loaded "${info.title || info.name}" from the .flatpakrepo file.` });
    } catch (e: unknown) {
      setImportMsg({ variant: "danger", text: e instanceof Error ? e.message : "Could not fetch the .flatpakrepo file" });
    } finally {
      setImportBusy(false);
    }
  };

  const handleKeyFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    if (file.size > 64 * 1024) {
      setError("GPG key file exceeds 64 KiB");
      return;
    }
    try {
      const b64 = await fileToBase64(file);
      patch({ gpgKeyData: b64, clearGpgKey: false });
      setKeyLabel(file.name);
      setError(null);
    } catch {
      setError("Could not read the key file");
    }
  };

  const save = async () => {
    const problem = validate(form);
    if (problem) {
      setError(problem);
      return;
    }
    setSaving(true);
    setError(null);
    const req: FlatpakRepositoryRequest = {
      name: form.name.trim(),
      title: form.title.trim(),
      url: form.url.trim(),
      flatpakrepo_url: form.flatpakrepoUrl.trim(),
      homepage: form.homepage.trim(),
      comment: form.comment.trim(),
      description: form.description.trim(),
      icon_url: form.iconUrl.trim(),
      gpg_key_data: form.gpgKeyData || undefined,
      clear_gpg_key: form.clearGpgKey || undefined,
      gpg_key_id: form.gpgKeyId.trim(),
      collection_id: form.collectionId.trim(),
      default_branch: form.defaultBranch.trim(),
      subset: form.subset.trim(),
      appstream_url: form.appstreamUrl.trim(),
      arches: form.arches,
      catalog_enabled: form.catalogEnabled,
      refresh_interval_s: Math.round(form.refreshHours * 3600),
    };
    try {
      const saved = isEdit && initial
        ? await updateFlatpakRepository(initial.id, req)
        : await createFlatpakRepository(req);
      onSaved(saved);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setSaving(false);
    }
  };

  const keyStatus = form.clearGpgKey
    ? "Key will be removed"
    : form.gpgKeyData
    ? `New key: ${keyLabel || "selected"}`
    : initial?.has_gpg_key
    ? "Stored key kept"
    : "No key";

  const errId = `${idp}-error`;

  return (
    <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={onClose} aria-labelledby={`${idp}-title`}>
      <ModalHeader
        title={isEdit ? `Edit repository ${initial?.name ?? ""}` : "Add Flatpak repository"}
        labelId={`${idp}-title`}
        description="Repositories listed here are indexed by the server so the policy editor can search applications. Nodes receive remotes through Flatpak policies, not from this list."
      />
      <ModalBody>
        {error && <LiveAlert id={errId} variant="danger" isInline message={error} style={{ marginBottom: 12 }} />}
        {importMsg && <LiveAlert variant={importMsg.variant} isInline message={importMsg.text} style={{ marginBottom: 12 }} />}
        <Form id={`${idp}-form`} onSubmit={(e) => { e.preventDefault(); void save(); }}>
          {!isEdit && (
            <FormGroup label="Import from .flatpakrepo URL" fieldId={`${idp}-import`}>
              <InputGroup>
                <InputGroupItem isFill>
                  <TextInput
                    id={`${idp}-import`}
                    type="url"
                    value={form.flatpakrepoUrl}
                    placeholder="https://example.com/repo/example.flatpakrepo"
                    onChange={(_e, v) => patch({ flatpakrepoUrl: v })}
                  />
                </InputGroupItem>
                <InputGroupItem>
                  <Button variant="control" onClick={() => void runImport(form.flatpakrepoUrl)} isDisabled={importBusy || !form.flatpakrepoUrl.trim()} isLoading={importBusy}>
                    Import
                  </Button>
                </InputGroupItem>
              </InputGroup>
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Presets:{" "}
                    {PRESETS.map((p) => (
                      <Button key={p.url} variant="link" isInline onClick={() => void runImport(p.url)} isDisabled={importBusy}>
                        {p.label}
                      </Button>
                    ))}
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
          )}

          <FormGroup label="Name" fieldId={`${idp}-name`} isRequired>
            <TextInput
              id={`${idp}-name`}
              value={form.name}
              isDisabled={!!initial?.builtin}
              onChange={(_e, v) => patch({ name: v })}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? errId : undefined}
            />
            <FormHelperText><HelperText><HelperTextItem>Suggested remote name for policies (e.g. <code>flathub</code>).</HelperTextItem></HelperText></FormHelperText>
          </FormGroup>
          <FormGroup label="Title" fieldId={`${idp}-title-field`}>
            <TextInput id={`${idp}-title-field`} value={form.title} onChange={(_e, v) => patch({ title: v })} />
          </FormGroup>
          <FormGroup label="Repository URL" fieldId={`${idp}-url`} isRequired>
            <TextInput id={`${idp}-url`} type="url" value={form.url} placeholder="https://dl.flathub.org/repo/" onChange={(_e, v) => patch({ url: v })} />
          </FormGroup>
          <FormGroup label="Subset" fieldId={`${idp}-subset`}>
            <FormSelect
              id={`${idp}-subset`}
              value={customSubset ? CUSTOM_SUBSET : form.subset}
              onChange={(_e, v) => {
                if (v === CUSTOM_SUBSET) { setCustomSubset(true); return; }
                setCustomSubset(false);
                patch({ subset: v });
              }}
              aria-label="Subset"
            >
              {SUBSET_OPTIONS.map((o) => <FormSelectOption key={o.value || "all"} value={o.value} label={o.label} />)}
              <FormSelectOption value={CUSTOM_SUBSET} label="Custom subset…" />
            </FormSelect>
            {customSubset && (
              <TextInput id={`${idp}-subset-custom`} aria-label="Custom subset" value={form.subset} onChange={(_e, v) => patch({ subset: v })} style={{ marginTop: 8 }} />
            )}
            <FormHelperText><HelperText><HelperTextItem>Suggested to policies that copy this repository; does not affect indexing.</HelperTextItem></HelperText></FormHelperText>
          </FormGroup>

          <FormGroup label={<><abbr title="GNU Privacy Guard">GPG</abbr> public key</>} fieldId={`${idp}-key`}>
            <input id={`${idp}-key`} type="file" accept=".gpg,.asc,.key,.pub" aria-label="GPG public key file" onChange={(e) => void handleKeyFile(e)} />
            <div style={{ marginTop: 6, display: "flex", gap: 8, alignItems: "center" }}>
              <Label isCompact color={form.clearGpgKey ? "orange" : form.gpgKeyData || initial?.has_gpg_key ? "green" : "grey"}>{keyStatus}</Label>
              {(form.gpgKeyData || initial?.has_gpg_key) && !form.clearGpgKey && (
                <Button variant="link" isInline onClick={() => { patch({ gpgKeyData: "", clearGpgKey: true }); setKeyLabel(""); }}>Remove key</Button>
              )}
              {form.clearGpgKey && (
                <Button variant="link" isInline onClick={() => patch({ clearGpgKey: false })}>Keep key</Button>
              )}
            </div>
            <FormHelperText><HelperText><HelperTextItem>Copied into policies that add this remote so nodes verify downloads.</HelperTextItem></HelperText></FormHelperText>
          </FormGroup>

          <FormGroup label="Catalog" fieldId={`${idp}-catalog`}>
            <Switch
              id={`${idp}-catalog`}
              label="Index this repository's applications"
              isChecked={form.catalogEnabled}
              onChange={(_e, checked) => patch({ catalogEnabled: checked })}
            />
          </FormGroup>
          <FormGroup label="Refresh interval (hours)" fieldId={`${idp}-interval`}>
            <NumberInput
              id={`${idp}-interval`}
              value={form.refreshHours}
              min={1}
              max={720}
              onMinus={() => patch({ refreshHours: Math.max(1, form.refreshHours - 1) })}
              onPlus={() => patch({ refreshHours: Math.min(720, form.refreshHours + 1) })}
              onChange={(e) => {
                const v = Number((e.target as HTMLInputElement).value);
                if (Number.isFinite(v)) patch({ refreshHours: v });
              }}
              inputAriaLabel="Refresh interval in hours"
              minusBtnAriaLabel="Decrease refresh interval"
              plusBtnAriaLabel="Increase refresh interval"
              isDisabled={!form.catalogEnabled}
            />
          </FormGroup>
          <FormGroup label="Architectures" fieldId={`${idp}-arches`} role="group">
            <div style={{ display: "flex", gap: 16 }}>
              {ARCHES.map((a) => (
                <Checkbox
                  key={a}
                  id={`${idp}-arch-${a}`}
                  label={a}
                  isChecked={form.arches.includes(a)}
                  onChange={(_e, checked) =>
                    patch({ arches: checked ? [...form.arches, a] : form.arches.filter((x) => x !== a) })
                  }
                />
              ))}
            </div>
            <FormHelperText><HelperText><HelperTextItem>Each architecture is indexed separately; the editor searches all of them.</HelperTextItem></HelperText></FormHelperText>
          </FormGroup>

          <ExpandableSection toggleText="Advanced" isExpanded={advancedOpen} onToggle={(_e, v) => setAdvancedOpen(v)}>
            <FormGroup label="AppStream URL override" fieldId={`${idp}-appstream`}>
              <TextInput id={`${idp}-appstream`} type="url" value={form.appstreamUrl} placeholder="https://…/appstream/{arch}/appstream.xml.gz" onChange={(_e, v) => patch({ appstreamUrl: v })} />
              <FormHelperText><HelperText><HelperTextItem>Leave empty to use &lt;URL&gt;/appstream/&lt;arch&gt;/appstream.xml.gz. <code>{"{arch}"}</code> is replaced per architecture.</HelperTextItem></HelperText></FormHelperText>
            </FormGroup>
            <FormGroup label="Collection ID" fieldId={`${idp}-collection`}>
              <TextInput id={`${idp}-collection`} value={form.collectionId} placeholder="org.flathub.Stable" onChange={(_e, v) => patch({ collectionId: v })} />
            </FormGroup>
            <FormGroup label="Default branch" fieldId={`${idp}-branch`}>
              <TextInput id={`${idp}-branch`} value={form.defaultBranch} placeholder="stable" onChange={(_e, v) => patch({ defaultBranch: v })} />
            </FormGroup>
            <FormGroup label="GPG key fingerprint (optional)" fieldId={`${idp}-fpr`}>
              <TextInput id={`${idp}-fpr`} value={form.gpgKeyId} placeholder="40 hexadecimal characters" onChange={(_e, v) => patch({ gpgKeyId: v })} />
            </FormGroup>
            <FormGroup label="Homepage" fieldId={`${idp}-homepage`}>
              <TextInput id={`${idp}-homepage`} type="url" value={form.homepage} onChange={(_e, v) => patch({ homepage: v })} />
            </FormGroup>
            <FormGroup label="Comment" fieldId={`${idp}-comment`}>
              <TextInput id={`${idp}-comment`} value={form.comment} onChange={(_e, v) => patch({ comment: v })} />
            </FormGroup>
            <FormGroup label="Description" fieldId={`${idp}-description`}>
              <TextArea id={`${idp}-description`} value={form.description} onChange={(_e, v) => patch({ description: v })} resizeOrientation="vertical" />
            </FormGroup>
            <FormGroup label="Icon URL" fieldId={`${idp}-icon`}>
              <TextInput id={`${idp}-icon`} type="url" value={form.iconUrl} onChange={(_e, v) => patch({ iconUrl: v })} />
            </FormGroup>
          </ExpandableSection>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={() => void save()} isDisabled={saving} isLoading={saving}>
          {isEdit ? "Save changes" : "Add repository"}
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={saving}>Cancel</Button>
      </ModalFooter>
    </Modal>
  );
};
