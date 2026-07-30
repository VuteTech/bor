// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * PolicyTreePanel — the shared left-hand tree for the policy editors
 * (Firefox / Thunderbird / Chrome / Edge / KConfig). These editors previously
 * duplicated this markup verbatim; extracting it keeps the tree behaviour,
 * styling, and accessibility (role="tree"/"treeitem", aria-expanded/selected/
 * level) in one place.
 *
 * Generic over the item type — the editors pass their own typed policy defs; the
 * panel only needs `key` and `label`.
 */

import React from "react";
import { Button } from "@patternfly/react-core";

interface TreeItem {
  key: string;
  label: string;
}

interface PolicyTreePanelProps<T extends TreeItem> {
  ariaLabel: string;
  /** group name -> items in that group */
  tree: Map<string, T[]>;
  /** keys currently present in the policy content (shown with a ● marker). */
  configuredKeys: string[];
  selectedKey: string | null;
  expandedGroups: Set<string>;
  onToggleGroup: (group: string) => void;
  onSelect: (item: T) => void;
  onRemove: (key: string) => void;
}

export function PolicyTreePanel<T extends TreeItem>({
  ariaLabel,
  tree,
  configuredKeys,
  selectedKey,
  expandedGroups,
  onToggleGroup,
  onSelect,
  onRemove,
}: PolicyTreePanelProps<T>) {
  return (
    <div role="tree" aria-label={ariaLabel} style={{
      width: "260px",
      minWidth: "260px",
      borderRight: "1px solid var(--pf-t--global--border--color--default)",
      overflowY: "auto",
      paddingRight: "0",
    }}>
      {Array.from(tree.entries()).map(([group, items]) => {
        const expanded = expandedGroups.has(group);
        return (
          <div key={group} style={{ marginBottom: "2px" }}>
            {/* Group header */}
            <div
              role="treeitem"
              aria-expanded={expanded}
              aria-selected={false}
              aria-level={1}
              tabIndex={0}
              onClick={() => onToggleGroup(group)}
              onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onToggleGroup(group); } }}
              style={{
                padding: "0.4rem 0.75rem",
                cursor: "pointer",
                fontWeight: 600,
                fontSize: "0.8rem",
                textTransform: "uppercase",
                letterSpacing: "0.03em",
                color: "var(--pf-t--global--text--color--regular)",
                backgroundColor: "var(--pf-t--global--background--color--secondary--default)",
                borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                userSelect: "none",
                display: "flex",
                alignItems: "center",
                gap: "0.4rem",
              }}
            >
              <span style={{
                display: "inline-block",
                width: 0,
                height: 0,
                borderStyle: "solid",
                ...(expanded
                  ? { borderWidth: "5px 4px 0 4px", borderColor: "var(--pf-t--global--text--color--regular) transparent transparent transparent" }
                  : { borderWidth: "4px 0 4px 5px", borderColor: "transparent transparent transparent var(--pf-t--global--text--color--regular)" }),
              }} />
              {group}
            </div>
            {/* Policy items */}
            {expanded && items.map((item) => {
              const isSelected = selectedKey === item.key;
              const isConfigured = configuredKeys.includes(item.key);
              return (
                <div
                  key={item.key}
                  role="treeitem"
                  aria-selected={isSelected}
                  aria-level={2}
                  tabIndex={0}
                  onClick={() => onSelect(item)}
                  onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(item); } }}
                  style={{
                    padding: "0.35rem 0.75rem 0.35rem 1.5rem",
                    cursor: "pointer",
                    fontSize: "0.85rem",
                    backgroundColor: isSelected ? "#2d6a4f" : isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent",
                    color: isSelected ? "#fff" : "var(--pf-t--global--text--color--regular)",
                    fontWeight: isSelected || isConfigured ? 600 : 400,
                    borderBottom: "1px solid var(--pf-t--global--border--color--default)",
                    userSelect: "none",
                    transition: "background-color 0.1s ease",
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                  }}
                  onMouseEnter={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.22)" : "rgba(45, 106, 79, 0.1)"; }}
                  onMouseLeave={(e) => { if (!isSelected) (e.currentTarget as HTMLElement).style.backgroundColor = isConfigured ? "rgba(45, 106, 79, 0.13)" : "transparent"; }}
                >
                  <span style={{ display: "flex", alignItems: "center", gap: "0.35rem" }}>
                    {isConfigured && <span style={{ color: isSelected ? "#fff" : "var(--pf-t--global--color--brand--200)", fontSize: "0.7rem" }}>●</span>}
                    {item.label}
                  </span>
                  {isConfigured && (
                    <Button
                      variant="plain"
                      size="sm"
                      onClick={(e) => { e.stopPropagation(); onRemove(item.key); }}
                      style={{
                        fontSize: "0.75rem",
                        color: isSelected ? "#fff" : "var(--pf-t--global--text--color--subtle)",
                        padding: "0 0.25rem",
                        minWidth: "auto",
                      }}
                      aria-label={`Remove ${item.label}`}
                    >✕</Button>
                  )}
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
