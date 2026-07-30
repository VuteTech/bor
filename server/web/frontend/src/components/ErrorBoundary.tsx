// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ErrorBoundary — top-level render-error guard.
 *
 * Without this, any uncaught render exception unmounts the whole React tree and
 * leaves the user on a blank white page. This catches it and shows a recoverable
 * fallback with a reload action.
 */

import React from "react";
import {
  EmptyState,
  EmptyStateBody,
  EmptyStateFooter,
  EmptyStateActions,
  Button,
  PageSection,
} from "@patternfly/react-core";
import ExclamationCircleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-circle-icon";

interface ErrorBoundaryProps {
  children: React.ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    // Surface to the console for diagnostics; no PII is logged.
    console.error("Unhandled render error:", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <PageSection>
          <EmptyState
            titleText="Something went wrong"
            headingLevel="h1"
            icon={ExclamationCircleIcon}
            status="danger"
          >
            <EmptyStateBody>
              The page hit an unexpected error and couldn’t be displayed. Reloading
              usually fixes it. If it keeps happening, contact your administrator.
            </EmptyStateBody>
            <EmptyStateFooter>
              <EmptyStateActions>
                <Button variant="primary" onClick={() => window.location.reload()}>
                  Reload page
                </Button>
              </EmptyStateActions>
            </EmptyStateFooter>
          </EmptyState>
        </PageSection>
      );
    }
    return this.props.children;
  }
}
