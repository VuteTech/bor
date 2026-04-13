// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VuteTech/Bor/server/internal/authz"
	"github.com/VuteTech/Bor/server/internal/services"
)

type contextKey string

const userContextKey contextKey = "user"

// SessionCookieName is the name of the httpOnly cookie that carries the JWT.
const SessionCookieName = "bor_session"

// AuthMiddleware validates JWT tokens on protected routes.
// It checks the bor_session httpOnly cookie first, then falls back to the
// Authorization: Bearer header (for API clients and the agent).
func AuthMiddleware(authSvc *services.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := tokenFromRequest(r)
			if token == "" {
				http.Error(w, `{"error":"authorization required"}`, http.StatusUnauthorized)
				return
			}

			claims, err := authSvc.ValidateToken(token)
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// tokenFromRequest extracts the JWT from the request, checking the session
// cookie first, then the Authorization header.
func tokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	authHeader := r.Header.Get("Authorization")
	if parts := strings.SplitN(authHeader, " ", 2); len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}

// SetSessionCookie sets the bor_session httpOnly cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// RefreshCookieName is the name of the httpOnly cookie that carries the refresh token.
const RefreshCookieName = "bor_refresh"

// SetRefreshCookie sets the bor_refresh httpOnly cookie on the response.
func SetRefreshCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     "/api/v1/auth/refresh",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie removes the session and refresh cookies.
func ClearSessionCookie(w http.ResponseWriter) {
	SetSessionCookie(w, "", -1)
	SetRefreshCookie(w, "", -1)
	clearCSRFCookie(w)
}

// CSRFCookieName is the non-httpOnly cookie used for double-submit CSRF protection.
const CSRFCookieName = "bor_csrf"

// SetCSRFCookie generates a random CSRF token and sets it as a non-httpOnly
// cookie (readable by JavaScript) alongside the session cookie.
func SetCSRFCookie(w http.ResponseWriter) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: false, // must be readable by JS
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// csrfExemptPaths are public auth endpoints that don't have a CSRF cookie yet.
var csrfExemptPaths = map[string]bool{
	"/api/v1/auth/login":           true,
	"/api/v1/auth/begin":           true,
	"/api/v1/auth/step":            true,
	"/api/v1/auth/logout":          true,
	"/api/v1/auth/refresh":         true,
	"/api/v1/auth/webauthn/begin":  true,
	"/api/v1/auth/webauthn/finish": true,
}

// CSRFMiddleware validates the double-submit CSRF token on state-changing
// requests (POST, PUT, PATCH, DELETE). The X-CSRF-Token header must match the
// bor_csrf cookie value. GET, HEAD, OPTIONS, and public auth endpoints are exempt.
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}

		// Skip CSRF check for public auth endpoints and non-API routes.
		if csrfExemptPaths[r.URL.Path] || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"missing CSRF token"}`, http.StatusForbidden)
			return
		}

		header := r.Header.Get("X-CSRF-Token")
		if header == "" || header != cookie.Value {
			http.Error(w, `{"error":"invalid CSRF token"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// ipBucket tracks request counts within a sliding time window for one IP.
type ipBucket struct {
	count       int
	windowStart time.Time
}

// ipRateLimiter is a simple per-IP sliding-window rate limiter.
type ipRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*ipBucket
	rate    int           // max requests allowed per window
	window  time.Duration // length of the window
}

// newIPRateLimiter creates a rate limiter and starts a background cleanup goroutine.
func newIPRateLimiter(rate int, window time.Duration) *ipRateLimiter {
	rl := &ipRateLimiter{
		clients: make(map[string]*ipBucket),
		rate:    rate,
		window:  window,
	}
	go rl.cleanupLoop()
	return rl
}

// allow returns true if the IP is within the rate limit, false if it should be rejected.
func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, ok := rl.clients[ip]
	if !ok || now.Sub(b.windowStart) >= rl.window {
		rl.clients[ip] = &ipBucket{count: 1, windowStart: now}
		return true
	}
	if b.count >= rl.rate {
		return false
	}
	b.count++
	return true
}

// cleanupLoop removes stale entries every two windows to bound memory use.
func (rl *ipRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.window * 2)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, b := range rl.clients {
			if now.Sub(b.windowStart) >= rl.window*2 {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP extracts the request's remote IP from RemoteAddr, ignoring the port.
// RemoteAddr is set by the HTTP server and cannot be spoofed by the client.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// NewRateLimitMiddleware returns a middleware that limits callers to rate requests
// per window per remote IP. Excess requests receive 429 with a Retry-After header.
func NewRateLimitMiddleware(rate int, window time.Duration) func(http.Handler) http.Handler {
	rl := newIPRateLimiter(rate, window)
	retryAfter := strconv.Itoa(int(window.Seconds()))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientIP(r)) {
				w.Header().Set("Retry-After", retryAfter)
				http.Error(w, `{"error":"rate limit exceeded, try again later"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware adds standard security headers to all HTTP responses.
// Applied to the UI/API handler chain (not gRPC).
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
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
