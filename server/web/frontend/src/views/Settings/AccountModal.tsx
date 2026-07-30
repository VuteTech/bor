// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState } from "react";
import {
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  Tabs,
  Tab,
  TabTitleText,
} from "@patternfly/react-core";
import { MFAEnrollmentTab } from "./MFAEnrollmentTab";
import { PasswordTab } from "./PasswordTab";

interface AccountModalProps {
  isOpen: boolean;
  onClose: () => void;
  userSource?: string;
}

export const AccountModal: React.FC<AccountModalProps> = ({ isOpen, onClose, userSource }) => {
  const [activeTab, setActiveTab] = useState<string | number>("security");
  const isLocal = !userSource || userSource === "local";

  return (
    <Modal
      variant={ModalVariant.medium}
      isOpen={isOpen}
      onClose={onClose}
    >
      <ModalHeader title="Account Settings" />
      <ModalBody>
        <Tabs
          activeKey={activeTab}
          onSelect={(_ev, k) => setActiveTab(k)}
        >
          <Tab eventKey="security" title={<TabTitleText>Security</TabTitleText>}>
            <div style={{ paddingTop: 16 }}>
              <MFAEnrollmentTab />
            </div>
          </Tab>
          {isLocal && (
            <Tab eventKey="password" title={<TabTitleText>Change Password</TabTitleText>}>
              <div style={{ paddingTop: 16 }}>
                <PasswordTab />
              </div>
            </Tab>
          )}
        </Tabs>
      </ModalBody>
    </Modal>
  );
};
