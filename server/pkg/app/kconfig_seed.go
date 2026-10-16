// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package app

import (
	"context"
	"encoding/json"
	"log"

	"github.com/VuteTech/Bor/server/assets"
	"github.com/VuteTech/Bor/server/internal/database"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// seedKConfigBuiltinCatalogue loads the embedded KDE settings catalogue
// into the kconfig_catalogue_entries table. Rows with source = 'builtin'
// are refreshed; node-reported rows are left alone. Runs at server
// startup and is idempotent.
func seedKConfigBuiltinCatalogue(ctx context.Context, repo *database.KConfigRepository) {
	var raw []json.RawMessage
	if err := json.Unmarshal(assets.KConfigBuiltinCatalogueJSON, &raw); err != nil {
		log.Printf("WARNING: kconfig seed: failed to parse built-in catalogue JSON: %v", err)
		return
	}

	uOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
	seeded := 0
	for _, r := range raw {
		var e pb.KConfigCatalogueEntry
		if err := uOpts.Unmarshal(r, &e); err != nil {
			log.Printf("WARNING: kconfig seed: failed to unmarshal entry: %v", err)
			continue
		}
		if err := repo.UpsertEntry(ctx, &e, "builtin"); err != nil {
			log.Printf("WARNING: kconfig seed: failed to upsert entry %s: %v", e.GetId(), err)
			continue
		}
		seeded++
	}
	log.Printf("kconfig: seeded %d built-in catalogue entries (of %d total)", seeded, len(raw))
}
