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
import { StatCard } from "../../components/StatCard";

interface PolicyBindingsSectionProps {
  data: BindingsOverview;
}

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
          <StatCard title="Enabled" value={data.enabledBindings} color="green" />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Disabled"
            value={data.disabledBindings}
            color={data.disabledBindings > 0 ? "orange" : undefined}
          />
        </GridItem>
        <GridItem span={3}>
          <StatCard
            title="Groups Without Policy"
            value={data.groupsWithoutBindings}
            color={data.groupsWithoutBindings > 0 ? "orange" : undefined}
          />
        </GridItem>

        <GridItem span={12}>
          <Card>
            <CardTitle>Coverage by Group</CardTitle>
            <CardBody>
              {groupedBindings.length === 0 ? (
                <div style={{ padding: "1.5rem", textAlign: "center", color: "var(--pf-t--global--text--color--subtle)" }}>
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
                          <div className="bor-text-secondary">
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
