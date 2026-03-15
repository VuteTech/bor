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
  Button,
  Alert,
  ActionGroup,
} from "@patternfly/react-core";
import { authBegin, authStep, UserInfo } from "../apiClient/authApi";
import logo from "../assets/logo.svg";
import background from "../assets/background.svg";

interface LoginPageProps {
  onLoggedIn: (token: string, user: { username: string; full_name: string }) => void;
}

type Phase = "username" | "totp" | "password";

export const LoginPage: React.FC<LoginPageProps> = ({ onLoggedIn }) => {
  const [phase, setPhase] = useState<Phase>("username");
  const [usernameInput, setUsernameInput] = useState("");
  const [currentUsername, setCurrentUsername] = useState("");
  const [sessionToken, setSessionToken] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [passwordInput, setPasswordInput] = useState("");
  const [pending, setPending] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    const injected = document.createElement("style");
    injected.textContent = `
      .pf-v5-c-login {
        background: url(${background}) no-repeat center center fixed !important;
        background-size: cover !important;
      }
      .pf-v5-c-login__container {
        background: transparent !important;
      }
    `;
    document.head.appendChild(injected);
    return () => {
      injected.remove();
    };
  }, []);

  const resetToPhase1 = () => {
    setPhase("username");
    setSessionToken("");
    setTotpCode("");
    setPasswordInput("");
    setErrorMsg(null);
  };

  const handleUsernameSubmit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setErrorMsg(null);
    setPending(true);
    try {
      const result = await authBegin(usernameInput);
      setCurrentUsername(usernameInput);
      setSessionToken(result.session_token);
      if (result.next === "totp") {
        setPhase("totp");
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
      : "Enter your password";

  const loginForm = (
    <div style={{ padding: "24px 0" }}>
      {errorMsg && (
        <Alert
          variant="danger"
          title={errorMsg}
          isInline
          actionClose={
            <Button variant="plain" onClick={() => setErrorMsg(null)}>
              &times;
            </Button>
          }
          style={{ marginBottom: 16 }}
        />
      )}

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
            <TextInput
              id="login-password"
              type="password"
              value={passwordInput}
              onChange={(_ev, v) => setPasswordInput(v)}
              autoFocus
              autoComplete="current-password"
            />
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
