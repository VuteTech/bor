// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// maxRedirects caps redirect chains for catalog fetches.
const maxRedirects = 5

// ErrTooLarge is returned when a response exceeds the caller's size cap.
var ErrTooLarge = errors.New("response exceeds the size limit")

// ErrBlockedAddress is returned when a catalog URL points at (or resolves to)
// an address the server refuses to contact: loopback, link-local (including
// cloud metadata endpoints), multicast, unspecified, and — unless private
// networks are explicitly allowed — RFC 1918 / CGNAT ranges.
var ErrBlockedAddress = errors.New("address is not allowed for catalog fetches")

// outboundURLRE is the only shape of URL the catalog service will fetch:
// https, a DNS name or IPv4 literal (no userinfo, no IPv6 literal, no
// whitespace or quotes), an optional port and an optional path/query. It is
// applied to the raw string before parsing so every downstream use of the
// value is guarded by the same check.
var outboundURLRE = regexp.MustCompile(`^https://[A-Za-z0-9](?:[A-Za-z0-9.-]{0,252}[A-Za-z0-9])?(?::[0-9]{1,5})?(?:/[^\s"'<>\\` + "`" + `]*)?$`)

// allowlistedRedirect mirrors api.allowlistedRedirect: redirects are followed
// only to https URLs on one of allowedHosts. Without it an admin-registered
// remote could bounce the server to internal services (SSRF).
func allowlistedRedirect(allowedHosts ...string) func(*http.Request, []*http.Request) error {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, h := range allowedHosts {
		allowed[strings.ToLower(h)] = struct{}{}
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-https redirect to %s", req.URL.Redacted())
		}
		if _, ok := allowed[strings.ToLower(req.URL.Hostname())]; !ok {
			return fmt.Errorf("refusing redirect to non-allowlisted host %q", req.URL.Hostname())
		}
		return nil
	}
}

// ValidateHTTPSURL checks rawURL against outboundURLRE and parses it. Only
// https URLs with a plain host (no credentials) are accepted.
func ValidateHTTPSURL(rawURL string) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if len(rawURL) > 2048 || !outboundURLRE.MatchString(rawURL) {
		return nil, fmt.Errorf("invalid URL %q: only https:// URLs with a host name or IPv4 address are allowed", rawURL)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("invalid URL %q: only https:// URLs without credentials are allowed", rawURL)
	}
	return u, nil
}

// ── address policy ──────────────────────────────────────────────────────────

// ipPolicy decides which resolved addresses the fetcher may connect to.
type ipPolicy struct {
	allowPrivate  bool
	allowLoopback bool
}

var (
	cgnatNet     = mustCIDR("100.64.0.0/10")
	zeroNet      = mustCIDR("0.0.0.0/8")
	benchmarkNet = mustCIDR("198.18.0.0/15")
)

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// check returns ErrBlockedAddress (wrapped with the reason) when ip must not
// be contacted under this policy.
func (p ipPolicy) check(ip net.IP) error {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	reason := ""
	switch {
	case ip.IsUnspecified(), zeroNet.Contains(ip):
		reason = "unspecified"
	case ip.IsLoopback():
		if !p.allowLoopback {
			reason = "loopback"
		}
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip.IsInterfaceLocalMulticast(), ip.IsMulticast():
		reason = "link-local or multicast"
	case ip.Equal(net.IPv4bcast):
		reason = "broadcast"
	case ip.IsPrivate(), cgnatNet.Contains(ip), benchmarkNet.Contains(ip):
		if !p.allowPrivate {
			reason = "private network (set BOR_FLATPAK_CATALOG_ALLOW_PRIVATE_NETWORKS=true to allow LAN mirrors)"
		}
	}
	if reason != "" {
		return fmt.Errorf("%w: %s is a %s address", ErrBlockedAddress, ip, reason)
	}
	return nil
}

// ── fetcher ─────────────────────────────────────────────────────────────────

// Fetcher performs the outbound HTTP requests of the catalog service. Every
// request goes through ValidateHTTPSURL, a pre-flight address check and a
// dialer that connects only to addresses the policy accepted (so a DNS name
// cannot be re-pointed at an internal address between check and connect).
type Fetcher struct {
	transport      http.RoundTripper
	maxGzBytes     int64
	requestTimeout time.Duration
	userAgent      string
	policy         ipPolicy
	dialer         *net.Dialer
	// lookup resolves a host name; tests replace it.
	lookup func(ctx context.Context, host string) ([]net.IPAddr, error)
}

// NewFetcher builds a Fetcher whose catalog downloads are capped at
// maxDownloadMB (compressed; zero or negative means 64 MiB). When
// allowPrivateNetworks is false, repositories on RFC 1918 / CGNAT addresses
// are refused; loopback, link-local and multicast addresses are always refused.
func NewFetcher(maxDownloadMB int, allowPrivateNetworks bool) *Fetcher {
	if maxDownloadMB <= 0 {
		maxDownloadMB = 64
	}
	f := &Fetcher{
		maxGzBytes:     int64(maxDownloadMB) << 20,
		requestTimeout: 10 * time.Minute,
		userAgent:      "bor-server flatpak-catalog",
		policy:         ipPolicy{allowPrivate: allowPrivateNetworks},
		dialer:         &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second},
		lookup:         net.DefaultResolver.LookupIPAddr,
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = f.dialContext
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	f.transport = tr
	return f
}

// MaxGzBytes is the compressed download cap.
func (f *Fetcher) MaxGzBytes() int64 { return f.maxGzBytes }

// MaxDecompressedBytes is the decompressed parse cap (8× the download cap).
func (f *Fetcher) MaxDecompressedBytes() int64 { return f.maxGzBytes * 8 }

func (f *Fetcher) client(host string) *http.Client {
	return &http.Client{
		Transport:     f.transport,
		CheckRedirect: allowlistedRedirect(host),
	}
}

// allowedAddrs resolves host and returns the addresses the policy accepts.
// An IP literal is checked directly. It fails when nothing acceptable remains.
func (f *Fetcher) allowedAddrs(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if err := f.policy.check(ip); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}
	addrs, err := f.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	var ok []net.IP
	var blocked error
	for _, a := range addrs {
		if cerr := f.policy.check(a.IP); cerr != nil {
			blocked = cerr
			continue
		}
		ok = append(ok, a.IP)
	}
	if len(ok) == 0 {
		if blocked != nil {
			return nil, fmt.Errorf("%s: %w", host, blocked)
		}
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	return ok, nil
}

// checkTarget is the pre-flight address check run before every request.
func (f *Fetcher) checkTarget(ctx context.Context, host string) error {
	_, err := f.allowedAddrs(ctx, host)
	return err
}

// dialContext connects only to policy-approved addresses of the requested
// host (DNS pinning: the name is resolved and checked here, not by the OS).
func (f *Fetcher) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	ips, err := f.allowedAddrs(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		conn, derr := f.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	return nil, fmt.Errorf("dial %s: %w", host, lastErr)
}

// ConditionalResult is the outcome of OpenConditional. When NotModified is
// false the caller must Close the Body.
type ConditionalResult struct {
	Body         io.ReadCloser
	ETag         string
	LastModified string
	NotModified  bool
}

type limitedBody struct {
	io.Reader
	io.Closer
}

// OpenConditional performs a conditional GET (If-None-Match / If-Modified-
// Since) and returns a body capped at the compressed download limit. The
// caller detects an over-limit body by reading past MaxGzBytes.
func (f *Fetcher) OpenConditional(ctx context.Context, rawURL, etag, lastModified string) (*ConditionalResult, error) {
	u, err := ValidateHTTPSURL(rawURL)
	if err != nil {
		return nil, err
	}
	if terr := f.checkTarget(ctx, u.Hostname()); terr != nil {
		return nil, terr
	}
	ctx, cancel := context.WithTimeout(ctx, f.requestTimeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	resp, err := f.client(u.Hostname()).Do(req) //nolint:gosec,bodyclose // G704: URL matched outboundURLRE (https, plain host), target address policy-checked and pinned in dialContext, redirects allowlisted; the body is returned in ConditionalResult and closed by the caller
	if err != nil {
		cancel()
		return nil, fmt.Errorf("request %s: %w", u.Redacted(), err)
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		_ = resp.Body.Close()
		cancel()
		return &ConditionalResult{NotModified: true, ETag: etag, LastModified: lastModified}, nil
	case http.StatusOK:
		body := &limitedBody{
			Reader: io.LimitReader(resp.Body, f.maxGzBytes+1),
			Closer: closerFunc(func() error { cancel(); return resp.Body.Close() }),
		}
		return &ConditionalResult{
			Body:         body,
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}, nil
	default:
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("%s returned HTTP %d", u.Redacted(), resp.StatusCode)
	}
}

type closerFunc func() error

func (c closerFunc) Close() error { return c() }

// GetSmall downloads a small resource (a .flatpakrepo file or an icon) capped
// at maxBytes and returns the body with its Content-Type.
func (f *Fetcher) GetSmall(ctx context.Context, rawURL string, maxBytes int64) (data []byte, contentType string, err error) {
	u, err := ValidateHTTPSURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	if terr := f.checkTarget(ctx, u.Hostname()); terr != nil {
		return nil, "", terr
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client(u.Hostname()).Do(req) //nolint:gosec // G704: URL matched outboundURLRE (https, plain host), target address policy-checked and pinned in dialContext, redirects allowlisted
	if err != nil {
		return nil, "", fmt.Errorf("request %s: %w", u.Redacted(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s returned HTTP %d", u.Redacted(), resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", u.Redacted(), err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", ErrTooLarge
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ErrNotFound is returned by GetSmall for HTTP 404.
var ErrNotFound = errors.New("resource not found")
