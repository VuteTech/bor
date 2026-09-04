// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useRef } from "react";
import {
  Button,
  Divider,
  Drawer,
  DrawerActions,
  DrawerCloseButton,
  DrawerContent,
  DrawerContentBody,
  DrawerHead,
  DrawerPanelBody,
  DrawerPanelContent,
  Flex,
  FlexItem,
  Title,
} from "@patternfly/react-core";
import OutlinedQuestionCircleIcon from "@patternfly/react-icons/dist/esm/icons/outlined-question-circle-icon";

import { PolicyTypeIcon } from "../../../components/PolicyTypeIcon";
import { getPolicyType, type PolicyTypeDef, type PolicyTypeId } from "../../../policyTypes/registry";
import { PolicyTypeDescriptionBody } from "./PolicyTypeDescriptionBody";

export type HelpStep = "details" | "configuration" | "review";

/** Types edited through the master-detail tree (preview / add-to-policy model). */
const TREE_TYPES = new Set<PolicyTypeId>(["Firefox", "Thunderbird", "Chrome", "Edge", "Kconfig"]);

const STEP_HINTS: Record<HelpStep, (def: PolicyTypeDef) => string> = {
  details: () =>
    "The name appears in the policies list, in bindings and in compliance reports, so say what the policy does and who it is for. Names do not have to be unique. The description is free text for the why: owners, rooms, tickets.",
  configuration: (def) =>
    TREE_TYPES.has(def.id)
      ? "Pick a setting in the tree to preview it. It joins the policy when you change its value or click “Add to policy”; settings that are in the policy show a dot in the tree and can be removed there or from the form. Next unlocks once at least one setting is in the policy."
      : "Add at least one rule or entry; Next unlocks when the content is complete. Everything you enter is kept while you move between steps.",
  review: () =>
    "Check the settings table; each Edit link jumps back to the step that owns that part. Creating makes a draft: nothing reaches a node until the policy is released and bound to a node group, and the next screen offers both.",
};

interface WizardHelpDrawerProps {
  type: PolicyTypeId;
  step: HelpStep;
  isOpen: boolean;
  onOpen: () => void;
  onClose: () => void;
  children: React.ReactNode;
}

const PANEL_ID = "policy-wizard-help";
const PANEL_TITLE_ID = "policy-wizard-help-title";

/**
 * Wraps a step body in an inline PatternFly Drawer (the "in page, with drawer
 * and informational step" pattern): a link above the content opens a panel
 * with a tip for the current step and the type's description, so help sits
 * *beside* the wizard and never covers it. Opening moves focus into the
 * panel; closing returns it to the link.
 */
export const WizardHelpDrawer: React.FC<WizardHelpDrawerProps> = ({ type, step, isOpen, onOpen, onClose, children }) => {
  const def = getPolicyType(type);
  const headRef = useRef<HTMLSpanElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);

  if (!def) return <>{children}</>;

  const close = () => {
    onClose();
    // The close button is about to unmount; put focus back where the user started.
    window.requestAnimationFrame(() => toggleRef.current?.focus());
  };

  const panel = (
    <DrawerPanelContent
      id={PANEL_ID}
      isResizable
      defaultSize="20rem"
      minSize="16rem"
      aria-labelledby={PANEL_TITLE_ID}
    >
      <DrawerHead>
        <span ref={headRef} tabIndex={-1}>
          <Flex alignItems={{ default: "alignItemsCenter" }} gap={{ default: "gapSm" }} flexWrap={{ default: "nowrap" }}>
            <FlexItem style={{ display: "flex", color: "var(--pf-t--global--color--brand--default)" }}>
              <PolicyTypeIcon type={def.id} size="lg" />
            </FlexItem>
            <FlexItem>
              <Title headingLevel="h3" size="md" id={PANEL_TITLE_ID}>
                About {def.label} policies
              </Title>
              {def.technicalName && <div className="bor-text-secondary">{def.technicalName}</div>}
            </FlexItem>
          </Flex>
        </span>
        <DrawerActions>
          <DrawerCloseButton onClick={close} aria-label="Close help" />
        </DrawerActions>
      </DrawerHead>
      <DrawerPanelBody>
        <Title headingLevel="h4" size="md" style={{ marginBottom: "0.5rem" }}>
          On this step
        </Title>
        <p style={{ marginBottom: "1rem" }}>{STEP_HINTS[step](def)}</p>
        <Divider style={{ marginBottom: "1rem" }} />
        <PolicyTypeDescriptionBody def={def} />
      </DrawerPanelBody>
    </DrawerPanelContent>
  );

  return (
    <Drawer isExpanded={isOpen} isInline position="end" onExpand={() => headRef.current?.focus()}>
      <DrawerContent panelContent={panel}>
        <DrawerContentBody hasPadding>
          <Flex justifyContent={{ default: "justifyContentFlexEnd" }} style={{ marginBottom: "0.5rem" }}>
            <FlexItem>
              <Button
                ref={toggleRef}
                variant="link"
                isInline
                icon={<OutlinedQuestionCircleIcon />}
                onClick={isOpen ? close : onOpen}
                aria-expanded={isOpen}
                aria-controls={PANEL_ID}
              >
                {isOpen ? "Hide help" : `About ${def.label} policies`}
              </Button>
            </FlexItem>
          </Flex>
          {children}
        </DrawerContentBody>
      </DrawerContent>
    </Drawer>
  );
};
