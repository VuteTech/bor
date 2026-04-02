// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// DConfRepository handles dconf schema catalogue database operations.
type DConfRepository struct {
	db *DB
}

// NewDConfRepository creates a new DConfRepository.
func NewDConfRepository(db *DB) *DConfRepository {
	return &DConfRepository{db: db}
}

// UpsertSchema inserts or updates a single GSettings schema definition.
// The source parameter should be "builtin" or "agent".
func (r *DConfRepository) UpsertSchema(ctx context.Context, schema *pb.GSettingsSchema, source string) error {
	keysJSON, err := marshalKeys(schema.GetKeys())
	if err != nil {
		return fmt.Errorf("dconf: marshal keys for %s: %w", schema.GetSchemaId(), err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO dconf_schemas (schema_id, path, relocatable, keys_json, source, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (schema_id) DO UPDATE
		  SET path        = EXCLUDED.path,
		      relocatable = EXCLUDED.relocatable,
		      keys_json   = EXCLUDED.keys_json,
		      source      = CASE WHEN dconf_schemas.source = 'builtin' THEN 'builtin' ELSE EXCLUDED.source END,
		      updated_at  = EXCLUDED.updated_at`,
		schema.GetSchemaId(),
		nullableString(schema.GetPath()),
		schema.GetRelocatable(),
		keysJSON,
		source,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("dconf: upsert schema %s: %w", schema.GetSchemaId(), err)
	}
	return nil
}

// ReplaceNodeSchemas replaces the full set of schema IDs for a node.
func (r *DConfRepository) ReplaceNodeSchemas(ctx context.Context, nodeID string, schemaIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("dconf: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_dconf_schemas WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("dconf: delete node schemas: %w", err)
	}

	if len(schemaIDs) > 0 {
		for _, sid := range schemaIDs {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO node_dconf_schemas (node_id, schema_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				nodeID, sid,
			); err != nil {
				return fmt.Errorf("dconf: insert node schema %s: %w", sid, err)
			}
		}
	}

	return tx.Commit()
}

// ListSchemasByNode returns schemas available on the given node (via the
// node_dconf_schemas join table).
func (r *DConfRepository) ListSchemasByNode(ctx context.Context, nodeID string) ([]*pb.GSettingsSchema, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ds.schema_id, ds.path, ds.relocatable, ds.keys_json
		FROM dconf_schemas ds
		JOIN node_dconf_schemas nds ON nds.schema_id = ds.schema_id
		WHERE nds.node_id = $1
		ORDER BY ds.schema_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("dconf: list schemas by node: %w", err)
	}
	defer rows.Close()

	var schemas []*pb.GSettingsSchema
	for rows.Next() {
		var schemaID, keysJSON string
		var path *string
		var relocatable bool
		if err := rows.Scan(&schemaID, &path, &relocatable, &keysJSON); err != nil {
			return nil, fmt.Errorf("dconf: scan schema: %w", err)
		}
		keys, err := unmarshalKeys(keysJSON)
		if err != nil {
			return nil, fmt.Errorf("dconf: unmarshal keys for %s: %w", schemaID, err)
		}
		s := &pb.GSettingsSchema{
			SchemaId:    schemaID,
			Relocatable: relocatable,
			Keys:        keys,
		}
		if path != nil {
			s.Path = *path
		}
		schemas = append(schemas, s)
	}
	return schemas, rows.Err()
}

// UpsertComplianceResult inserts or updates a compliance result for a (node, policy) pair.
func (r *DConfRepository) UpsertComplianceResult(ctx context.Context, nodeID, policyID, statusStr, message string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO compliance_results (node_id, policy_id, status, message, reported_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (node_id, policy_id) DO UPDATE
		  SET status      = EXCLUDED.status,
		      message     = EXCLUDED.message,
		      reported_at = EXCLUDED.reported_at`,
		nodeID, policyID, statusStr, nullableString(message),
	)
	if err != nil {
		return fmt.Errorf("dconf: upsert compliance result: %w", err)
	}
	return nil
}

// ComplianceRow is a single compliance result row with joined names.
type ComplianceRow struct {
	NodeID      string  `json:"node_id"`
	NodeName    string  `json:"node_name"`
	PolicyID    string  `json:"policy_id"`
	PolicyName  string  `json:"policy_name"`
	Status      string  `json:"status"`
	Message     *string `json:"message,omitempty"`
	ReportedAt  string  `json:"reported_at"`
}

// ListComplianceResults returns all compliance results joined with node and policy names.
func (r *DConfRepository) ListComplianceResults(ctx context.Context) ([]*ComplianceRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT cr.node_id, n.name, cr.policy_id, p.name, cr.status, cr.message, cr.reported_at
		FROM compliance_results cr
		JOIN nodes    n ON n.id    = cr.node_id
		JOIN policies p ON p.id    = cr.policy_id
		ORDER BY cr.reported_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("dconf: list compliance results: %w", err)
	}
	defer rows.Close()

	var results []*ComplianceRow
	for rows.Next() {
		var cr ComplianceRow
		var reportedAt interface{}
		if err := rows.Scan(&cr.NodeID, &cr.NodeName, &cr.PolicyID, &cr.PolicyName, &cr.Status, &cr.Message, &reportedAt); err != nil {
			return nil, fmt.Errorf("dconf: scan compliance row: %w", err)
		}
		switch v := reportedAt.(type) {
		case []byte:
			cr.ReportedAt = string(v)
		case string:
			cr.ReportedAt = v
		default:
			cr.ReportedAt = fmt.Sprintf("%v", v)
		}
		results = append(results, &cr)
	}
	return results, rows.Err()
}

// ListSchemas returns all schemas in the catalogue.
func (r *DConfRepository) ListSchemas(ctx context.Context) ([]*pb.GSettingsSchema, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT schema_id, path, relocatable, keys_json FROM dconf_schemas ORDER BY schema_id`)
	if err != nil {
		return nil, fmt.Errorf("dconf: list schemas: %w", err)
	}
	defer rows.Close()

	var schemas []*pb.GSettingsSchema
	for rows.Next() {
		var schemaID, keysJSON string
		var path *string
		var relocatable bool
		if err := rows.Scan(&schemaID, &path, &relocatable, &keysJSON); err != nil {
			return nil, fmt.Errorf("dconf: scan schema: %w", err)
		}
		keys, err := unmarshalKeys(keysJSON)
		if err != nil {
			return nil, fmt.Errorf("dconf: unmarshal keys for %s: %w", schemaID, err)
		}
		s := &pb.GSettingsSchema{
			SchemaId:   schemaID,
			Relocatable: relocatable,
			Keys:       keys,
		}
		if path != nil {
			s.Path = *path
		}
		schemas = append(schemas, s)
	}
	return schemas, rows.Err()
}

// marshalKeys serialises a slice of GSettingsKey to a JSON array for storage.
// Each element is a protojson-encoded GSettingsKey object.
func marshalKeys(keys []*pb.GSettingsKey) ([]byte, error) {
	m := protojson.MarshalOptions{EmitUnpopulated: false}
	arr := make([]json.RawMessage, 0, len(keys))
	for _, k := range keys {
		b, err := m.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("marshal key %q: %w", k.GetName(), err)
		}
		arr = append(arr, json.RawMessage(b))
	}
	return json.Marshal(arr)
}

// unmarshalKeys deserialises a JSON array of protojson-encoded GSettingsKey objects.
func unmarshalKeys(raw string) ([]*pb.GSettingsKey, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil, err
	}
	keys := make([]*pb.GSettingsKey, 0, len(arr))
	for _, elem := range arr {
		var k pb.GSettingsKey
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(elem, &k); err != nil {
			// Skip malformed entries rather than failing the whole list.
			continue
		}
		keys = append(keys, &k)
	}
	return keys, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
