// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ListEditor — the shared "editable list" shell used by the structured policy
 * editors (Firewalld string/port lists, Polkit action/condition lists, etc.).
 * It owns the repeated pattern: a row per item (item content + a remove
 * TrashIcon), an optional empty message, and an "Add" button.
 *
 * The per-item input is supplied via `renderItem`, so each caller keeps its own
 * field(s) and sizing; the shell only standardizes the remove affordance and the
 * add button.
 */

import React from "react";
import { Button } from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

interface ListEditorProps<T> {
  items: T[];
  renderItem: (item: T, index: number) => React.ReactNode;
  onRemove: (index: number) => void;
  onAdd: () => void;
  addLabel: string;
  removeAriaLabel: (index: number) => string;
  /** "secondary" (a small filled button) or "link" (inline). Default "secondary". */
  addVariant?: "secondary" | "link";
  /** Shown when the list is empty. */
  emptyText?: string;
  /** Gap between the item content and the remove button. Default "0.5rem". */
  rowGap?: string;
  isDisabled?: boolean;
}

export function ListEditor<T>({
  items,
  renderItem,
  onRemove,
  onAdd,
  addLabel,
  removeAriaLabel,
  addVariant = "secondary",
  emptyText,
  rowGap = "0.5rem",
  isDisabled,
}: ListEditorProps<T>) {
  return (
    <>
      {items.length === 0 && emptyText && (
        <p style={{ fontSize: "0.85rem", color: "var(--pf-t--global--text--color--subtle)", marginBottom: "0.4rem" }}>
          {emptyText}
        </p>
      )}
      {items.map((item, idx) => (
        <div key={idx} style={{ display: "flex", gap: rowGap, marginBottom: "0.4rem", alignItems: "center" }}>
          {renderItem(item, idx)}
          <Button
            variant="plain"
            onClick={() => onRemove(idx)}
            isDisabled={isDisabled}
            aria-label={removeAriaLabel(idx)}
            style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
          >
            <TrashIcon />
          </Button>
        </div>
      ))}
      <Button
        variant={addVariant}
        icon={<PlusCircleIcon />}
        onClick={onAdd}
        isDisabled={isDisabled}
        size={addVariant === "secondary" ? "sm" : undefined}
        style={addVariant === "link" ? { paddingLeft: 0 } : undefined}
      >
        {addLabel}
      </Button>
    </>
  );
}
