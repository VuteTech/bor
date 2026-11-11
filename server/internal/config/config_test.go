// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear env vars that might interfere
	for _, key := range []string{
		"BOR_TLS_CERT_FILE", "BOR_TLS_KEY_FILE",
		"BOR_CA_CERT_FILE", "BOR_CA_KEY_FILE",
		"BOR_ADDRESS", "BOR_ENROLLMENT_PORT", "BOR_POLICY_PORT", "BOR_ADMIN_TOKEN",
	} {
		os.Unsetenv(key)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.EnrollmentPort != 8443 {
		t.Errorf("Server.EnrollmentPort = %d, want %d", cfg.Server.EnrollmentPort, 8443)
	}
	if cfg.Server.PolicyPort != 8444 {
		t.Errorf("Server.PolicyPort = %d, want %d", cfg.Server.PolicyPort, 8444)
	}
	if cfg.Server.EnrollmentAddr() != ":8443" {
		t.Errorf("Server.EnrollmentAddr() = %q, want %q", cfg.Server.EnrollmentAddr(), ":8443")
	}
	if cfg.TLS.AutogenDir != "/var/lib/bor/pki/ui" {
		t.Errorf("TLS.AutogenDir = %q, want %q", cfg.TLS.AutogenDir, "/var/lib/bor/pki/ui")
	}
	if cfg.CA.AutogenDir != "/var/lib/bor/pki/ca" {
		t.Errorf("CA.AutogenDir = %q, want %q", cfg.CA.AutogenDir, "/var/lib/bor/pki/ca")
	}
	if want := 10 * 365 * 24 * time.Hour; cfg.CA.Lifetime != want {
		t.Errorf("CA.Lifetime = %v, want %v", cfg.CA.Lifetime, want)
	}
}

func TestLoad_CALifetime(t *testing.T) {
	const day = 24 * time.Hour
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr bool
	}{
		{name: "years", value: "5y", want: 5 * 365 * day},
		{name: "days", value: "1000d", want: 1000 * day},
		{name: "go duration", value: "43800h", want: 43800 * time.Hour},
		{name: "surrounding space", value: " 3y ", want: 3 * 365 * day},
		{name: "lower bound", value: "2y", want: 2 * 365 * day},
		{name: "upper bound", value: "30y", want: 30 * 365 * day},
		{name: "too short", value: "1y", wantErr: true},
		{name: "too long", value: "31y", wantErr: true},
		{name: "no unit", value: "10", wantErr: true},
		{name: "fractional years", value: "2.5y", wantErr: true},
		{name: "negative", value: "-5y", wantErr: true},
		{name: "overflow", value: "999999999999y", wantErr: true},
		{name: "garbage", value: "forever", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOR_CA_LIFETIME", tt.value)
			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() with BOR_CA_LIFETIME=%q succeeded, want error", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.CA.Lifetime != tt.want {
				t.Errorf("CA.Lifetime = %v, want %v", cfg.CA.Lifetime, tt.want)
			}
		})
	}
}

func TestLoad_FailFast_TLSCertWithoutKey(t *testing.T) {
	os.Setenv("BOR_TLS_CERT_FILE", "/some/cert.pem")
	os.Unsetenv("BOR_TLS_KEY_FILE")
	defer os.Unsetenv("BOR_TLS_CERT_FILE")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail when BOR_TLS_CERT_FILE is set without BOR_TLS_KEY_FILE")
	}
}

func TestLoad_FailFast_TLSKeyWithoutCert(t *testing.T) {
	os.Unsetenv("BOR_TLS_CERT_FILE")
	os.Setenv("BOR_TLS_KEY_FILE", "/some/key.pem")
	defer os.Unsetenv("BOR_TLS_KEY_FILE")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail when BOR_TLS_KEY_FILE is set without BOR_TLS_CERT_FILE")
	}
}

func TestLoad_FailFast_CACertWithoutKey(t *testing.T) {
	os.Setenv("BOR_CA_CERT_FILE", "/some/ca.crt")
	os.Unsetenv("BOR_CA_KEY_FILE")
	defer os.Unsetenv("BOR_CA_CERT_FILE")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail when BOR_CA_CERT_FILE is set without BOR_CA_KEY_FILE")
	}
}

func TestLoad_FailFast_CAKeyWithoutCert(t *testing.T) {
	os.Unsetenv("BOR_CA_CERT_FILE")
	os.Setenv("BOR_CA_KEY_FILE", "/some/ca.key")
	defer os.Unsetenv("BOR_CA_KEY_FILE")

	_, err := Load()
	if err == nil {
		t.Error("Load() should fail when BOR_CA_KEY_FILE is set without BOR_CA_CERT_FILE")
	}
}

func TestLoad_CustomPort(t *testing.T) {
	os.Setenv("BOR_ENROLLMENT_PORT", "9443")
	defer os.Unsetenv("BOR_ENROLLMENT_PORT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.EnrollmentPort != 9443 {
		t.Errorf("Server.EnrollmentPort = %d, want %d", cfg.Server.EnrollmentPort, 9443)
	}
	if cfg.Server.EnrollmentAddr() != ":9443" {
		t.Errorf("Server.EnrollmentAddr() = %q, want %q", cfg.Server.EnrollmentAddr(), ":9443")
	}
}

func TestLoad_AdminToken(t *testing.T) {
	os.Setenv("BOR_ADMIN_TOKEN", "secret123")
	defer os.Unsetenv("BOR_ADMIN_TOKEN")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Security.AdminToken != "secret123" {
		t.Errorf("Security.AdminToken = %q, want %q", cfg.Security.AdminToken, "secret123")
	}
}

func TestLoad_FlatpakCatalog(t *testing.T) {
	os.Unsetenv("BOR_FLATPAK_CATALOG_REFRESH")
	os.Unsetenv("BOR_FLATPAK_CATALOG_MAX_DOWNLOAD_MB")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FlatpakCatalog.AllowPrivateNetworks {
		t.Error("FlatpakCatalog.AllowPrivateNetworks should default to false")
	}
	if !cfg.FlatpakCatalog.RefreshEnabled {
		t.Error("FlatpakCatalog.RefreshEnabled should default to true")
	}
	if cfg.FlatpakCatalog.MaxDownloadMB != 64 {
		t.Errorf("FlatpakCatalog.MaxDownloadMB = %d, want 64", cfg.FlatpakCatalog.MaxDownloadMB)
	}

	t.Setenv("BOR_FLATPAK_CATALOG_REFRESH", "false")
	t.Setenv("BOR_FLATPAK_CATALOG_MAX_DOWNLOAD_MB", "128")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FlatpakCatalog.RefreshEnabled {
		t.Error("BOR_FLATPAK_CATALOG_REFRESH=false not honoured")
	}
	if cfg.FlatpakCatalog.MaxDownloadMB != 128 {
		t.Errorf("MaxDownloadMB = %d, want 128", cfg.FlatpakCatalog.MaxDownloadMB)
	}

	t.Setenv("BOR_FLATPAK_CATALOG_MAX_DOWNLOAD_MB", "zero")
	if _, err := Load(); err == nil {
		t.Error("expected error for non-numeric BOR_FLATPAK_CATALOG_MAX_DOWNLOAD_MB")
	}
}

func TestLoad_CAAgentTrust(t *testing.T) {
	tests := []struct {
		name    string
		value   string // "" leaves the variable unset
		want    string
		wantErr bool
	}{
		{name: "default", want: AgentTrustSigning},
		{name: "signing", value: "signing", want: AgentTrustSigning},
		{name: "chain", value: "chain", want: AgentTrustChain},
		{name: "unknown", value: "root", wantErr: true},
		{name: "wrong case", value: "Chain", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				t.Setenv("BOR_CA_AGENT_TRUST", tt.value)
			}
			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() with BOR_CA_AGENT_TRUST=%q succeeded, want error", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.CA.AgentTrust != tt.want {
				t.Errorf("CA.AgentTrust = %q, want %q", cfg.CA.AgentTrust, tt.want)
			}
		})
	}
}
