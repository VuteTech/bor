// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"encoding/json"
	"net/http"
)

// VersionResponse is the payload for GET /api/v1/version.
type VersionResponse struct {
	Version string `json:"version"`
}

// NewVersionHandler returns an http.HandlerFunc that serves the server version.
// The response JSON is pre-encoded at startup so the handler allocates nothing
// per request. No authentication is required — the version string is not sensitive.
func NewVersionHandler(version string) http.HandlerFunc {
	payload, _ := json.Marshal(VersionResponse{Version: version})
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}
}
