// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ExtensionPanelTab renders a settings panel served by a server extension
 * (pkg/ext Panel JSON): per section a status, read-only fields, tables and
 * actions that call the extension's admin routes, optionally after asking
 * for a value (typed or loaded from a file). It contains no
 * extension-specific code.
 */

import React, { useCallback, useEffect, useState } from "react";
import {
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  ClipboardCopy,
  Content,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  FileUpload,
  Flex,
  FlexItem,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  Label,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  Spinner,
  TextInput,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import CheckCircleIcon from "@patternfly/react-icons/dist/esm/icons/check-circle-icon";
import ExclamationTriangleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-triangle-icon";
import ExclamationCircleIcon from "@patternfly/react-icons/dist/esm/icons/exclamation-circle-icon";
import BanIcon from "@patternfly/react-icons/dist/esm/icons/ban-icon";

import { LiveAlert } from "../../components/LiveAlert";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import {
  fetchPanel,
  Panel,
  PanelAction,
  PanelSection,
  runPanelAction,
} from "../../apiClient/extPanelsApi";

const STATUS: Record<string, { color: "green" | "orange" | "red" | "grey"; icon: React.ReactNode; text: string }> = {
  ok: { color: "green", icon: <CheckCircleIcon />, text: "OK" },
  warning: { color: "orange", icon: <ExclamationTriangleIcon />, text: "Warning" },
  error: { color: "red", icon: <ExclamationCircleIcon />, text: "Error" },
  disabled: { color: "grey", icon: <BanIcon />, text: "Disabled" },
};

export const ExtensionPanelTab: React.FC<{ dataUrl: string }> = ({ dataUrl }) => {
  const [panel, setPanel] = useState<Panel | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState<PanelAction | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [secret, setSecret] = useState<string | null>(null);
  // The action waiting for its input value, and that value.
  const [inputAction, setInputAction] = useState<PanelAction | null>(null);
  const [inputValue, setInputValue] = useState("");
  const [inputFilename, setInputFilename] = useState("");
  const [inputPhrase, setInputPhrase] = useState("");
  const { addToast } = useToast();

  const reload = useCallback(() => {
    fetchPanel(dataUrl)
      .then((p) => {
        setPanel(p);
        setError(null);
      })
      .catch((e: unknown) => setError(e instanceof Error ? e.message : "Failed to load"));
  }, [dataUrl]);

  useEffect(() => {
    reload();
  }, [reload]);

  const run = async (action: PanelAction, value?: string) => {
    setBusy(true);
    setActionError(null);
    try {
      const res = await runPanelAction(action, value);
      setPending(null);
      setInputAction(null);
      if (action.result === "secret" && res.token) {
        setSecret(res.token);
      } else {
        addToast({ variant: "success", title: action.label, detail: res.message });
      }
      reload();
    } catch (e: unknown) {
      setActionError(e instanceof Error ? e.message : "Action failed");
    } finally {
      setBusy(false);
    }
  };

  const start = (action: PanelAction) => {
    setActionError(null);
    if (action.input) {
      setInputValue("");
      setInputFilename("");
      setInputPhrase("");
      setInputAction(action);
    } else if (action.danger || action.confirm) {
      setPending(action);
    } else {
      void run(action);
    }
  };

  const actionButtons = (actions: PanelAction[] | undefined, compact: boolean) =>
    (actions ?? []).map((a) => (
      <Button
        key={a.label + a.url}
        variant={a.danger ? (compact ? "link" : "danger") : compact ? "link" : "secondary"}
        isDanger={a.danger && compact}
        isInline={compact}
        size={compact ? "sm" : undefined}
        onClick={() => start(a)}
        isDisabled={busy}
        style={compact ? { marginRight: 12 } : { marginRight: 8 }}
      >
        {a.label}
      </Button>
    ));

  const section = (s: PanelSection, i: number) => {
    const status = s.status ? STATUS[s.status] : undefined;
    return (
      <Card key={s.title + i} isCompact style={{ marginBottom: 16 }}>
        <CardHeader>
          <CardTitle>
            <Flex alignItems={{ default: "alignItemsCenter" }}>
              <FlexItem>
                <Content component="h3" style={{ margin: 0 }}>
                  {s.title}
                </Content>
              </FlexItem>
              {status && (
                <FlexItem>
                  <Label color={status.color} icon={status.icon} isCompact>
                    {s.status_text || status.text}
                  </Label>
                </FlexItem>
              )}
            </Flex>
            {s.subtitle && (
              <Content component="small" style={{ color: "var(--pf-t--global--text--color--subtle)" }}>
                {s.subtitle}
              </Content>
            )}
          </CardTitle>
        </CardHeader>
        <CardBody>
          {s.fields && s.fields.length > 0 && (
            <DescriptionList isHorizontal isCompact style={{ marginBottom: 16 }}>
              {s.fields.map((f) => (
                <DescriptionListGroup key={f.label}>
                  <DescriptionListTerm>{f.label}</DescriptionListTerm>
                  <DescriptionListDescription>
                    {f.copy ? (
                      <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied" aria-label={`Copy ${f.label}`}>
                        {f.value}
                      </ClipboardCopy>
                    ) : (
                      f.value
                    )}
                  </DescriptionListDescription>
                </DescriptionListGroup>
              ))}
            </DescriptionList>
          )}
          {(s.tables ?? []).map((t) => ({ ...t, rows: t.rows ?? [] })).map((t) => (
            <div key={t.title} style={{ marginBottom: 16 }}>
              <Content component="h4">{t.title}</Content>
              {t.rows.length === 0 ? (
                <Content component="p" style={{ color: "var(--pf-t--global--text--color--subtle)" }}>
                  {t.empty || "None."}
                </Content>
              ) : (
                <Table aria-label={t.title} variant="compact">
                  <Thead>
                    <Tr>
                      {t.columns.map((c) => (
                        <Th key={c}>{c}</Th>
                      ))}
                      {t.rows.some((r) => r.actions?.length) && <Th screenReaderText="Actions" />}
                    </Tr>
                  </Thead>
                  <Tbody>
                    {t.rows.map((r, ri) => (
                      <Tr key={ri}>
                        {r.cells.map((c, ci) => (
                          <Td key={ci} dataLabel={t.columns[ci]}>
                            {c}
                          </Td>
                        ))}
                        {t.rows.some((row) => row.actions?.length) && <Td>{actionButtons(r.actions, true)}</Td>}
                      </Tr>
                    ))}
                  </Tbody>
                </Table>
              )}
            </div>
          ))}
          {s.actions && s.actions.length > 0 && <div>{actionButtons(s.actions, false)}</div>}
        </CardBody>
      </Card>
    );
  };

  const maxBytes = inputAction?.input?.max_bytes ?? 0;
  const inputTooLarge = maxBytes > 0 && new TextEncoder().encode(inputValue).length > maxBytes;
  // A destructive action that also takes a value asks for its confirm
  // phrase in the same dialog.
  const inputPhraseOK =
    !inputAction?.danger || !inputAction.confirm_phrase || inputPhrase === inputAction.confirm_phrase;

  if (error) return <LiveAlert variant="danger" isInline message={error} />;
  if (!panel) return <Spinner size="lg" aria-label="Loading" />;

  return (
    <>
      {panel.description && (
        <Content component="p" style={{ marginBottom: 16 }}>
          {panel.description}
        </Content>
      )}
      {panel.sections.map(section)}

      <ConfirmModal
        isOpen={pending !== null}
        title={pending ? `${pending.label}?` : ""}
        confirmLabel={pending?.label}
        isDanger={pending?.danger}
        confirmPhrase={pending?.danger ? pending.confirm_phrase : undefined}
        confirmPhraseLabel={pending?.danger && pending.confirm_phrase ? `Type ${pending.confirm_phrase} to confirm` : undefined}
        isBusy={busy}
        error={actionError}
        onConfirm={() => pending && void run(pending)}
        onCancel={() => setPending(null)}
      >
        {pending?.confirm}
      </ConfirmModal>

      <Modal variant={ModalVariant.medium} isOpen={secret !== null} onClose={() => setSecret(null)} aria-labelledby="ext-secret-title">
        <ModalHeader title="Copy the token now" labelId="ext-secret-title" />
        <ModalBody>
          <LiveAlert
            variant="warning"
            isInline
            message="This token is shown only once. Store it in the identity provider's provisioning settings."
            style={{ marginBottom: 16 }}
          />
          <ClipboardCopy isReadOnly hoverTip="Copy" clickTip="Copied" aria-label="Copy token">
            {secret ?? ""}
          </ClipboardCopy>
        </ModalBody>
        <ModalFooter>
          <Button variant="primary" onClick={() => setSecret(null)}>
            Done
          </Button>
        </ModalFooter>
      </Modal>
      <Modal
        variant={ModalVariant.medium}
        isOpen={inputAction !== null}
        onClose={() => setInputAction(null)}
        aria-labelledby="ext-input-title"
      >
        <ModalHeader title={inputAction?.label ?? ""} labelId="ext-input-title" />
        <ModalBody>
          {inputAction?.confirm && (
            <Content component="p" style={{ marginBottom: 16 }}>
              {inputAction.confirm}
            </Content>
          )}
          {inputAction?.input && (
            <Form
              id="ext-input-form"
              onSubmit={(e) => {
                e.preventDefault();
                if (inputValue.trim() && !inputTooLarge && inputPhraseOK) void run(inputAction, inputValue);
              }}
            >
              <FormGroup label={inputAction.input.label} fieldId="ext-input-value">
                <FileUpload
                  id="ext-input-value"
                  type="text"
                  value={inputValue}
                  filename={inputFilename}
                  filenamePlaceholder="Drag a file here, browse for one, or paste below"
                  browseButtonText="Browse..."
                  allowEditingUploadedText
                  onFileInputChange={(_e, file) => setInputFilename(file.name)}
                  onDataChange={(_e, data) => {
                    setInputValue(data);
                    setActionError(null);
                  }}
                  onTextChange={(_e, text) => {
                    setInputValue(text);
                    setActionError(null);
                  }}
                  onClearClick={() => {
                    setInputValue("");
                    setInputFilename("");
                    setActionError(null);
                  }}
                  validated={inputTooLarge || actionError ? "error" : "default"}
                  aria-invalid={inputTooLarge || actionError ? true : undefined}
                  aria-describedby={actionError ? "ext-input-error" : undefined}
                />
                {(inputAction.input.help || inputTooLarge) && (
                  <FormHelperText>
                    <HelperText>
                      <HelperTextItem variant={inputTooLarge ? "error" : "default"}>
                        {inputTooLarge ? "The value is too large." : inputAction.input.help}
                      </HelperTextItem>
                    </HelperText>
                  </FormHelperText>
                )}
              </FormGroup>
              {inputAction.danger && inputAction.confirm_phrase && (
                <FormGroup label={`Type ${inputAction.confirm_phrase} to confirm`} fieldId="ext-input-phrase" isRequired>
                  <TextInput
                    id="ext-input-phrase"
                    value={inputPhrase}
                    onChange={(_e, v) => setInputPhrase(v)}
                    aria-label="Confirmation phrase"
                  />
                </FormGroup>
              )}
            </Form>
          )}
          <LiveAlert id="ext-input-error" variant="danger" isInline message={inputAction ? actionError : null} style={{ marginTop: 16 }} />
        </ModalBody>
        <ModalFooter>
          <Button
            variant={inputAction?.danger ? "danger" : "primary"}
            type="submit"
            form="ext-input-form"
            isLoading={busy}
            isDisabled={busy || !inputValue.trim() || inputTooLarge || !inputPhraseOK}
          >
            {inputAction?.label}
          </Button>
          <Button variant="link" onClick={() => setInputAction(null)} isDisabled={busy}>
            Cancel
          </Button>
        </ModalFooter>
      </Modal>
      {!pending && !inputAction && <LiveAlert variant="danger" isInline message={actionError} />}
    </>
  );
};
