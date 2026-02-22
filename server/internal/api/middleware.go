// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/services"
)

type contextKey string

const userContextKey contextKey = "user"

// AuthMiddleware validates JWT tokens on protected routes
func AuthMiddleware(authSvc *services.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"authorization header required"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, `{"error":"invalid authorization header format"}`, http.StatusUnauthorized)
				return
			}

			claims, err := authSvc.ValidateToken(parts[1])
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequirePermission checks that the authenticated user has a specific permission
// via the Authorizer. It replaces hardcoded role checks like AdminOnly.
func RequirePermission(az authz.Authorizer, resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			scopeType := "global"
			allowed, err := az.HasPermission(r.Context(), claims.UserID, resource, action, scopeType, nil)
			if err != nil {
				http.Error(w, `{"error":"authorization check failed"}`, http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// MethodPermission maps an HTTP method to a resource:action pair
type MethodPermission struct {
	Method   string
	Resource string
	Action   string
}

// RequireMethodPermission checks permissions based on the HTTP method.
// If no matching method is found, the request is denied with 405 Method Not Allowed.
func RequireMethodPermission(az authz.Authorizer, perms []MethodPermission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			var resource, action string
			found := false
			for _, p := range perms {
				if p.Method == r.Method {
					resource = p.Resource
					action = p.Action
					found = true
					break
				}
			}
			if !found {
				http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
				return
			}

			scopeType := "global"
			allowed, err := az.HasPermission(r.Context(), claims.UserID, resource, action, scopeType, nil)
			if err != nil {
				http.Error(w, `{"error":"authorization check failed"}`, http.StatusInternalServerError)
				return
			}
			if !allowed {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AdminOnly restricts access to users with the "user:manage" permission.
// This is a backward-compatible wrapper around RequirePermission.
func AdminOnly(az authz.Authorizer) func(http.Handler) http.Handler {
	return RequirePermission(az, "user", "manage")
}

// GetUserFromContext retrieves user claims from context
func GetUserFromContext(ctx context.Context) *services.Claims {
	claims, ok := ctx.Value(userContextKey).(*services.Claims)
	if !ok {
		return nil
	}
	return claims
}
