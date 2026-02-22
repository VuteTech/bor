// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Alert,
  Button,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  NumberInput,
  Switch,
  TextArea,
  Spinner,
  ActionGroup,
} from "@patternfly/react-core";
import {
  fetchAgentNotificationSettings,
  updateAgentNotificationSettings,
  AgentNotificationSettings,
} from "../../apiClient/settingsApi";

export const AgentNotificationsTab: React.FC = () => {
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const [notifyUsers, setNotifyUsers] = useState(false);
  const [notifyCooldown, setNotifyCooldown] = useState(300);
  const [notifyMessage, setNotifyMessage] = useState("");
  const [notifyMessageFirefox, setNotifyMessageFirefox] = useState("");
  const [notifyMessageChrome, setNotifyMessageChrome] = useState("");

  const load = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchAgentNotificationSettings()
      .then((s) => {
        setNotifyUsers(s.notify_users);
        setNotifyCooldown(s.notify_cooldown);
        setNotifyMessage(s.notify_message);
        setNotifyMessageFirefox(s.notify_message_firefox ?? "");
        setNotifyMessageChrome(s.notify_message_chrome ?? "");
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      const updated = await updateAgentNotificationSettings({
        notify_users: notifyUsers,
        notify_cooldown: notifyCooldown,
        notify_message: notifyMessage,
        notify_message_firefox: notifyMessageFirefox,
        notify_message_chrome: notifyMessageChrome,
      });
      setNotifyUsers(updated.notify_users);
      setNotifyCooldown(updated.notify_cooldown);
      setNotifyMessage(updated.notify_message);
      setNotifyMessageFirefox(updated.notify_message_firefox);
      setNotifyMessageChrome(updated.notify_message_chrome ?? "");
      setSuccess("Agent notification settings saved successfully.");
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to save settings");
    } finally {
      setSaving(false);
    }
  }, [notifyUsers, notifyCooldown, notifyMessage, notifyMessageFirefox, notifyMessageChrome]);

  if (loading) return <Spinner size="lg" />;

  const MIN_COOLDOWN = 60;

  const onMinus = () => {
    setNotifyCooldown((prev) => Math.max(MIN_COOLDOWN, prev - 1));
  };
  const onPlus = () => {
    setNotifyCooldown((prev) => prev + 1);
  };
  const onCooldownChange = (event: React.FormEvent<HTMLInputElement>) => {
    const val = Number((event.target as HTMLInputElement).value);
    if (!isNaN(val) && val >= MIN_COOLDOWN) {
      setNotifyCooldown(val);
    }
  };

  return (
    <>
      {error && (
        <Alert
          variant="danger"
          title={error}
          isInline
          actionClose={
            <Button variant="plain" onClick={() => setError(null)}>
              &times;
            </Button>
          }
          style={{ marginBottom: 16 }}
        />
      )}
      {success && (
        <Alert
          variant="success"
          title={success}
          isInline
          actionClose={
            <Button variant="plain" onClick={() => setSuccess(null)}>
              &times;
            </Button>
          }
          style={{ marginBottom: 16 }}
        />
      )}

      <Form style={{ maxWidth: 600 }}>
        <FormGroup label="Enable desktop notifications" fieldId="an-notify-users">
          <Switch
            id="an-notify-users"
            isChecked={notifyUsers}
            onChange={(_ev, v) => setNotifyUsers(v)}
          />
        </FormGroup>

        <FormGroup label="Notification cooldown (seconds)" fieldId="an-cooldown">
          <NumberInput
            id="an-cooldown"
            value={notifyCooldown}
            min={MIN_COOLDOWN}
            onMinus={onMinus}
            onPlus={onPlus}
            onChange={onCooldownChange}
            inputName="an-cooldown"
            inputAriaLabel="Notification cooldown in seconds"
            minusBtnAriaLabel="Decrease cooldown"
            plusBtnAriaLabel="Increase cooldown"
          />
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                Minimum time between repeated notifications to the same user session (in seconds)
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>

        <FormGroup label="Notification message" fieldId="an-message">
          <TextArea
            id="an-message"
            value={notifyMessage}
            onChange={(_ev, v) => setNotifyMessage(v)}
            rows={4}
          />
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                Message shown to desktop users when policies are updated
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>

        <FormGroup label="Firefox policy notification message" fieldId="an-message-firefox">
          <TextArea
            id="an-message-firefox"
            value={notifyMessageFirefox}
            onChange={(_ev, v) => setNotifyMessageFirefox(v)}
            rows={3}
            resizeOrientation="vertical"
          />
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                Message shown to users when Firefox browser policies are updated
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>

        <FormGroup label="Chrome/Chromium policy notification message" fieldId="an-message-chrome">
          <TextArea
            id="an-message-chrome"
            value={notifyMessageChrome}
            onChange={(_ev, v) => setNotifyMessageChrome(v)}
            rows={3}
            resizeOrientation="vertical"
          />
          <FormHelperText>
            <HelperText>
              <HelperTextItem>
                Message shown to users when Chrome/Chromium browser policies are updated
              </HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>

        <ActionGroup>
          <Button
            variant="primary"
            onClick={handleSave}
            isDisabled={saving}
            isLoading={saving}
          >
            Save
          </Button>
        </ActionGroup>
      </Form>
    </>
  );
};
