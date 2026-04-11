// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/VuteTech/Bor/server/assets"
	"github.com/VuteTech/Bor/server/internal/database"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// seedPolkitBuiltinActions loads the embedded built-in polkit action catalogue
// into the polkit_actions table.  Existing rows with source = 'builtin' are
// updated; rows with source = 'agent' are not downgraded.
// This runs at server startup and is idempotent.
func seedPolkitBuiltinActions(ctx context.Context, repo *database.PolkitRepository) {
	var rawActions []json.RawMessage
	if err := json.Unmarshal(assets.PolkitBuiltinActionsJSON, &rawActions); err != nil {
		log.Printf("WARNING: polkit seed: failed to parse built-in actions JSON: %v", err)
		return
	}

	uOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
	seeded := 0
	for _, raw := range rawActions {
		var a pb.PolkitActionDescription
		if err := uOpts.Unmarshal(raw, &a); err != nil {
			log.Printf("WARNING: polkit seed: failed to unmarshal action: %v", err)
			continue
		}
		if a.GetActionId() == "" {
			continue
		}
		if err := repo.UpsertAction(ctx, &a, "builtin"); err != nil {
			log.Printf("WARNING: polkit seed: failed to upsert action %s: %v", a.GetActionId(), err)
			continue
		}
		seeded++
	}
	log.Printf("polkit: seeded %d built-in actions (of %d total)", seeded, len(rawActions))
}
