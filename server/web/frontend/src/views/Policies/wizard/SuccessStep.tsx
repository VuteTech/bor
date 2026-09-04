// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useRef } from "react";
import {
  Button,
  EmptyState,
  EmptyStateActions,
  EmptyStateBody,
  EmptyStateFooter,
  Flex,
  FlexItem,
  Label,
} from "@patternfly/react-core";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";

import type { Policy } from "../../../apiClient/policiesApi";
import { LiveAlert } from "../../../components/LiveAlert";
import { PolicyTypeLabel } from "../../../components/PolicyTypeLabel";

interface SuccessStepProps {
  policy: Policy;
  released: boolean;
  releasing: boolean;
  releaseError: string | null;
  onRelease: () => void;
  onAssign: () => void;
  onOpen: () => void;
  onCreateAnother: () => void;
  onBackToList: () => void;
}

/**
 * Shown in place of the wizard once the policy exists: confirms the outcome
 * and offers the two things people do next — release it and bind it to a
 * node group — plus the usual ways out. Focus lands on the heading because
 * the wizard (and whatever had focus in it) has just unmounted.
 */
export const SuccessStep: React.FC<SuccessStepProps> = ({
  policy,
  released,
  releasing,
  releaseError,
  onRelease,
  onAssign,
  onOpen,
  onCreateAnother,
  onBackToList,
}) => {
  const titleRef = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    titleRef.current?.focus();
  }, []);

  return (
    <EmptyState
      status="success"
      icon={CheckCircleIcon}
      headingLevel="h2"
      titleText={
        <span ref={titleRef} tabIndex={-1} id="policy-created-title">
          Policy “{policy.name}” created
        </span>
      }
      variant="lg"
    >
      <EmptyStateBody>
        <Flex justifyContent={{ default: "justifyContentCenter" }} alignItems={{ default: "alignItemsCenter" }} gap={{ default: "gapSm" }} style={{ marginBottom: "1rem" }}>
          <FlexItem>
            <PolicyTypeLabel type={policy.type} />
          </FlexItem>
          <FlexItem>
            <Label color={released ? "green" : "blue"} icon={released ? <CheckCircleIcon /> : undefined}>
              {released ? "Released" : "Draft"}
            </Label>
          </FlexItem>
        </Flex>
        {released ? (
          <p>
            The policy is released. It reaches nodes as soon as it is bound to a node group and the binding is
            enabled.
          </p>
        ) : (
          <p>
            It is a draft: nothing reaches a node until the policy is released and bound to a node group. You can do
            both now, or later from the policy page.
          </p>
        )}
        <LiveAlert
          message={released ? `“${policy.name}” is now released.` : null}
          variant="success"
          isInline
          isPlain
          style={{ marginTop: "0.75rem" }}
        />
        <LiveAlert message={releaseError} variant="danger" isInline style={{ marginTop: "0.75rem" }} />
      </EmptyStateBody>
      <EmptyStateFooter>
        <EmptyStateActions>
          <Button
            variant="primary"
            onClick={onRelease}
            isLoading={releasing}
            isDisabled={released || releasing}
            icon={released ? <CheckCircleIcon /> : undefined}
          >
            {released ? "Released" : "Release now"}
          </Button>
          <Button variant="secondary" onClick={onAssign}>
            Assign to a node group
          </Button>
        </EmptyStateActions>
        <EmptyStateActions>
          <Button variant="link" onClick={onOpen}>
            Open the policy
          </Button>
          <Button variant="link" onClick={onCreateAnother}>
            Create another policy
          </Button>
          <Button variant="link" onClick={onBackToList}>
            Back to policies
          </Button>
        </EmptyStateActions>
      </EmptyStateFooter>
    </EmptyState>
  );
};
