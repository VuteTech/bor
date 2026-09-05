// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * FlatpakCatalogUploadModal — import an AppStream catalog (appstream.xml.gz)
 * for one repository by hand. This is the air-gapped path: the file is
 * downloaded elsewhere (e.g. https://dl.flathub.org/repo/appstream/x86_64/appstream.xml.gz)
 * and uploaded here.
 */

import React, { useEffect, useId, useState } from "react";
import {
  Button,
  Form,
  FormGroup,
  FormHelperText,
  FormSelect,
  FormSelectOption,
  HelperText,
  HelperTextItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
} from "@patternfly/react-core";
import { LiveAlert } from "../../components/LiveAlert";
import { FlatpakRepository, uploadFlatpakCatalog } from "../../apiClient/flatpakApi";

export interface FlatpakCatalogUploadModalProps {
  repo: FlatpakRepository | null;
  onDone: (appCount: number) => void;
  onClose: () => void;
}

const ARCHES = ["x86_64", "aarch64", "i686"];

export const FlatpakCatalogUploadModal: React.FC<FlatpakCatalogUploadModalProps> = ({ repo, onDone, onClose }) => {
  const idp = useId();
  const [file, setFile] = useState<File | null>(null);
  const [arch, setArch] = useState("x86_64");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (repo) {
      setFile(null);
      setArch(repo.arches[0] ?? "x86_64");
      setError(null);
    }
  }, [repo]);

  const submit = async () => {
    if (!repo || !file) return;
    setBusy(true);
    setError(null);
    try {
      const res = await uploadFlatpakCatalog(repo.id, file, arch);
      onDone(res.app_count);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Upload failed");
    } finally {
      setBusy(false);
    }
  };

  const archSource = repo
    ? `${repo.url.replace(/\/+$/, "")}/appstream/${arch}/appstream.xml.gz`
    : "";

  return (
    <Modal variant={ModalVariant.small} isOpen={!!repo} onClose={onClose} aria-labelledby={`${idp}-title`}>
      <ModalHeader title={`Upload catalog for ${repo?.title || repo?.name || ""}`} labelId={`${idp}-title`} />
      <ModalBody>
        <p style={{ marginBottom: 12 }}>
          For servers without internet access: download the repository&apos;s AppStream
          catalog on a connected machine and import it here. The expected source for the
          selected architecture is:
        </p>
        <code style={{ wordBreak: "break-all", display: "block", marginBottom: 16 }}>{archSource}</code>
        {error && <LiveAlert id={`${idp}-error`} variant="danger" isInline message={error} style={{ marginBottom: 12 }} />}
        <Form onSubmit={(e) => { e.preventDefault(); void submit(); }}>
          <FormGroup label="Architecture" fieldId={`${idp}-arch`}>
            <FormSelect id={`${idp}-arch`} value={arch} onChange={(_e, v) => setArch(v)} aria-label="Architecture">
              {ARCHES.map((a) => <FormSelectOption key={a} value={a} label={a} />)}
            </FormSelect>
          </FormGroup>
          <FormGroup label="Catalog file (appstream.xml.gz)" fieldId={`${idp}-file`} isRequired>
            <input
              id={`${idp}-file`}
              type="file"
              accept=".gz,application/gzip,application/x-gzip"
              aria-describedby={error ? `${idp}-error` : undefined}
              aria-invalid={error ? true : undefined}
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
            <FormHelperText>
              <HelperText>
                <HelperTextItem>Gzip-compressed AppStream XML, at most 64 MiB. Replaces the indexed apps of this architecture.</HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={() => void submit()} isDisabled={!file || busy} isLoading={busy}>
          Upload and index
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={busy}>Cancel</Button>
      </ModalFooter>
    </Modal>
  );
};
