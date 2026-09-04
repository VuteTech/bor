// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import {
  Button,
  Flex,
  FlexItem,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  TextArea,
  TextInput,
  useWizardContext,
} from "@patternfly/react-core";

import { PolicyTypeLabel } from "../../../components/PolicyTypeLabel";
import { policyTypeLabel } from "../../../policyTypes/registry";

interface DetailsStepProps {
  type: string;
  name: string;
  description: string;
  onNameChange: (v: string) => void;
  onDescriptionChange: (v: string) => void;
}

/** Step 2: name and description; the chosen type is shown with a way back. */
export const DetailsStep: React.FC<DetailsStepProps> = ({ type, name, description, onNameChange, onDescriptionChange }) => {
  const { goToStepById } = useWizardContext();
  const nameMissing = name.trim() === "";

  return (
    <Form isWidthLimited onSubmit={(e) => e.preventDefault()}>
      <Flex alignItems={{ default: "alignItemsCenter" }} gap={{ default: "gapSm" }}>
        <FlexItem>
          <PolicyTypeLabel type={type} />
        </FlexItem>
        <FlexItem>
          <Button variant="link" isInline onClick={() => goToStepById("type")}>
            Change type
          </Button>
        </FlexItem>
      </Flex>
      <FormGroup label="Name" isRequired fieldId="wizard-policy-name">
        <TextInput
          id="wizard-policy-name"
          value={name}
          onChange={(_ev, v) => onNameChange(v)}
          isRequired
          placeholder={`e.g. ${policyTypeLabel(type)} — corporate baseline`}
          aria-describedby="wizard-policy-name-help"
        />
        <FormHelperText>
          <HelperText>
            <HelperTextItem id="wizard-policy-name-help" variant={nameMissing ? "indeterminate" : "default"}>
              Shown in the policies list and in compliance reports. Say what the policy does and who it is for.
            </HelperTextItem>
          </HelperText>
        </FormHelperText>
      </FormGroup>
      <FormGroup label="Description" fieldId="wizard-policy-description">
        <TextArea
          id="wizard-policy-description"
          value={description}
          onChange={(_ev, v) => onDescriptionChange(v)}
          rows={3}
          placeholder="Optional. Why this policy exists, which teams or rooms it targets, who owns it."
        />
      </FormGroup>
    </Form>
  );
};
