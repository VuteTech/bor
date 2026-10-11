// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import "@patternfly/patternfly/patternfly.css";
import "./bor-theme.css";
import React from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { Shell } from "./Shell";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { ToastProvider } from "./components/ToastHost";
import { finishStepUpPopup } from "./utils/stepUpPopup";

const mountPoint = document.getElementById("app-root");
if (!mountPoint) {
  throw new Error("Could not locate #app-root element in the DOM");
}
// A step-up popup reports to its opener and closes instead of rendering.
if (!finishStepUpPopup()) {
  const reactRoot = createRoot(mountPoint);
  reactRoot.render(
    <React.StrictMode>
      <BrowserRouter>
        <ErrorBoundary>
          <ToastProvider>
            <Shell />
          </ToastProvider>
        </ErrorBoundary>
      </BrowserRouter>
    </React.StrictMode>
  );
}
