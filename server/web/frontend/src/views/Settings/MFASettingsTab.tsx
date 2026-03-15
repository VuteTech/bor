// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Alert,
  Button,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Switch,
  Spinner,
  ActionGroup,
  FormSelect,
  FormSelectOption,
} from "@patternfly/react-core";
import { getMFASettings, updateMFASettings, MFASettings } from "../../apiClient/authApi";

export const MFASettingsTab: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);
  const [mfaRequired, setMfaRequired] = useState(false);
  const [totpAlgorithm, setTotpAlgorithm] = useState<"SHA256" | "SHA512">("SHA256");

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const settings = await getMFASettings();
      setMfaRequired(settings.mfa_required);
      setTotpAlgorithm(settings.totp_algorithm);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to load MFA settings");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const updated = await updateMFASettings({
        mfa_required: mfaRequired,
        totp_algorithm: totpAlgorithm,
      } as MFASettings);
      setMfaRequired(updated.mfa_required);
      setTotpAlgorithm(updated.totp_algorithm);
      setSuccess("MFA settings saved successfully.");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to save MFA settings");
    } finally {
      setSaving(false);
    }
  }, [mfaRequired, totpAlgorithm]);

  if (loading) return <Spinner size="lg" />;

  return (
    <>
      {error && (
        <Alert
          variant="danger"
          title={error}
          isInline
          actionClose={
            <Button variant="plain" onClick={() => setError(null)}>
              &times;
            </Button>
          }
          style={{ marginBottom: 16 }}
        />
      )}
      {success && (
        <Alert
          variant="success"
          title={success}
          isInline
          actionClose={
            <Button variant="plain" onClick={() => setSuccess(null)}>
              &times;
            </Button>
          }
          style={{ marginBottom: 16 }}
        />
      )}

      <Form style={{ maxWidth: 600 }}>
        <FormGroup label="Require MFA for all local users" fieldId="mfa-required">
          <Switch
            id="mfa-required"
            isChecked={mfaRequired}
            onChange={(_ev, v) => setMfaRequired(v)}
            label="Enabled"
            labelOff="Disabled"
          />
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                When enabled, all local user accounts must configure TOTP MFA before
                they can log in.
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>

        <FormGroup label="TOTP algorithm" fieldId="mfa-totp-algorithm">
          <FormSelect
            id="mfa-totp-algorithm"
            value={totpAlgorithm}
            onChange={(_ev, v) => setTotpAlgorithm(v as "SHA256" | "SHA512")}
            aria-label="TOTP algorithm"
          >
            <FormSelectOption value="SHA256" label="SHA256" />
            <FormSelectOption value="SHA512" label="SHA512" />
          </FormSelect>
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                Hashing algorithm used for TOTP code generation. SHA256 is widely
                supported; SHA512 offers a higher security margin.
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>

        <ActionGroup>
          <Button
            variant="primary"
            onClick={handleSave}
            isDisabled={saving}
            isLoading={saving}
          >
            Save
          </Button>
        </ActionGroup>
      </Form>
    </>
  );
};
