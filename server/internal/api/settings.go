// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// SettingsHandler handles settings API endpoints
type SettingsHandler struct {
	settingsSvc *services.SettingsService
}

// NewSettingsHandler creates a new SettingsHandler
func NewSettingsHandler(settingsSvc *services.SettingsService) *SettingsHandler {
	return &SettingsHandler{settingsSvc: settingsSvc}
}

// AgentNotifications handles GET/PUT /api/v1/settings/agent-notifications
func (h *SettingsHandler) AgentNotifications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getAgentNotifications(w, r)
	case http.MethodPut:
		h.updateAgentNotifications(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *SettingsHandler) getAgentNotifications(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsSvc.GetAgentNotificationSettings(r.Context())
	if err != nil {
		log.Printf("Failed to get agent notification settings: %v", err)
		http.Error(w, `{"error":"failed to get agent notification settings"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(settings); err != nil {
		log.Printf("Failed to encode agent notification settings: %v", err)
	}
}

func (h *SettingsHandler) updateAgentNotifications(w http.ResponseWriter, r *http.Request) {
	var settings models.AgentNotificationSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if err := h.settingsSvc.UpdateAgentNotificationSettings(r.Context(), &settings); err != nil {
		log.Printf("Failed to update agent notification settings: %v", err)
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	// Return the updated settings
	updated, err := h.settingsSvc.GetAgentNotificationSettings(r.Context())
	if err != nil {
		log.Printf("Failed to get updated agent notification settings: %v", err)
		http.Error(w, `{"error":"failed to get updated settings"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updated); err != nil {
		log.Printf("Failed to encode updated agent notification settings: %v", err)
	}
}
