// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useEffect, useState } from "react";
import {
  Button,
  Checkbox,
  Form,
  FormGroup,
  FormHelperText,
  FormSection,
  HelperText,
  HelperTextItem,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  FormSelect,
  FormSelectOption,
  Switch,
  TextArea,
  TextInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { fetchNodeGroups, NodeGroup } from "../../apiClient/nodeGroupsApi";
import {
  createSchedule,
  ReportSchedule,
  ScheduleCadence,
  ScheduleInput,
  SchedulesResponse,
  updateSchedule,
} from "../../apiClient/reportsApi";
import { ScopeFields } from "./ScopeFields";

interface ScheduleModalProps {
  isOpen: boolean;
  /** The schedule to edit, or null to create one. */
  schedule: ReportSchedule | null;
  delivery: SchedulesResponse["delivery"];
  canRefresh: boolean;
  onClose: () => void;
  onSaved: (s: ReportSchedule) => void;
}

const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const FORMAT_LABELS: Record<string, string> = {
  pdf: "PDF document",
  html: "HTML document",
  json: "JSON evidence bundle",
  csv: "CSV (results)",
  jws: "Signature (JWS)",
};

function localZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function toTime(minutes: number): string {
  return `${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
}

function fromTime(v: string): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec(v.trim());
  if (!m) return null;
  const h = Number(m[1]);
  const min = Number(m[2]);
  if (h > 23 || min > 59) return null;
  return h * 60 + min;
}

export const ScheduleModal: React.FC<ScheduleModalProps> = ({ isOpen, schedule, delivery, canRefresh, onClose, onSaved }) => {
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [name, setName] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [allNodes, setAllNodes] = useState(true);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [policyTypes, setPolicyTypes] = useState<string[]>([]);
  const [cadence, setCadence] = useState<ScheduleCadence>("weekly");
  const [weekday, setWeekday] = useState(1);
  const [monthDay, setMonthDay] = useState(1);
  const [time, setTime] = useState("06:00");
  const [timeZone, setTimeZone] = useState(localZone());
  const [labelTemplate, setLabelTemplate] = useState("{name} {date}");
  const [notes, setNotes] = useState("");
  const [includeItems, setIncludeItems] = useState(true);
  const [refresh, setRefresh] = useState(false);
  const [webhookURL, setWebhookURL] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [clearSecret, setClearSecret] = useState(false);
  const [emailTo, setEmailTo] = useState("");
  const [attach, setAttach] = useState<string[]>(["pdf"]);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!isOpen) return;
    setError(null);
    fetchNodeGroups().then(setGroups).catch(() => setGroups([]));
    const s = schedule;
    setName(s?.name ?? "");
    setEnabled(s?.enabled ?? true);
    setAllNodes(s?.all_nodes ?? true);
    setGroupIds(s?.node_group_ids ?? []);
    setPolicyTypes(s?.policy_types ?? []);
    setCadence(s?.cadence ?? "weekly");
    setWeekday(s?.weekday ?? 1);
    setMonthDay(s?.month_day ?? 1);
    setTime(toTime(s?.at_minute ?? 360));
    setTimeZone(s?.time_zone ?? localZone());
    setLabelTemplate(s?.label_template ?? "{name} {date}");
    setNotes(s?.notes ?? "");
    setIncludeItems(s?.include_items ?? true);
    setRefresh(s?.refresh ?? false);
    setWebhookURL(s?.webhook_url ?? "");
    setWebhookSecret("");
    setClearSecret(false);
    setEmailTo((s?.email_to ?? []).join(", "));
    setAttach(s?.attach_formats ?? ["pdf"]);
  }, [isOpen, schedule]);

  const atMinute = fromTime(time);
  const nameInvalid = name.trim() === "";
  const scopeInvalid = !allNodes && groupIds.length === 0;
  const timeInvalid = atMinute === null;
  const recipients = emailTo.split(/[,;\s]+/).map((e) => e.trim()).filter(Boolean);

  const save = async () => {
    if (nameInvalid || scopeInvalid || timeInvalid) {
      setError(nameInvalid ? "The name is required." : scopeInvalid ? "Select at least one node group, or cover all nodes." : "Enter the time as HH:MM.");
      return;
    }
    const body: ScheduleInput = {
      name: name.trim(),
      enabled,
      all_nodes: allNodes,
      node_group_ids: allNodes ? [] : groupIds,
      policy_types: policyTypes,
      include_items: includeItems,
      refresh: canRefresh && refresh,
      cadence,
      at_minute: atMinute as number,
      weekday: cadence === "weekly" ? weekday : undefined,
      month_day: cadence === "monthly" ? monthDay : undefined,
      time_zone: timeZone.trim() || "UTC",
      label_template: labelTemplate,
      notes,
      webhook_url: webhookURL.trim(),
      webhook_secret: webhookSecret || undefined,
      clear_webhook_secret: clearSecret || undefined,
      email_to: recipients,
      attach_formats: attach,
    };
    setSaving(true);
    setError(null);
    try {
      const saved = schedule ? await updateSchedule(schedule.id, body) : await createSchedule(body);
      onSaved(saved);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save the schedule");
    } finally {
      setSaving(false);
    }
  };

  const toggleAttach = (f: string, on: boolean) => setAttach((prev) => (on ? [...prev, f] : prev.filter((x) => x !== f)));

  return (
    <Modal variant={ModalVariant.medium} isOpen={isOpen} onClose={() => !saving && onClose()} aria-label={schedule ? "Edit report schedule" : "New report schedule"}>
      <ModalHeader title={schedule ? `Edit schedule: ${schedule.name}` : "New report schedule"} description="Generates a compliance report on a fixed cadence and delivers it. Scheduled reports are signed and kept like any other report." />
      <ModalBody>
        <LiveAlert id="err-schedule-form" message={error} variant="danger" isInline style={{ marginBottom: "1rem" }} />
        <Form onSubmit={(e) => { e.preventDefault(); void save(); }}>
          <FormGroup label="Name" isRequired fieldId="schedule-name">
            <TextInput
              id="schedule-name"
              value={name}
              onChange={(_e, v) => setName(v)}
              maxLength={255}
              aria-invalid={error && nameInvalid ? true : undefined}
              aria-describedby={error && nameInvalid ? "err-schedule-form" : undefined}
            />
          </FormGroup>
          <FormGroup fieldId="schedule-enabled">
            <Switch id="schedule-enabled" label="Enabled" isChecked={enabled} onChange={(_e, v) => setEnabled(v)} />
          </FormGroup>
          <FormSection title="Scope" titleElement="h3">
            <ScopeFields
              idPrefix="schedule"
              groups={groups}
              allNodes={allNodes}
              onAllNodesChange={setAllNodes}
              groupIds={groupIds}
              onGroupIdsChange={setGroupIds}
              policyTypes={policyTypes}
              onPolicyTypesChange={setPolicyTypes}
              errorId="err-schedule-form"
              groupsInvalid={!!error && scopeInvalid}
            />
            <Checkbox id="schedule-items" label="Include per-setting items" isChecked={includeItems} onChange={(_e, v) => setIncludeItems(v)} />
            {canRefresh && (
              <Checkbox
                id="schedule-refresh"
                label="Collect fresh evidence first"
                description="Ask the online agents in scope to re-check before each report."
                isChecked={refresh}
                onChange={(_e, v) => setRefresh(v)}
              />
            )}
          </FormSection>
          <FormSection title="When" titleElement="h3">
            <FormGroup label="Cadence" fieldId="schedule-cadence">
              <FormSelect id="schedule-cadence" value={cadence} onChange={(_e, v) => setCadence(v as ScheduleCadence)}>
                <FormSelectOption value="daily" label="Daily" />
                <FormSelectOption value="weekly" label="Weekly" />
                <FormSelectOption value="monthly" label="Monthly" />
              </FormSelect>
            </FormGroup>
            {cadence === "weekly" && (
              <FormGroup label="Day of the week" fieldId="schedule-weekday">
                <FormSelect id="schedule-weekday" value={String(weekday)} onChange={(_e, v) => setWeekday(Number(v))}>
                  {WEEKDAYS.map((d, i) => <FormSelectOption key={d} value={String(i)} label={d} />)}
                </FormSelect>
              </FormGroup>
            )}
            {cadence === "monthly" && (
              <FormGroup label="Day of the month" fieldId="schedule-monthday">
                <FormSelect id="schedule-monthday" value={String(monthDay)} onChange={(_e, v) => setMonthDay(Number(v))}>
                  {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => <FormSelectOption key={d} value={String(d)} label={String(d)} />)}
                  <FormSelectOption value="0" label="Last day of the month" />
                </FormSelect>
              </FormGroup>
            )}
            <FormGroup label="Time of day" fieldId="schedule-time">
              <TextInput
                id="schedule-time"
                type="time"
                value={time}
                onChange={(_e, v) => setTime(v)}
                aria-invalid={timeInvalid ? true : undefined}
                aria-describedby={timeInvalid ? "err-schedule-form" : undefined}
              />
            </FormGroup>
            <FormGroup label="Time zone" fieldId="schedule-tz">
              <TextInput id="schedule-tz" value={timeZone} onChange={(_e, v) => setTimeZone(v)} />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>An IANA time zone such as Europe/Sofia. Runs follow daylight saving time in this zone.</HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
          </FormSection>
          <FormSection title="Report" titleElement="h3">
            <FormGroup label="Label" fieldId="schedule-label">
              <TextInput id="schedule-label" value={labelTemplate} onChange={(_e, v) => setLabelTemplate(v)} maxLength={255} />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>{"{name}"} is the schedule's name, {"{date}"} and {"{datetime}"} the run's local date and time.</HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            <FormGroup label="Notes" fieldId="schedule-notes">
              <TextArea id="schedule-notes" value={notes} onChange={(_e, v) => setNotes(v)} rows={2} />
            </FormGroup>
          </FormSection>
          <FormSection title="Delivery" titleElement="h3">
            <FormGroup label="Webhook URL" fieldId="schedule-webhook">
              <TextInput id="schedule-webhook" type="url" value={webhookURL} onChange={(_e, v) => setWebhookURL(v)} placeholder="https://hooks.example.com/bor" />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Receives a JSON summary with links after every run{delivery.http_webhooks ? "" : " (https only)"}.
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
            </FormGroup>
            <FormGroup label="Webhook signing secret" fieldId="schedule-secret">
              <TextInput
                id="schedule-secret"
                type="password"
                autoComplete="new-password"
                value={webhookSecret}
                onChange={(_e, v) => setWebhookSecret(v)}
                placeholder={schedule?.webhook_secret_set ? "Set; leave empty to keep it" : "Optional"}
                isDisabled={clearSecret}
              />
              <FormHelperText>
                <HelperText>
                  <HelperTextItem>
                    Requests carry X-Bor-Signature: sha256=HMAC of "&lt;X-Bor-Timestamp&gt;.&lt;body&gt;".
                  </HelperTextItem>
                </HelperText>
              </FormHelperText>
              {schedule?.webhook_secret_set && (
                <Checkbox id="schedule-clear-secret" label="Remove the signing secret" isChecked={clearSecret} onChange={(_e, v) => setClearSecret(v)} />
              )}
            </FormGroup>
            <FormGroup label="Email recipients" fieldId="schedule-email">
              <TextArea
                id="schedule-email"
                value={emailTo}
                onChange={(_e, v) => setEmailTo(v)}
                rows={2}
                isDisabled={!delivery.email}
                placeholder={delivery.email ? "ciso@example.com, auditor@example.com" : "Email delivery is not configured on this server"}
              />
            </FormGroup>
            <FormGroup label="Attachments" fieldId="schedule-attach-pdf" role="group">
              {delivery.formats.map((f) => (
                <Checkbox
                  key={f}
                  id={`schedule-attach-${f}`}
                  label={FORMAT_LABELS[f] ?? f}
                  isChecked={attach.includes(f)}
                  isDisabled={!delivery.email}
                  onChange={(_e, v) => toggleAttach(f, v)}
                />
              ))}
            </FormGroup>
          </FormSection>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button variant="primary" onClick={() => void save()} isLoading={saving} isDisabled={saving}>
          {schedule ? "Save" : "Create schedule"}
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={saving}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};
