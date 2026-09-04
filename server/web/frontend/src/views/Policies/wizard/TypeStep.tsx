// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { Content, Grid, GridItem } from "@patternfly/react-core";

import type { PolicyTypeId } from "../../../policyTypes/registry";
import { PolicyTypeDescriptionPanel } from "./PolicyTypeDescriptionPanel";
import { PolicyTypeTileGrid } from "./PolicyTypeTileGrid";

interface TypeStepProps {
  selected: PolicyTypeId | null;
  onSelect: (id: PolicyTypeId) => void;
}

/** Step 1: tile grid on the left, description of the selected type on the right (stacked below `lg`). */
export const TypeStep: React.FC<TypeStepProps> = ({ selected, onSelect }) => (
  <>
    <Content component="p" style={{ marginBottom: "1rem" }}>
      Choose what this policy will configure. The type decides which settings you can configure in the next steps.
    </Content>
    <Grid hasGutter>
      <GridItem span={12} lg={7} xl={8}>
        <PolicyTypeTileGrid selected={selected} onSelect={onSelect} />
      </GridItem>
      <GridItem span={12} lg={5} xl={4}>
        <div style={{ position: "sticky", top: "0.5rem" }}>
          <PolicyTypeDescriptionPanel type={selected} />
        </div>
      </GridItem>
    </Grid>
  </>
);
