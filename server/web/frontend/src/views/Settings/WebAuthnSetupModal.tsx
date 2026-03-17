// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect } from "react";
import {
  Modal,
  ModalHeader,
  ModalBody,
  ModalFooter,
  ModalVariant,
  Button,
  TextInput,
  Alert,
  Form,
  FormGroup,
  Spinner,
  Content,
} from "@patternfly/react-core";
import { startRegistration } from "@simplewebauthn/browser";
import {
  webAuthnRegisterBegin,
  webAuthnRegisterFinish,
  WebAuthnCredential,
} from "../../apiClient/authApi";

interface WebAuthnSetupModalProps {
  isOpen: boolean;
  onClose: () => void;
  onSuccess: (cred: WebAuthnCredential) => void;
}

type Step = "name" | "registering" | "success";

export const WebAuthnSetupModal: React.FC<WebAuthnSetupModalProps> = ({
  isOpen,
  onClose,
  onSuccess,
}) => {
  const [step, setStep] = useState<Step>("name");
  const [credentialName, setCredentialName] = useState("Security Key");
  const [statusMsg, setStatusMsg] = useState("");
  const [registeredCred, setRegisteredCred] = useState<WebAuthnCredential | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) {
      setStep("name");
      setCredentialName("Security Key");
      setStatusMsg("");
      setRegisteredCred(null);
      setError(null);
    }
  }, [isOpen]);

  const handleRegister = async () => {
    setError(null);
    setStep("registering");
    setStatusMsg("Contacting server…");
    try {
      const { publicKey } = await webAuthnRegisterBegin();
      setStatusMsg("Waiting for security key — follow your browser's prompt…");
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const credential = await startRegistration({ optionsJSON: publicKey as any });
      setStatusMsg("Finishing registration…");
      const saved = await webAuthnRegisterFinish(credentialName, credential);
      setRegisteredCred(saved);
      setStep("success");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Registration failed");
      setStep("name");
    }
  };

  const handleDone = () => {
    if (registeredCred) {
      onSuccess(registeredCred);
    }
    onClose();
  };

  const nameActions = [
    <Button
      key="register"
      variant="primary"
      onClick={handleRegister}
      isDisabled={!credentialName.trim()}
    >
      Register
    </Button>,
    <Button key="cancel" variant="link" onClick={onClose}>
      Cancel
    </Button>,
  ];

  const successActions = [
    <Button key="done" variant="primary" onClick={handleDone}>
      Done
    </Button>,
  ];

  const title =
    step === "success"
      ? "Security key registered"
      : "Register a security key";

  return (
    <Modal
      variant={ModalVariant.small}
      isOpen={isOpen}
      onClose={onClose}
    >
      <ModalHeader title={title} />
      <ModalBody>
      {step === "name" && (
        <>
          {error && (
            <Alert
              variant="danger"
              title={error}
              isInline
              style={{ marginBottom: 16 }}
            />
          )}
          <Content style={{ marginBottom: 16 }}>
            <Content>
              Give your security key a name so you can identify it later (e.g.
              "YubiKey 5", "Bitwarden", "Face ID").
            </Content>
          </Content>
          <Form>
            <FormGroup label="Security key name" fieldId="webauthn-cred-name">
              <TextInput
                id="webauthn-cred-name"
                type="text"
                value={credentialName}
                onChange={(_ev, v) => setCredentialName(v)}
                autoFocus
              />
            </FormGroup>
          </Form>
        </>
      )}

      {step === "registering" && (
        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <Spinner size="lg" />
          <Content>
            <Content>{statusMsg}</Content>
          </Content>
        </div>
      )}

      {step === "success" && (
        <Content>
          <Content>
            Security key registered successfully!
          </Content>
          <Content>
            Name: <strong>{registeredCred?.name}</strong>
          </Content>
        </Content>
      )}
      </ModalBody>
      <ModalFooter>
        {step === "success" ? successActions : step === "name" ? nameActions : null}
      </ModalFooter>
    </Modal>
  );
};
