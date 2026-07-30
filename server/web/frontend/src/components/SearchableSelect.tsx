// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * SearchableSelect — a single-select PF6 typeahead with a built-in scroll cap.
 *
 * Use it for dropdowns whose option lists are data-driven or can grow long
 * (node groups, agent versions, …): the user can filter by typing, navigate
 * with the arrow keys, and the menu scrolls (`isScrollable`) instead of running
 * off the screen. Short fixed enums (2–3 options) don't need this — a plain
 * <Select> is fine there.
 *
 * Controlled: pass the current `selected` value and an `onSelect` handler.
 */

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Select,
  SelectOption,
  SelectList,
  MenuToggle,
  MenuToggleElement,
  TextInputGroup,
  TextInputGroupMain,
} from "@patternfly/react-core";

export interface SearchableSelectOption {
  value: string;
  label: string;
  /** Optional secondary text shown under the label in the menu. */
  description?: string;
}

interface SearchableSelectProps {
  options: SearchableSelectOption[];
  /** Currently selected value (must match one of the options' `value`). */
  selected: string;
  onSelect: (value: string) => void;
  /** Accessible name for the combobox input. */
  ariaLabel: string;
  /** Placeholder shown when the input is empty (e.g. "Filter by group"). */
  placeholder?: string;
  /**
   * A value that represents the "no filter" / default choice (e.g. "All"). When
   * `selected` equals it, the input renders empty so the naming `placeholder`
   * shows instead of the option label — useful in filter toolbars. The option
   * itself still appears in the list so it can be re-selected.
   */
  emptyValue?: string;
  isDisabled?: boolean;
  /** Optional explicit id root; falls back to a generated one. */
  id?: string;
}

export const SearchableSelect: React.FC<SearchableSelectProps> = ({
  options,
  selected,
  onSelect,
  ariaLabel,
  placeholder,
  emptyValue,
  isDisabled,
  id,
}) => {
  const reactId = React.useId();
  const baseId = id ?? reactId;
  const listboxId = `${baseId}-listbox`;
  const optionId = (index: number) => `${baseId}-option-${index}`;

  const labelFor = (value: string): string =>
    emptyValue !== undefined && value === emptyValue
      ? ""
      : options.find((o) => o.value === value)?.label ?? "";

  const selectedLabel = useMemo(
    () => labelFor(selected),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [options, selected, emptyValue],
  );

  const [isOpen, setIsOpen] = useState(false);
  const [inputValue, setInputValue] = useState(selectedLabel);
  const [filter, setFilter] = useState("");
  const [focusedIndex, setFocusedIndex] = useState<number | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Reflect an externally-changed selection in the input text.
  useEffect(() => {
    setInputValue(selectedLabel);
  }, [selectedLabel]);

  const filtered = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return options;
    return options.filter(
      (o) =>
        o.label.toLowerCase().includes(needle) ||
        (o.description?.toLowerCase().includes(needle) ?? false),
    );
  }, [options, filter]);

  const openMenu = () => {
    if (!isOpen) setIsOpen(true);
  };

  const closeMenu = () => {
    setIsOpen(false);
    setFilter("");
    setFocusedIndex(null);
    setInputValue(selectedLabel); // restore display to the real selection
  };

  const choose = (value: string) => {
    onSelect(value);
    setInputValue(labelFor(value));
    setFilter("");
    setFocusedIndex(null);
    setIsOpen(false);
  };

  const onTextChange = (_ev: React.FormEvent<HTMLInputElement>, value: string) => {
    setInputValue(value);
    setFilter(value);
    setFocusedIndex(null);
    openMenu();
  };

  const onInputKeyDown = (ev: React.KeyboardEvent<HTMLInputElement>) => {
    const count = filtered.length;
    switch (ev.key) {
      case "ArrowDown":
      case "ArrowUp": {
        ev.preventDefault();
        if (!isOpen) {
          openMenu();
          return;
        }
        if (count === 0) return;
        const cur = focusedIndex ?? -1;
        setFocusedIndex(
          ev.key === "ArrowDown" ? (cur + 1) % count : (cur - 1 + count) % count,
        );
        break;
      }
      case "Enter":
        if (isOpen && focusedIndex !== null && filtered[focusedIndex]) {
          ev.preventDefault();
          choose(filtered[focusedIndex].value);
        }
        break;
      case "Escape":
        closeMenu();
        break;
    }
  };

  const toggle = (toggleRef: React.Ref<MenuToggleElement>) => (
    <MenuToggle
      ref={toggleRef}
      variant="typeahead"
      aria-label={ariaLabel}
      onClick={() => (isOpen ? closeMenu() : openMenu())}
      isExpanded={isOpen}
      isFullWidth
      isDisabled={isDisabled}
    >
      <TextInputGroup isPlain>
        <TextInputGroupMain
          value={inputValue}
          onClick={(ev) => {
            // Keep clicks inside the input from bubbling to the toggle (which
            // would close an open menu); just ensure it's open.
            ev.stopPropagation();
            openMenu();
          }}
          onChange={onTextChange}
          innerRef={inputRef}
          placeholder={placeholder}
          role="combobox"
          aria-label={ariaLabel}
          aria-controls={listboxId}
          aria-activedescendant={focusedIndex !== null ? optionId(focusedIndex) : undefined}
          isExpanded={isOpen}
          inputProps={{ autoComplete: "off", onKeyDown: onInputKeyDown }}
        />
      </TextInputGroup>
    </MenuToggle>
  );

  return (
    <Select
      id={baseId}
      isOpen={isOpen}
      selected={selected}
      onSelect={(_ev, value) => value != null && choose(String(value))}
      onOpenChange={(open) => (open ? setIsOpen(true) : closeMenu())}
      toggle={toggle}
      isScrollable
      shouldFocusFirstItemOnOpen={false}
    >
      <SelectList id={listboxId}>
        {filtered.length === 0 ? (
          <SelectOption isDisabled>No results found</SelectOption>
        ) : (
          filtered.map((o, index) => (
            <SelectOption
              key={o.value}
              value={o.value}
              id={optionId(index)}
              isFocused={focusedIndex === index}
              description={o.description}
            >
              {o.label}
            </SelectOption>
          ))
        )}
      </SelectList>
    </Select>
  );
};
