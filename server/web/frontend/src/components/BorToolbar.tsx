// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * BorToolbar — the single search + filter/actions toolbar shell.
 *
 * Wraps PF6 Toolbar/ToolbarContent with a standard SearchInput and a slot for
 * extra items (filter controls, bulk-action menus). Replaces the hand-rolled
 * copies on the list pages so the search box, clear-all, and spacing stay
 * consistent.
 *
 * Pass `onSearch` for pages that apply search on Enter/submit; omit it for
 * instant-filter pages (search updates as the user types).
 */

import React from "react";
import { Toolbar, ToolbarContent, ToolbarItem, SearchInput } from "@patternfly/react-core";

interface BorToolbarProps {
  searchValue: string;
  onSearchChange: (value: string) => void;
  searchAriaLabel: string;
  searchPlaceholder?: string;
  /** Commit handler (Enter / search button). Omit for instant-filter pages. */
  onSearch?: () => void;
  /** Clears the search and any active filters (wired to Toolbar's clear-all). */
  onClearAll: () => void;
  /** Extra toolbar items after the search box (filter controls, bulk actions). */
  children?: React.ReactNode;
}

export const BorToolbar: React.FC<BorToolbarProps> = ({
  searchValue,
  onSearchChange,
  searchAriaLabel,
  searchPlaceholder,
  onSearch,
  onClearAll,
  children,
}) => (
  <Toolbar style={{ padding: 0, marginBottom: "0.5rem" }} clearAllFilters={onClearAll}>
    <ToolbarContent>
      <ToolbarItem>
        <SearchInput
          aria-label={searchAriaLabel}
          placeholder={searchPlaceholder}
          value={searchValue}
          onChange={(_ev, val) => onSearchChange(val)}
          onSearch={onSearch ? () => onSearch() : undefined}
          onClear={() => onSearchChange("")}
        />
      </ToolbarItem>
      {children}
    </ToolbarContent>
  </Toolbar>
);
