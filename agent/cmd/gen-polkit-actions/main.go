// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// gen-polkit-actions discovers all polkit actions installed on the local system
// via `pkaction --verbose` and writes the resulting catalogue to a JSON file
// for embedding in the Bor server as the built-in polkit action seed.
//
// Usage (on a reference Linux installation with polkit installed):
//
//	cd agent && go run ./cmd/gen-polkit-actions \
//	    --out ../server/assets/polkit_builtin_actions.json
//
// The output is a JSON array of PolkitActionDescription objects (protojson / snake_case).
// It is embedded by the server at build time via go:embed and used to seed the
// polkit_actions table before any agent reports its catalogue.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/VuteTech/Bor/agent/internal/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	outFile := flag.String("out", "../server/assets/polkit_builtin_actions.json", "output JSON file path")
	flag.Parse()

	log.Printf("Discovering polkit actions via pkaction --verbose...")

	actions, err := policy.DiscoverPolkitActions()
	if err != nil {
		log.Fatalf("discovery failed: %v", err)
	}

	log.Printf("Found %d polkit actions", len(actions))

	// Serialise each action as a protojson object, then wrap in a JSON array.
	m := protojson.MarshalOptions{
		EmitUnpopulated: false,
		UseProtoNames:   true, // snake_case field names
	}

	arr := make([]json.RawMessage, 0, len(actions))
	for _, a := range actions {
		b, marshalErr := m.Marshal(a)
		if marshalErr != nil {
			log.Printf("WARNING: failed to marshal action %s: %v", a.GetActionId(), marshalErr)
			continue
		}
		arr = append(arr, json.RawMessage(b))
	}

	out, jsonErr := json.MarshalIndent(arr, "", "  ")
	if jsonErr != nil {
		log.Fatalf("marshal array: %v", jsonErr)
	}

	if writeErr := os.WriteFile(*outFile, append(out, '\n'), 0o600); writeErr != nil {
		log.Fatalf("write %s: %v", *outFile, writeErr)
	}

	fmt.Printf("Wrote %d actions to %s (%d bytes)\n", len(arr), *outFile, len(out))
}
