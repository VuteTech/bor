// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"sort"
	"strings"
	"testing"
)

func TestMigrationFilesEmbedded(t *testing.T) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations directory: %v", err)
	}

	if len(entries) == 0 {
		t.Fatal("no migration files found in embedded directory")
	}

	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}

	if len(upFiles) == 0 {
		t.Fatal("no .up.sql migration files found")
	}

	// Verify they sort in the expected order
	sorted := make([]string, len(upFiles))
	copy(sorted, upFiles)
	sort.Strings(sorted)

	for i, name := range sorted {
		if upFiles[i] != name {
			t.Errorf("migration files not in expected order: got %v, want %v", upFiles, sorted)
			break
		}
	}

	// Verify each migration file is readable and non-empty
	for _, name := range upFiles {
		content, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			t.Errorf("failed to read migration file %s: %v", name, err)
			continue
		}
		if len(content) == 0 {
			t.Errorf("migration file %s is empty", name)
		}
	}
}

func TestMigrationFilesContainExpected(t *testing.T) {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("failed to read embedded migrations directory: %v", err)
	}

	expected := map[string]bool{
		"000001_initial_schema.up.sql": false,
		"000002_add_users.up.sql":      false,
	}

	for _, e := range entries {
		if _, ok := expected[e.Name()]; ok {
			expected[e.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected migration file %s not found in embedded files", name)
		}
	}
}
