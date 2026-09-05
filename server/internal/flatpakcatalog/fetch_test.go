// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

// testFetcher returns a Fetcher whose transport trusts the httptest TLS server
// and rewrites every host to it, so allowlist logic sees the real hostnames.
func testFetcher(t *testing.T, srv *httptest.Server) *Fetcher {
	t.Helper()
	f := NewFetcher(1, false)
	// Test hosts (mirror.example, ...) resolve to a public TEST-NET address so
	// the address policy accepts them; the round-tripper below then rewrites
	// the connection to the httptest server.
	f.lookup = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}}, nil
	}
	tr := srv.Client().Transport.(*http.Transport).Clone()
	f.transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Host = strings.TrimPrefix(srv.URL, "https://")
		return tr.RoundTrip(req)
	})
	return f
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenConditional(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repo/appstream/x86_64/appstream.xml.gz":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Fri, 04 Sep 2026 23:13:26 GMT")
			_, _ = w.Write([]byte("payload"))
		case "/redirect":
			http.Redirect(w, r, "https://evil.example/x", http.StatusFound)
		case "/redirect-http":
			http.Redirect(w, r, "http://mirror.example/x", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	f := testFetcher(t, srv)
	ctx := context.Background()

	res, err := f.OpenConditional(ctx, "https://mirror.example/repo/appstream/x86_64/appstream.xml.gz", "", "")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if string(body) != "payload" || res.ETag != `"v1"` || res.LastModified == "" || res.NotModified {
		t.Errorf("unexpected result: %q %+v", body, res)
	}

	res, err = f.OpenConditional(ctx, "https://mirror.example/repo/appstream/x86_64/appstream.xml.gz", `"v1"`, "")
	if err != nil || !res.NotModified {
		t.Fatalf("conditional fetch: %v %+v", err, res)
	}

	if _, err := f.OpenConditional(ctx, "https://mirror.example/redirect", "", ""); err == nil {
		t.Error("redirect to another host must be refused")
	}
	if _, err := f.OpenConditional(ctx, "https://mirror.example/redirect-http", "", ""); err == nil {
		t.Error("redirect to http must be refused")
	}
	if _, err := f.OpenConditional(ctx, "http://mirror.example/repo/x", "", ""); err == nil {
		t.Error("plain http must be refused")
	}
	if _, err := f.OpenConditional(ctx, "https://mirror.example/missing", "", ""); err == nil {
		t.Error("404 must be an error")
	}
}

func TestGetSmall_SizeCap(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()
	f := testFetcher(t, srv)

	data, ct, err := f.GetSmall(context.Background(), "https://mirror.example/icon.png", 200)
	if err != nil || len(data) != 100 || ct != "image/png" {
		t.Fatalf("GetSmall: %v %d %q", err, len(data), ct)
	}
	if _, _, err := f.GetSmall(context.Background(), "https://mirror.example/icon.png", 50); !errors.Is(err, ErrTooLarge) {
		t.Errorf("expected ErrTooLarge, got %v", err)
	}
	if _, _, err := f.GetSmall(context.Background(), "https://mirror.example/missing", 50); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAppstreamURL(t *testing.T) {
	repo := &models.FlatpakRepository{URL: "https://dl.flathub.org/repo/"}
	if u, err := AppstreamURL(repo, "x86_64"); err != nil || u != "https://dl.flathub.org/repo/appstream/x86_64/appstream.xml.gz" {
		t.Errorf("derived url = %q %v", u, err)
	}
	repo.AppstreamURL = "https://mirror.example/as/{arch}/appstream.xml.gz"
	if u, _ := AppstreamURL(repo, "aarch64"); u != "https://mirror.example/as/aarch64/appstream.xml.gz" {
		t.Errorf("override url = %q", u)
	}
	oci := &models.FlatpakRepository{URL: "oci+https://registry.fedoraproject.org"}
	if _, err := AppstreamURL(oci, "x86_64"); !errors.Is(err, ErrNoAppstreamURL) {
		t.Errorf("oci without override: %v", err)
	}
}

func TestValidateHTTPSURL(t *testing.T) {
	good := []string{
		"https://dl.flathub.org/repo/",
		"https://dl.flathub.org/repo/flathub.flatpakrepo",
		"https://mirror.example:8443/as/x86_64/appstream.xml.gz?x=1",
		"https://203.0.113.10/repo/",
		" https://dl.flathub.org/repo/ ",
	}
	for _, u := range good {
		if _, err := ValidateHTTPSURL(u); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	bad := []string{
		"", "dl.flathub.org/repo/", "http://dl.flathub.org/repo/", "ftp://x/", "file:///etc/passwd",
		"https://user:pw@dl.flathub.org/repo/", "https://dl.flathub.org/repo/ x", "https://[::1]/repo/",
		"https://dl.flathub.org/repo/\"; ls", "https://-bad.example/", "https://exa mple.com/",
		"https://" + strings.Repeat("a", 300) + ".example/",
	}
	for _, u := range bad {
		if _, err := ValidateHTTPSURL(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}

func TestIPPolicy(t *testing.T) {
	strict := ipPolicy{}
	lan := ipPolicy{allowPrivate: true}
	cases := []struct {
		ip          string
		strictOK    bool
		allowPrivOK bool
	}{
		{"203.0.113.10", true, true},
		{"2606:4700::1111", true, true},
		{"127.0.0.1", false, false},
		{"::1", false, false},
		{"0.0.0.0", false, false},
		{"0.1.2.3", false, false},
		{"169.254.169.254", false, false},
		{"fe80::1", false, false},
		{"224.0.0.1", false, false},
		{"255.255.255.255", false, false},
		{"10.1.2.3", false, true},
		{"172.16.5.5", false, true},
		{"192.168.122.1", false, true},
		{"100.64.0.1", false, true},
		{"198.18.0.1", false, true},
		{"fd00::1", false, true},
		{"::ffff:192.168.1.1", false, true},
		{"::ffff:127.0.0.1", false, false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test ip %q", c.ip)
		}
		if got := strict.check(ip) == nil; got != c.strictOK {
			t.Errorf("strict %s: allowed=%v want %v", c.ip, got, c.strictOK)
		}
		if got := lan.check(ip) == nil; got != c.allowPrivOK {
			t.Errorf("allowPrivate %s: allowed=%v want %v", c.ip, got, c.allowPrivOK)
		}
	}
}

func TestFetcher_BlockedTargets(t *testing.T) {
	called := false
	f := NewFetcher(1, false)
	f.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be reached")
	})
	f.lookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "internal.example":
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
		case "rebind.example":
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("169.254.169.254")}}, nil
		}
		return nil, errors.New("nxdomain")
	}
	ctx := context.Background()
	for _, u := range []string{
		"https://127.0.0.1/repo/", "https://169.254.169.254/latest/meta-data/", "https://10.0.0.5/repo/",
		"https://internal.example/repo/", "https://rebind.example/repo/", "https://nxdomain.example/repo/",
	} {
		if _, _, err := f.GetSmall(ctx, u, 100); err == nil {
			t.Errorf("%s: expected an error", u)
		} else if !strings.Contains(u, "nxdomain") && !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%s: expected ErrBlockedAddress, got %v", u, err)
		}
		if _, err := f.OpenConditional(ctx, u, "", ""); err == nil {
			t.Errorf("%s: OpenConditional expected an error", u)
		}
	}
	if called {
		t.Error("transport was used for a blocked target")
	}

	// The same private host is acceptable once private networks are allowed.
	f2 := NewFetcher(1, true)
	f2.lookup = f.lookup
	f2.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}, Request: r}, nil
	})
	if data, _, err := f2.GetSmall(ctx, "https://internal.example/repo/x.flatpakrepo", 100); err != nil || string(data) != "ok" {
		t.Errorf("private target with allowPrivate: %v %q", err, data)
	}
}
