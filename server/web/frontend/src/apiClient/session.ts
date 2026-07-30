// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Session-expiry signalling. The API layer (non-React) calls
// notifySessionExpired() when a request fails authentication after a refresh
// attempt; the app shell subscribes and drops the user back to the login screen
// while preserving the current URL (so re-login returns them where they were).

const SESSION_EXPIRED_EVENT = "bor:session-expired";

// notifySessionExpired signals that the current session is no longer valid.
export function notifySessionExpired(): void {
  window.dispatchEvent(new CustomEvent(SESSION_EXPIRED_EVENT));
}

// onSessionExpired registers a listener; returns an unsubscribe function.
export function onSessionExpired(handler: () => void): () => void {
  window.addEventListener(SESSION_EXPIRED_EVENT, handler);
  return () => window.removeEventListener(SESSION_EXPIRED_EVENT, handler);
}
