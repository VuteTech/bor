// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect } from "react";
import {
  LoginPage as PFLoginPage,
  LoginMainFooterLinksItem,
  ListVariant,
  Form,
  FormGroup,
  TextInput,
  InputGroup,
  InputGroupItem,
  Button,
  ActionGroup,
  Content,
} from "@patternfly/react-core";
import EyeIcon from "@patternfly/react-icons/dist/esm/icons/eye-icon";
import EyeSlashIcon from "@patternfly/react-icons/dist/esm/icons/eye-slash-icon";
import { LiveAlert } from "../components/LiveAlert";
import { startAuthentication } from "@simplewebauthn/browser";
import {
  authBegin,
  authStep,
  webAuthnAuthBegin,
  webAuthnAuthFinish,
  UserInfo,
} from "../apiClient/authApi";
import logo from "../assets/logo.svg";

interface LoginPageProps {
  onLoggedIn: (token: string, user: { username: string; full_name: string }) => void;
}

type Phase = "username" | "totp" | "password" | "webauthn";

export const LoginPage: React.FC<LoginPageProps> = ({ onLoggedIn }) => {
  const [phase, setPhase] = useState<Phase>("username");
  const [usernameInput, setUsernameInput] = useState("");
  const [currentUsername, setCurrentUsername] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [passwordInput, setPasswordInput] = useState("");
  const [mfaMethods, setMfaMethods] = useState<string[]>([]);
  const [pending, setPending] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const [capsLockOn, setCapsLockOn] = useState(false);

  // Reflect the Caps Lock state so we can warn before a failed password attempt.
  const trackCapsLock = (e: React.KeyboardEvent) => {
    if (typeof e.getModifierState === "function") {
      setCapsLockOn(e.getModifierState("CapsLock"));
    }
  };

  const resetToPhase1 = () => {
    setPhase("username");
    setSessionToken("");
    setTotpCode("");
    setPasswordInput("");
    setMfaMethods([]);
    setErrorMsg(null);
    setShowPassword(false);
    setCapsLockOn(false);
  };

  const handleUsernameSubmit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setErrorMsg(null);
    setPending(true);
    try {
      const result = await authBegin(usernameInput);
      setCurrentUsername(usernameInput);
      setSessionToken(result.session_token);
      if (result.next === "mfa") {
        const methods = result.mfa_methods ?? [];
        setMfaMethods(methods);
        if (methods.includes("webauthn")) {
          setPhase("webauthn");
        } else {
          setPhase("totp");
        }
      } else {
        setPhase("password");
      }
    } catch (err: unknown) {
      setErrorMsg(err instanceof Error ? err.message : "Request failed");
    } finally {
      setPending(false);
    }
  };

  const handleTotpSubmit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setErrorMsg(null);
    setPending(true);
    try {
      const result = await authStep(sessionToken, "totp", totpCode);
      if (result.token && result.user) {
        onLoggedIn(result.token, result.user as UserInfo);
      } else if (result.session_token && result.next === "password") {
        setSessionToken(result.session_token);
        setTotpCode("");
        setPhase("password");
      } else {
        setErrorMsg("Unexpected response from server");
      }
    } catch (err: unknown) {
      setErrorMsg(err instanceof Error ? err.message : "Authentication failed");
    } finally {
      setPending(false);
    }
  };

  const handleWebAuthnSubmit = async () => {
    setErrorMsg(null);
    setPending(true);
    try {
      const { publicKey } = await webAuthnAuthBegin(sessionToken);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const assertion = await startAuthentication({ optionsJSON: publicKey as any });
      const result = await webAuthnAuthFinish(sessionToken, assertion);
      if (result.token && result.user) {
        onLoggedIn(result.token, result.user as UserInfo);
      } else {
        setErrorMsg("Unexpected response from server");
      }
    } catch (err: unknown) {
      setErrorMsg(err instanceof Error ? err.message : "WebAuthn authentication failed");
    } finally {
      setPending(false);
    }
  };

  const handlePasswordSubmit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setErrorMsg(null);
    setPending(true);
    try {
      const result = await authStep(sessionToken, "password", passwordInput);
      if (result.token && result.user) {
        onLoggedIn(result.token, result.user as UserInfo);
      } else {
        setErrorMsg("Invalid credentials");
      }
    } catch (err: unknown) {
      setErrorMsg(err instanceof Error ? err.message : "Authentication failed");
    } finally {
      setPending(false);
    }
  };

  const footerLinks = (
    <LoginMainFooterLinksItem
      href="https://github.com/VuteTech/Bor"
      target="_blank"
      rel="noopener noreferrer"
    >
      Bor on GitHub
    </LoginMainFooterLinksItem>
  );

  const subtitle =
    phase === "username"
      ? "Enter your username to continue"
      : phase === "totp"
      ? "Enter your authenticator code"
      : phase === "webauthn"
      ? "Verify your identity"
      : "Enter your password";

  const loginForm = (
    <div style={{ padding: "24px 0" }}>
      <LiveAlert
        message={errorMsg}
        isInline
        actionClose={
          <Button variant="plain" aria-label="Close" onClick={() => setErrorMsg(null)}>
            &times;
          </Button>
        }
        style={{ marginBottom: 16 }}
      />

      {phase === "username" && (
        <Form onSubmit={handleUsernameSubmit}>
          <FormGroup label="Username" fieldId="login-username">
            <TextInput
              id="login-username"
              type="text"
              value={usernameInput}
              onChange={(_ev, v) => setUsernameInput(v)}
              autoFocus
              autoComplete="username"
            />
          </FormGroup>
          <ActionGroup>
            <Button
              variant="primary"
              type="submit"
              isDisabled={pending || !usernameInput.trim()}
              isLoading={pending}
            >
              Continue
            </Button>
          </ActionGroup>
        </Form>
      )}

      {phase === "totp" && (
        <Form onSubmit={handleTotpSubmit}>
          <FormGroup label="Username" fieldId="login-totp-user">
            <TextInput
              id="login-totp-user"
              type="text"
              value={currentUsername}
              isDisabled
            />
          </FormGroup>
          <FormGroup label="Authenticator code" fieldId="login-totp-code">
            <TextInput
              id="login-totp-code"
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              maxLength={6}
              value={totpCode}
              onChange={(_ev, v) => setTotpCode(v.replace(/\D/g, ""))}
              autoFocus
              autoComplete="one-time-code"
              placeholder="6-digit code"
            />
          </FormGroup>
          <ActionGroup>
            <Button
              variant="primary"
              type="submit"
              isDisabled={pending || totpCode.length !== 6}
              isLoading={pending}
            >
              Verify
            </Button>
            <Button variant="link" onClick={resetToPhase1} isDisabled={pending}>
              Back
            </Button>
          </ActionGroup>
        </Form>
      )}

      {phase === "webauthn" && (
        <div>
          <Content style={{ marginBottom: 24 }}>
            <Content component="h3">{currentUsername}</Content>
            <Content>Use your security key or passkey to continue.</Content>
          </Content>
          <ActionGroup>
            <Button
              variant="primary"
              onClick={handleWebAuthnSubmit}
              isDisabled={pending}
              isLoading={pending}
            >
              Authenticate with security key
            </Button>
            <Button variant="link" onClick={resetToPhase1} isDisabled={pending}>
              Back
            </Button>
          </ActionGroup>
          {mfaMethods.includes("totp") && (
            <div style={{ marginTop: 16 }}>
              <Button
                variant="link"
                isInline
                onClick={() => {
                  setErrorMsg(null);
                  setPhase("totp");
                }}
                isDisabled={pending}
              >
                Use authenticator code instead
              </Button>
            </div>
          )}
        </div>
      )}

      {phase === "password" && (
        <Form onSubmit={handlePasswordSubmit}>
          <FormGroup label="Username" fieldId="login-pw-user">
            <TextInput
              id="login-pw-user"
              type="text"
              value={currentUsername}
              isDisabled
            />
          </FormGroup>
          <FormGroup label="Password" fieldId="login-password">
            <InputGroup>
              <InputGroupItem isFill>
                <TextInput
                  id="login-password"
                  type={showPassword ? "text" : "password"}
                  value={passwordInput}
                  onChange={(_ev, v) => setPasswordInput(v)}
                  onKeyUp={trackCapsLock}
                  onKeyDown={trackCapsLock}
                  onBlur={() => setCapsLockOn(false)}
                  autoFocus
                  autoComplete="current-password"
                  aria-describedby={capsLockOn ? "login-capslock-hint" : undefined}
                />
              </InputGroupItem>
              <InputGroupItem>
                <Button
                  variant="control"
                  aria-label={showPassword ? "Hide password" : "Show password"}
                  aria-pressed={showPassword}
                  onClick={() => setShowPassword((v) => !v)}
                >
                  {showPassword ? <EyeSlashIcon /> : <EyeIcon />}
                </Button>
              </InputGroupItem>
            </InputGroup>
            {capsLockOn && (
              <div
                id="login-capslock-hint"
                aria-live="polite"
                style={{ marginTop: 4, fontSize: "0.85rem", color: "var(--pf-t--global--text--color--status--warning--default, #f0ab00)" }}
              >
                Caps Lock is on.
              </div>
            )}
          </FormGroup>
          <ActionGroup>
            <Button
              variant="primary"
              type="submit"
              isDisabled={pending || !passwordInput}
              isLoading={pending}
            >
              {pending ? "Logging in..." : "Log in"}
            </Button>
            <Button variant="link" onClick={resetToPhase1} isDisabled={pending}>
              Back
            </Button>
          </ActionGroup>
        </Form>
      )}
    </div>
  );

  return (
    <PFLoginPage
      footerListVariants={ListVariant.inline}
      brandImgSrc={logo}
      brandImgAlt="Bor logo"
      textContent="Enterprise Linux Desktop Policy Manager"
      loginTitle="Log in to your account"
      loginSubtitle={subtitle}
      footerListItems={footerLinks}
    >
      {loginForm}
    </PFLoginPage>
  );
};
