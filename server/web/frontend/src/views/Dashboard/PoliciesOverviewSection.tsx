// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import {
  Card,
  CardTitle,
  CardBody,
  Grid,
  GridItem,
  Title,
  Label,
  LabelGroup,
  Flex,
  FlexItem,
} from "@patternfly/react-core";

import type { PoliciesOverview } from "../../apiClient/dashboardApi";

interface PoliciesOverviewSectionProps {
  data: PoliciesOverview;
}

const StatCard: React.FC<{
  title: string;
  value: number | string;
  color?: string;
}> = ({ title, value, color }) => (
  <Card isCompact isFlat>
    <CardBody>
      <Flex direction={{ default: "column" }} alignItems={{ default: "alignItemsCenter" }}>
        <FlexItem>
          <span style={{ fontSize: "0.85rem", color: "#6a6e73" }}>{title}</span>
        </FlexItem>
        <FlexItem>
          <span style={{ fontSize: "1.75rem", fontWeight: 700, color }}>{value}</span>
        </FlexItem>
      </Flex>
    </CardBody>
  </Card>
);

const typeColor = (type: string): "blue" | "green" | "purple" | "orange" | "grey" => {
  switch (type) {
    case "firefox":
      return "orange";
    case "chrome":
      return "blue";
    case "kconfig":
      return "purple";
    default:
      return "grey";
  }
};

export const PoliciesOverviewSection: React.FC<PoliciesOverviewSectionProps> = ({ data }) => {
  const typeEntries = Object.entries(data.byType).sort((a, b) => b[1] - a[1]);

  return (
    <>
      <Title headingLevel="h2" size="lg" style={{ marginBottom: "1rem", marginTop: "2rem" }}>
        Policies
      </Title>
      <Grid hasGutter>
        <GridItem span={3}>
          <StatCard title="Total Policies" value={data.totalPolicies} />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Released"
            value={data.released}
            color="var(--pf-v5-global--success-color--100)"
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Draft"
            value={data.draft}
            color="var(--pf-v5-global--info-color--100)"
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Archived"
            value={data.archived}
            color="var(--pf-v5-global--Color--200)"
          />
        </GridItem>

        <GridItem span={12}>
          <Card isCompact isFlat>
            <CardTitle>Policy Types</CardTitle>
            <CardBody>
              {typeEntries.length === 0 ? (
                <span style={{ color: "#6a6e73", fontSize: "0.875rem" }}>
                  No policies configured
                </span>
              ) : (
                <LabelGroup>
                  {typeEntries.map(([type, count]) => (
                    <Label key={type} color={typeColor(type)}>
                      {type} &nbsp;·&nbsp; {count}
                    </Label>
                  ))}
                </LabelGroup>
              )}
            </CardBody>
          </Card>
        </GridItem>
      </Grid>
    </>
  );
};
