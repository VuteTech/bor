// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"strings"
	"testing"
)

func TestValidateFirefoxContent_Empty(t *testing.T) {
	if err := ValidateFirefoxContent("{}"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_EmptyString(t *testing.T) {
	if err := ValidateFirefoxContent(""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_BooleanPolicies(t *testing.T) {
	content := `{
		"DisableAppUpdate": true,
		"DisablePocket": true,
		"DisableTelemetry": false,
		"BlockAboutConfig": true
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_StringPolicies(t *testing.T) {
	content := `{
		"DefaultDownloadDirectory": "${home}/Downloads",
		"SSLVersionMin": "tls1.2"
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_InvalidSSLVersion(t *testing.T) {
	content := `{"SSLVersionMin": "ssl3"}`
	err := ValidateFirefoxContent(content)
	if err == nil {
		t.Fatal("expected error for invalid SSLVersionMin")
	}
	if !strings.Contains(err.Error(), "SSLVersionMin") {
		t.Errorf("error should mention SSLVersionMin, got: %v", err)
	}
}

func TestValidateFirefoxContent_Homepage(t *testing.T) {
	content := `{
		"Homepage": {
			"URL": "https://example.com",
			"Locked": true,
			"StartPage": "homepage",
			"Additional": ["https://extra.com"]
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_InvalidStartPage(t *testing.T) {
	content := `{"Homepage": {"URL": "https://x.com", "StartPage": "invalid"}}`
	if err := ValidateFirefoxContent(content); err == nil {
		t.Fatal("expected error for invalid StartPage")
	}
}

func TestValidateFirefoxContent_TrackingProtection(t *testing.T) {
	content := `{
		"EnableTrackingProtection": {
			"Value": true,
			"Locked": true,
			"Cryptomining": true,
			"Fingerprinting": true
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_DNSOverHTTPS(t *testing.T) {
	content := `{
		"DNSOverHTTPS": {
			"Enabled": true,
			"ProviderURL": "https://dns.example.com/query",
			"Locked": true,
			"ExcludedDomains": ["internal.corp"],
			"Fallback": true
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_Extensions(t *testing.T) {
	content := `{
		"Extensions": {
			"Install": ["https://addons.mozilla.org/firefox/downloads/somefile.xpi"],
			"Uninstall": ["bad-extension@example.com"],
			"Locked": ["required-extension@example.com"]
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_Proxy(t *testing.T) {
	content := `{
		"Proxy": {
			"Mode": "manual",
			"Locked": true,
			"HTTPProxy": "proxy.corp.com",
			"HTTPPort": 8080
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_InvalidProxyMode(t *testing.T) {
	content := `{"Proxy": {"Mode": "bad"}}`
	if err := ValidateFirefoxContent(content); err == nil {
		t.Fatal("expected error for invalid proxy mode")
	}
}

func TestValidateFirefoxContent_InvalidJSON(t *testing.T) {
	err := ValidateFirefoxContent("{bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestValidateFirefoxContent_Cookies(t *testing.T) {
	content := `{
		"Cookies": {
			"Allow": ["https://trusted.com"],
			"Block": ["https://ads.com"],
			"Behavior": "reject-foreign",
			"Locked": true
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_FirefoxHome(t *testing.T) {
	content := `{
		"FirefoxHome": {
			"Search": true,
			"TopSites": true,
			"SponsoredTopSites": false,
			"Pocket": false,
			"Locked": true
		}
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFirefoxContent_InvalidHomepageURL(t *testing.T) {
	content := `{"Homepage": {"URL": "javascript:alert(1)"}}`
	err := ValidateFirefoxContent(content)
	if err == nil {
		t.Fatal("expected error for javascript: URL scheme")
	}
	if !strings.Contains(err.Error(), "Homepage.URL") {
		t.Errorf("error should mention Homepage.URL, got: %v", err)
	}
}

func TestValidateFirefoxContent_InvalidDNSOverHTTPSURL(t *testing.T) {
	content := `{"DNSOverHTTPS": {"Enabled": true, "ProviderURL": "ftp://bad.com"}}`
	err := ValidateFirefoxContent(content)
	if err == nil {
		t.Fatal("expected error for ftp: URL scheme in DNSOverHTTPS")
	}
	if !strings.Contains(err.Error(), "DNSOverHTTPS.ProviderURL") {
		t.Errorf("error should mention DNSOverHTTPS.ProviderURL, got: %v", err)
	}
}

func TestValidateFirefoxContent_ValidHTTPURL(t *testing.T) {
	content := `{"Homepage": {"URL": "http://intranet.corp"}}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error for http: URL: %v", err)
	}
}

func TestValidateFirefoxContent_Bookmarks(t *testing.T) {
	content := `{
		"Bookmarks": [
			{"Title": "Example", "URL": "https://example.com", "Placement": "toolbar"},
			{"Title": "Corp Intranet", "URL": "https://intranet.corp", "Folder": "Work"}
		]
	}`
	if err := ValidateFirefoxContent(content); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
