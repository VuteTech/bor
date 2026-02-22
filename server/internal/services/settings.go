// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"context"
	"fmt"

	"github.com/VuteTech/Bor/server/internal/database"
	"github.com/VuteTech/Bor/server/internal/models"
)

// SettingsService provides settings management functionality
type SettingsService struct {
	repo *database.SettingsRepository
}

// NewSettingsService creates a new SettingsService
func NewSettingsService(repo *database.SettingsRepository) *SettingsService {
	return &SettingsService{repo: repo}
}

// GetAgentNotificationSettings retrieves the current agent notification settings
func (s *SettingsService) GetAgentNotificationSettings(ctx context.Context) (*models.AgentNotificationSettings, error) {
	return s.repo.GetAgentNotificationSettings(ctx)
}

// UpdateAgentNotificationSettings validates and updates agent notification settings
func (s *SettingsService) UpdateAgentNotificationSettings(ctx context.Context, settings *models.AgentNotificationSettings) error {
	if settings.NotifyCooldown < 60 {
		return fmt.Errorf("notify_cooldown must be at least 60 seconds")
	}
	if settings.NotifyMessage == "" {
		return fmt.Errorf("notify_message must not be empty")
	}
	if settings.NotifyMessageFirefox == "" {
		return fmt.Errorf("notify_message_firefox must not be empty")
	}
	if settings.NotifyMessageChrome == "" {
		return fmt.Errorf("notify_message_chrome must not be empty")
	}

	return s.repo.UpdateAgentNotificationSettings(ctx, settings)
}
