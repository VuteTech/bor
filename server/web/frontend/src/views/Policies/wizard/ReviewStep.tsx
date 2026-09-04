// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState } from "react";
import {
  Alert,
  Button,
  CodeBlock,
  CodeBlockCode,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  ExpandableSection,
  Label,
  Title,
  useWizardContext,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";

import { LiveAlert } from "../../../components/LiveAlert";
import { PolicyTypeLabel } from "../../../components/PolicyTypeLabel";
import { getPolicyType } from "../../../policyTypes/registry";
import { buildSettingsRows } from "../policySummary";

interface ReviewStepProps {
  type: string;
  name: string;
  description: string;
  content: string;
  /** Server error from the last create attempt. */
  error: string | null;
}

const EditLink: React.FC<{ stepId: string }> = ({ stepId }) => {
  const { goToStepById } = useWizardContext();
  return (
    <Button variant="link" isInline onClick={() => goToStepById(stepId)} style={{ marginLeft: "0.75rem" }}>
      Edit
    </Button>
  );
};

/** Step 4: everything the previous steps collected, and the draft notice. */
export const ReviewStep: React.FC<ReviewStepProps> = ({ type, name, description, content, error }) => {
  const def = getPolicyType(type);
  const rows = buildSettingsRows(type, content);
  const hasLockedColumn = rows.some((r) => r.locked !== null);
  const [showJson, setShowJson] = useState(false);
  let prettyJson = content;
  try {
    prettyJson = JSON.stringify(JSON.parse(content), null, 2);
  } catch {
    /* show as-is */
  }

  return (
    <>
      <LiveAlert message={error} variant="danger" isInline title="The policy could not be created" style={{ marginBottom: "1rem" }}>
        {error}
      </LiveAlert>

      <DescriptionList isHorizontal style={{ marginBottom: "1.5rem" }}>
        <DescriptionListGroup>
          <DescriptionListTerm>Type</DescriptionListTerm>
          <DescriptionListDescription>
            <PolicyTypeLabel type={type} />
            {def && <span className="bor-text-secondary" style={{ marginLeft: "0.5rem" }}>{def.tagline}</span>}
            <EditLink stepId="type" />
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Name</DescriptionListTerm>
          <DescriptionListDescription>
            {name}
            <EditLink stepId="details" />
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Description</DescriptionListTerm>
          <DescriptionListDescription>
            {description.trim() ? description : <span className="bor-text-secondary">—</span>}
            <EditLink stepId="details" />
          </DescriptionListDescription>
        </DescriptionListGroup>
      </DescriptionList>

      <Title headingLevel="h3" size="lg" style={{ marginBottom: "0.75rem" }}>
        Settings
        <EditLink stepId="configuration" />
      </Title>
      {rows.length > 0 ? (
        <Table aria-label="Configured settings" variant="compact">
          <Thead>
            <Tr>
              <Th>Setting</Th>
              <Th>Value</Th>
              {hasLockedColumn && <Th>Locked</Th>}
            </Tr>
          </Thead>
          <Tbody>
            {rows.map((row, i) => (
              <Tr key={`${row.setting}-${i}`}>
                <Td dataLabel="Setting">{row.setting}</Td>
                <Td dataLabel="Value">{row.value}</Td>
                {hasLockedColumn && (
                  <Td dataLabel="Locked">
                    {row.locked !== null ? (
                      <Label color={row.locked === "Yes" ? "orange" : "grey"} isCompact>
                        {row.locked}
                      </Label>
                    ) : (
                      "—"
                    )}
                  </Td>
                )}
              </Tr>
            ))}
          </Tbody>
        </Table>
      ) : (
        <Alert variant="warning" isInline title="No settings configured">
          Go back to Configuration and add at least one setting.
        </Alert>
      )}

      <ExpandableSection
        toggleText={showJson ? "Hide policy JSON" : "Show policy JSON"}
        isExpanded={showJson}
        onToggle={(_ev, expanded) => setShowJson(expanded)}
        style={{ marginTop: "1rem" }}
      >
        <CodeBlock>
          <CodeBlockCode>{prettyJson}</CodeBlockCode>
        </CodeBlock>
      </ExpandableSection>

      <Alert variant="info" isInline isPlain title="Created as a draft" style={{ marginTop: "1.5rem" }}>
        Nothing reaches a node until you release the policy and bind it to a node group. You can do both from the
        policy page after it is created.
      </Alert>
    </>
  );
};
