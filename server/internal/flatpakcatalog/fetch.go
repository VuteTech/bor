// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package flatpakcatalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxRedirects caps redirect chains for catalog fetches.
const maxRedirects = 5

// ErrTooLarge is returned when a response exceeds the caller's size cap.
var ErrTooLarge = errors.New("response exceeds the size limit")

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

// ValidateHTTPSURL parses rawURL and requires the https scheme.
func ValidateHTTPSURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid URL %q: only https:// URLs are allowed", rawURL)
	}
	return u, nil
}

// Fetcher performs the outbound HTTP requests of the catalog service.
type Fetcher struct {
	transport      http.RoundTripper
	maxGzBytes     int64
	requestTimeout time.Duration
	userAgent      string
}

// NewFetcher builds a Fetcher whose catalog downloads are capped at
// maxDownloadMB (compressed). Zero or negative means 64 MiB.
func NewFetcher(maxDownloadMB int) *Fetcher {
	if maxDownloadMB <= 0 {
		maxDownloadMB = 64
	}
	return &Fetcher{
		transport:      http.DefaultTransport,
		maxGzBytes:     int64(maxDownloadMB) << 20,
		requestTimeout: 10 * time.Minute,
		userAgent:      "bor-server flatpak-catalog",
	}
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
	resp, err := f.client(u.Hostname()).Do(req) //nolint:gosec,bodyclose // G704: URL validated (https), host allowlisted for redirects, admin-registered remote; the body is returned in ConditionalResult and closed by the caller
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
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	resp, err := f.client(u.Hostname()).Do(req) //nolint:gosec // G704: URL validated (https), host allowlisted for redirects, admin-registered remote
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
