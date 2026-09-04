// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { Label } from "@patternfly/react-core";

import { policyTypeLabel } from "../policyTypes/registry";
import { PolicyTypeIcon } from "./PolicyTypeIcon";

interface PolicyTypeLabelProps {
  /** `Policy.type` wire value. */
  type: string;
  isCompact?: boolean;
  variant?: "filled" | "outline";
  /** Extra text after the label, e.g. a count. */
  suffix?: React.ReactNode;
}

/**
 * Icon + human label for a policy type, as a PatternFly `Label`. Used in the
 * policies list, filters, bindings and the dashboard so a type always looks
 * the same everywhere.
 */
export const PolicyTypeLabel: React.FC<PolicyTypeLabelProps> = ({
  type,
  isCompact = false,
  variant = "filled",
  suffix,
}) => (
  <Label color="blue" isCompact={isCompact} variant={variant} icon={<PolicyTypeIcon type={type} size="sm" />}>
    {policyTypeLabel(type)}
    {suffix}
  </Label>
);
