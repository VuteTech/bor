// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ToastHost — a single app-wide toast region.
 *
 * Provides a `useToast()` hook so any page can surface success/error feedback
 * without wiring its own alert region. Toasts render in a PF6 AlertGroup with a
 * polite/assertive live region, so they are announced to screen readers
 * (WCAG 4.1.3). Success/info toasts auto-dismiss; danger/warning stay until
 * dismissed so the user doesn't miss them.
 */

import React, { createContext, useCallback, useContext, useState } from "react";
import {
  AlertGroup,
  Alert,
  AlertActionCloseButton,
  AlertProps,
} from "@patternfly/react-core";

type ToastVariant = AlertProps["variant"];

interface ToastOptions {
  variant?: ToastVariant;
  title: string;
  detail?: string;
}

interface Toast extends ToastOptions {
  key: number;
}

interface ToastContextValue {
  addToast: (opts: ToastOptions) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

// useToast returns { addToast }. Safe to call even outside the provider
// (addToast becomes a no-op) so components remain usable in isolation/tests.
export function useToast(): ToastContextValue {
  return useContext(ToastContext) ?? { addToast: () => undefined };
}

const AUTO_DISMISS_MS = 6000;

export const ToastProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [toasts, setToasts] = useState<Toast[]>([]);
  // Monotonic key without Date.now()/Math.random() (unavailable in some envs).
  const nextKey = React.useRef(0);

  const remove = useCallback((key: number) => {
    setToasts((prev) => prev.filter((t) => t.key !== key));
  }, []);

  const addToast = useCallback(
    (opts: ToastOptions) => {
      const key = nextKey.current++;
      const variant: ToastVariant = opts.variant ?? "success";
      setToasts((prev) => [...prev, { ...opts, variant, key }]);
      // Positive/neutral toasts auto-dismiss; danger/warning stay until closed.
      if (variant === "success" || variant === "info") {
        window.setTimeout(() => remove(key), AUTO_DISMISS_MS);
      }
    },
    [remove],
  );

  return (
    <ToastContext.Provider value={{ addToast }}>
      {children}
      <AlertGroup isToast isLiveRegion>
        {toasts.map((t) => (
          <Alert
            key={t.key}
            variant={t.variant}
            title={t.title}
            timeout={false}
            actionClose={
              <AlertActionCloseButton
                title="Close"
                onClose={() => remove(t.key)}
              />
            }
          >
            {t.detail}
          </Alert>
        ))}
      </AlertGroup>
    </ToastContext.Provider>
  );
};
