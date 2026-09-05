// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * FlatpakRemoteModal — add or edit one remote of a Flatpak policy. Supports
 * prefilling from a `.flatpakrepo` URL (probed server-side to avoid CORS and
 * to keep the browser off arbitrary hosts).
 */

import React, { useEffect, useId, useState } from "react";
import {
  Button,
  Checkbox,
  ExpandableSection,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  InputGroup,
  InputGroupItem,
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
  TextArea,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { ListEditor } from "./ListEditor";
import { fileToBase64, probeFlatpakRemote } from "../../apiClient/flatpakApi";
import {
  FILTER_MODE_OPTIONS,
  FlatpakFilterMode,
  FlatpakRemoteEntry,
  SUBSET_OPTIONS,
  newRemoteEntry,
  validateRemoteEntry,
} from "./flatpakModel";

export interface FlatpakRemoteModalProps {
  isOpen: boolean;
  /** Existing entry to edit; undefined = add. */
  initial?: FlatpakRemoteEntry;
  /** Names already used by other remotes in the policy (duplicate guard). */
  takenNames: string[];
  /** Open with the ".flatpakrepo URL" import section expanded. */
  startWithImport?: boolean;
  onSave: (entry: FlatpakRemoteEntry) => void;
  onClose: () => void;
}

const CUSTOM_SUBSET = "__custom__";

export const FlatpakRemoteModal: React.FC<FlatpakRemoteModalProps> = ({
  isOpen,
  initial,
  takenNames,
  startWithImport,
  onSave,
  onClose,
}) => {
  const idp = useId();
  const [entry, setEntry] = useState<FlatpakRemoteEntry>(initial ?? newRemoteEntry());
  const [error, setError] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(!!startWithImport);
  const [importUrl, setImportUrl] = useState("");
  const [importBusy, setImportBusy] = useState(false);
  const [importMsg, setImportMsg] = useState<{ variant: "success" | "warning" | "danger"; text: string } | null>(null);
  const [subsetOpen, setSubsetOpen] = useState(false);
  const [filterOpen, setFilterOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [pasteKey, setPasteKey] = useState("");

  useEffect(() => {
    if (isOpen) {
      setEntry(initial ?? newRemoteEntry());
      setError(null);
      setImportOpen(!!startWithImport);
      setImportUrl("");
      setImportMsg(null);
      setPasteKey("");
    }
  }, [isOpen, initial, startWithImport]);

  const patch = (p: Partial<FlatpakRemoteEntry>) => setEntry((e) => ({ ...e, ...p }));

  const knownSubset = SUBSET_OPTIONS.some((o) => o.value === entry.subset);
  const [customSubset, setCustomSubset] = useState(!knownSubset);
  useEffect(() => {
    setCustomSubset(!SUBSET_OPTIONS.some((o) => o.value === (initial?.subset ?? "")));
  }, [initial]);

  const handleImport = async () => {
    const url = importUrl.trim();
    if (!url) return;
    setImportBusy(true);
    setImportMsg(null);
    try {
      const info = await probeFlatpakRemote(url);
      patch({
        name: entry.name || info.name,
        url: info.url || entry.url,
        title: info.title || entry.title,
        homepage: info.homepage || entry.homepage,
        comment: info.comment || entry.comment,
        collectionId: info.collection_id || entry.collectionId,
        defaultBranch: info.default_branch || entry.defaultBranch,
        subset: info.subset || entry.subset,
        gpgKeyData: info.gpg_key_data || entry.gpgKeyData,
        gpgVerify: info.gpg_key_data ? true : entry.gpgVerify,
      });
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
      patch({ gpgKeyData: await fileToBase64(file), gpgVerify: true });
      setError(null);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Could not read the key file");
    }
  };

  const applyPastedKey = () => {
    const cleaned = pasteKey.replace(/\s+/g, "");
    if (!cleaned) return;
    if (!/^[A-Za-z0-9+/=]+$/.test(cleaned)) {
      setError("Pasted key must be base64 (the GPGKey= value of a .flatpakrepo file)");
      return;
    }
    patch({ gpgKeyData: cleaned, gpgVerify: true });
    setPasteKey("");
    setError(null);
  };

  const handleSave = () => {
    const cleaned: FlatpakRemoteEntry = {
      ...entry,
      name: entry.name.trim(),
      url: entry.url.trim(),
      title: entry.title.trim(),
      subset: entry.subset.trim(),
      defaultBranch: entry.defaultBranch.trim(),
      collectionId: entry.collectionId.trim(),
      homepage: entry.homepage.trim(),
      comment: entry.comment.trim(),
      gpgKeyId: entry.gpgKeyId.trim(),
      filterRefs: entry.filterMode === "FLATPAK_FILTER_MODE_NONE" ? [] : entry.filterRefs.map((r) => r.trim()).filter((r) => r !== ""),
    };
    const err = validateRemoteEntry(cleaned);
    if (err) {
      setError(err);
      return;
    }
    if (takenNames.includes(cleaned.name) && cleaned.name !== initial?.name) {
      setError(`A remote named "${cleaned.name}" is already in this policy`);
      return;
    }
    onSave(cleaned);
  };

  const errId = `${idp}-error`;
  const keyBytes = entry.gpgKeyData ? Math.floor((entry.gpgKeyData.length * 3) / 4) : 0;

  return (
    <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={onClose} aria-labelledby={`${idp}-title`}>
      <ModalHeader title={initial ? `Edit remote ${initial.name}` : "Add Flatpak remote"} labelId={`${idp}-title`} />
      <ModalBody>
        <LiveAlert id={errId} message={error} variant="danger" isInline style={{ marginBottom: 16 }} />

        {!initial && (
          <ExpandableSection
            toggleText="Import from a .flatpakrepo URL"
            isExpanded={importOpen}
            onToggle={(_ev, expanded) => setImportOpen(expanded)}
            style={{ marginBottom: 16 }}
          >
            <FormGroup label=".flatpakrepo URL" fieldId={`${idp}-import-url`}>
              <InputGroup>
                <InputGroupItem isFill>
                  <TextInput
                    id={`${idp}-import-url`}
                    value={importUrl}
                    onChange={(_ev, v) => setImportUrl(v)}
                    placeholder="https://dl.flathub.org/repo/flathub.flatpakrepo"
                    type="url"
                    onKeyDown={(ev) => { if (ev.key === "Enter") { ev.preventDefault(); void handleImport(); } }}
                  />
                </InputGroupItem>
                <InputGroupItem>
                  <Button variant="secondary" onClick={() => void handleImport()} isLoading={importBusy} isDisabled={importBusy || !importUrl.trim()}>
                    Fetch details
                  </Button>
                </InputGroupItem>
              </InputGroup>
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>The server downloads the file and fills in the name, URL, title and signing key below.</HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            <LiveAlert message={importMsg?.text ?? null} variant={importMsg?.variant ?? "info"} isInline style={{ marginTop: 8 }} />
          </ExpandableSection>
        )}

        <Form onSubmit={(e) => { e.preventDefault(); handleSave(); }}>
          <FormGroup label="Remote name" isRequired fieldId={`${idp}-name`}>
            <TextInput
              id={`${idp}-name`}
              value={entry.name}
              onChange={(_ev, v) => patch({ name: v })}
              placeholder="flathub"
              isRequired
              isDisabled={!!initial}
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? errId : undefined}
            />
            <FormHelperText>
              <HelperText>
                <HelperTextItem>Letters, digits, dots, underscores or hyphens. Keep the conventional name (e.g. <code>flathub</code>) so app stores and .flatpakref links keep working.</HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>

          <FormGroup label="Repository URL" isRequired fieldId={`${idp}-url`}>
            <TextInput
              id={`${idp}-url`}
              value={entry.url}
              onChange={(_ev, v) => patch({ url: v })}
              placeholder="https://dl.flathub.org/repo/"
              type="url"
              isRequired
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? errId : undefined}
            />
          </FormGroup>

          <FormGroup label="Title" fieldId={`${idp}-title-in`}>
            <TextInput id={`${idp}-title-in`} value={entry.title} onChange={(_ev, v) => patch({ title: v })} placeholder="Flathub" />
          </FormGroup>

          <FormGroup label="Enabled" fieldId={`${idp}-enabled`}>
            <Switch
              id={`${idp}-enabled`}
              label="Remote is enabled on the node"
              isChecked={entry.enabled}
              onChange={(_ev, checked) => patch({ enabled: checked })}
            />
          </FormGroup>

          <FormGroup
            label={<><abbr title="GNU Privacy Guard">GPG</abbr> signing key</>}
            fieldId={`${idp}-gpg-file`}
          >
            <p style={{ marginBottom: "0.4rem" }}>
              {entry.gpgKeyData ? (
                <>
                  Key provided ({keyBytes.toLocaleString()} bytes).{" "}
                  <Button variant="link" isInline onClick={() => patch({ gpgKeyData: "" })}>Remove key</Button>
                </>
              ) : (
                <span className="bor-text-secondary">No key provided.</span>
              )}
            </p>
            <input
              id={`${idp}-gpg-file`}
              type="file"
              accept=".gpg,.asc,.key,.pub"
              onChange={(e) => void handleKeyFile(e)}
              aria-label="Upload the remote's GPG public key file"
            />
            <div style={{ marginTop: "0.5rem" }}>
              <TextArea
                id={`${idp}-gpg-paste`}
                value={pasteKey}
                onChange={(_ev, v) => setPasteKey(v)}
                rows={2}
                placeholder="…or paste the base64 GPGKey= value here"
                aria-label="Paste base64 GPG key"
                style={{ fontFamily: "monospace", fontSize: "0.8rem" }}
              />
              <Button variant="link" isInline onClick={applyPastedKey} isDisabled={!pasteKey.trim()} style={{ marginTop: "0.25rem" }}>
                Use pasted key
              </Button>
            </div>
            <div style={{ marginTop: "0.5rem" }}>
              <Checkbox
                id={`${idp}-gpg-disable`}
                label="Disable GPG verification for this remote (not recommended)"
                isChecked={!entry.gpgVerify}
                onChange={(_ev, checked) => patch({ gpgVerify: !checked })}
              />
            </div>
            <LiveAlert
              message={!entry.gpgVerify ? "Without GPG verification the node cannot check that apps really come from this remote. The agent reports the remote as non-compliant as a permanent reminder." : null}
              variant="warning"
              isInline
              style={{ marginTop: 8 }}
            />
          </FormGroup>

          <FormGroup label="Key fingerprint (optional)" fieldId={`${idp}-gpg-id`}>
            <TextInput
              id={`${idp}-gpg-id`}
              value={entry.gpgKeyId}
              onChange={(_ev, v) => patch({ gpgKeyId: v })}
              placeholder="40 hexadecimal characters"
              style={{ fontFamily: "monospace" }}
            />
          </FormGroup>

          <FormGroup label="Subset" fieldId={`${idp}-subset`}>
            <Select
              id={`${idp}-subset`}
              isOpen={subsetOpen}
              onOpenChange={setSubsetOpen}
              selected={customSubset ? CUSTOM_SUBSET : entry.subset}
              onSelect={(_ev, val) => {
                if (val === CUSTOM_SUBSET) {
                  setCustomSubset(true);
                } else {
                  setCustomSubset(false);
                  patch({ subset: String(val ?? "") });
                }
                setSubsetOpen(false);
              }}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle ref={ref} onClick={() => setSubsetOpen((v) => !v)} isExpanded={subsetOpen} aria-label="Select remote subset" isFullWidth>
                  {customSubset ? "Custom subset…" : (SUBSET_OPTIONS.find((o) => o.value === entry.subset)?.label ?? "All apps")}
                </MenuToggle>
              )}
            >
              <SelectList>
                {SUBSET_OPTIONS.map((o) => (
                  <SelectOption key={o.value || "all"} value={o.value}>{o.label}</SelectOption>
                ))}
                <SelectOption value={CUSTOM_SUBSET}>Custom subset…</SelectOption>
              </SelectList>
            </Select>
            {customSubset && (
              <TextInput
                id={`${idp}-subset-custom`}
                value={entry.subset}
                onChange={(_ev, v) => patch({ subset: v })}
                placeholder="subset name"
                aria-label="Custom subset name"
                style={{ marginTop: "0.5rem" }}
              />
            )}
            <FormHelperText>
              <HelperText>
                <HelperTextItem>Flathub publishes <code>verified</code>, <code>floss</code> and <code>verified_floss</code>. A subset limits which refs the node can see from this remote.</HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>

          <FormGroup label="Local filter" fieldId={`${idp}-filter`}>
            <Select
              id={`${idp}-filter`}
              isOpen={filterOpen}
              onOpenChange={setFilterOpen}
              selected={entry.filterMode}
              onSelect={(_ev, val) => { patch({ filterMode: val as FlatpakFilterMode }); setFilterOpen(false); }}
              toggle={(ref: React.Ref<MenuToggleElement>) => (
                <MenuToggle ref={ref} onClick={() => setFilterOpen((v) => !v)} isExpanded={filterOpen} aria-label="Select filter mode" isFullWidth>
                  {FILTER_MODE_OPTIONS.find((o) => o.value === entry.filterMode)?.label}
                </MenuToggle>
              )}
            >
              <SelectList>
                {FILTER_MODE_OPTIONS.map((o) => (
                  <SelectOption key={o.value} value={o.value} description={o.description}>{o.label}</SelectOption>
                ))}
              </SelectList>
            </Select>
            {entry.filterMode !== "FLATPAK_FILTER_MODE_NONE" && (
              <div style={{ marginTop: "0.5rem" }}>
                <ListEditor
                  items={entry.filterRefs}
                  renderItem={(v, idx) => (
                    <TextInput
                      id={`${idp}-ref-${idx}`}
                      value={v}
                      onChange={(_ev, nv) => patch({ filterRefs: entry.filterRefs.map((x, i) => (i === idx ? nv : x)) })}
                      placeholder="app/org.gnome.*  or  org.signal.Signal/*/stable"
                      aria-label={`Filter ref ${idx + 1}`}
                      style={{ fontFamily: "monospace" }}
                    />
                  )}
                  onRemove={(idx) => patch({ filterRefs: entry.filterRefs.filter((_, i) => i !== idx) })}
                  onAdd={() => patch({ filterRefs: [...entry.filterRefs, ""] })}
                  addLabel="Add ref"
                  removeAriaLabel={(idx) => `Remove filter ref ${idx + 1}`}
                  emptyText={entry.filterMode === "FLATPAK_FILTER_MODE_ALLOWLIST" ? "No refs yet — only runtimes and the apps required by this policy will be allowed." : "No refs yet — nothing is denied."}
                />
                <FormHelperText>
                  <HelperText>
                    <HelperTextItem>
                      Partial refs match everything below them; <code>*</code> matches one segment. Allow lists always keep <code>runtime/*</code> and the apps listed in this policy.
                    </HelperTextItem>
                  </HelperText>
                </FormHelperText>
              </div>
            )}
          </FormGroup>

          <ExpandableSection toggleText="Advanced" isExpanded={advancedOpen} onToggle={(_ev, ex) => setAdvancedOpen(ex)}>
            <FormGroup label="Priority" fieldId={`${idp}-prio`}>
              <NumberInput
                id={`${idp}-prio`}
                value={entry.priority}
                min={0}
                max={1000}
                onMinus={() => patch({ priority: Math.max(0, entry.priority - 1) })}
                onPlus={() => patch({ priority: Math.min(1000, entry.priority + 1) })}
                onChange={(ev) => {
                  const n = Number((ev.target as HTMLInputElement).value);
                  patch({ priority: Number.isFinite(n) ? Math.min(1000, Math.max(0, Math.round(n))) : 0 });
                }}
                inputAriaLabel="Remote priority"
                minusBtnAriaLabel="Decrease priority"
                plusBtnAriaLabel="Increase priority"
                widthChars={5}
              />
              <FormHelperText>
                <HelperText><HelperTextItem>0 keeps Flatpak&apos;s default (1). Higher wins when several remotes offer the same app.</HelperTextItem></HelperText>
              </FormHelperText>
            </FormGroup>
            <FormGroup label="Default branch" fieldId={`${idp}-branch`}>
              <TextInput id={`${idp}-branch`} value={entry.defaultBranch} onChange={(_ev, v) => patch({ defaultBranch: v })} placeholder="stable" />
            </FormGroup>
            <FormGroup label="Collection ID" fieldId={`${idp}-coll`}>
              <TextInput id={`${idp}-coll`} value={entry.collectionId} onChange={(_ev, v) => patch({ collectionId: v })} placeholder="org.flathub.Stable" />
            </FormGroup>
            <FormGroup label="Homepage" fieldId={`${idp}-home`}>
              <TextInput id={`${idp}-home`} type="url" value={entry.homepage} onChange={(_ev, v) => patch({ homepage: v })} />
            </FormGroup>
            <FormGroup label="Comment" fieldId={`${idp}-comment`}>
              <TextInput id={`${idp}-comment`} value={entry.comment} onChange={(_ev, v) => patch({ comment: v })} />
            </FormGroup>
            <FormGroup fieldId={`${idp}-noenum`}>
              <Switch id={`${idp}-noenum`} label="Hide from app stores (no-enumerate)" isChecked={entry.noEnumerate} onChange={(_ev, c) => patch({ noEnumerate: c })} />
            </FormGroup>
            <FormGroup fieldId={`${idp}-nodeps`}>
              <Switch id={`${idp}-nodeps`} label="Do not use for runtime dependencies (no-use-for-deps)" isChecked={entry.noUseForDeps} onChange={(_ev, c) => patch({ noUseForDeps: c })} />
            </FormGroup>
          </ExpandableSection>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={handleSave}>{initial ? "Save remote" : "Add remote"}</Button>
        <Button variant="link" onClick={onClose}>Cancel</Button>
      </ModalFooter>
    </Modal>
  );
};
