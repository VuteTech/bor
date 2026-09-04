// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { Card, CardBody, CardHeader, CardTitle, EmptyState, EmptyStateBody, Flex, FlexItem } from "@patternfly/react-core";
import InfoCircleIcon from "@patternfly/react-icons/dist/esm/icons/info-circle-icon";

import { PolicyTypeIcon } from "../../../components/PolicyTypeIcon";
import { getPolicyType } from "../../../policyTypes/registry";
import { PolicyTypeDescriptionBody } from "./PolicyTypeDescriptionBody";

interface PolicyTypeDescriptionPanelProps {
  /** Selected type id, or null before a choice is made. */
  type: string | null;
}

const PANEL_TITLE_ID = "policy-type-panel-title";

/**
 * The help panel beside the tile grid: what the selected type does, what it
 * changes on the node, what it applies to and what it is good for. It follows
 * the *selection* (not hover) so keyboard and touch users see the same thing,
 * and it is a plain labelled region rather than a live region — announcing a
 * whole panel on every arrow key would be noise; the tile's own label and
 * tagline already describe the choice.
 */
export const PolicyTypeDescriptionPanel: React.FC<PolicyTypeDescriptionPanelProps> = ({ type }) => {
  const def = type ? getPolicyType(type) : undefined;

  if (!def) {
    return (
      <Card component="section" aria-labelledby={PANEL_TITLE_ID} isFullHeight>
        <CardBody>
          <EmptyState
            variant="sm"
            icon={InfoCircleIcon}
            titleText="What does each type do?"
            headingLevel="h3"
            id={PANEL_TITLE_ID}
          >
            <EmptyStateBody>
              Select a policy type to see what it configures, what it changes on managed nodes and what it is
              typically used for.
            </EmptyStateBody>
          </EmptyState>
        </CardBody>
      </Card>
    );
  }

  return (
    <Card component="section" aria-labelledby={PANEL_TITLE_ID} isFullHeight>
      <CardHeader>
        <Flex alignItems={{ default: "alignItemsCenter" }} gap={{ default: "gapMd" }} flexWrap={{ default: "nowrap" }}>
          <FlexItem style={{ display: "flex", color: "var(--pf-t--global--color--brand--default)" }}>
            <PolicyTypeIcon type={def.id} size="xl" />
          </FlexItem>
          <FlexItem>
            <CardTitle id={PANEL_TITLE_ID} component="h3">
              {def.label}
            </CardTitle>
            {def.technicalName && <div className="bor-text-secondary">{def.technicalName}</div>}
          </FlexItem>
        </Flex>
      </CardHeader>
      <CardBody>
        <PolicyTypeDescriptionBody def={def} />
      </CardBody>
    </Card>
  );
};
