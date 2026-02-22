// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
server:
  address: "myserver:9090"
agent:
  client_id: "test-agent"
firefox:
  policies_path: "/tmp/test/policies.json"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Address != "myserver:9090" {
		t.Errorf("expected address myserver:9090, got %s", cfg.Server.Address)
	}
	if cfg.Agent.ClientID != "test-agent" {
		t.Errorf("expected client_id test-agent, got %s", cfg.Agent.ClientID)
	}
	if cfg.Firefox.PoliciesPath != "/tmp/test/policies.json" {
		t.Errorf("expected policies_path /tmp/test/policies.json, got %s", cfg.Firefox.PoliciesPath)
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Minimal config (empty YAML)
	if err := os.WriteFile(cfgPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Server.Address != "localhost:8443" {
		t.Errorf("expected default address localhost:8443, got %s", cfg.Server.Address)
	}
	if cfg.Firefox.PoliciesPath != "/etc/firefox/policies/policies.json" {
		t.Errorf("expected default policies_path, got %s", cfg.Firefox.PoliciesPath)
	}
	// client_id should default to hostname
	if cfg.Agent.ClientID == "" {
		t.Error("expected client_id to default to hostname")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for missing config file")
	}
}
