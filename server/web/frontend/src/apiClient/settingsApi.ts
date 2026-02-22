// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

const TOKEN_STORAGE_KEY = "bor_token";

function authHeaders(): Record<string, string> {
  const tk = localStorage.getItem(TOKEN_STORAGE_KEY);
  const hdrs: Record<string, string> = { "Content-Type": "application/json" };
  if (tk) hdrs["Authorization"] = `Bearer ${tk}`;
  return hdrs;
}

async function apiRequest<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const b = await res.json();
      if (b.error) detail = b.error;
    } catch {
      /* swallow */
    }
    throw new Error(detail);
  }
  return res.json();
}

export interface AgentNotificationSettings {
  notify_users: boolean;
  notify_cooldown: number;
  notify_message: string;
  notify_message_firefox: string;
  notify_message_chrome: string;
}

export async function fetchAgentNotificationSettings(): Promise<AgentNotificationSettings> {
  return apiRequest<AgentNotificationSettings>("/api/v1/settings/agent-notifications", {
    headers: authHeaders(),
  });
}

export async function updateAgentNotificationSettings(
  settings: AgentNotificationSettings
): Promise<AgentNotificationSettings> {
  return apiRequest<AgentNotificationSettings>("/api/v1/settings/agent-notifications", {
    method: "PUT",
    headers: authHeaders(),
    body: JSON.stringify(settings),
  });
}
