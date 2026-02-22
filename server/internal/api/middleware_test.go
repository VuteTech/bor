// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/services"
)

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	middleware := AuthMiddleware(nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthMiddleware_InvalidFormat(t *testing.T) {
	middleware := AuthMiddleware(nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "InvalidFormat")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// mockAuthorizer is a test double for authz.Authorizer
type mockAuthorizer struct {
	result bool
	err    error
}

func (m *mockAuthorizer) HasPermission(_ context.Context, _ string, _ string, _ string, _ string, _ *string) (bool, error) {
	return m.result, m.err
}

// Compile-time check that mockAuthorizer implements authz.Authorizer
var _ authz.Authorizer = (*mockAuthorizer)(nil)

func TestAdminOnly_NoUser(t *testing.T) {
	az := &mockAuthorizer{result: false}
	middleware := AdminOnly(az)
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestExtractIDFromPath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		prefix string
		want   string
	}{
		{
			name:   "valid id",
			path:   "/api/v1/users/abc-123",
			prefix: "/api/v1/users/",
			want:   "abc-123",
		},
		{
			name:   "trailing slash",
			path:   "/api/v1/users/abc-123/",
			prefix: "/api/v1/users/",
			want:   "abc-123",
		},
		{
			name:   "no id",
			path:   "/api/v1/users",
			prefix: "/api/v1/users/",
			want:   "",
		},
		{
			name:   "wrong prefix",
			path:   "/api/v1/other/abc",
			prefix: "/api/v1/users/",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractIDFromPath(tt.path, tt.prefix)
			if got != tt.want {
				t.Errorf("extractIDFromPath(%q, %q) = %q, want %q", tt.path, tt.prefix, got, tt.want)
			}
		})
	}
}

// ── Deny-by-default tests ──

// permCheckingAuthorizer tracks what permission was checked and returns a
// configurable result per resource:action pair.
type permCheckingAuthorizer struct {
	allowed map[string]bool
	calls   []string
}

func (m *permCheckingAuthorizer) HasPermission(_ context.Context, _ string, resource string, action string, _ string, _ *string) (bool, error) {
	key := resource + ":" + action
	m.calls = append(m.calls, key)
	return m.allowed[key], nil
}

// helper to build a request with user claims in context
func reqWithUser(method, url string) *http.Request {
	r := httptest.NewRequest(method, url, nil)
	claims := &services.Claims{UserID: "test-user", Username: "tester"}
	ctx := context.WithValue(r.Context(), userContextKey, claims)
	return r.WithContext(ctx)
}

func TestRequirePermission_DenyNoRoles(t *testing.T) {
	// User with no permissions should get 403
	az := &mockAuthorizer{result: false}
	mw := RequirePermission(az, "policy", "view")

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called when permission is denied")
	}))

	req := reqWithUser(http.MethodGet, "/api/v1/policies")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (403 Forbidden)", rec.Code, http.StatusForbidden)
	}
}

func TestRequirePermission_AllowWithPermission(t *testing.T) {
	az := &mockAuthorizer{result: true}
	mw := RequirePermission(az, "policy", "view")

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := reqWithUser(http.MethodGet, "/api/v1/policies")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called when permission is granted")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireMethodPermission_DenyUserWithNoRoles(t *testing.T) {
	// Authorizer returns false for everything — simulates user with no roles
	az := &permCheckingAuthorizer{allowed: map[string]bool{}}

	perms := []MethodPermission{
		{Method: http.MethodGet, Resource: "policy", Action: "view"},
		{Method: http.MethodPost, Resource: "policy", Action: "create"},
		{Method: http.MethodPut, Resource: "policy", Action: "edit"},
		{Method: http.MethodDelete, Resource: "policy", Action: "delete"},
	}

	mw := RequireMethodPermission(az, perms)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for denied user")
	}))

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	for _, method := range methods {
		rec := httptest.NewRecorder()
		req := reqWithUser(method, "/api/v1/policies/all")
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want %d (403)", method, rec.Code, http.StatusForbidden)
		}
	}
}

func TestRequireMethodPermission_AllowViewDenyCreate(t *testing.T) {
	// Simulates user with only policy:view permission
	az := &permCheckingAuthorizer{
		allowed: map[string]bool{
			"policy:view": true,
		},
	}

	perms := []MethodPermission{
		{Method: http.MethodGet, Resource: "policy", Action: "view"},
		{Method: http.MethodPost, Resource: "policy", Action: "create"},
	}

	mw := RequireMethodPermission(az, perms)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// GET should be allowed
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, reqWithUser(http.MethodGet, "/api/v1/policies/all"))
	if rec.Code != http.StatusOK {
		t.Errorf("GET: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// POST should be denied
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, reqWithUser(http.MethodPost, "/api/v1/policies/all"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST: status = %d, want %d (403)", rec.Code, http.StatusForbidden)
	}
}

func TestRequireMethodPermission_UnmappedMethodDenied(t *testing.T) {
	az := &permCheckingAuthorizer{allowed: map[string]bool{"policy:view": true}}

	perms := []MethodPermission{
		{Method: http.MethodGet, Resource: "policy", Action: "view"},
	}

	mw := RequireMethodPermission(az, perms)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for unmapped method")
	}))

	// PATCH is not mapped — should be denied
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, reqWithUser(http.MethodPatch, "/api/v1/policies/all"))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH: status = %d, want %d (405)", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestRequireMethodPermission_AllEndpoints_DenyByDefault(t *testing.T) {
	// This test simulates a user with ZERO role bindings (authorizer denies everything).
	// All protected endpoints should return 403.
	az := &permCheckingAuthorizer{allowed: map[string]bool{}}

	tests := []struct {
		name   string
		method string
		perms  []MethodPermission
	}{
		{
			name:   "policies GET",
			method: http.MethodGet,
			perms:  []MethodPermission{{Method: http.MethodGet, Resource: "policy", Action: "view"}},
		},
		{
			name:   "policies POST",
			method: http.MethodPost,
			perms:  []MethodPermission{{Method: http.MethodPost, Resource: "policy", Action: "create"}},
		},
		{
			name:   "nodes GET",
			method: http.MethodGet,
			perms:  []MethodPermission{{Method: http.MethodGet, Resource: "node", Action: "view"}},
		},
		{
			name:   "node_groups GET",
			method: http.MethodGet,
			perms:  []MethodPermission{{Method: http.MethodGet, Resource: "node_group", Action: "view"}},
		},
		{
			name:   "bindings GET",
			method: http.MethodGet,
			perms:  []MethodPermission{{Method: http.MethodGet, Resource: "binding", Action: "view"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := RequireMethodPermission(az, tt.perms)
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be called")
			}))

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, reqWithUser(tt.method, "/test"))

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d (403 Forbidden)", rec.Code, http.StatusForbidden)
			}
		})
	}
}
