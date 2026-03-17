// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * LiveAlert — accessible wrapper around PF6 Alert.
 *
 * The outer <div> with aria-live is always present in the DOM.  When the
 * `message` prop is truthy (or explicit `title` + children are provided)
 * the PF6 Alert renders inside it, and screen readers announce the change.
 *
 * WCAG 2.1 SC 4.1.3 — Status Messages
 *   variant danger | warning  → aria-live="assertive"  (role="alert")
 *   variant success | info    → aria-live="polite"     (role="status")
 */

import React from "react";
import { Alert, AlertProps } from "@patternfly/react-core";

export interface LiveAlertProps extends Omit<AlertProps, "title"> {
  /**
   * When truthy the Alert is shown and used as its title unless `title` is
   * also provided. When falsy the Alert is hidden but the live region stays
   * in the DOM (required for screen readers to detect future changes).
   */
  message?: string | null;
  /** Explicit title; if omitted `message` is used as the title. */
  title?: string;
  children?: React.ReactNode;
}

export const LiveAlert: React.FC<LiveAlertProps> = ({
  message,
  title,
  variant = "danger",
  children,
  id,
  ...rest
}) => {
  const assertive = variant === "danger" || variant === "warning";
  const resolvedTitle = title ?? message ?? "";
  const visible = !!(title || message);

  return (
    <div
      id={id}
      aria-live={assertive ? "assertive" : "polite"}
      aria-atomic="true"
    >
      {visible && (
        <Alert variant={variant} title={resolvedTitle} {...rest}>
          {children}
        </Alert>
      )}
    </div>
  );
};
