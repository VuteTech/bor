// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import { Card, CardBody, CardHeader, CardTitle, Flex, FlexItem, Gallery, Title } from "@patternfly/react-core";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";

import { PolicyTypeIcon } from "../../../components/PolicyTypeIcon";
import { policyTypesByCategory, type PolicyTypeId } from "../../../policyTypes/registry";

interface PolicyTypeTileGridProps {
  selected: PolicyTypeId | null;
  onSelect: (id: PolicyTypeId) => void;
}

/**
 * The "choose a type" grid: one selectable tile per policy type, grouped by
 * category. Tiles are PatternFly "cards as tiles": a real (visually hidden)
 * radio per card, all sharing one `name`, so arrow keys move the selection and
 * the card is the radio's label. Selection is signalled by the card's
 * selected styling *and* a check icon with hidden text, never by colour alone.
 */
export const PolicyTypeTileGrid: React.FC<PolicyTypeTileGridProps> = ({ selected, onSelect }) => (
  <div role="group" aria-label="Policy types">
    {policyTypesByCategory().map(({ category, types }) => (
      <div key={category.id} style={{ marginBottom: "1.5rem" }}>
        <Title headingLevel="h3" size="md" style={{ marginBottom: "0.75rem" }}>
          {category.label}
        </Title>
        <Gallery hasGutter minWidths={{ default: "12rem" }}>
          {types.map((t) => {
            const isSelected = selected === t.id;
            const titleId = `policy-type-title-${t.id}`;
            const descId = `policy-type-tagline-${t.id}`;
            return (
              <Card
                key={t.id}
                id={`policy-type-tile-${t.id}`}
                isSelectable
                isSelected={isSelected}
                isCompact
                isFullHeight
              >
                <CardHeader
                  selectableActions={{
                    selectableActionId: `policy-type-${t.id}`,
                    selectableActionAriaLabelledby: titleId,
                    selectableActionProps: { "aria-describedby": descId },
                    name: "policy-type",
                    variant: "single",
                    onChange: () => onSelect(t.id),
                    isHidden: true,
                  }}
                >
                  <Flex alignItems={{ default: "alignItemsCenter" }} gap={{ default: "gapSm" }} flexWrap={{ default: "nowrap" }}>
                    <FlexItem
                      style={{
                        display: "flex",
                        color: isSelected
                          ? "var(--pf-t--global--color--brand--default)"
                          : "var(--pf-t--global--icon--color--regular)",
                      }}
                    >
                      <PolicyTypeIcon type={t.id} size="lg" />
                    </FlexItem>
                    <FlexItem grow={{ default: "grow" }}>
                      <CardTitle id={titleId}>{t.label}</CardTitle>
                      {t.technicalName && <div className="bor-text-secondary">{t.technicalName}</div>}
                    </FlexItem>
                    {isSelected && (
                      <FlexItem style={{ display: "flex", color: "var(--pf-t--global--color--brand--default)" }}>
                        <CheckCircleIcon aria-hidden="true" />
                        <span className="pf-v6-screen-reader">Selected</span>
                      </FlexItem>
                    )}
                  </Flex>
                </CardHeader>
                <CardBody id={descId} className="bor-text-secondary">
                  {t.tagline}
                </CardBody>
              </Card>
            );
          })}
        </Gallery>
      </div>
    ))}
  </div>
);
