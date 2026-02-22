// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"fmt"
	"strconv"

	"github.com/VuteTech/Bor/server/internal/models"
)

// SettingsRepository handles agent settings database operations
type SettingsRepository struct {
	db *DB
}

// NewSettingsRepository creates a new SettingsRepository
func NewSettingsRepository(db *DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

// GetAgentNotificationSettings retrieves the current agent notification settings
func (r *SettingsRepository) GetAgentNotificationSettings(ctx context.Context) (*models.AgentNotificationSettings, error) {
	query := `SELECT key, value FROM agent_settings WHERE key IN ('notify_users', 'notify_cooldown', 'notify_message', 'notify_message_firefox', 'notify_message_chrome')`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent notification settings: %w", err)
	}
	defer rows.Close()

	settings := &models.AgentNotificationSettings{
		NotifyUsers:          true,
		NotifyCooldown:       300,
		NotifyMessage:        "Desktop policies have been updated. Please log out and log back in for all changes to take effect.",
		NotifyMessageFirefox: "Firefox policies have been updated. Please restart Firefox for all changes to take effect.",
		NotifyMessageChrome:  "Chrome/Chromium policies have been updated. Please restart your browser for all changes to take effect.",
	}

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("failed to scan agent setting: %w", err)
		}

		switch key {
		case "notify_users":
			settings.NotifyUsers = value == "true"
		case "notify_cooldown":
			if v, err := strconv.Atoi(value); err == nil {
				settings.NotifyCooldown = v
			}
		case "notify_message":
			settings.NotifyMessage = value
		case "notify_message_firefox":
			settings.NotifyMessageFirefox = value
		case "notify_message_chrome":
			settings.NotifyMessageChrome = value
		}
	}

	return settings, rows.Err()
}

// UpdateAgentNotificationSettings upserts the agent notification settings
func (r *SettingsRepository) UpdateAgentNotificationSettings(ctx context.Context, settings *models.AgentNotificationSettings) error {
	query := `INSERT INTO agent_settings (key, value, updated_at) VALUES ($1, $2, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`

	pairs := []struct {
		key   string
		value string
	}{
		{"notify_users", strconv.FormatBool(settings.NotifyUsers)},
		{"notify_cooldown", strconv.Itoa(settings.NotifyCooldown)},
		{"notify_message", settings.NotifyMessage},
		{"notify_message_firefox", settings.NotifyMessageFirefox},
		{"notify_message_chrome", settings.NotifyMessageChrome},
	}

	for _, p := range pairs {
		if _, err := r.db.ExecContext(ctx, query, p.key, p.value); err != nil {
			return fmt.Errorf("failed to update agent setting %s: %w", p.key, err)
		}
	}

	return nil
}
