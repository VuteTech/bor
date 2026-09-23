// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * TangServerModal - add or edit a Tang server. Adding probes GET /adv and
 * shows the advertised signing thumbprints with the instruction to compare
 * them against `tang-show-keys` on the Tang host: trust on first use, made
 * explicit and human-verified.
 * Editing confirms newly advertised keys (rotation): a confirmed key becomes
 * preferred while the old ones stay accepted until every volume re-bound.
 */

import React, { useEffect, useState } from "react";
import {
  Button,
  Checkbox,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import {
  createTangServer,
  isValidTangUrl,
  probeTangServer,
  TangServer,
  updateTangServer,
} from "../../apiClient/diskEncryptionApi";

interface Props {
  isOpen: boolean;
  /** null = add a new server. */
  initial: TangServer | null;
  onSaved: (saved: TangServer) => void;
  onClose: () => void;
}

export const TangServerModal: React.FC<Props> = ({ isOpen, initial, onSaved, onClose }) => {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [probed, setProbed] = useState<string[] | null>(null);
  const [confirmedKeys, setConfirmedKeys] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) return;
    setName(initial?.name ?? "");
    setUrl(initial?.url ?? "");
    setProbed(initial ? initial.advertised_thumbprints : null);
    setConfirmedKeys(new Set(initial?.trusted_thumbprints ?? []));
    setBusy(false);
    setError(null);
  }, [isOpen, initial]);

  const doProbe = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await probeTangServer(url);
      setProbed(res.signing_thumbprints);
      if (!name) {
        try {
          setName(new URL(res.url).host);
        } catch {
          /* keep the empty name */
        }
      }
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Probe failed");
      setProbed(null);
    } finally {
      setBusy(false);
    }
  };

  const toggleKey = (thp: string, checked: boolean) => {
    setConfirmedKeys((prev) => {
      const next = new Set(prev);
      if (checked) next.add(thp);
      else next.delete(thp);
      return next;
    });
  };

  const knownKeys = Array.from(
    new Set([...(initial?.trusted_thumbprints ?? []), ...(probed ?? [])]),
  );
  // Newly confirmed keys become preferred (list head); previously trusted
  // ones keep their order behind them.
  const trustedOrdered = [
    ...knownKeys.filter((k) => confirmedKeys.has(k) && !(initial?.trusted_thumbprints ?? []).includes(k)),
    ...(initial?.trusted_thumbprints ?? []).filter((k) => confirmedKeys.has(k)),
  ];

  const canSave = name.trim() !== "" && isValidTangUrl(url) && trustedOrdered.length > 0;

  const doSave = async () => {
    setBusy(true);
    setError(null);
    try {
      const req = { name: name.trim(), url: url.trim(), trusted_thumbprints: trustedOrdered };
      const saved = initial ? await updateTangServer(initial.id, req) : await createTangServer(req);
      onSaved(saved);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Save failed");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={onClose} aria-label={initial ? "Edit Tang server" : "Add Tang server"}>
      <ModalHeader title={initial ? "Edit Tang server" : "Add Tang server"} />
      <ModalBody>
        <Form onSubmit={(e) => e.preventDefault()}>
          <FormGroup label="Name" isRequired fieldId="tang-name">
            <TextInput
              id="tang-name"
              value={name}
              onChange={(_e, v) => setName(v)}
              isRequired
              aria-label="Tang server name"
            />
          </FormGroup>
          <FormGroup label="URL" isRequired fieldId="tang-url">
            <div style={{ display: "flex", gap: "0.5rem" }}>
              <TextInput
                id="tang-url"
                value={url}
                onChange={(_e, v) => {
                  setUrl(v);
                  setProbed(initial ? probed : null);
                }}
                placeholder="http://tang1.example.com"
                isRequired
                aria-label="Tang server URL"
                validated={url === "" || isValidTangUrl(url) ? "default" : "error"}
              />
              <Button variant="secondary" onClick={() => void doProbe()} isDisabled={busy || !isValidTangUrl(url)} isLoading={busy}>
                Probe
              </Button>
            </div>
            <FormHelperText>
              <HelperText>
                <HelperTextItem>
                  Tang serves plain <abbr title="Hypertext Transfer Protocol">HTTP</abbr> by design
                  (the exchange never reveals key material). Probing fetches the advertisement so
                  you can verify the keys below.
                </HelperTextItem>
              </HelperText>
            </FormHelperText>
          </FormGroup>

          {knownKeys.length > 0 && (
            <FormGroup label="Signing keys" fieldId="tang-keys">
              <FormHelperText>
                <HelperText>
                  <HelperTextItem variant="warning">
                    Compare each thumbprint with the output of <code>tang-show-keys</code> on the
                    Tang host before confirming it. Agents refuse to bind to keys you have not
                    confirmed.
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
              {knownKeys.map((thp) => {
                const wasTrusted = (initial?.trusted_thumbprints ?? []).includes(thp);
                const advertisedNow = (probed ?? initial?.advertised_thumbprints ?? []).includes(thp);
                return (
                  <div key={thp} style={{ display: "flex", alignItems: "center", gap: "0.5rem", marginBottom: "0.3rem" }}>
                    <Checkbox
                      id={`tang-key-${thp}`}
                      label={<code>{thp}</code>}
                      isChecked={confirmedKeys.has(thp)}
                      onChange={(_e, v) => toggleKey(thp, v)}
                      aria-label={`Trust signing key ${thp}`}
                    />
                    {!wasTrusted && advertisedNow && <Label color="orange" isCompact>new</Label>}
                    {wasTrusted && !advertisedNow && <Label color="grey" isCompact>no longer advertised</Label>}
                  </div>
                );
              })}
            </FormGroup>
          )}

          <LiveAlert id="tang-modal-error" variant="danger" isInline message={error} />
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={() => void doSave()} isDisabled={!canSave || busy} isLoading={busy}>
          {initial ? "Save" : "Add server"}
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={busy}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};
