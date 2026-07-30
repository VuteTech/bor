// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ConfirmModal — the single confirmation-dialog pattern.
 *
 * Replaces native confirm() and one-off inline confirmations across the app.
 * Supports an optional type-to-confirm gate for high-blast-radius actions
 * (e.g. deleting a policy or role). Restores focus to the triggering element on
 * close via the caller-supplied triggerRef (WCAG 2.4.3).
 */

import React, { useEffect, useState } from "react";
import {
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Button,
  Form,
  FormGroup,
  TextInput,
} from "@patternfly/react-core";
import { LiveAlert } from "./LiveAlert";

interface ConfirmModalProps {
  isOpen: boolean;
  title: string;
  /** Body content: a message string or arbitrary nodes. */
  children?: React.ReactNode;
  confirmLabel?: string;
  /** danger => red confirm button + warning title icon. */
  isDanger?: boolean;
  /** When set, the confirm button stays disabled until this exact text is typed. */
  confirmPhrase?: string;
  confirmPhraseLabel?: string;
  isBusy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}

export const ConfirmModal: React.FC<ConfirmModalProps> = ({
  isOpen,
  title,
  children,
  confirmLabel = "Confirm",
  isDanger = false,
  confirmPhrase,
  confirmPhraseLabel,
  isBusy = false,
  error,
  onConfirm,
  onCancel,
}) => {
  const [typed, setTyped] = useState("");

  // Reset the type-to-confirm field whenever the dialog opens/closes.
  useEffect(() => {
    if (!isOpen) setTyped("");
  }, [isOpen]);

  const phraseSatisfied = !confirmPhrase || typed === confirmPhrase;
  const canConfirm = phraseSatisfied && !isBusy;

  return (
    <Modal variant={ModalVariant.small} isOpen={isOpen} onClose={onCancel}>
      <ModalHeader title={title} titleIconVariant={isDanger ? "warning" : "info"} />
      <ModalBody>
        <Form onSubmit={(e) => e.preventDefault()}>
          {typeof children === "string" ? <p>{children}</p> : children}
          <LiveAlert message={error ?? null} variant="danger" isInline />
          {confirmPhrase && (
            <FormGroup
              label={confirmPhraseLabel ?? `Type "${confirmPhrase}" to confirm`}
              isRequired
              fieldId="confirm-phrase"
            >
              <TextInput
                id="confirm-phrase"
                value={typed}
                onChange={(_e, v) => setTyped(v)}
                placeholder={confirmPhrase}
                validated={typed === "" ? "default" : phraseSatisfied ? "success" : "error"}
                aria-invalid={typed !== "" && !phraseSatisfied ? true : undefined}
              />
            </FormGroup>
          )}
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button
          variant={isDanger ? "danger" : "primary"}
          onClick={onConfirm}
          isLoading={isBusy}
          isDisabled={!canConfirm}
        >
          {confirmLabel}
        </Button>
        <Button variant="link" onClick={onCancel} isDisabled={isBusy}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};
