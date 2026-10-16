// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package services

import (
	"encoding/json"
	"testing"

	"github.com/VuteTech/Bor/server/assets"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestBuiltinKConfigCatalogue keeps the embedded catalogue honest: every
// entry must parse strictly as a KConfigCatalogueEntry, carry a unique id and
// a writable target, and only use the KConfigXT types the editor knows.
func TestBuiltinKConfigCatalogue(t *testing.T) {
	var raw []json.RawMessage
	if err := json.Unmarshal(assets.KConfigBuiltinCatalogueJSON, &raw); err != nil {
		t.Fatalf("catalogue is not a JSON array: %v", err)
	}
	if len(raw) < 100 {
		t.Fatalf("catalogue unexpectedly small: %d entries", len(raw))
	}

	knownTypes := map[string]bool{
		"Bool": true, "Int": true, "UInt": true, "Double": true, "String": true,
		"StringList": true, "Enum": true, "Path": true, "Url": true, "Color": true, "Font": true,
	}
	knownReload := map[string]bool{"": true, "kwin": true, "plasmashell": true, "screenlocker": true, "kded": true, "app": true, "logout": true}

	ids := make(map[string]bool, len(raw))
	targets := make(map[string]string, len(raw))
	// Strict unmarshal: a typo in a field name must fail the test.
	strict := protojson.UnmarshalOptions{}
	for i, r := range raw {
		var e pb.KConfigCatalogueEntry
		if err := strict.Unmarshal(r, &e); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if e.GetId() == "" || e.GetLabel() == "" || e.GetCategory() == "" {
			t.Errorf("entry %d (%q): id, label and category are required", i, e.GetId())
		}
		if ids[e.GetId()] {
			t.Errorf("duplicate id %q", e.GetId())
		}
		ids[e.GetId()] = true

		if err := ValidateKConfigTarget(e.GetFile(), e.GetGroupPath(), e.GetKey()); err != nil {
			t.Errorf("%s: %v", e.GetId(), err)
		}
		target := e.GetFile() + "\x00" + joinGroupPath(e.GetGroupPath()) + "\x00" + e.GetKey()
		if other, dup := targets[target]; dup {
			t.Errorf("%s and %s write the same key", e.GetId(), other)
		}
		targets[target] = e.GetId()

		if !knownTypes[e.GetType()] {
			t.Errorf("%s: unknown type %q", e.GetId(), e.GetType())
		}
		if e.GetType() == "Enum" && len(e.GetChoices()) == 0 {
			t.Errorf("%s: Enum without choices", e.GetId())
		}
		if e.GetType() != "Enum" && len(e.GetChoices()) > 0 {
			t.Errorf("%s: choices on a non-Enum entry", e.GetId())
		}
		if e.GetInverted() && e.GetType() != "Bool" {
			t.Errorf("%s: inverted is only meaningful for Bool", e.GetId())
		}
		if !knownReload[e.GetReload()] {
			t.Errorf("%s: unknown reload hint %q", e.GetId(), e.GetReload())
		}
		if e.GetSource() != "builtin" {
			t.Errorf("%s: built-in entries must have source builtin, got %q", e.GetId(), e.GetSource())
		}
		for _, w := range e.GetExtraWrites() {
			if err := ValidateKConfigTarget(w.GetFile(), w.GetGroupPath(), w.GetKey()); err != nil {
				t.Errorf("%s extra write: %v", e.GetId(), err)
			}
			if w.GetValue() == "" && !w.GetDelete() {
				t.Errorf("%s extra write %s: value required", e.GetId(), w.GetKey())
			}
		}
	}
}
