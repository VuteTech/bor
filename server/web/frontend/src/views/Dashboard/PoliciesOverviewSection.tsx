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
  LabelGroup,
} from "@patternfly/react-core";

import type { PoliciesOverview } from "../../apiClient/dashboardApi";
import { StatCard } from "../../components/StatCard";
import { PolicyTypeLabel } from "../../components/PolicyTypeLabel";

interface PoliciesOverviewSectionProps {
  data: PoliciesOverview;
}

export const PoliciesOverviewSection: React.FC<PoliciesOverviewSectionProps> = ({ data }) => {
  const typeEntries = Object.entries(data.byType).sort((a, b) => b[1] - a[1]);

  return (
    <>
      <Title headingLevel="h2" size="lg" style={{ marginBottom: "1rem", marginTop: "2rem" }}>
        Policies
      </Title>
      <Grid hasGutter>
        <GridItem span={3}>
          <StatCard title="Total Policies" value={data.totalPolicies} href="/policies" />
        </GridItem>
        <GridItem span={3}>
          <StatCard title="Released" value={data.released} color="green" href="/policies?state=released" />
        </GridItem>
        <GridItem span={3}>
          <StatCard title="Draft" value={data.draft} color="blue" href="/policies?state=draft" />
        </GridItem>
        <GridItem span={3}>
          <StatCard title="Archived" value={data.archived} color="grey" href="/policies?state=archived" />
        </GridItem>

        <GridItem span={12}>
          <Card isCompact>
            <CardTitle>Policy Types</CardTitle>
            <CardBody>
              {typeEntries.length === 0 ? (
                <span style={{ color: "var(--pf-t--global--text--color--subtle)", fontSize: "0.875rem" }}>
                  No policies configured
                </span>
              ) : (
                <LabelGroup>
                  {typeEntries.map(([type, count]) => (
                    <PolicyTypeLabel key={type} type={type} suffix={<>&nbsp;·&nbsp;{count}</>} />
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
