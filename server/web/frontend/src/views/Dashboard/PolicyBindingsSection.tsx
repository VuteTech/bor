// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useMemo } from "react";
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
  Divider,
} from "@patternfly/react-core";

import type { BindingsOverview, BindingEntry } from "../../apiClient/dashboardApi";

interface PolicyBindingsSectionProps {
  data: BindingsOverview;
}

const StatCard: React.FC<{
  title: string;
  value: number;
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

const policyStateColor = (state: string): "blue" | "grey" => {
  return state === "released" ? "blue" : "grey";
};

export const PolicyBindingsSection: React.FC<PolicyBindingsSectionProps> = ({ data }) => {
  const groupedBindings = useMemo(() => {
    const map = new Map<string, BindingEntry[]>();
    for (const b of data.bindings) {
      if (!map.has(b.groupName)) map.set(b.groupName, []);
      map.get(b.groupName)!.push(b);
    }
    return Array.from(map.entries());
  }, [data.bindings]);

  return (
    <>
      <Title headingLevel="h2" size="lg" style={{ marginBottom: "1rem", marginTop: "2rem" }}>
        Policy Bindings
      </Title>
      <Grid hasGutter>
        <GridItem span={3}>
          <StatCard title="Total Bindings" value={data.totalBindings} />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Enabled"
            value={data.enabledBindings}
            color="var(--pf-v5-global--success-color--100)"
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Disabled"
            value={data.disabledBindings}
            color={
              data.disabledBindings > 0
                ? "var(--pf-v5-global--warning-color--100)"
                : undefined
            }
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Groups Without Policy"
            value={data.groupsWithoutBindings}
            color={
              data.groupsWithoutBindings > 0
                ? "var(--pf-v5-global--warning-color--100)"
                : undefined
            }
          />
        </GridItem>

        <GridItem span={12}>
          <Card isFlat>
            <CardTitle>Coverage by Group</CardTitle>
            <CardBody>
              {groupedBindings.length === 0 ? (
                <div style={{ padding: "1.5rem", textAlign: "center", color: "#6a6e73" }}>
                  No policy bindings configured. Bind released policies to node groups to enforce
                  desktop configuration.
                </div>
              ) : (
                <Stack hasGutter>
                  {groupedBindings.map(([groupName, entries], idx) => (
                    <StackItem key={groupName}>
                      {idx > 0 && <Divider style={{ marginBottom: "0.75rem" }} />}
                      <Flex
                        alignItems={{ default: "alignItemsFlexStart" }}
                        spaceItems={{ default: "spaceItemsMd" }}
                      >
                        <FlexItem style={{ minWidth: "180px" }}>
                          <span style={{ fontWeight: 600 }}>{groupName}</span>
                          <div style={{ fontSize: "0.8rem", color: "#6a6e73" }}>
                            {entries[0]?.nodeCount ?? 0} nodes
                          </div>
                        </FlexItem>
                        <FlexItem>
                          <Flex spaceItems={{ default: "spaceItemsSm" }} flexWrap={{ default: "wrap" }}>
                            {entries.map((b) => (
                              <FlexItem key={b.id}>
                                <Flex
                                  alignItems={{ default: "alignItemsCenter" }}
                                  spaceItems={{ default: "spaceItemsXs" }}
                                >
                                  <FlexItem>
                                    <span style={{ fontSize: "0.875rem" }}>{b.policyName}</span>
                                  </FlexItem>
                                  <FlexItem>
                                    <Label
                                      color={b.state === "enabled" ? "green" : "grey"}
                                      isCompact
                                    >
                                      {b.state}
                                    </Label>
                                  </FlexItem>
                                  {b.policyState !== "released" && (
                                    <FlexItem>
                                      <Label color={policyStateColor(b.policyState)} isCompact>
                                        {b.policyState}
                                      </Label>
                                    </FlexItem>
                                  )}
                                </Flex>
                              </FlexItem>
                            ))}
                          </Flex>
                        </FlexItem>
                      </Flex>
                    </StackItem>
                  ))}
                </Stack>
              )}
            </CardBody>
          </Card>
        </GridItem>
      </Grid>
    </>
  );
};
