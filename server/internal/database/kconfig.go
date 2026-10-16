// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/lib/pq"

	pb "github.com/VuteTech/Bor/server/pkg/grpc/policy"
	"google.golang.org/protobuf/encoding/protojson"
)

// KConfigRepository stores the KDE settings catalogue: the built-in entries
// seeded at startup and the KConfigXT schema entries and System Settings
// modules that nodes report.
type KConfigRepository struct {
	db *DB
}

// NewKConfigRepository creates a new KConfigRepository.
func NewKConfigRepository(db *DB) *KConfigRepository {
	return &KConfigRepository{db: db}
}

var kconfigEntryMarshal = protojson.MarshalOptions{UseProtoNames: false}

// UpsertEntry inserts or updates one catalogue entry. source is "builtin"
// or "kcfg"; a built-in row is never downgraded to a node-reported one.
func (r *KConfigRepository) UpsertEntry(ctx context.Context, e *pb.KConfigCatalogueEntry, source string) error {
	if e.GetId() == "" {
		return fmt.Errorf("kconfig: entry without id")
	}
	e.Source = source
	data, err := kconfigEntryMarshal.Marshal(e)
	if err != nil {
		return fmt.Errorf("kconfig: marshal entry %s: %w", e.GetId(), err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO kconfig_catalogue_entries (id, source, entry_json, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		  SET entry_json = CASE WHEN kconfig_catalogue_entries.source = 'builtin' AND EXCLUDED.source <> 'builtin'
		                        THEN kconfig_catalogue_entries.entry_json ELSE EXCLUDED.entry_json END,
		      source     = CASE WHEN kconfig_catalogue_entries.source = 'builtin' THEN 'builtin' ELSE EXCLUDED.source END,
		      updated_at = EXCLUDED.updated_at`,
		e.GetId(), source, string(data), time.Now(),
	)
	if err != nil {
		return fmt.Errorf("kconfig: upsert entry %s: %w", e.GetId(), err)
	}
	return nil
}

// ReplaceNodeEntries replaces the set of catalogue entries a node reported.
func (r *KConfigRepository) ReplaceNodeEntries(ctx context.Context, nodeID string, entryIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kconfig: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_kconfig_entries WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("kconfig: delete node entries: %w", err)
	}
	if len(entryIDs) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_kconfig_entries (node_id, entry_id)
			SELECT $1, unnest($2::varchar[])
			ON CONFLICT DO NOTHING`, nodeID, pq.Array(entryIDs)); err != nil {
			return fmt.Errorf("kconfig: insert node entries: %w", err)
		}
	}
	return tx.Commit()
}

// ReplaceNodeKCMs replaces the System Settings modules a node reported.
func (r *KConfigRepository) ReplaceNodeKCMs(ctx context.Context, nodeID string, modules []*pb.KcmModule) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kconfig: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_kcm_modules WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("kconfig: delete node modules: %w", err)
	}
	if len(modules) > 0 {
		ids := make([]string, 0, len(modules))
		names := make([]string, 0, len(modules))
		for _, m := range modules {
			if m.GetId() == "" {
				continue
			}
			ids = append(ids, m.GetId())
			names = append(names, m.GetName())
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_kcm_modules (node_id, module_id, name)
			SELECT $1, * FROM unnest($2::varchar[], $3::varchar[])
			ON CONFLICT DO NOTHING`, nodeID, pq.Array(ids), pq.Array(names)); err != nil {
			return fmt.Errorf("kconfig: insert node modules: %w", err)
		}
	}
	return tx.Commit()
}

// ListEntries returns the whole catalogue, built-in and node-reported.
func (r *KConfigRepository) ListEntries(ctx context.Context) ([]*pb.KConfigCatalogueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT entry_json, source FROM kconfig_catalogue_entries ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("kconfig: list entries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanKConfigEntries(rows)
}

// ListEntriesByNode returns the catalogue entries one node reported.
func (r *KConfigRepository) ListEntriesByNode(ctx context.Context, nodeID string) ([]*pb.KConfigCatalogueEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.entry_json, e.source
		FROM kconfig_catalogue_entries e
		JOIN node_kconfig_entries ne ON ne.entry_id = e.id
		WHERE ne.node_id = $1
		ORDER BY e.id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("kconfig: list entries by node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanKConfigEntries(rows)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanKConfigEntries(rows rowScanner) ([]*pb.KConfigCatalogueEntry, error) {
	var out []*pb.KConfigCatalogueEntry
	opts := protojson.UnmarshalOptions{DiscardUnknown: true}
	for rows.Next() {
		var raw, source string
		if err := rows.Scan(&raw, &source); err != nil {
			return nil, fmt.Errorf("kconfig: scan entry: %w", err)
		}
		var e pb.KConfigCatalogueEntry
		if err := opts.Unmarshal([]byte(raw), &e); err != nil {
			return nil, fmt.Errorf("kconfig: unmarshal entry: %w", err)
		}
		e.Source = source
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ListKCMs returns every System Settings module any node reported, with
// one display name per module id.
func (r *KConfigRepository) ListKCMs(ctx context.Context) ([]*pb.KcmModule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT module_id, MAX(name) FROM node_kcm_modules GROUP BY module_id ORDER BY module_id`)
	if err != nil {
		return nil, fmt.Errorf("kconfig: list modules: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanKCMs(rows)
}

// ListKCMsByNode returns the System Settings modules one node reported.
func (r *KConfigRepository) ListKCMsByNode(ctx context.Context, nodeID string) ([]*pb.KcmModule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT module_id, name FROM node_kcm_modules WHERE node_id = $1 ORDER BY module_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("kconfig: list modules by node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanKCMs(rows)
}

func scanKCMs(rows rowScanner) ([]*pb.KcmModule, error) {
	var out []*pb.KcmModule
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("kconfig: scan module: %w", err)
		}
		out = append(out, &pb.KcmModule{Id: id, Name: name})
	}
	return out, rows.Err()
}

// ReplaceNodeLookAndFeel replaces the global themes recorded for a node.
func (r *KConfigRepository) ReplaceNodeLookAndFeel(ctx context.Context, nodeID string, themes []*pb.LookAndFeelPackage) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("kconfig: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	if _, err := tx.ExecContext(ctx, `DELETE FROM node_lookandfeel_packages WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("kconfig: delete node themes: %w", err)
	}
	if len(themes) > 0 {
		ids := make([]string, 0, len(themes))
		names := make([]string, 0, len(themes))
		for _, t := range themes {
			if t.GetId() == "" {
				continue
			}
			ids = append(ids, t.GetId())
			names = append(names, t.GetName())
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_lookandfeel_packages (node_id, package_id, name)
			SELECT $1, * FROM unnest($2::varchar[], $3::varchar[])
			ON CONFLICT DO NOTHING`, nodeID, pq.Array(ids), pq.Array(names)); err != nil {
			return fmt.Errorf("kconfig: insert node themes: %w", err)
		}
	}
	return tx.Commit()
}

// ListLookAndFeel returns every global theme any node reported, with one
// display name per package id.
func (r *KConfigRepository) ListLookAndFeel(ctx context.Context) ([]*pb.LookAndFeelPackage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT package_id, MAX(name) FROM node_lookandfeel_packages GROUP BY package_id ORDER BY package_id`)
	if err != nil {
		return nil, fmt.Errorf("kconfig: list themes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanLookAndFeel(rows)
}

// ListLookAndFeelByNode returns the global themes one node reported.
func (r *KConfigRepository) ListLookAndFeelByNode(ctx context.Context, nodeID string) ([]*pb.LookAndFeelPackage, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT package_id, name FROM node_lookandfeel_packages WHERE node_id = $1 ORDER BY package_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("kconfig: list themes by node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanLookAndFeel(rows)
}

func scanLookAndFeel(rows rowScanner) ([]*pb.LookAndFeelPackage, error) {
	var out []*pb.LookAndFeelPackage
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("kconfig: scan theme: %w", err)
		}
		out = append(out, &pb.LookAndFeelPackage{Id: id, Name: name})
	}
	return out, rows.Err()
}
