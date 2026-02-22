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
  Flex,
  FlexItem,
  Stack,
  StackItem,
} from "@patternfly/react-core";

import type { NodesGroupsOverview, GroupSummary } from "../../apiClient/dashboardApi";

interface NodesGroupsSectionProps {
  data: NodesGroupsOverview;
}

const GroupCard: React.FC<{ group: GroupSummary }> = ({ group }) => {
  const onlineColor = group.onlineNodes > 0
    ? "var(--pf-v5-global--success-color--100)"
    : "#6a6e73";
  const offlineColor = group.offlineNodes > 0
    ? "var(--pf-v5-global--danger-color--100)"
    : "#6a6e73";

  return (
    <Card isCompact isFlat>
      <CardTitle style={{ paddingBottom: "0.25rem" }}>{group.name}</CardTitle>
      <CardBody>
        <Stack hasGutter>
          <StackItem>
            <Flex spaceItems={{ default: "spaceItemsMd" }}>
              <FlexItem>
                <span style={{ fontSize: "1.5rem", fontWeight: 700 }}>
                  {group.totalNodes}
                </span>
                <span style={{ fontSize: "0.8rem", color: "#6a6e73", marginLeft: "0.25rem" }}>
                  nodes
                </span>
              </FlexItem>
            </Flex>
          </StackItem>
          <StackItem>
            <Flex spaceItems={{ default: "spaceItemsSm" }}>
              <FlexItem>
                <Label color="green" isCompact icon={null}>
                  <span style={{ color: onlineColor }}>{group.onlineNodes} online</span>
                </Label>
              </FlexItem>
              <FlexItem>
                <Label color="grey" isCompact>
                  <span style={{ color: offlineColor }}>{group.offlineNodes} offline</span>
                </Label>
              </FlexItem>
            </Flex>
          </StackItem>
          <StackItem>
            <span style={{ fontSize: "0.8rem", color: "#6a6e73" }}>
              {group.enabledPolicies + group.disabledPolicies === 0
                ? "No policies bound"
                : `${group.enabledPolicies} polic${group.enabledPolicies !== 1 ? "ies" : "y"} enabled`}
              {group.disabledPolicies > 0 && `, ${group.disabledPolicies} disabled`}
            </span>
          </StackItem>
        </Stack>
      </CardBody>
    </Card>
  );
};

export const NodesGroupsSection: React.FC<NodesGroupsSectionProps> = ({ data }) => {
  return (
    <>
      <Title headingLevel="h2" size="lg" style={{ marginBottom: "1rem", marginTop: "2rem" }}>
        Nodes &amp; Groups
      </Title>
      <Grid hasGutter>
        <GridItem span={3}>
          <Card isCompact isFlat>
            <CardBody>
              <Flex direction={{ default: "column" }} alignItems={{ default: "alignItemsCenter" }}>
                <FlexItem>
                  <span style={{ fontSize: "0.85rem", color: "#6a6e73" }}>Total Groups</span>
                </FlexItem>
                <FlexItem>
                  <span style={{ fontSize: "1.75rem", fontWeight: 700 }}>{data.totalGroups}</span>
                </FlexItem>
              </Flex>
            </CardBody>
          </Card>
        </GridItem>

        <GridItem span={3}>
          <Card isCompact isFlat>
            <CardBody>
              <Flex direction={{ default: "column" }} alignItems={{ default: "alignItemsCenter" }}>
                <FlexItem>
                  <span style={{ fontSize: "0.85rem", color: "#6a6e73" }}>Unassigned Nodes</span>
                </FlexItem>
                <FlexItem>
                  <span
                    style={{
                      fontSize: "1.75rem",
                      fontWeight: 700,
                      color:
                        data.nodesWithoutGroup > 0
                          ? "var(--pf-v5-global--warning-color--100)"
                          : undefined,
                    }}
                  >
                    {data.nodesWithoutGroup}
                  </span>
                </FlexItem>
              </Flex>
            </CardBody>
          </Card>
        </GridItem>

        {data.groups.length === 0 ? (
          <GridItem span={12}>
            <Card isFlat>
              <CardBody>
                <div style={{ padding: "1.5rem", textAlign: "center", color: "#6a6e73" }}>
                  No node groups defined. Create a group and enroll agents to get started.
                </div>
              </CardBody>
            </Card>
          </GridItem>
        ) : (
          data.groups.map((group) => (
            <GridItem key={group.id} span={3}>
              <GroupCard group={group} />
            </GridItem>
          ))
        )}
      </Grid>
    </>
  );
};
