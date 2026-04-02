// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// gen-dconf-schemas scans a GSettings schema directory and writes the
// resulting catalogue to a JSON file for embedding in the Bor server.
//
// Usage (local system):
//
//	cd agent && go run ./cmd/gen-dconf-schemas \
//	    --schemas-dir /usr/share/glib-2.0/schemas \
//	    --gsettings \
//	    --out ../server/assets/dconf_builtin_schemas.json
//
// Usage (schemas copied from remote system, no gsettings available):
//
//	cd agent && go run ./cmd/gen-dconf-schemas \
//	    --schemas-dir /tmp/gschemas \
//	    --out ../server/assets/dconf_builtin_schemas.json
//
// --gsettings enriches enum-type keys whose definitions live in compiled GLib
// types (not in XML) by running "gsettings range <schema> <key>" for each such
// key.  Requires gsettings(1) and the schemas to be installed on the running
// system.
//
// The output is a JSON array of GSettingsSchema objects (protojson / snake_case).
// It is embedded by the server at build time via go:embed and used to seed the
// dconf_schemas table before any agent reports its catalogue.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/VuteTech/Bor/agent/internal/policy"
	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	schemasDir := flag.String("schemas-dir", "/usr/share/glib-2.0/schemas", "directory containing *.gschema.xml files")
	outFile := flag.String("out", "../server/assets/dconf_builtin_schemas.json", "output JSON file path")
	useGSettings := flag.Bool("gsettings", false, "enrich enum keys via 'gsettings range' (requires schemas installed on this system)")
	flag.Parse()

	log.Printf("Scanning schemas from %s ...", *schemasDir)

	schemas, err := policy.ScanGSettingsSchemasFrom(*schemasDir)
	if err != nil {
		log.Fatalf("scan failed: %v", err)
	}

	log.Printf("Found %d schemas", len(schemas))

	if *useGSettings {
		enriched := enrichEnums(schemas)
		log.Printf("Enriched %d enum keys via gsettings range", enriched)
	}

	// Serialise each schema as a protojson object, then wrap in a JSON array.
	m := protojson.MarshalOptions{
		EmitUnpopulated: false,
		UseProtoNames:   true, // snake_case field names
	}

	arr := make([]json.RawMessage, 0, len(schemas))
	for _, s := range schemas {
		b, err := m.Marshal(s)
		if err != nil {
			log.Printf("WARNING: failed to marshal schema %s: %v", s.GetSchemaId(), err)
			continue
		}
		arr = append(arr, json.RawMessage(b))
	}

	out, err := json.MarshalIndent(arr, "", "  ")
	if err != nil {
		log.Fatalf("marshal array: %v", err)
	}

	if err := os.WriteFile(*outFile, append(out, '\n'), 0o644); err != nil {
		log.Fatalf("write %s: %v", *outFile, err)
	}

	fmt.Printf("Wrote %d schemas to %s (%d bytes)\n", len(arr), *outFile, len(out))
}

// enrichEnums calls "gsettings range <schema> <key>" for each key that has
// type "s" but no enum_values or choices populated (enum resolution failed
// because the definition lives in compiled GLib C types, not schema XML).
// It parses the "enum\n'val1'\n'val2'\n..." output and fills in Choices.
func enrichEnums(schemas []*pb.GSettingsSchema) int {
	enriched := 0
	for _, s := range schemas {
		for _, k := range s.GetKeys() {
			// Only target string-typed keys with no choices/enum metadata yet.
			if k.GetType() != "s" {
				continue
			}
			if len(k.GetChoices()) > 0 || len(k.GetEnumValues()) > 0 {
				continue
			}

			out, err := exec.Command("gsettings", "range", s.GetSchemaId(), k.GetName()).Output() //nolint:gosec
			if err != nil {
				continue
			}
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) < 2 || lines[0] != "enum" {
				continue
			}
			var choices []string
			for _, line := range lines[1:] {
				// lines look like: 'prefer-dark'
				val := strings.Trim(strings.TrimSpace(line), "'")
				if val != "" {
					choices = append(choices, val)
				}
			}
			if len(choices) > 0 {
				k.Choices = choices
				enriched++
			}
		}
	}
	return enriched
}
