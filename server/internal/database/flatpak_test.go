// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"strings"
	"testing"

	"github.com/VuteTech/Bor/server/internal/models"
)

func TestBuildFlatpakTsQuery(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"firefox", "firefox:*"},
		{"Fire Fox", "fire:* & fox:*"},
		{"org.mozilla.firefox", "org:* & mozilla:* & firefox:*"},
		{"lm-studio", "lm:* & studio:*"},
		{"'; DROP TABLE x; --", "drop:* & table:* & x:*"},
		{"!!!", ""},
		{"", ""},
		{"a & b | c", "a:* & b:* & c:*"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := buildFlatpakTsQuery(tt.in); got != tt.want {
				t.Errorf("buildFlatpakTsQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildFlatpakCatalogFilter(t *testing.T) {
	yes := true
	tests := []struct {
		name     string
		req      *models.FlatpakCatalogSearchRequest
		wantArgs int
		contains []string
		rankIsTs bool
	}{
		{"defaults", &models.FlatpakCatalogSearchRequest{}, 1,
			[]string{"r.catalog_enabled = true", "a.kind = ANY($1)"}, false},
		{"repo + verified + arch", &models.FlatpakCatalogSearchRequest{Repo: "flathub", Verified: &yes, Arch: "aarch64"}, 4,
			[]string{"r.name = $1", "a.kind = ANY($2)", "a.verified = $3", "a.arch = $4"}, false},
		{"search", &models.FlatpakCatalogSearchRequest{Search: "fire"}, 3,
			[]string{"a.search_tsv @@ to_tsquery('simple', $3)", "a.app_id ILIKE $2"}, true},
		{"search punctuation only", &models.FlatpakCatalogSearchRequest{Search: "%_"}, 2,
			[]string{"a.app_id ILIKE $2"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args, rank := buildFlatpakCatalogFilter(tt.req)
			if len(args) != tt.wantArgs {
				t.Fatalf("args = %d, want %d (%s)", len(args), tt.wantArgs, where)
			}
			for _, c := range tt.contains {
				if !strings.Contains(where, c) {
					t.Errorf("where %q lacks %q", where, c)
				}
			}
			if tt.rankIsTs != strings.HasPrefix(rank, "ts_rank(") {
				t.Errorf("rank = %q", rank)
			}
			if strings.Contains(where, "DROP") || strings.Contains(where, "%") {
				t.Errorf("raw input leaked into SQL: %q", where)
			}
		})
	}
}

func TestEscapeFlatpakLike(t *testing.T) {
	if got := escapeFlatpakLike(`a%b_c\d`); got != `a\%b\_c\\d` {
		t.Errorf("escapeFlatpakLike = %q", got)
	}
}

func TestSplitJoinFlatpakList(t *testing.T) {
	if got := splitFlatpakList(" x86_64, aarch64 ,,", ","); len(got) != 2 || got[0] != "x86_64" || got[1] != "aarch64" {
		t.Errorf("splitFlatpakList = %v", got)
	}
	if got := joinFlatpakList([]string{"Audio", " ", "Video"}, ";"); got != "Audio;Video" {
		t.Errorf("joinFlatpakList = %q", got)
	}
	if got := splitFlatpakList("", ","); len(got) != 0 {
		t.Errorf("empty split = %v", got)
	}
}
