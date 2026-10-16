// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { FormHelperText, HelperText, HelperTextItem } from "@patternfly/react-core";

/** Helper text under a form field, in the PatternFly helper-text markup. */
export function FieldHelp({ children }: { children: React.ReactNode }) {
  return (
    <FormHelperText>
      <HelperText>
        <HelperTextItem>{children}</HelperTextItem>
      </HelperText>
    </FormHelperText>
  );
}
