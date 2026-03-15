// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Modal,
  ModalVariant,
  Button,
  TextInput,
  Alert,
  ClipboardCopy,
  Form,
  FormGroup,
  Spinner,
  Text,
  TextContent,
  TextList,
  TextListItem,
} from "@patternfly/react-core";
import { mfaSetupBegin, mfaSetupFinish } from "../../apiClient/authApi";

interface MFASetupModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

type Step = "qr" | "backup";

export const MFASetupModal: React.FC<MFASetupModalProps> = ({
  isOpen,
  onClose,
  onSuccess,
}) => {
  const [step, setStep] = useState<Step>("qr");
  const [secret, setSecret] = useState("");
  const [qrCodeUrl, setQrCodeUrl] = useState("");
  const [algorithm, setAlgorithm] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [backupCodes, setBackupCodes] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const beginSetup = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const result = await mfaSetupBegin();
      setSecret(result.secret);
      setQrCodeUrl(result.qr_code_url);
      setAlgorithm(result.algorithm);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to begin MFA setup");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (isOpen) {
      setStep("qr");
      setTotpCode("");
      setBackupCodes([]);
      setError(null);
      beginSetup();
    }
  }, [isOpen, beginSetup]);

  const handleVerify = async () => {
    setVerifying(true);
    setError(null);
    try {
      const result = await mfaSetupFinish(totpCode);
      setBackupCodes(result.backup_codes);
      setStep("backup");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Verification failed");
    } finally {
      setVerifying(false);
    }
  };

  const handleDone = () => {
    onSuccess();
    onClose();
  };

  const qrImageUrl = qrCodeUrl
    ? `https://api.qrserver.com/v1/create-qr-code/?data=${encodeURIComponent(qrCodeUrl)}&size=200x200`
    : "";

  const qrActions = [
    <Button
      key="verify"
      variant="primary"
      onClick={handleVerify}
      isDisabled={verifying || loading || totpCode.length !== 6}
      isLoading={verifying}
    >
      Verify
    </Button>,
    <Button key="cancel" variant="link" onClick={onClose} isDisabled={verifying}>
      Cancel
    </Button>,
  ];

  const backupActions = [
    <Button key="done" variant="primary" onClick={handleDone}>
      Done
    </Button>,
  ];

  return (
    <Modal
      variant={ModalVariant.medium}
      title={step === "qr" ? "Set up two-factor authentication" : "Save your backup codes"}
      isOpen={isOpen}
      onClose={onClose}
      actions={step === "qr" ? qrActions : backupActions}
    >
      {step === "qr" && (
        <>
          {error && (
            <Alert
              variant="danger"
              title={error}
              isInline
              style={{ marginBottom: 16 }}
            />
          )}
          {loading ? (
            <Spinner size="lg" />
          ) : (
            <>
              <TextContent style={{ marginBottom: 16 }}>
                <Text>
                  Scan the QR code below with your authenticator app (e.g. Google
                  Authenticator, FreeOTP, Aegis). Algorithm: <strong>{algorithm}</strong>
                </Text>
              </TextContent>
              {qrImageUrl && (
                <div style={{ textAlign: "center", marginBottom: 16 }}>
                  <img
                    src={qrImageUrl}
                    alt="TOTP QR Code"
                    width={200}
                    height={200}
                  />
                </div>
              )}
              <FormGroup label="Manual entry secret" fieldId="mfa-secret" style={{ marginBottom: 16 }}>
                <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied">
                  {secret}
                </ClipboardCopy>
              </FormGroup>
              <Form>
                <FormGroup
                  label="Enter 6-digit code from your authenticator app"
                  fieldId="mfa-totp-verify"
                >
                  <TextInput
                    id="mfa-totp-verify"
                    type="text"
                    inputMode="numeric"
                    pattern="[0-9]*"
                    maxLength={6}
                    value={totpCode}
                    onChange={(_ev, v) => setTotpCode(v.replace(/\D/g, ""))}
                    placeholder="6-digit code"
                    autoComplete="one-time-code"
                  />
                </FormGroup>
              </Form>
            </>
          )}
        </>
      )}

      {step === "backup" && (
        <>
          <Alert
            variant="warning"
            title="Store these backup codes in a safe place"
            isInline
            style={{ marginBottom: 16 }}
          >
            Each code can only be used once. If you lose access to your authenticator
            app, you can use a backup code to sign in.
          </Alert>
          <TextContent style={{ marginBottom: 16 }}>
            <TextList>
              {backupCodes.map((code) => (
                <TextListItem key={code} style={{ fontFamily: "monospace" }}>
                  {code}
                </TextListItem>
              ))}
            </TextList>
          </TextContent>
        </>
      )}
    </Modal>
  );
};
