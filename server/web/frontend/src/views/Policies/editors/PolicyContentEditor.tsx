// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useState } from "react";
import { TextArea } from "@patternfly/react-core";

import { FIREFOX_ALL_POLICIES } from "../../../generated/proto/firefox_ui";
import { THUNDERBIRD_ALL_POLICIES } from "../../../generated/proto/thunderbird_ui";
import { CHROME_ALL_POLICIES } from "../../../generated/proto/chrome_ui";
import { EDGE_ALL_POLICIES } from "../../../generated/proto/edge_ui";
import { LiveAlert } from "../../../components/LiveAlert";
import { DConfPolicyEditor } from "../DConfPolicyEditor";
import { PackagePolicyEditor } from "../PackagePolicyEditor";
import { PolkitPolicyEditor } from "../PolkitPolicyEditor";
import { FirewalldPolicyEditor } from "../FirewalldPolicyEditor";
import { SessionAccessPolicyEditor } from "../SessionAccessPolicyEditor";
import { FlatpakPolicyEditor } from "../FlatpakPolicyEditor";
import { KconfigPolicyEditor } from "./KconfigPolicyEditor";
import { TreePolicyEditor } from "./TreePolicyEditor";
import type { PolicyContentEditorProps } from "./PolicyContentEditorProps";

export type { PolicyContentEditorProps } from "./PolicyContentEditorProps";

interface Props extends PolicyContentEditorProps {
  /** `Policy.type` wire value. */
  type: string;
}

/** Raw JSON fallback for a type the UI has no editor for (server-side forward compatibility). */
const RawJsonContentEditor: React.FC<PolicyContentEditorProps> = ({ contentRaw, onChange, onValidityChange }) => {
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    onValidityChange?.(error === null);
  }, [error, onValidityChange]);
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);
  return (
    <div>
      <TextArea
        id="raw-json-editor"
        value={contentRaw}
        onChange={(_ev, val) => {
          onChange(val);
          try {
            JSON.parse(val);
            setError(null);
          } catch {
            setError("Invalid JSON format");
          }
        }}
        rows={16}
        style={{ fontFamily: "monospace", fontSize: "0.85rem" }}
        aria-label="Policy content JSON editor"
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? "raw-json-editor-error" : undefined}
      />
      <LiveAlert id="raw-json-editor-error" message={error} variant="danger" isInline />
    </div>
  );
};

/**
 * The one entry point for editing a policy's content: picks the editor for
 * `type` and hands it the shared `{contentRaw, onChange, isDisabled,
 * onValidityChange}` contract. Used by the edit page's Configuration tab and
 * by the create wizard's Configuration step.
 *
 * Keyed by type so switching types always mounts a fresh editor (the four
 * browser types share one component and must not carry selection state over).
 */
export const PolicyContentEditor: React.FC<Props> = ({ type, ...rest }) => {
  const editor = (() => {
    switch (type) {
      case "Firefox":
        return (
          <TreePolicyEditor
            fields={FIREFOX_ALL_POLICIES}
            productLabel="Firefox"
            idPrefix="ff"
            ariaLabel="Firefox policy settings"
            {...rest}
          />
        );
      case "Thunderbird":
        return (
          <TreePolicyEditor
            fields={THUNDERBIRD_ALL_POLICIES}
            productLabel="Thunderbird"
            idPrefix="tb"
            ariaLabel="Thunderbird policy settings"
            {...rest}
          />
        );
      case "Chrome":
        return (
          <TreePolicyEditor
            fields={CHROME_ALL_POLICIES}
            productLabel="Chrome"
            idPrefix="cr"
            ariaLabel="Chrome policy settings"
            {...rest}
          />
        );
      case "Edge":
        return (
          <TreePolicyEditor
            fields={EDGE_ALL_POLICIES}
            productLabel="Microsoft Edge"
            idPrefix="ed"
            ariaLabel="Edge policy settings"
            {...rest}
          />
        );
      case "Kconfig":
        return <KconfigPolicyEditor {...rest} />;
      case "Dconf":
        return <DConfPolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
      case "Polkit":
        return <PolkitPolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
      case "Package":
        return <PackagePolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
      case "Firewalld":
        return <FirewalldPolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
      case "SessionAccess":
        return <SessionAccessPolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
      case "Flatpak":
        return <FlatpakPolicyEditor contentRaw={rest.contentRaw} onChange={rest.onChange} isDisabled={rest.isDisabled} />;
      default:
        return <RawJsonContentEditor {...rest} />;
    }
  })();

  return (
    <div key={type} style={{ padding: "1rem 0" }}>
      {editor}
    </div>
  );
};
