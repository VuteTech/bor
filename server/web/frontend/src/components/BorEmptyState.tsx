// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * BorEmptyState — the single empty-state pattern for list pages.
 *
 * Distinguishes "there is no data yet" from "no rows match the current filters"
 * (a distinction several pages currently miss), and offers an optional primary
 * action for the no-data case.
 */

import React from "react";
import {
  EmptyState,
  EmptyStateBody,
  EmptyStateFooter,
  EmptyStateActions,
  Button,
} from "@patternfly/react-core";
import CubesIcon from "@patternfly/react-icons/dist/esm/icons/cubes-icon";
import SearchIcon from "@patternfly/react-icons/dist/esm/icons/search-icon";

interface BorEmptyStateProps {
  /** True when the underlying data set is empty (vs. filtered to nothing). */
  isEmptyData: boolean;
  /** Noun shown in default copy, e.g. "policies", "nodes". */
  itemsLabel: string;
  emptyTitle?: string;
  emptyBody?: React.ReactNode;
  /** Primary action for the no-data case (e.g. "Create a policy"). */
  action?: React.ReactNode;
  /** Clears filters in the no-match case. */
  onClearFilters?: () => void;
}

export const BorEmptyState: React.FC<BorEmptyStateProps> = ({
  isEmptyData,
  itemsLabel,
  emptyTitle,
  emptyBody,
  action,
  onClearFilters,
}) => {
  if (isEmptyData) {
    return (
      <EmptyState titleText={emptyTitle ?? `No ${itemsLabel} yet`} headingLevel="h2" icon={CubesIcon}>
        <EmptyStateBody>{emptyBody ?? `There are no ${itemsLabel} to show yet.`}</EmptyStateBody>
        {action && (
          <EmptyStateFooter>
            <EmptyStateActions>{action}</EmptyStateActions>
          </EmptyStateFooter>
        )}
      </EmptyState>
    );
  }
  return (
    <EmptyState titleText={`No ${itemsLabel} match your filters`} headingLevel="h2" icon={SearchIcon}>
      <EmptyStateBody>Try adjusting or clearing the filters to see more results.</EmptyStateBody>
      {onClearFilters && (
        <EmptyStateFooter>
          <EmptyStateActions>
            <Button variant="link" onClick={onClearFilters}>
              Clear all filters
            </Button>
          </EmptyStateActions>
        </EmptyStateFooter>
      )}
    </EmptyState>
  );
};
