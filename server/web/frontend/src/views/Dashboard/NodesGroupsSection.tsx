// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { Grid, GridItem, Title } from "@patternfly/react-core";

import type { NodesGroupsOverview } from "../../apiClient/dashboardApi";
import { StatCard } from "../../components/StatCard";

interface NodesGroupsSectionProps {
  data: NodesGroupsOverview;
}

export const NodesGroupsSection: React.FC<NodesGroupsSectionProps> = ({ data }) => (
  <>
    <Title headingLevel="h2" size="lg" style={{ marginBottom: "1rem", marginTop: "2rem" }}>
      Nodes &amp; Groups
    </Title>
    <Grid hasGutter>
      <GridItem span={3}>
        <StatCard title="Total Groups" value={data.totalGroups} />
      </GridItem>
      <GridItem span={3}>
        <StatCard
          title="Unassigned Nodes"
          value={data.nodesWithoutGroup}
          color={data.nodesWithoutGroup > 0 ? "orange" : undefined}
        />
      </GridItem>
    </Grid>
  </>
);
