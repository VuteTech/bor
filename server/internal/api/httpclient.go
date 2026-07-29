// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"fmt"
	"net/http"
)

// maxOutboundRedirects caps redirect chains for the package-metadata fetchers.
const maxOutboundRedirects = 5

// allowlistedRedirect returns an http.Client CheckRedirect policy that permits
// redirects only to https URLs whose host is in allowedHosts. This is a
// defense-in-depth control for the server-side package-metadata fetchers
// (ppa-info, copr-info): the initial request URL is built from an allowlisted
// domain, but without this guard the client would transparently follow a 3xx
// redirect from that host to any address — including internal services or the
// cloud metadata endpoint — turning the fetchers into an SSRF primitive.
//
// A blocked redirect fails the request, which the callers already degrade
// gracefully (GPG verification is disabled and a warning is surfaced).
func allowlistedRedirect(allowedHosts ...string) func(*http.Request, []*http.Request) error {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[h] = struct{}{}
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxOutboundRedirects {
			return fmt.Errorf("stopped after %d redirects", maxOutboundRedirects)
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-https redirect to %s", req.URL.Redacted())
		}
		if _, ok := allowed[req.URL.Hostname()]; !ok {
			return fmt.Errorf("refusing redirect to non-allowlisted host %q", req.URL.Hostname())
		}
		return nil
	}
}
