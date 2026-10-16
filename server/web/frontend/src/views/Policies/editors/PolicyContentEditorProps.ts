// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * The contract every per-type configuration editor implements. The editor
 * owns only UI state (which key is selected, which groups are expanded); the
 * policy content itself lives in the caller as the raw JSON string and every
 * edit is pushed up immediately through `onChange`.
 */
export interface PolicyContentEditorProps {
  contentRaw: string;
  onChange: (raw: string) => void;
  /** Read-only rendering: hides add/remove affordances. Inputs are disabled by the caller's fieldset. */
  isDisabled?: boolean;
  /**
   * Reports whether the content can currently be saved as far as the editor
   * can tell (e.g. a JSON field mid-edit is invalid). Editors that cannot
   * produce invalid content never call it.
   */
  onValidityChange?: (ok: boolean) => void;
  /**
   * The policy's name as the caller knows it (the saved name, or the name
   * typed so far in the create wizard). Editors use it for labels that
   * refer to the policy itself, such as the look-and-feel package a KDE
   * policy generates.
   */
  policyName?: string;
}
