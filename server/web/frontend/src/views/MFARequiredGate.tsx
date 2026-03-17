// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState } from "react";
import {
  Page,
  PageSection,
  EmptyState,
  EmptyStateBody,
  EmptyStateFooter,
  EmptyStateActions,
  Button,
} from "@patternfly/react-core";
import LockIcon from "@patternfly/react-icons/dist/esm/icons/lock-icon";
import { MFASetupModal } from "./Settings/MFASetupModal";

interface MFARequiredGateProps {
  onMFAConfigured: () => void;
  onLogout: () => void;
}

export const MFARequiredGate: React.FC<MFARequiredGateProps> = ({
  onMFAConfigured,
  onLogout,
}) => {
  const [setupModalOpen, setSetupModalOpen] = useState(false);

  return (
    <Page>
      <PageSection
        isFilled
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          minHeight: "100vh",
        }}
      >
        <EmptyState variant="full" titleText="Two-factor authentication required" headingLevel="h1" icon={LockIcon}>
          <EmptyStateBody>
            Your organization requires all users to set up two-factor
            authentication before accessing the application. Please set up an
            authenticator app (such as FreeOTP, Aegis, or Google Authenticator)
            and enrol below.
          </EmptyStateBody>
          <EmptyStateFooter>
            <EmptyStateActions>
              <Button variant="primary" onClick={() => setSetupModalOpen(true)}>
                Set up two-factor authentication
              </Button>
            </EmptyStateActions>
            <EmptyStateActions>
              <Button variant="link" onClick={onLogout}>
                Log out
              </Button>
            </EmptyStateActions>
          </EmptyStateFooter>
        </EmptyState>
      </PageSection>

      <MFASetupModal
        isOpen={setupModalOpen}
        onClose={() => setSetupModalOpen(false)}
        onSuccess={onMFAConfigured}
      />
    </Page>
  );
};
