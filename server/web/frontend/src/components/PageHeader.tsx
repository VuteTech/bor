// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * PageHeader — the single page-title pattern.
 *
 * Renders the required per-page <h1> (WCAG 2.4.6 / heading order) plus an
 * optional subtitle, using PF6 tokens so it adapts to light/dark themes.
 */

import React from "react";
import { PageSection, Title, Content } from "@patternfly/react-core";

interface PageHeaderProps {
  title: string;
  subtitle?: string;
  /** Optional actions rendered on the trailing edge of the header row. */
  actions?: React.ReactNode;
}

export const PageHeader: React.FC<PageHeaderProps> = ({ title, subtitle, actions }) => (
  <PageSection
    style={{
      borderBottom: "1px solid var(--pf-t--global--border--color--default)",
    }}
  >
    {/* The flex row must sit inside PF's injected PageBody wrapper — styles on
        the PageSection itself would treat that wrapper as the flex item and
        shrink-wrap the title instead of spanning the full row. */}
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: "1rem",
        flexWrap: "wrap",
      }}
    >
      <div>
        <Title headingLevel="h1" size="xl">
          {title}
        </Title>
        {subtitle && (
          <Content
            component="p"
            style={{
              marginTop: "0.25rem",
              color: "var(--pf-t--global--text--color--subtle)",
              fontSize: "0.875rem",
            }}
          >
            {subtitle}
          </Content>
        )}
      </div>
      {actions && <div>{actions}</div>}
    </div>
  </PageSection>
);
