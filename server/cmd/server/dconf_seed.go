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

// seedDConfBuiltinSchemas loads the embedded built-in GSettings schema
// catalogue into the dconf_schemas table.  Existing rows with
// source = 'builtin' are updated; rows with source = 'agent' are not
// downgraded.  This runs at server startup and is idempotent.
func seedDConfBuiltinSchemas(ctx context.Context, repo *database.DConfRepository) {
	var rawSchemas []json.RawMessage
	if err := json.Unmarshal(assets.DConfBuiltinSchemasJSON, &rawSchemas); err != nil {
		log.Printf("WARNING: dconf seed: failed to parse built-in schemas JSON: %v", err)
		return
	}

	uOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
	seeded := 0
	for _, raw := range rawSchemas {
		var s pb.GSettingsSchema
		if err := uOpts.Unmarshal(raw, &s); err != nil {
			log.Printf("WARNING: dconf seed: failed to unmarshal schema: %v", err)
			continue
		}
		if err := repo.UpsertSchema(ctx, &s, "builtin"); err != nil {
			log.Printf("WARNING: dconf seed: failed to upsert schema %s: %v", s.GetSchemaId(), err)
			continue
		}
		seeded++
	}
	log.Printf("dconf: seeded %d built-in schemas (of %d total)", seeded, len(rawSchemas))
}
