// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Alert,
  Button,
  Form,
  FormGroup,
  TextInput,
  Spinner,
  TextContent,
  Text,
  ActionGroup,
} from "@patternfly/react-core";
import { getMFAStatus, mfaDisable, MFAStatus } from "../../apiClient/authApi";
import { MFASetupModal } from "./MFASetupModal";

export const MFATab: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [mfaStatus, setMfaStatus] = useState<MFAStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [setupModalOpen, setSetupModalOpen] = useState(false);
  const [disablePassword, setDisablePassword] = useState("");
  const [showDisableForm, setShowDisableForm] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [disableError, setDisableError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  const loadStatus = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const status = await getMFAStatus();
      setMfaStatus(status);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to load MFA status");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadStatus();
  }, [loadStatus]);

  const handleDisable = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setDisabling(true);
    setDisableError(null);
    try {
      await mfaDisable(disablePassword);
      setDisablePassword("");
      setShowDisableForm(false);
      setSuccessMsg("Two-factor authentication has been disabled.");
      await loadStatus();
    } catch (err: unknown) {
      setDisableError(err instanceof Error ? err.message : "Failed to disable MFA");
    } finally {
      setDisabling(false);
    }
  };

  const handleSetupSuccess = async () => {
    setSuccessMsg("Two-factor authentication has been enabled successfully.");
    await loadStatus();
  };

  if (loading) return <Spinner size="lg" />;

  return (
    <>
      {error && (
        <Alert
          variant="danger"
          title={error}
          isInline
          style={{ marginBottom: 16 }}
        />
      )}
      {successMsg && (
        <Alert
          variant="success"
          title={successMsg}
          isInline
          actionClose={
            <Button variant="plain" onClick={() => setSuccessMsg(null)}>
              &times;
            </Button>
          }
          style={{ marginBottom: 16 }}
        />
      )}

      <TextContent style={{ marginBottom: 16 }}>
        <Text component="h2">Two-Factor Authentication (TOTP)</Text>
        {mfaStatus?.enabled ? (
          <Text>
            MFA is <strong>enabled</strong>
            {mfaStatus.algorithm ? ` (${mfaStatus.algorithm})` : ""}.
          </Text>
        ) : (
          <Text>MFA is <strong>not enabled</strong> for your account.</Text>
        )}
      </TextContent>

      {!mfaStatus?.enabled && (
        <Button variant="primary" onClick={() => setSetupModalOpen(true)}>
          Enable MFA
        </Button>
      )}

      {mfaStatus?.enabled && !showDisableForm && (
        <Button
          variant="danger"
          onClick={() => {
            setShowDisableForm(true);
            setDisableError(null);
          }}
        >
          Disable MFA
        </Button>
      )}

      {mfaStatus?.enabled && showDisableForm && (
        <Form onSubmit={handleDisable} style={{ maxWidth: 400, marginTop: 16 }}>
          {disableError && (
            <Alert
              variant="danger"
              title={disableError}
              isInline
              style={{ marginBottom: 16 }}
            />
          )}
          <FormGroup
            label="Confirm your password to disable MFA"
            fieldId="mfa-disable-password"
          >
            <TextInput
              id="mfa-disable-password"
              type="password"
              value={disablePassword}
              onChange={(_ev, v) => setDisablePassword(v)}
              autoComplete="current-password"
              autoFocus
            />
          </FormGroup>
          <ActionGroup>
            <Button
              variant="danger"
              type="submit"
              isDisabled={disabling || !disablePassword}
              isLoading={disabling}
            >
              Disable MFA
            </Button>
            <Button
              variant="link"
              onClick={() => {
                setShowDisableForm(false);
                setDisablePassword("");
                setDisableError(null);
              }}
              isDisabled={disabling}
            >
              Cancel
            </Button>
          </ActionGroup>
        </Form>
      )}

      <MFASetupModal
        isOpen={setupModalOpen}
        onClose={() => setSetupModalOpen(false)}
        onSuccess={handleSetupSuccess}
      />
    </>
  );
};
