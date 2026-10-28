// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { EmptyState, EmptyStateBody, PageSection } from "@patternfly/react-core";
import LockIcon from "@patternfly/react-icons/dist/esm/icons/lock-icon";

/**
 * Shown in place of the Reports pages when the running edition does not
 * enable compliance reports. The live Compliance page is unaffected.
 */
export const EditionNotice: React.FC = () => (
  <PageSection>
    <EmptyState headingLevel="h2" icon={LockIcon} titleText="Compliance reports are a Bor Enterprise feature">
      <EmptyStateBody>
        This server runs an edition without compliance reports. The live Compliance page stays available; point-in-time
        reports with hashed and signed evidence bundles need a Bor Enterprise license that includes the reports feature.
        Reports generated while the feature was enabled are kept and reappear when it is enabled again.
      </EmptyStateBody>
    </EmptyState>
  </PageSection>
);
