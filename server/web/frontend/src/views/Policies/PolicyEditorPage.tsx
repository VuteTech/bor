// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
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
import { PolicyDetailsModal } from "./PolicyDetailsModal";

/**
 * PolicyEditorPage is the full-page route wrapper around PolicyDetailsModal.
 * It is mounted at `/policies/new` (create) and `/policies/:policyId/edit`
 * (edit/view), loading the target policy before rendering the editor in its
 * `asPage` mode. Saving or cancelling returns to the policies list; the list
 * re-fetches on mount, so no explicit refresh is needed here.
 */
export const PolicyEditorPage: React.FC = () => {
  const { policyId } = useParams<{ policyId: string }>();
  const navigate = useNavigate();

  const [policy, setPolicy] = useState<Policy | null>(null);
  const [loading, setLoading] = useState<boolean>(!!policyId);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // Create mode: no policy to load.
    if (!policyId) {
      setPolicy(null);
      setLoading(false);
      return;
    }
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

  const backToList = () => navigate("/policies");

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

  // The editor's Cancel button routes through its own unsaved-changes guard
  // before calling onClose, so browsing away here stays safe. onSaved is a
  // no-op: PoliciesPage re-fetches when it re-mounts on return.
  return (
    <PolicyDetailsModal
      asPage
      isOpen
      policy={policy}
      onClose={backToList}
      onSaved={() => {
        /* list refreshes on return */
      }}
      onDeleted={backToList}
    />
  );
};
