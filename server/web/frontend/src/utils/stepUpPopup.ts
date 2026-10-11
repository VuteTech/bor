// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * Re-authentication at an identity provider in a popup window. The popup
 * ends on "/?sso_stepup=done" (or "=failed&reason=..."); finishStepUpPopup,
 * run at startup, reports that to the opener and closes the popup.
 */

const MESSAGE_TYPE = "bor-step-up";

interface StepUpMessage {
  type: typeof MESSAGE_TYPE;
  ok: boolean;
  reason?: string;
}

const REASONS: Record<string, string> = {
  not_fresh: "The identity provider did not ask you to sign in again.",
  wrong_account: "You signed in as a different user.",
  disabled: "Your account is disabled.",
  unavailable:
    "Re-authentication through your identity provider is not available.",
};

/**
 * When this window is a step-up popup, reports the result to the opener,
 * closes the window and returns true (the app must not render).
 */
export function finishStepUpPopup(): boolean {
  const params = new URLSearchParams(window.location.search);
  const result = params.get("sso_stepup");
  if (!result || !window.opener) return false;
  const msg: StepUpMessage = { type: MESSAGE_TYPE, ok: result === "done" };
  if (!msg.ok) msg.reason = params.get("reason") ?? "failed";
  window.opener.postMessage(msg, window.location.origin);
  window.close();
  return true;
}

/** Opens url in a popup and resolves when the re-authentication succeeded. */
export function reauthenticateInPopup(url: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const popup = window.open(url, "bor-step-up", "popup,width=520,height=720");
    if (!popup) {
      reject(
        new Error(
          "Allow pop-ups for this site to re-authenticate with your identity provider.",
        ),
      );
      return;
    }
    const done = () => {
      window.removeEventListener("message", onMessage);
      window.clearInterval(closedPoll);
    };
    const onMessage = (ev: MessageEvent) => {
      if (ev.origin !== window.location.origin || ev.source !== popup) return;
      const data = ev.data as StepUpMessage;
      if (data?.type !== MESSAGE_TYPE) return;
      done();
      if (data.ok) resolve();
      else
        reject(
          new Error(REASONS[data.reason ?? ""] ?? "Re-authentication failed."),
        );
    };
    const closedPoll = window.setInterval(() => {
      if (popup.closed) {
        done();
        reject(new Error("Re-authentication was cancelled."));
      }
    }, 500);
    window.addEventListener("message", onMessage);
  });
}
