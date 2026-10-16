// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path"
	"sort"
	"strings"
)

// ExternalIdentityRepository links identities at external identity
// providers to Bor users.
type ExternalIdentityRepository struct {
	db *DB
}

// NewExternalIdentityRepository creates a new ExternalIdentityRepository.
func NewExternalIdentityRepository(db *DB) *ExternalIdentityRepository {
	return &ExternalIdentityRepository{db: db}
}

// FindUserID returns the Bor user linked to (provider, subject), or "" when
// there is no link.
func (r *ExternalIdentityRepository) FindUserID(ctx context.Context, provider, subject string) (string, error) {
	var userID string
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id FROM user_external_identities WHERE provider = $1 AND subject = $2`,
		provider, subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to look up external identity: %w", err)
	}
	return userID, nil
}

// Link records that (provider, subject) is userID. Linking an identity that
// is already linked to a different user fails.
func (r *ExternalIdentityRepository) Link(ctx context.Context, provider, subject, userID string) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO user_external_identities (provider, subject, user_id) VALUES ($1, $2, $3)
		 ON CONFLICT (provider, subject) DO UPDATE SET user_id = EXCLUDED.user_id
		 WHERE user_external_identities.user_id = EXCLUDED.user_id`,
		provider, subject, userID)
	if err != nil {
		return fmt.Errorf("failed to link external identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("external identity is already linked to another user")
	}
	return nil
}

// TouchLogin records a successful login through the identity.
func (r *ExternalIdentityRepository) TouchLogin(ctx context.Context, provider, subject string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE user_external_identities SET last_login_at = NOW() WHERE provider = $1 AND subject = $2`,
		provider, subject)
	if err != nil {
		return fmt.Errorf("failed to record external login: %w", err)
	}
	return nil
}

// RunModuleMigrations applies the *.up.sql files in dir of fsys for a
// server extension module, in name order, each in its own transaction, and
// records them in ext_schema_migrations under the module's name.
func (db *DB) RunModuleMigrations(module string, fsys fs.FS, dir string) error {
	if module == "" {
		return fmt.Errorf("module name is required")
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("failed to read %s migrations: %w", module, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")
		var exists bool
		if err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM ext_schema_migrations WHERE module = $1 AND version = $2)`,
			module, version).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check %s migration %s: %w", module, version, err)
		}
		if exists {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return fmt.Errorf("failed to read %s migration %s: %w", module, name, err)
		}
		if err := db.applyModuleMigration(module, version, string(content)); err != nil {
			return err
		}
		log.Printf("Applied %s migration: %s", module, version)
	}
	return nil
}

func (db *DB) applyModuleMigration(module, version, content string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin %s migration %s: %w", module, version, err)
	}
	if _, err := tx.Exec(content); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to execute %s migration %s: %w", module, version, err)
	}
	if _, err := tx.Exec(`INSERT INTO ext_schema_migrations (module, version) VALUES ($1, $2)`, module, version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to record %s migration %s: %w", module, version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit %s migration %s: %w", module, version, err)
	}
	return nil
}
