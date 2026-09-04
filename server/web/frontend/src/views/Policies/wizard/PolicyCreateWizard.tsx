// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useReducer, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";
import { PageSection, Wizard, WizardStep } from "@patternfly/react-core";

import { createPolicy, setPolicyState, type Policy } from "../../../apiClient/policiesApi";
import { ConfirmModal } from "../../../components/ConfirmModal";
import { LiveAlert } from "../../../components/LiveAlert";
import {
  defaultContentForType,
  isPolicyTypeId,
  policyTypeLabel,
  validatePolicyContent,
  type PolicyTypeId,
} from "../../../policyTypes/registry";
import { PolicyContentEditor } from "../editors/PolicyContentEditor";
import { normalizePolicyContent } from "../editors/policyContentJson";
import { DetailsStep } from "./DetailsStep";
import { ReviewStep } from "./ReviewStep";
import { SuccessStep } from "./SuccessStep";
import { TypeStep } from "./TypeStep";
import { WizardHelpDrawer } from "./WizardHelpDrawer";

/* ── Wizard state ── */

interface WizardState {
  type: PolicyTypeId | null;
  name: string;
  description: string;
  content: string;
  /** False while the content editor holds something unparsable (a JSON field mid-edit). */
  contentValid: boolean;
}

type WizardAction =
  | { kind: "selectType"; type: PolicyTypeId }
  | { kind: "setName"; name: string }
  | { kind: "setDescription"; description: string }
  | { kind: "setContent"; content: string }
  | { kind: "setContentValid"; ok: boolean }
  | { kind: "reset"; type: PolicyTypeId | null };

function initialState(type: PolicyTypeId | null): WizardState {
  return {
    type,
    name: "",
    description: "",
    content: type ? defaultContentForType(type) : "{}",
    contentValid: true,
  };
}

function reducer(state: WizardState, action: WizardAction): WizardState {
  switch (action.kind) {
    case "selectType":
      if (action.type === state.type) return state;
      // A new type starts from its own empty content; whatever was configured
      // for the previous type is gone (the caller confirms this first).
      return { ...state, type: action.type, content: defaultContentForType(action.type), contentValid: true };
    case "setName":
      return { ...state, name: action.name };
    case "setDescription":
      return { ...state, description: action.description };
    case "setContent":
      return { ...state, content: action.content };
    case "setContentValid":
      return state.contentValid === action.ok ? state : { ...state, contentValid: action.ok };
    case "reset":
      return initialState(action.type);
  }
}

/* ── Component ── */

/**
 * The full-page "Create a policy" wizard at `/policies/new`:
 * Type → Details → Configuration → Review. Creates the policy as a draft and
 * then shows a success screen offering Release, Assign to a node group, and
 * the ways out (open the policy, create another, back to the list).
 *
 * `?type=<id>` preselects a type and starts on Details (deep links from the
 * dashboard, docs and empty states).
 */
export const PolicyCreateWizard: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  // Origin page to return to on cancel, set by whoever navigated here.
  const from = (location.state as { from?: string } | null)?.from ?? "/policies";

  const [initialType] = useState<PolicyTypeId | null>(() => {
    const preset = searchParams.get("type");
    return preset && isPolicyTypeId(preset) ? preset : null;
  });

  const [state, dispatch] = useReducer(reducer, initialType, initialState);

  const [pendingType, setPendingType] = useState<PolicyTypeId | null>(null);
  const [showDiscard, setShowDiscard] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The policy once it exists; the wizard gives way to the success screen.
  const [created, setCreated] = useState<Policy | null>(null);
  const [released, setReleased] = useState(false);
  const [releasing, setReleasing] = useState(false);
  const [releaseError, setReleaseError] = useState<string | null>(null);
  const [confirmRelease, setConfirmRelease] = useState(false);
  // The contextual help drawer on steps 2–4; stays open while moving between them.
  const [helpOpen, setHelpOpen] = useState(false);

  // Does the content hold anything beyond the type's empty default?
  const contentHasEdits =
    state.type !== null &&
    normalizePolicyContent(state.content) !== normalizePolicyContent(defaultContentForType(state.type));
  // Anything worth a "discard?" prompt (nothing once the policy is saved).
  const hasWork =
    created === null &&
    state.type !== null &&
    (state.name.trim() !== "" || state.description.trim() !== "" || contentHasEdits);

  // Tab close / reload while there is work in progress.
  useEffect(() => {
    if (!hasWork) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [hasWork]);

  const contentError = state.type ? validatePolicyContent(state.type, state.content) : null;

  const requestSelectType = (id: PolicyTypeId) => {
    if (id === state.type) return;
    if (contentHasEdits) {
      setPendingType(id);
      return;
    }
    dispatch({ kind: "selectType", type: id });
  };

  const leave = () => navigate(from);

  const handleCancel = () => {
    if (hasWork) {
      setShowDiscard(true);
      return;
    }
    leave();
  };

  const handleCreate = async () => {
    if (!state.type) return;
    setError(null);
    const validation = validatePolicyContent(state.type, state.content);
    if (validation) {
      setError(validation);
      return;
    }
    setSaving(true);
    try {
      const policy = await createPolicy({
        name: state.name.trim(),
        description: state.description,
        type: state.type,
        content: state.content,
      });
      setCreated(policy);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create policy");
    } finally {
      setSaving(false);
    }
  };

  const handleRelease = async () => {
    if (!created) return;
    setReleaseError(null);
    setReleasing(true);
    try {
      await setPolicyState(created.id, { state: "released" });
      setReleased(true);
      setConfirmRelease(false);
    } catch (err) {
      setReleaseError(err instanceof Error ? err.message : "Failed to release the policy");
      setConfirmRelease(false);
    } finally {
      setReleasing(false);
    }
  };

  const startAnother = () => {
    setCreated(null);
    setReleased(false);
    setReleaseError(null);
    setError(null);
    dispatch({ kind: "reset", type: initialType });
  };

  const setContent = (content: string) => dispatch({ kind: "setContent", content });
  // Stable identity matters: the editors register/unregister this in effects.
  const [setContentValid] = useState(() => (ok: boolean) => dispatch({ kind: "setContentValid", ok }));

  if (created) {
    return (
      <>
        <PageSection isFilled>
          <SuccessStep
            policy={created}
            released={released}
            releasing={releasing}
            releaseError={releaseError}
            onRelease={() => setConfirmRelease(true)}
            onAssign={() => navigate(`/policy-bindings?policy=${encodeURIComponent(created.id)}`)}
            onOpen={() => navigate(`/policies/${created.id}/edit`, { state: { from } })}
            onCreateAnother={startAnother}
            onBackToList={leave}
          />
        </PageSection>

        {/* Release is outward-facing: confirm it, as the policies list does. */}
        <ConfirmModal
          isOpen={confirmRelease}
          title={`Release “${created.name}”?`}
          confirmLabel="Release"
          isBusy={releasing}
          onConfirm={handleRelease}
          onCancel={() => setConfirmRelease(false)}
        >
          <p>
            Released policies become available for binding and are delivered to agents on all bound node groups. This
            policy has no bindings yet, so nothing changes on any node until you assign it to a node group.
          </p>
        </ConfirmModal>
      </>
    );
  }

  return (
    <>
      {/* type="wizard" makes the section a full-height flex container so the
          wizard body scrolls and the Back/Next footer stays in view. */}
      <PageSection type="wizard" isFilled>
          <Wizard
            navAriaLabel="Create policy steps"
            isVisitRequired
            shouldFocusContent
            startIndex={initialType ? 2 : 1}
            onClose={handleCancel}
            onSave={handleCreate}
          >
            <WizardStep id="type" name="Type" footer={{ isNextDisabled: state.type === null, isBackHidden: true }}>
              <TypeStep selected={state.type} onSelect={requestSelectType} />
            </WizardStep>

            <WizardStep
              id="details"
              name="Details"
              body={{ hasNoPadding: true }}
              footer={{ isNextDisabled: state.name.trim() === "" }}
            >
              {state.type && (
                <WizardHelpDrawer
                  type={state.type}
                  step="details"
                  isOpen={helpOpen}
                  onOpen={() => setHelpOpen(true)}
                  onClose={() => setHelpOpen(false)}
                >
                  <DetailsStep
                    type={state.type}
                    name={state.name}
                    description={state.description}
                    onNameChange={(name) => dispatch({ kind: "setName", name })}
                    onDescriptionChange={(description) => dispatch({ kind: "setDescription", description })}
                  />
                </WizardHelpDrawer>
              )}
            </WizardStep>

            <WizardStep
              id="configuration"
              name="Configuration"
              body={{ hasNoPadding: true }}
              status={state.contentValid ? "default" : "error"}
              footer={{ isNextDisabled: contentError !== null || !state.contentValid }}
            >
              {state.type && (
                <WizardHelpDrawer
                  type={state.type}
                  step="configuration"
                  isOpen={helpOpen}
                  onOpen={() => setHelpOpen(true)}
                  onClose={() => setHelpOpen(false)}
                >
                  <PolicyContentEditor
                    type={state.type}
                    contentRaw={state.content}
                    onChange={setContent}
                    onValidityChange={setContentValid}
                  />
                  <LiveAlert
                    message={state.contentValid ? contentError : "Fix the invalid JSON value to continue."}
                    variant="info"
                    isInline
                    isPlain
                  />
                </WizardHelpDrawer>
              )}
            </WizardStep>

            <WizardStep
              id="review"
              name="Review"
              body={{ hasNoPadding: true }}
              footer={{ nextButtonText: "Create policy", isNextDisabled: saving, isBackDisabled: saving }}
            >
              {state.type && (
                <WizardHelpDrawer
                  type={state.type}
                  step="review"
                  isOpen={helpOpen}
                  onOpen={() => setHelpOpen(true)}
                  onClose={() => setHelpOpen(false)}
                >
                  <ReviewStep
                    type={state.type}
                    name={state.name}
                    description={state.description}
                    content={state.content}
                    error={error}
                  />
                </WizardHelpDrawer>
              )}
            </WizardStep>
          </Wizard>
      </PageSection>

      {/* Switching type throws away the configured content — confirm first. */}
      <ConfirmModal
        isOpen={pendingType !== null}
        title="Change policy type?"
        isDanger
        confirmLabel="Change type and clear settings"
        onConfirm={() => {
          if (pendingType) dispatch({ kind: "selectType", type: pendingType });
          setPendingType(null);
        }}
        onCancel={() => setPendingType(null)}
      >
        {/* One element: ConfirmModal's body is a Form, which would lay out each text node as its own row. */}
        <p>
          Switching to <strong>{pendingType ? policyTypeLabel(pendingType) : ""}</strong> clears the settings you have
          configured for <strong>{state.type ? policyTypeLabel(state.type) : ""}</strong>. This cannot be undone.
        </p>
      </ConfirmModal>

      {/* Cancel with work in progress. */}
      <ConfirmModal
        isOpen={showDiscard}
        title="Discard this policy?"
        isDanger
        confirmLabel="Discard"
        onConfirm={() => {
          setShowDiscard(false);
          leave();
        }}
        onCancel={() => setShowDiscard(false)}
      >
        Nothing has been saved yet. If you leave now, the type, name and settings you entered are lost.
      </ConfirmModal>
    </>
  );
};
