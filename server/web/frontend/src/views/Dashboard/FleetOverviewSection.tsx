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
  DescriptionList,
  DescriptionListGroup,
  DescriptionListTerm,
  DescriptionListDescription,
  Label,
  LabelGroup,
  Flex,
  FlexItem,
} from "@patternfly/react-core";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import ExclamationCircleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-circle-icon";
import QuestionCircleIcon from "@patternfly/react-icons/dist/esm/icons/question-circle-icon";

import type { FleetOverview } from "../../apiClient/dashboardApi";

interface FleetOverviewSectionProps {
  data: FleetOverview;
}

const StatCard: React.FC<{
  title: string;
  value: number | string;
  icon?: React.ReactNode;
  color?: "green" | "red" | "blue" | "grey";
}> = ({ title, value, icon, color }) => {
  const colorMap: Record<string, string> = {
    green: "var(--pf-v5-global--success-color--100)",
    red: "var(--pf-v5-global--danger-color--100)",
    blue: "var(--pf-v5-global--info-color--100)",
    grey: "var(--pf-v5-global--Color--200)",
  };

  return (
    <Card isCompact isFlat>
      <CardBody>
        <Flex
          alignItems={{ default: "alignItemsCenter" }}
          justifyContent={{ default: "justifyContentCenter" }}
          direction={{ default: "column" }}
        >
          <FlexItem>
            <span style={{ fontSize: "0.85rem", color: "#6a6e73" }}>{title}</span>
          </FlexItem>
          <FlexItem>
            <Flex alignItems={{ default: "alignItemsCenter" }} spaceItems={{ default: "spaceItemsSm" }}>
              {icon && (
                <FlexItem>
                  <span style={{ color: color ? colorMap[color] : undefined }}>{icon}</span>
                </FlexItem>
              )}
              <FlexItem>
                <span
                  style={{
                    fontSize: "1.75rem",
                    fontWeight: 700,
                    color: color ? colorMap[color] : undefined,
                  }}
                >
                  {value}
                </span>
              </FlexItem>
            </Flex>
          </FlexItem>
        </Flex>
      </CardBody>
    </Card>
  );
};

export const FleetOverviewSection: React.FC<FleetOverviewSectionProps> = ({ data }) => {
  const versionEntries = Object.entries(data.agentVersions).sort((a, b) => b[1] - a[1]);
  const osEntries = Object.entries(data.osDistribution).sort((a, b) => b[1] - a[1]);
  const deEntries = Object.entries(data.desktopEnvironment).sort((a, b) => b[1] - a[1]);

  return (
    <>
      <Title headingLevel="h2" size="lg" style={{ marginBottom: "1rem" }}>
        Fleet Overview
      </Title>
      <Grid hasGutter>
        <GridItem span={3}>
          <StatCard title="Total Nodes" value={data.totalNodes} color="blue" />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Online"
            value={data.online}
            icon={<CheckCircleIcon />}
            color="green"
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Offline"
            value={data.offline}
            icon={<ExclamationCircleIcon />}
            color="red"
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Unknown"
            value={data.unknown}
            icon={<QuestionCircleIcon />}
            color="grey"
          />
        </GridItem>

        <GridItem span={4}>
          <Card isCompact isFlat>
            <CardTitle>Agent Versions</CardTitle>
            <CardBody>
              {versionEntries.length === 0 ? (
                <span style={{ color: "#6a6e73", fontSize: "0.875rem" }}>
                  No agents registered
                </span>
              ) : (
                <DescriptionList isHorizontal isCompact>
                  {versionEntries.map(([ver, count]) => (
                    <DescriptionListGroup key={ver}>
                      <DescriptionListTerm>{ver}</DescriptionListTerm>
                      <DescriptionListDescription>{count} nodes</DescriptionListDescription>
                    </DescriptionListGroup>
                  ))}
                </DescriptionList>
              )}
            </CardBody>
          </Card>
        </GridItem>

        <GridItem span={4}>
          <Card isCompact isFlat>
            <CardTitle>OS / Distribution</CardTitle>
            <CardBody>
              {osEntries.length === 0 ? (
                <span style={{ color: "#6a6e73", fontSize: "0.875rem" }}>No data available</span>
              ) : (
                <LabelGroup>
                  {osEntries.map(([os, count]) => (
                    <Label key={os} color="blue" isCompact>
                      {os} &nbsp;·&nbsp; {count}
                    </Label>
                  ))}
                </LabelGroup>
              )}
            </CardBody>
          </Card>
        </GridItem>

        <GridItem span={4}>
          <Card isCompact isFlat>
            <CardTitle>Desktop Environment</CardTitle>
            <CardBody>
              {deEntries.length === 0 ? (
                <span style={{ color: "#6a6e73", fontSize: "0.875rem" }}>No data available</span>
              ) : (
                <LabelGroup>
                  {deEntries.map(([de, count]) => (
                    <Label key={de} color="purple" isCompact>
                      {de} &nbsp;·&nbsp; {count}
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
