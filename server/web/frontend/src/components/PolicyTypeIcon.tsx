// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";

import { getPolicyType, UNKNOWN_POLICY_TYPE_ICON } from "../policyTypes/registry";

export type PolicyTypeIconSize = "sm" | "md" | "lg" | "xl";

const SIZE_REM: Record<PolicyTypeIconSize, number> = {
  sm: 1, // inline with text / compact labels
  md: 1.5, // table cells, headings
  lg: 2.5, // wizard tiles
  xl: 3.5, // description panel header
};

interface PolicyTypeIconProps {
  /** `Policy.type` wire value; unknown types render a generic cog. */
  type: string;
  size?: PolicyTypeIconSize;
  className?: string;
  style?: React.CSSProperties;
  /** Accessible name. Omit when a visible label sits next to the icon (default: decorative). */
  title?: string;
}

/**
 * The one way to draw a policy-type icon. Colour is inherited (`currentColor`),
 * so wrap it in an element that sets a `--pf-t--global--icon--color--*` or
 * text token; never pass a raw colour.
 */
export const PolicyTypeIcon: React.FC<PolicyTypeIconProps> = ({ type, size = "md", className, style, title }) => {
  const Icon = getPolicyType(type)?.Icon ?? UNKNOWN_POLICY_TYPE_ICON;
  return (
    <Icon
      className={className}
      title={title}
      style={{ fontSize: `${SIZE_REM[size]}rem`, verticalAlign: "-0.125em", flexShrink: 0, ...style }}
    />
  );
};
