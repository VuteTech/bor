// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * RevealKeyModal - reveals an escrowed LUKS recovery key. Requires a reason
 * (audited) and step-up re-authentication (password, plus a TOTP code when
 * the account has one enrolled). The key is shown once, never cached in
 * client state beyond this modal, and auto-hidden after two minutes
 *.
 */

import React, { useEffect, useRef, useState } from "react";
import {
  Button,
  ClipboardCopy,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import {
  issueStepUp,
  LuksVolume,
  revealRecoveryKey,
  RevealRecoveryKeyResponse,
  STEP_UP_PURPOSE_REVEAL,
} from "../../apiClient/diskEncryptionApi";

const AUTO_HIDE_MS = 2 * 60 * 1000;

interface Props {
  /** null keeps the modal closed. */
  volume: LuksVolume | null;
  /** Reveal a previous key instead of the active one. */
  escrowId?: string;
  onClose: () => void;
}

export const RevealKeyModal: React.FC<Props> = ({ volume, escrowId, onClose }) => {
  const [reason, setReason] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<RevealRecoveryKeyResponse | null>(null);
  const [hidden, setHidden] = useState(false);
  const hideTimer = useRef<number | null>(null);

  useEffect(() => {
    if (!volume) return;
    setReason("");
    setPassword("");
    setTotpCode("");
    setBusy(false);
    setError(null);
    setRevealed(null);
    setHidden(false);
    return () => {
      if (hideTimer.current !== null) window.clearTimeout(hideTimer.current);
    };
  }, [volume, escrowId]);

  const doReveal = async () => {
    if (!volume) return;
    setBusy(true);
    setError(null);
    try {
      const stepUp = await issueStepUp(STEP_UP_PURPOSE_REVEAL, password, totpCode || undefined);
      const res = await revealRecoveryKey(volume.id, reason.trim(), stepUp.token, escrowId);
      setPassword("");
      setTotpCode("");
      setRevealed(res);
      hideTimer.current = window.setTimeout(() => setHidden(true), AUTO_HIDE_MS);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Reveal failed");
    } finally {
      setBusy(false);
    }
  };

  const close = () => {
    // Clear the key from state before unmounting.
    setRevealed(null);
    setHidden(false);
    onClose();
  };

  const canSubmit = reason.trim() !== "" && password !== "" && !busy;

  return (
    <Modal
      variant={ModalVariant.medium}
      isOpen={volume !== null}
      onClose={close}
      aria-label="Reveal recovery key"
    >
      <ModalHeader
        title="Reveal recovery key"
        description={
          volume
            ? `${volume.node_name} · ${volume.mapping_name || volume.luks_uuid}`
            : undefined
        }
      />
      <ModalBody>
        {!revealed ? (
          <Form
            onSubmit={(e) => {
              e.preventDefault();
              if (canSubmit) void doReveal();
            }}
          >
            <LiveAlert
              id="reveal-explainer"
              variant="info"
              isInline
              message={
                "Revealing is audited with your name and reason" +
                (escrowId ? "." : ", and the key is replaced automatically after the device next connects (when the policy keeps the default).")
              }
            />
            <FormGroup label="Reason or ticket number" isRequired fieldId="reveal-reason">
              <TextInput
                id="reveal-reason"
                value={reason}
                onChange={(_e, v) => setReason(v)}
                isRequired
                aria-label="Reason for revealing the recovery key"
              />
            </FormGroup>
            <FormGroup label="Your password" isRequired fieldId="reveal-password">
              <TextInput
                id="reveal-password"
                type="password"
                value={password}
                onChange={(_e, v) => setPassword(v)}
                isRequired
                autoComplete="current-password"
                aria-label="Password for re-authentication"
                aria-invalid={error ? true : undefined}
                aria-describedby={error ? "reveal-error" : undefined}
              />
            </FormGroup>
            <FormGroup
              label={
                <>
                  <abbr title="Time-based One-Time Password">TOTP</abbr> code
                </>
              }
              fieldId="reveal-totp"
            >
              <TextInput
                id="reveal-totp"
                value={totpCode}
                onChange={(_e, v) => setTotpCode(v)}
                autoComplete="one-time-code"
                aria-label="TOTP code"
              />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Required when your account has multi-factor authentication enrolled (the
                    server may require enrollment before any reveal).
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            <LiveAlert id="reveal-error" variant="danger" isInline message={error} />
          </Form>
        ) : (
          <div aria-live="polite">
            {!hidden ? (
              <>
                <p style={{ marginBottom: 8 }}>
                  Recovery key displayed - type it at the boot prompt. Escrow{" "}
                  <code>{revealed.escrow_id.slice(0, 8)}</code>
                  {revealed.keyslot !== null && <>, keyslot {revealed.keyslot}</>}.
                </p>
                <ClipboardCopy
                  isReadOnly
                  hoverTip="Copy"
                  clickTip="Copied"
                  variant="expansion"
                  isExpanded
                  style={{ fontFamily: "monospace" }}
                >
                  {revealed.recovery_key}
                </ClipboardCopy>
                {revealed.rotation_pending && (
                  <LiveAlert
                    id="reveal-rotation-note"
                    variant="info"
                    isInline
                    message="This key will be replaced automatically after the device next connects."
                    style={{ marginTop: 12 }}
                  />
                )}
              </>
            ) : (
              <LiveAlert
                id="reveal-hidden"
                variant="info"
                isInline
                message="The key was hidden after two minutes. Reveal it again if you still need it (each reveal is audited)."
              />
            )}
          </div>
        )}
      </ModalBody>
      <ModalFooter>
        {!revealed ? (
          <>
            <Button variant="danger" onClick={() => void doReveal()} isDisabled={!canSubmit} isLoading={busy}>
              Reveal key
            </Button>
            <Button variant="link" onClick={close} isDisabled={busy}>
              Cancel
            </Button>
          </>
        ) : (
          <Button variant="primary" onClick={close}>
            Done
          </Button>
        )}
      </ModalFooter>
    </Modal>
  );
};
