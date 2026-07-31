// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * StatCard — the single dashboard stat-tile pattern (replaces three near-
 * duplicate implementations). Uses PF6 status tokens for coloring and can
 * optionally be made navigable (href) so tiles double as drill-down links.
 */

import React from "react";
import { Link } from "react-router";
import { Card, CardBody, Flex, FlexItem } from "@patternfly/react-core";

export type StatColor = "green" | "red" | "blue" | "orange" | "grey";

const STAT_COLOR_TOKEN: Record<StatColor, string> = {
  green: "var(--pf-t--global--text--color--status--success--default)",
  red: "var(--pf-t--global--text--color--status--danger--default)",
  blue: "var(--pf-t--global--text--color--status--info--default)",
  orange: "var(--pf-t--global--text--color--status--warning--default)",
  grey: "var(--pf-t--global--text--color--subtle)",
};

interface StatCardProps {
  title: string;
  value: number | string;
  icon?: React.ReactNode;
  color?: StatColor;
  /** When set, the whole tile becomes a link (drill-down). */
  href?: string;
}

export const StatCard: React.FC<StatCardProps> = ({ title, value, icon, color, href }) => {
  const tint = color ? STAT_COLOR_TOKEN[color] : undefined;
  const body = (
    <CardBody>
      <Flex
        direction={{ default: "column" }}
        alignItems={{ default: "alignItemsCenter" }}
        justifyContent={{ default: "justifyContentCenter" }}
      >
        <FlexItem>
          <span style={{ fontSize: "0.85rem", color: "var(--pf-t--global--text--color--subtle)" }}>
            {title}
          </span>
        </FlexItem>
        <FlexItem>
          <Flex alignItems={{ default: "alignItemsCenter" }} spaceItems={{ default: "spaceItemsSm" }}>
            {icon && <FlexItem style={{ color: tint }}>{icon}</FlexItem>}
            <FlexItem>
              <span style={{ fontSize: "1.75rem", fontWeight: 700, color: tint }}>{value}</span>
            </FlexItem>
          </Flex>
        </FlexItem>
      </Flex>
    </CardBody>
  );

  return (
    <Card isCompact isClickable={!!href}>
      {href ? (
        <Link to={href} style={{ textDecoration: "none", color: "inherit", display: "block" }}>
          {body}
        </Link>
      ) : (
        body
      )}
    </Card>
  );
};
