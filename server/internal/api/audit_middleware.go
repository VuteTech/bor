// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// AuditMiddleware logs state-changing HTTP requests (POST, PUT, PATCH, DELETE) as audit events
func AuditMiddleware(auditSvc *services.AuditService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit state-changing methods
			if r.Method != http.MethodPost && r.Method != http.MethodPut &&
				r.Method != http.MethodPatch && r.Method != http.MethodDelete {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response status
			recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(recorder, r)

			// Only log successful state-changing requests (2xx status codes)
			if recorder.statusCode < 200 || recorder.statusCode >= 300 {
				return
			}

			// Extract user info from context
			claims := GetUserFromContext(r.Context())
			username := ""
			var userID *string
			if claims != nil {
				username = claims.Username
				uid := claims.UserID
				userID = &uid
			}

			// Determine action from HTTP method
			action := methodToAction(r.Method)

			// Determine resource type from path
			resourceType, resourceID := parseResourceFromPath(r.URL.Path)

			entry := &models.AuditLog{
				UserID:       userID,
				Username:     username,
				Action:       action,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				Details:      r.Method + " " + r.URL.Path,
				IPAddress:    extractIP(r),
			}

			auditSvc.LogEvent(r.Context(), entry)
		})
	}
}

// statusRecorder wraps http.ResponseWriter to capture the status code
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// methodToAction maps HTTP methods to audit action names
func methodToAction(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return method
	}
}

// parseResourceFromPath extracts resource type and ID from API paths
func parseResourceFromPath(path string) (string, string) {
	// Remove /api/v1/ prefix
	trimmed := strings.TrimPrefix(path, "/api/v1/")
	trimmed = strings.TrimSuffix(trimmed, "/")

	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return "unknown", ""
	}

	resourceType := parts[0]
	resourceID := ""
	if len(parts) > 1 {
		resourceID = parts[1]
	}

	return resourceType, resourceID
}

// extractIP extracts the client IP address from the request
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For first (behind proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	addr := r.RemoteAddr
	// Remove port if present
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
