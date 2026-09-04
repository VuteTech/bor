// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect } from "react";
import { useParams, useNavigate, useLocation, Navigate } from "react-router";
import {
  PageSection,
  Spinner,
  Alert,
  Button,
  Flex,
  FlexItem,
} from "@patternfly/react-core";
import ArrowLeftIcon from "@patternfly/react-icons/dist/esm/icons/arrow-left-icon";

import { fetchPolicy, Policy } from "../../apiClient/policiesApi";
import { PolicyEditor } from "./PolicyEditor";

/**
 * PolicyEditorPage is the full-page route wrapper around PolicyEditor, mounted
 * at `/policies/:policyId/edit` (edit/view). It loads the target policy before
 * rendering the editor. Saving or cancelling returns to wherever the user came
 * from (the policies list, or the policy-bindings page when opened from
 * there); that page re-fetches on mount, so no explicit refresh is needed
 * here. Creation lives in the wizard at `/policies/new`.
 */
export const PolicyEditorPage: React.FC = () => {
  const { policyId } = useParams<{ policyId: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  // Origin page to return to, set by whoever navigated here (defaults to the
  // policies list for direct/deep links).
  const from = (location.state as { from?: string } | null)?.from ?? "/policies";

  const [policy, setPolicy] = useState<Policy | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!policyId) return;
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchPolicy(policyId)
      .then((p) => {
        if (!cancelled) setPolicy(p);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load policy");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [policyId]);

  const backToList = () => navigate(from);

  if (loading) {
    return (
      <PageSection>
        <Flex justifyContent={{ default: "justifyContentCenter" }}>
          <FlexItem>
            <Spinner size="xl" aria-label="Loading" />
          </FlexItem>
        </Flex>
      </PageSection>
    );
  }

  if (error) {
    return (
      <PageSection>
        <div aria-live="assertive" aria-atomic="true">
          <Alert variant="danger" title="Error loading policy" isInline>
            {error}
          </Alert>
        </div>
        <Button
          variant="link"
          icon={<ArrowLeftIcon />}
          onClick={backToList}
          style={{ marginTop: "1rem", paddingLeft: 0 }}
        >
          Back to policies
        </Button>
      </PageSection>
    );
  }

  // No id (or a policy that never loaded): creation has its own route.
  if (!policyId || !policy) {
    return <Navigate to="/policies/new" replace />;
  }

  // The editor's Cancel button routes through its own unsaved-changes guard
  // before calling onClose, so browsing away here stays safe. onSaved is a
  // no-op: the origin page re-fetches when it re-mounts on return.
  return (
    <PolicyEditor
      policy={policy}
      onClose={backToList}
      onSaved={() => {
        /* origin page refreshes on return */
      }}
      onDeleted={backToList}
    />
  );
};
