// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState } from "react";
import {
  Form,
  FormGroup,
  TextInput,
  Button,
  ActionGroup,
} from "@patternfly/react-core";
import { LiveAlert } from "../../components/LiveAlert";
import { changePassword } from "../../apiClient/authApi";

export const PasswordTab: React.FC = () => {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const mismatch = confirm !== "" && next !== confirm;
  const canSubmit = current !== "" && next !== "" && confirm !== "" && !mismatch;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await changePassword(current, next);
      setSuccess("Password changed successfully.");
      setCurrent("");
      setNext("");
      setConfirm("");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to change password");
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <LiveAlert message={error} isInline style={{ marginBottom: 16 }} />
      <LiveAlert message={success} isInline variant="success" style={{ marginBottom: 16 }} />
      <Form onSubmit={handleSubmit} style={{ maxWidth: 480 }}>
        <FormGroup label="Current password" isRequired fieldId="pw-current">
          <TextInput
            id="pw-current"
            type="password"
            value={current}
            onChange={(_ev, v) => setCurrent(v)}
            autoComplete="current-password"
            aria-required
          />
        </FormGroup>
        <FormGroup label="New password" isRequired fieldId="pw-new">
          <TextInput
            id="pw-new"
            type="password"
            value={next}
            onChange={(_ev, v) => setNext(v)}
            autoComplete="new-password"
            aria-required
          />
        </FormGroup>
        <FormGroup
          label="Confirm new password"
          isRequired
          fieldId="pw-confirm"
          helperTextInvalid="Passwords do not match"
          validated={mismatch ? "error" : "default"}
        >
          <TextInput
            id="pw-confirm"
            type="password"
            value={confirm}
            onChange={(_ev, v) => setConfirm(v)}
            autoComplete="new-password"
            validated={mismatch ? "error" : "default"}
            aria-required
            aria-invalid={mismatch || undefined}
          />
        </FormGroup>
        <ActionGroup>
          <Button
            type="submit"
            variant="primary"
            isDisabled={!canSubmit || saving}
            isLoading={saving}
          >
            Change password
          </Button>
        </ActionGroup>
      </Form>
    </>
  );
};
