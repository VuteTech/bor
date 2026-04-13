// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package policy

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
)

// GSettingsSchemasDir is the canonical directory for GSettings schema XML files.
const GSettingsSchemasDir = "/usr/share/glib-2.0/schemas"

// xmlSchemaList is the top-level element in a .gschema.xml file.
type xmlSchemaList struct {
	Enums   []xmlEnum   `xml:"enum"`
	Flags   []xmlEnum   `xml:"flags"`
	Schemas []xmlSchema `xml:"schema"`
}

type xmlEnum struct {
	ID     string       `xml:"id,attr"`
	Values []xmlEnumVal `xml:"value"`
}

type xmlEnumVal struct {
	Nick  string `xml:"nick,attr"`
	Value int32  `xml:"value,attr"`
}

type xmlSchema struct {
	ID          string     `xml:"id,attr"`
	Path        string     `xml:"path,attr"`
	GetText     string     `xml:"gettext-domain,attr"`
	Keys        []xmlKey   `xml:"key"`
	ChildSchema []xmlChild `xml:"child"`
}

type xmlChild struct {
	Name   string `xml:"name,attr"`
	Schema string `xml:"schema,attr"`
}

type xmlKey struct {
	Name        string      `xml:"name,attr"`
	Type        string      `xml:"type,attr"`
	Enum        string      `xml:"enum,attr"`
	Flags       string      `xml:"flags,attr"`
	Default     xmlDefault  `xml:"default"`
	Summary     string      `xml:"summary"`
	Description string      `xml:"description"`
	Range       *xmlRange   `xml:"range"`
	Choices     []xmlChoice `xml:"choices>choice"`
}

type xmlDefault struct {
	Value string `xml:",chardata"`
}

type xmlRange struct {
	Min string `xml:"min,attr"`
	Max string `xml:"max,attr"`
}

type xmlChoice struct {
	Value string `xml:"value,attr"`
}

// ScanGSettingsSchemas reads all *.gschema.xml files from GSettingsSchemasDir
// and returns a slice of GSettingsSchema proto messages with full key metadata.
//
// The scan is best-effort: parse errors in individual files are skipped.
func ScanGSettingsSchemas() ([]*pb.GSettingsSchema, error) {
	return ScanGSettingsSchemasFrom(GSettingsSchemasDir)
}

// ScanGSettingsSchemasFrom scans schema XML files from the given directory.
// It is exported so that tests can point it at fixture directories.
func ScanGSettingsSchemasFrom(dir string) ([]*pb.GSettingsSchema, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no schemas installed; not an error
		}
		return nil, fmt.Errorf("dconf catalogue: read dir %s: %w", dir, err)
	}

	// First pass: collect all enum/flags definitions across all files
	// so that key references can be resolved.  Both *.gschema.xml and
	// *.enums.xml files can carry <enum> / <flags> elements.
	enumIndex := make(map[string]*xmlEnum)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".gschema.xml") && !strings.HasSuffix(name, ".enums.xml") {
			continue
		}
		list, err := parseSchemaFile(filepath.Join(dir, name))
		if err != nil {
			continue // skip malformed files
		}
		for i := range list.Enums {
			enumIndex[list.Enums[i].ID] = &list.Enums[i]
		}
		for i := range list.Flags {
			enumIndex[list.Flags[i].ID] = &list.Flags[i]
		}
	}

	// Second pass: build the schema list (only *.gschema.xml carry <schema> elements).
	var schemas []*pb.GSettingsSchema
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".gschema.xml") {
			continue
		}
		list, err := parseSchemaFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		for _, xs := range list.Schemas {
			schemas = append(schemas, xmlSchemaToProto(&xs, enumIndex))
		}
	}

	return schemas, nil
}

// parseSchemaFile parses a single .gschema.xml file.
func parseSchemaFile(path string) (*xmlSchemaList, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path constructed from trusted dir
	if err != nil {
		return nil, err
	}
	var list xmlSchemaList
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &list, nil
}

// xmlSchemaToProto converts a parsed XML schema to a GSettingsSchema proto message.
func xmlSchemaToProto(xs *xmlSchema, enumIndex map[string]*xmlEnum) *pb.GSettingsSchema {
	relocatable := xs.Path == ""
	schema := &pb.GSettingsSchema{
		SchemaId:    xs.ID,
		Path:        xs.Path,
		Relocatable: relocatable,
	}

	for i := range xs.Keys {
		xk := &xs.Keys[i]
		key := &pb.GSettingsKey{
			Name:         xk.Name,
			Summary:      strings.TrimSpace(xk.Summary),
			Description:  strings.TrimSpace(xk.Description),
			DefaultValue: strings.TrimSpace(xk.Default.Value),
		}

		// Resolve type: prefer explicit type attr, fall back to enum/flags reference.
		switch {
		case xk.Type != "":
			key.Type = xk.Type
		case xk.Enum != "":
			key.Type = "s" // enums stored as string nick
		case xk.Flags != "":
			key.Type = "as" // flags stored as array of string nicks
		}

		// Enum values.
		if xk.Enum != "" {
			if ev, ok := enumIndex[xk.Enum]; ok {
				for _, v := range ev.Values {
					key.EnumValues = append(key.EnumValues, &pb.GSettingsEnumValue{
						Nick:  v.Nick,
						Value: v.Value,
					})
				}
			}
		}
		if xk.Flags != "" {
			if fv, ok := enumIndex[xk.Flags]; ok {
				for _, v := range fv.Values {
					key.EnumValues = append(key.EnumValues, &pb.GSettingsEnumValue{
						Nick:  v.Nick,
						Value: v.Value,
					})
				}
			}
		}

		// Range constraints.
		if xk.Range != nil {
			key.RangeMin = xk.Range.Min
			key.RangeMax = xk.Range.Max
		}

		// Choices.
		for _, c := range xk.Choices {
			key.Choices = append(key.Choices, c.Value)
		}

		schema.Keys = append(schema.Keys, key)
	}

	return schema
}
