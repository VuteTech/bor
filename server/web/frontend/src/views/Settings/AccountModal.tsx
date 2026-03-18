// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { Modal, ModalVariant, ModalHeader, ModalBody } from "@patternfly/react-core";
import { MFATab } from "./MFATab";

interface AccountModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const AccountModal: React.FC<AccountModalProps> = ({ isOpen, onClose }) => {
  return (
    <Modal
      variant={ModalVariant.medium}
      isOpen={isOpen}
      onClose={onClose}
    >
      <ModalHeader title="Account Security" />
      <ModalBody>
        <MFATab />
      </ModalBody>
    </Modal>
  );
};
