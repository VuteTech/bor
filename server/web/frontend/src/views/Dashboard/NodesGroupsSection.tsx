// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import {
  Card,
  CardBody,
  Grid,
  GridItem,
  Title,
  Flex,
  FlexItem,
} from "@patternfly/react-core";

import type { NodesGroupsOverview } from "../../apiClient/dashboardApi";

interface NodesGroupsSectionProps {
  data: NodesGroupsOverview;
}

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
      </Grid>
    </>
  );
};
