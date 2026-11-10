// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestUniqueNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{}},
		{"blanks dropped", []string{"", "a", ""}, []string{"a"}},
		{"duplicates dropped, order kept", []string{"b", "a", "b", "c", "a"}, []string{"b", "a", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := uniqueNonEmpty(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("uniqueNonEmpty(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Requests that fail validation are answered before any service is touched.
func TestNodeGroupMembersHandler_Add_RejectsBadBodies(t *testing.T) {
	tooMany := `{"node_ids":["` + strings.Repeat(`x","`, maxGroupMemberBatch) + `y"]}`
	// Distinct ids are needed to exceed the cap after de-duplication.
	ids := make([]string, 0, maxGroupMemberBatch+1)
	for i := 0; i <= maxGroupMemberBatch; i++ {
		ids = append(ids, `"n`+strings.Repeat("i", i)+`"`)
	}
	tooManyDistinct := `{"node_ids":[` + strings.Join(ids, ",") + `]}`

	tests := []struct {
		name string
		body string
		want int
	}{
		{"not JSON", "nope", http.StatusBadRequest},
		{"missing node_ids", `{}`, http.StatusBadRequest},
		{"only blanks", `{"node_ids":["",""]}`, http.StatusBadRequest},
		{"over the batch cap", tooManyDistinct, http.StatusBadRequest},
		// Duplicates collapse to two ids; with no grant in the request the
		// call is then refused as out of scope.
		{"duplicates collapse, then scope applies", tooMany, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewNodeGroupMembersHandler(&NodeHandler{}, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/node-groups/g1/nodes", strings.NewReader(tt.body))
			req.SetPathValue("id", "g1")
			rr := httptest.NewRecorder()
			h.Add(rr, req)
			if rr.Code != tt.want {
				t.Errorf("Add() status = %d, want %d (body %s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}
