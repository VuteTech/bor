// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Button,
  Form,
  FormGroup,
  TextInput,
  Spinner,
  Label,
  ActionGroup,
} from "@patternfly/react-core";
import { LiveAlert } from "../../components/LiveAlert";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import PencilAltIcon from "@patternfly/react-icons/dist/esm/icons/pencil-alt-icon";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import KeyIcon from "@patternfly/react-icons/dist/esm/icons/key-icon";
import MobileAltIcon from "@patternfly/react-icons/dist/esm/icons/mobile-alt-icon";
import {
  getMFAStatus,
  mfaDisable,
  MFAStatus,
  listWebAuthnCredentials,
  renameWebAuthnCredential,
  deleteWebAuthnCredential,
  WebAuthnCredential,
} from "../../apiClient/authApi";
import { MFASetupModal } from "./MFASetupModal";
import { WebAuthnSetupModal } from "./WebAuthnSetupModal";

/* ── Shared card styles ─────────────────────────────────────────────────── */

const methodCard: React.CSSProperties = {
  border: "1px solid var(--pf-v5-global--BorderColor--100)",
  borderRadius: 8,
  padding: "20px 24px",
  marginBottom: 16,
  background: "var(--pf-v5-global--BackgroundColor--100)",
};

const methodHeader: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 12,
  marginBottom: 4,
};

const methodIcon: React.CSSProperties = {
  width: 36,
  height: 36,
  borderRadius: 8,
  background: "var(--pf-v5-global--BackgroundColor--200)",
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  flexShrink: 0,
  color: "var(--pf-v5-global--Color--100)",
};

const methodBody: React.CSSProperties = {
  flex: 1,
};

const methodTitle: React.CSSProperties = {
  fontWeight: 600,
  fontSize: "1rem",
  color: "var(--pf-v5-global--Color--100)",
  margin: 0,
};

const methodDesc: React.CSSProperties = {
  fontSize: "0.875rem",
  color: "var(--pf-v5-global--Color--200)",
  marginTop: 2,
};

const methodActions: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 8,
  flexShrink: 0,
};

/* ── Security key row ───────────────────────────────────────────────────── */

const keyRow: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  gap: 12,
  padding: "10px 0",
  borderTop: "1px solid var(--pf-v5-global--BorderColor--100)",
};

const keyMeta: React.CSSProperties = {
  flex: 1,
  minWidth: 0,
};

const keyName: React.CSSProperties = {
  fontWeight: 500,
  fontSize: "0.9375rem",
  color: "var(--pf-v5-global--Color--100)",
  marginBottom: 1,
};

const keyDate: React.CSSProperties = {
  fontSize: "0.8125rem",
  color: "var(--pf-v5-global--Color--200)",
};

/* ════════════════════════════════════════════════════════════════════════ */

export const MFATab: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [mfaStatus, setMfaStatus] = useState<MFAStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  /* TOTP disable form */
  const [setupModalOpen, setSetupModalOpen] = useState(false);
  const [showDisableForm, setShowDisableForm] = useState(false);
  const [disablePassword, setDisablePassword] = useState("");
  const [disabling, setDisabling] = useState(false);
  const [disableError, setDisableError] = useState<string | null>(null);

  /* WebAuthn state */
  const [webAuthnCreds, setWebAuthnCreds] = useState<WebAuthnCredential[]>([]);
  const [webAuthnModalOpen, setWebAuthnModalOpen] = useState(false);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [renameSaving, setRenameSaving] = useState(false);
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

  /* ── Data loading ───────────────────────────────────────────────────── */

  const loadStatus = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setMfaStatus(await getMFAStatus());
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to load MFA status");
    } finally {
      setLoading(false);
    }
  }, []);

  const loadWebAuthnCreds = useCallback(async () => {
    try {
      setWebAuthnCreds(await listWebAuthnCredentials());
    } catch {
      // WebAuthn is optional — silently ignore if not configured on server
    }
  }, []);

  useEffect(() => {
    loadStatus();
    loadWebAuthnCreds();
  }, [loadStatus, loadWebAuthnCreds]);

  /* ── TOTP handlers ──────────────────────────────────────────────────── */

  const handleDisable = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setDisabling(true);
    setDisableError(null);
    try {
      await mfaDisable(disablePassword);
      setDisablePassword("");
      setShowDisableForm(false);
      setSuccessMsg("Authenticator app has been removed from your account.");
      await loadStatus();
    } catch (err: unknown) {
      setDisableError(err instanceof Error ? err.message : "Failed to disable authenticator app");
    } finally {
      setDisabling(false);
    }
  };

  const handleSetupSuccess = async () => {
    setSuccessMsg("Authenticator app set up successfully.");
    await loadStatus();
  };

  /* ── WebAuthn handlers ──────────────────────────────────────────────── */

  const handleWebAuthnSuccess = async (cred: WebAuthnCredential) => {
    setSuccessMsg(`Security key "${cred.name}" added to your account.`);
    await loadWebAuthnCreds();
  };

  const startRename = (cred: WebAuthnCredential) => {
    setRenamingId(cred.id);
    setRenameValue(cred.name);
  };

  const cancelRename = () => {
    setRenamingId(null);
    setRenameValue("");
  };

  const saveRename = async (id: string) => {
    setRenameSaving(true);
    try {
      await renameWebAuthnCredential(id, renameValue);
      setRenamingId(null);
      await loadWebAuthnCreds();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to rename security key");
    } finally {
      setRenameSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    setDeleting(true);
    try {
      await deleteWebAuthnCredential(id);
      setConfirmDeleteId(null);
      await loadWebAuthnCreds();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to remove security key");
    } finally {
      setDeleting(false);
    }
  };

  /* ── Render ─────────────────────────────────────────────────────────── */

  if (loading) {
    return (
      <div style={{ padding: "32px 0", textAlign: "center" }}>
        <Spinner size="lg" aria-label="Loading" />
      </div>
    );
  }

  const totpEnabled = mfaStatus?.enabled ?? false;

  return (
    <div style={{ padding: "4px 0" }}>

      {/* ── Global alerts ── */}
      <LiveAlert
        message={error}
        isInline
        actionClose={<Button variant="plain" onClick={() => setError(null)}>&times;</Button>}
        style={{ marginBottom: 16 }}
      />
      <LiveAlert
        message={successMsg}
        variant="success"
        isInline
        actionClose={<Button variant="plain" onClick={() => setSuccessMsg(null)}>&times;</Button>}
        style={{ marginBottom: 16 }}
      />

      {/* ── Section header ── */}
      <div style={{ marginBottom: 20 }}>
        <p style={{ fontSize: "0.875rem", color: "var(--pf-v5-global--Color--200)", margin: 0 }}>
          Add an extra layer of security to your account. After signing in with your
          password, you'll be asked for a verification from one of the methods below.
        </p>
      </div>

      {/* ══════════════════════════════════════════════════════════════════
          Method 1: Authenticator app (TOTP)
      ══════════════════════════════════════════════════════════════════ */}
      <div style={methodCard}>
        <div style={methodHeader}>
          <div style={methodIcon}>
            <MobileAltIcon />
          </div>
          <div style={methodBody}>
            <p style={methodTitle}>Authenticator app</p>
            <p style={methodDesc}>
              Use FreeOTP, Aegis, Google Authenticator, or any TOTP-compatible app.
            </p>
          </div>
          <div style={methodActions}>
            {totpEnabled ? (
              <>
                <Label color="green" icon={<CheckCircleIcon />}>Configured</Label>
                <Button
                  variant="link"
                  isDanger
                  onClick={() => {
                    setShowDisableForm((v) => !v);
                    setDisableError(null);
                    setDisablePassword("");
                  }}
                >
                  Remove
                </Button>
              </>
            ) : (
              <Button variant="primary" onClick={() => setSetupModalOpen(true)}>
                Set up
              </Button>
            )}
          </div>
        </div>

        {/* Inline disable form */}
        {totpEnabled && showDisableForm && (
          <div
            style={{
              marginTop: 16,
              paddingTop: 16,
              borderTop: "1px solid var(--pf-v5-global--BorderColor--100)",
            }}
          >
            <Form onSubmit={handleDisable} style={{ maxWidth: 420 }}>
              <LiveAlert message={disableError} isInline style={{ marginBottom: 12 }} />
              <FormGroup
                label="Confirm your password to remove the authenticator app"
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
                  Remove authenticator app
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
          </div>
        )}
      </div>

      {/* ══════════════════════════════════════════════════════════════════
          Method 2: Security keys (WebAuthn)
      ══════════════════════════════════════════════════════════════════ */}
      <div style={methodCard}>
        <div style={methodHeader}>
          <div style={methodIcon}>
            <KeyIcon />
          </div>
          <div style={methodBody}>
            <p style={methodTitle}>Security keys</p>
            <p style={methodDesc}>
              Hardware tokens (YubiKey, FIDO2 USB/NFC) or software keys (Bitwarden, 1Password).
            </p>
          </div>
          <div style={methodActions}>
            {webAuthnCreds.length > 0 && (
              <Label color="green" icon={<CheckCircleIcon />}>
                {webAuthnCreds.length === 1 ? "1 key" : `${webAuthnCreds.length} keys`}
              </Label>
            )}
            <Button variant="secondary" onClick={() => setWebAuthnModalOpen(true)}>
              Add key
            </Button>
          </div>
        </div>

        {/* Key list */}
        {webAuthnCreds.length > 0 && (
          <div style={{ marginTop: 8 }}>
            {webAuthnCreds.map((cred) => (
              <div key={cred.id} style={keyRow}>
                {renamingId === cred.id ? (
                  /* Inline rename */
                  <>
                    <TextInput
                      id={`rename-${cred.id}`}
                      type="text"
                      value={renameValue}
                      onChange={(_ev, v) => setRenameValue(v)}
                      style={{ maxWidth: 240 }}
                      autoFocus
                    />
                    <Button
                      variant="primary"
                      isSmall
                      onClick={() => saveRename(cred.id)}
                      isDisabled={renameSaving || !renameValue.trim()}
                      isLoading={renameSaving}
                    >
                      Save
                    </Button>
                    <Button
                      variant="link"
                      isSmall
                      onClick={cancelRename}
                      isDisabled={renameSaving}
                    >
                      Cancel
                    </Button>
                  </>
                ) : confirmDeleteId === cred.id ? (
                  /* Inline delete confirm */
                  <>
                    <div style={keyMeta}>
                      <p style={keyName}>{cred.name}</p>
                    </div>
                    <span
                      style={{
                        fontSize: "0.875rem",
                        color: "var(--pf-v5-global--danger-color--100)",
                        marginRight: 4,
                      }}
                    >
                      Remove this key?
                    </span>
                    <Button
                      variant="danger"
                      isSmall
                      onClick={() => handleDelete(cred.id)}
                      isDisabled={deleting}
                      isLoading={deleting}
                    >
                      Remove
                    </Button>
                    <Button
                      variant="link"
                      isSmall
                      onClick={() => setConfirmDeleteId(null)}
                      isDisabled={deleting}
                    >
                      Cancel
                    </Button>
                  </>
                ) : (
                  /* Normal row */
                  <>
                    <div style={keyMeta}>
                      <p style={keyName}>{cred.name}</p>
                      <p style={keyDate}>
                        Added {new Date(cred.created_at).toLocaleDateString()}
                        {cred.last_used_at
                          ? ` · Last used ${new Date(cred.last_used_at).toLocaleDateString()}`
                          : " · Never used"}
                      </p>
                    </div>
                    <Button
                      variant="plain"
                      isSmall
                      aria-label={`Rename ${cred.name}`}
                      onClick={() => startRename(cred)}
                    >
                      <PencilAltIcon />
                    </Button>
                    <Button
                      variant="plain"
                      isSmall
                      aria-label={`Remove ${cred.name}`}
                      onClick={() => setConfirmDeleteId(cred.id)}
                      style={{ color: "var(--pf-v5-global--danger-color--100)" }}
                    >
                      <TrashIcon />
                    </Button>
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ── Modals ── */}
      <MFASetupModal
        isOpen={setupModalOpen}
        onClose={() => setSetupModalOpen(false)}
        onSuccess={handleSetupSuccess}
      />
      <WebAuthnSetupModal
        isOpen={webAuthnModalOpen}
        onClose={() => setWebAuthnModalOpen(false)}
        onSuccess={handleWebAuthnSuccess}
      />
    </div>
  );
};
