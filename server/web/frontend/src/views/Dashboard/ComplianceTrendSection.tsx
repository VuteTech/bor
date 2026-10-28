// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useState } from "react";
import { Link } from "react-router";
import { Card, CardBody, CardTitle } from "@patternfly/react-core";

import { fetchComplianceTrend, ReportTrendPoint } from "../../apiClient/reportsApi";
import { FEATURE_COMPLIANCE_REPORTS, hasFeature, hasGlobalPermission } from "../../apiClient/permissions";
import { TrendChart } from "../Reports/TrendChart";

/**
 * The fleet's compliance score over 30 days from the daily report summaries,
 * with the latest value and its change over the period. Shown only where
 * compliance reports are available and the user can view them fleet-wide.
 */
export const ComplianceTrendSection: React.FC = () => {
  const enabled = hasFeature(FEATURE_COMPLIANCE_REPORTS) && hasGlobalPermission("report:view");
  const [points, setPoints] = useState<ReportTrendPoint[] | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    fetchComplianceTrend(30)
      .then((p) => !cancelled && setPoints(p))
      .catch(() => !cancelled && setPoints(null));
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  if (!enabled || points === null || points.length === 0) return null;
  const scored = points.filter((p) => p.score_pct !== null);
  const latest = scored.length ? (scored[scored.length - 1].score_pct as number) : null;
  const first = scored.length ? (scored[0].score_pct as number) : null;
  const delta = latest !== null && first !== null && scored.length > 1 ? latest - first : null;

  return (
    <Card isCompact style={{ margin: "1rem 0 1.5rem" }}>
      <CardTitle>Compliance trend</CardTitle>
      <CardBody>
        <div style={{ display: "grid", gridTemplateColumns: "minmax(10rem, auto) minmax(0, 1fr)", gap: "1.5rem", alignItems: "center" }}>
          <div>
            <div style={{ fontSize: "1.75rem", fontWeight: 700 }}>{latest === null ? "n/a" : `${latest.toFixed(1)}%`}</div>
            <div className="bor-text-secondary">
              {delta === null
                ? "fleet score"
                : `${delta >= 0 ? "+" : ""}${delta.toFixed(1)} points over ${scored.length} days`}
            </div>
            <Link to="/reports">Reports</Link>
          </div>
          <div style={{ minWidth: 0 }}>
            <TrendChart points={points} height={72} compact title="Fleet compliance score, last 30 days" />
          </div>
        </div>
      </CardBody>
    </Card>
  );
};
