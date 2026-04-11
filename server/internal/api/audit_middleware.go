// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/VuteTech/Bor/server/internal/services"
)

// sensitiveKeys contains lowercase substrings that identify sensitive fields.
// Any JSON body field whose lowercased key contains one of these strings will
// be replaced with "[REDACTED]" before storing in the audit log.
var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "credential",
	"passphrase", "private_key", "privatekey", "api_key", "apikey",
	"jwt", "auth_token", "access_token", "refresh_token",
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// redactBody replaces sensitive values in a decoded JSON map with "[REDACTED]".
// Operates recursively on nested objects.
func redactBody(m map[string]interface{}) {
	for k, v := range m {
		if isSensitiveKey(k) {
			m[k] = "[REDACTED]"
			continue
		}
		if nested, ok := v.(map[string]interface{}); ok {
			redactBody(nested)
		}
	}
}

// captureDetails reads the request body (restoring it for the actual handler),
// parses it as JSON, redacts sensitive fields, and returns the result as a
// JSON string suitable for storing in the audit log Details field.
// Returns "" when there is no body or the body is not JSON.
func captureDetails(r *http.Request) string {
	if r.Body == nil || r.ContentLength == 0 {
		return ""
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 512*1024)) // 512 KB cap
	if err != nil {
		return ""
	}
	// Restore body so the actual handler can read it.
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var m map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &m); err != nil {
		// Body is not JSON (e.g. form data) — store nothing.
		return ""
	}

	redactBody(m)

	out, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(out)
}

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

			// Read and redact the request body before passing to the handler.
			details := captureDetails(r)

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
				Details:      details,
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
func parseResourceFromPath(path string) (resource, action string) {
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
