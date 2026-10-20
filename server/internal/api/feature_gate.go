// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import "net/http"

// RequireFeature answers 404 with message while enabled() is false, so a
// route of an optional feature behaves as if it did not exist in editions
// that do not enable it. enabled is consulted on every request, so an
// edition backed by a license switches the route on and off without a
// restart.
func RequireFeature(enabled func() bool, message string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled() {
				writeJSONError(w, http.StatusNotFound, message)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
