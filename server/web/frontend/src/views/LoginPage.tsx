// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect } from "react";
import {
  LoginPage as PFLoginPage,
  LoginForm,
  LoginMainFooterLinksItem,
  ListVariant,
} from "@patternfly/react-core";
import { login } from "../apiClient/authApi";
import logo from "../assets/logo.svg";
import background from "../assets/background.svg";

interface LoginPageProps {
  onLoggedIn: (token: string, user: { username: string; full_name: string }) => void;
}

export const LoginPage: React.FC<LoginPageProps> = ({ onLoggedIn }) => {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
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

  const performLogin = async (ev: React.MouseEvent | React.FormEvent) => {
    ev.preventDefault();
    setErrorMsg(null);
    setPending(true);
    try {
      const result = await login(username, password);
      if (result.token) {
        onLoggedIn(result.token, result.user);
      } else {
        setErrorMsg("Invalid username or password");
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Login failed";
      setErrorMsg(msg);
    } finally {
      setPending(false);
    }
  };

  const loginForm = (
    <LoginForm
      showHelperText={!!errorMsg}
      helperText={errorMsg || ""}
      usernameLabel="Username"
      usernameValue={username}
      onChangeUsername={(_ev, v) => setUsername(v)}
      passwordLabel="Password"
      passwordValue={password}
      onChangePassword={(_ev, v) => setPassword(v)}
      onLoginButtonClick={performLogin}
      isLoginButtonDisabled={pending || !username || !password}
      loginButtonLabel={pending ? "Logging in..." : "Log in"}
    />
  );

  const footerLinks = (
    <LoginMainFooterLinksItem
      href="https://github.com/VuteTech/Bor"
      target="_blank"
      rel="noopener noreferrer"
    >
      Bor on GitHub
    </LoginMainFooterLinksItem>
  );

  return (
    <PFLoginPage
      footerListVariants={ListVariant.inline}
      brandImgSrc={logo}
      brandImgAlt="Bor logo"
      textContent="Enterprise Linux Desktop Policy Manager"
      loginTitle="Log in to your account"
      loginSubtitle="Enter your credentials to continue"
      footerListItems={footerLinks}
    >
      {loginForm}
    </PFLoginPage>
  );
};
