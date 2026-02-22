// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import "@patternfly/patternfly/patternfly.css";
import "./bor-theme.css";
import React from "react";
import { createRoot } from "react-dom/client";
import { Shell } from "./Shell";

const mountPoint = document.getElementById("app-root");
if (!mountPoint) {
  throw new Error("Could not locate #app-root element in the DOM");
}
const reactRoot = createRoot(mountPoint);
reactRoot.render(
  <React.StrictMode>
    <Shell />
  </React.StrictMode>
);
