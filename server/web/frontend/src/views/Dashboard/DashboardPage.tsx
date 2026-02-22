// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  PageSection,
  Title,
  Alert,
  Spinner,
  Flex,
  FlexItem,
  Button,
} from "@patternfly/react-core";
import SyncAltIcon from "@patternfly/react-icons/dist/esm/icons/sync-alt-icon";

import { fetchDashboardData, DashboardData } from "../../apiClient/dashboardApi";
import { FleetOverviewSection } from "./FleetOverviewSection";
import { NodesGroupsSection } from "./NodesGroupsSection";
import { PoliciesOverviewSection } from "./PoliciesOverviewSection";
import { PolicyBindingsSection } from "./PolicyBindingsSection";

const REFRESH_INTERVAL_MS = 30_000;

export const DashboardPage: React.FC = () => {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);

  const load = useCallback(async (silent = false) => {
    try {
      if (!silent) setLoading(true);
      else setRefreshing(true);
      setError(null);
      const result = await fetchDashboardData();
      setData(result);
      setLastUpdated(new Date());
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load dashboard data");
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    load();
    const interval = setInterval(() => load(true), REFRESH_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [load]);

  if (loading) {
    return (
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentCenter" }}>
          <FlexItem>
            <Spinner size="xl" />
          </FlexItem>
        </Flex>
      </PageSection>
    );
  }

  if (error) {
    return (
      <PageSection>
        <Alert variant="danger" title="Dashboard Error">
          {error}
        </Alert>
      </PageSection>
    );
  }

  if (!data) return null;

  return (
    <>
      <PageSection variant="light">
        <Flex
          justifyContent={{ default: "justifyContentSpaceBetween" }}
          alignItems={{ default: "alignItemsCenter" }}
        >
          <FlexItem>
            <Title headingLevel="h1" size="2xl">
              Dashboard
            </Title>
            {lastUpdated && (
              <span style={{ color: "#6a6e73", fontSize: "0.8rem" }}>
                Updated {lastUpdated.toLocaleTimeString()}
              </span>
            )}
          </FlexItem>
          <FlexItem>
            <Button
              variant="secondary"
              icon={<SyncAltIcon />}
              isLoading={refreshing}
              onClick={() => load(true)}
              isDisabled={refreshing}
            >
              Refresh
            </Button>
          </FlexItem>
        </Flex>
      </PageSection>
      <PageSection>
        <FleetOverviewSection data={data.fleet} />
        <NodesGroupsSection data={data.nodesGroups} />
        <PoliciesOverviewSection data={data.policies} />
        <PolicyBindingsSection data={data.bindings} />
      </PageSection>
    </>
  );
};
