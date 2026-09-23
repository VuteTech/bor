// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
	"github.com/lib/pq"
)

// Sentinel errors returned by LuksRepository.
var (
	ErrLuksVolumeNotFound  = errors.New("LUKS volume not found")
	ErrRecoveryKeyNotFound = errors.New("recovery key not found")
	ErrTangServerNotFound  = errors.New("tang server not found")
	ErrTangServerConflict  = errors.New("a tang server with that name or URL already exists")
)

// LuksRepository handles luks_volumes, node_disk_encryption,
// luks_recovery_keys and tang_servers.
type LuksRepository struct {
	db *DB
}

// NewLuksRepository creates a new LuksRepository.
func NewLuksRepository(db *DB) *LuksRepository {
	return &LuksRepository{db: db}
}

// RecoveryKeyRow is the full recovery-key row including the sealed secret
// columns and the owning volume's identifiers (needed to rebuild the AAD).
// It never leaves the database/services layer.
type RecoveryKeyRow struct {
	models.LuksRecoveryKeyMeta
	KEKID      string
	WrappedDEK []byte
	Ciphertext []byte
	VolumeUUID string
	NodeID     *string
}

// ─── Volumes ─────────────────────────────────────────────────────────────

const luksVolumeColumns = `v.id, v.node_id, v.node_name, v.machine_id, v.luks_uuid, v.mapping_name,
	v.mountpoints, v.is_system, v.luks_version, v.cipher, v.volume_key_bits, v.state_json,
	v.rotation_requested_at, v.rotation_reason, v.last_reported_at, v.orphaned_at, v.created_at`

type luksRowScanner interface {
	Scan(dest ...interface{}) error
}

func scanLuksVolume(s luksRowScanner, withActiveKey bool) (*models.LuksVolume, error) {
	var (
		v              models.LuksVolume
		nodeID         sql.NullString
		mountpoints    pq.StringArray
		rotationAt     sql.NullTime
		rotationReason sql.NullString
		orphanedAt     sql.NullTime
		stateJSON      []byte
	)
	dest := []interface{}{
		&v.ID, &nodeID, &v.NodeName, &v.MachineID, &v.LuksUUID, &v.MappingName,
		&mountpoints, &v.IsSystem, &v.LuksVersion, &v.Cipher, &v.VolumeKeyBits, &stateJSON,
		&rotationAt, &rotationReason, &v.LastReportedAt, &orphanedAt, &v.CreatedAt,
	}
	var (
		keyID          sql.NullString
		keyStatus      sql.NullString
		keyReason      sql.NullString
		keySlot        sql.NullInt64
		keyFingerprint sql.NullString
		keyCreatedAt   sql.NullTime
		keyConfirmedAt sql.NullTime
		keyRevealCount sql.NullInt64
		keyRevealedAt  sql.NullTime
		keyRevealedBy  sql.NullString
	)
	if withActiveKey {
		dest = append(dest, &keyID, &keyStatus, &keyReason, &keySlot, &keyFingerprint,
			&keyCreatedAt, &keyConfirmedAt, &keyRevealCount, &keyRevealedAt, &keyRevealedBy)
	}
	if err := s.Scan(dest...); err != nil {
		return nil, err
	}
	if nodeID.Valid {
		id := nodeID.String
		v.NodeID = &id
	}
	v.Mountpoints = []string(mountpoints)
	if v.Mountpoints == nil {
		v.Mountpoints = []string{}
	}
	v.StateJSON = stateJSON
	if rotationAt.Valid {
		t := rotationAt.Time
		v.RotationRequestedAt = &t
	}
	v.RotationReason = rotationReason.String
	if orphanedAt.Valid {
		t := orphanedAt.Time
		v.OrphanedAt = &t
	}
	if withActiveKey && keyID.Valid {
		meta := &models.LuksRecoveryKeyMeta{
			ID:                 keyID.String,
			VolumeID:           v.ID,
			Status:             keyStatus.String,
			Reason:             keyReason.String,
			KeyslotFingerprint: keyFingerprint.String,
			CreatedAt:          keyCreatedAt.Time,
			RevealCount:        int(keyRevealCount.Int64),
			LastRevealedBy:     keyRevealedBy.String,
		}
		if keySlot.Valid {
			slot := int(keySlot.Int64)
			meta.Keyslot = &slot
		}
		if keyConfirmedAt.Valid {
			t := keyConfirmedAt.Time
			meta.ConfirmedAt = &t
		}
		if keyRevealedAt.Valid {
			t := keyRevealedAt.Time
			meta.LastRevealedAt = &t
		}
		v.ActiveKey = meta
	}
	return &v, nil
}

// activeKeyJoin joins the volume's ACTIVE recovery key metadata.
const activeKeyJoin = ` LEFT JOIN luks_recovery_keys ak
	ON ak.volume_id = v.id AND ak.status = 'active'`

const activeKeyColumns = `, ak.id, ak.status, ak.reason, ak.keyslot, ak.keyslot_fingerprint,
	ak.created_at, ak.confirmed_at, ak.reveal_count, ak.last_revealed_at, ak.last_revealed_by`

// UpsertVolume inserts or updates a node's volume inventory row and returns
// the volume ID.
func (r *LuksRepository) UpsertVolume(ctx context.Context, nodeID, nodeName, machineID string, v *models.LuksVolume) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO luks_volumes
		  (node_id, node_name, machine_id, luks_uuid, mapping_name, mountpoints, is_system,
		   luks_version, cipher, volume_key_bits, state_json, last_reported_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
		ON CONFLICT (node_id, luks_uuid) WHERE node_id IS NOT NULL DO UPDATE SET
		  node_name = EXCLUDED.node_name,
		  machine_id = EXCLUDED.machine_id,
		  mapping_name = EXCLUDED.mapping_name,
		  mountpoints = EXCLUDED.mountpoints,
		  is_system = EXCLUDED.is_system,
		  luks_version = EXCLUDED.luks_version,
		  cipher = EXCLUDED.cipher,
		  volume_key_bits = EXCLUDED.volume_key_bits,
		  state_json = EXCLUDED.state_json,
		  last_reported_at = NOW()
		RETURNING id`,
		nodeID, nodeName, machineID, v.LuksUUID, v.MappingName, pq.Array(v.Mountpoints), v.IsSystem,
		v.LuksVersion, v.Cipher, v.VolumeKeyBits, []byte(v.StateJSON),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("luks: upsert volume: %w", err)
	}
	return id, nil
}

// GetVolume returns one volume by ID, with its ACTIVE key metadata.
func (r *LuksRepository) GetVolume(ctx context.Context, id string) (*models.LuksVolume, error) {
	v, err := scanLuksVolume(r.db.QueryRowContext(ctx,
		`SELECT `+luksVolumeColumns+activeKeyColumns+` FROM luks_volumes v`+activeKeyJoin+` WHERE v.id = $1`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLuksVolumeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("luks: get volume: %w", err)
	}
	return v, nil
}

// GetVolumeByNodeAndUUID returns a node's volume by LUKS UUID.
func (r *LuksRepository) GetVolumeByNodeAndUUID(ctx context.Context, nodeID, luksUUID string) (*models.LuksVolume, error) {
	v, err := scanLuksVolume(r.db.QueryRowContext(ctx,
		`SELECT `+luksVolumeColumns+activeKeyColumns+` FROM luks_volumes v`+activeKeyJoin+`
		 WHERE v.node_id = $1 AND v.luks_uuid = $2`, nodeID, luksUUID), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLuksVolumeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("luks: get volume by node and uuid: %w", err)
	}
	return v, nil
}

// ListVolumesByNode returns a node's volumes with ACTIVE key metadata.
func (r *LuksRepository) ListVolumesByNode(ctx context.Context, nodeID string) ([]*models.LuksVolume, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+luksVolumeColumns+activeKeyColumns+` FROM luks_volumes v`+activeKeyJoin+`
		 WHERE v.node_id = $1 ORDER BY v.is_system DESC, v.mapping_name ASC`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("luks: list volumes by node: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectVolumes(rows)
}

func collectVolumes(rows *sql.Rows) ([]*models.LuksVolume, error) {
	volumes := []*models.LuksVolume{}
	for rows.Next() {
		v, err := scanLuksVolume(rows, true)
		if err != nil {
			return nil, fmt.Errorf("luks: scan volume: %w", err)
		}
		volumes = append(volumes, v)
	}
	return volumes, rows.Err()
}

// SearchVolumes runs the paginated fleet-directory query. search matches the
// node name, mapping name, LUKS UUID prefix (with or without dashes) and
// escrow-id prefix, all case-insensitively.
func (r *LuksRepository) SearchVolumes(ctx context.Context, search, nodeID string, page, perPage int) ([]*models.LuksVolume, int, error) {
	where := []string{"TRUE"}
	args := []interface{}{}
	if nodeID != "" {
		args = append(args, nodeID)
		where = append(where, fmt.Sprintf("v.node_id = $%d", len(args)))
	}
	if s := strings.TrimSpace(search); s != "" {
		like := "%" + strings.ToLower(s) + "%"
		// UUID / escrow-id prefixes are typed with or without dashes.
		normalized := strings.ToLower(strings.ReplaceAll(s, "-", "")) + "%"
		args = append(args, like)
		likeIdx := len(args)
		args = append(args, normalized)
		normIdx := len(args)
		where = append(where, fmt.Sprintf(`(
			lower(v.node_name) LIKE $%d OR lower(v.mapping_name) LIKE $%d
			OR replace(v.luks_uuid::text, '-', '') LIKE $%d
			OR EXISTS (
				SELECT 1 FROM luks_recovery_keys k
				WHERE k.volume_id = v.id AND replace(k.id::text, '-', '') LIKE $%d
			))`, likeIdx, likeIdx, normIdx, normIdx))
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM luks_volumes v WHERE `+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("luks: count volumes: %w", err)
	}

	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+luksVolumeColumns+activeKeyColumns+` FROM luks_volumes v`+activeKeyJoin+`
		 WHERE `+whereClause+`
		 ORDER BY lower(v.node_name) ASC, v.is_system DESC, v.mapping_name ASC
		 LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("luks: search volumes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	volumes, err := collectVolumes(rows)
	if err != nil {
		return nil, 0, err
	}
	return volumes, total, nil
}

// SetRotationRequested records a pending rotation for a volume. It keeps the
// earliest request time when one is already pending.
func (r *LuksRepository) SetRotationRequested(ctx context.Context, volumeID, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE luks_volumes
		SET rotation_requested_at = COALESCE(rotation_requested_at, NOW()), rotation_reason = $2
		WHERE id = $1`, volumeID, reason)
	if err != nil {
		return fmt.Errorf("luks: set rotation requested: %w", err)
	}
	return nil
}

// ClearRotationRequested removes a volume's pending rotation task.
func (r *LuksRepository) ClearRotationRequested(ctx context.Context, volumeID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE luks_volumes SET rotation_requested_at = NULL, rotation_reason = NULL WHERE id = $1`, volumeID)
	if err != nil {
		return fmt.Errorf("luks: clear rotation requested: %w", err)
	}
	return nil
}

// CountDistinctNodesForUUID counts how many distinct nodes report a LUKS
// UUID (clone detection: >1 means a shared volume key).
func (r *LuksRepository) CountDistinctNodesForUUID(ctx context.Context, luksUUID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT node_id) FROM luks_volumes
		WHERE luks_uuid = $1 AND node_id IS NOT NULL`, luksUUID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("luks: count nodes for uuid: %w", err)
	}
	return n, nil
}

// MarkOrphanedVolumes stamps orphaned_at on volumes whose node was deleted.
// Returns the number of newly orphaned volumes.
func (r *LuksRepository) MarkOrphanedVolumes(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE luks_volumes SET orphaned_at = NOW()
		WHERE node_id IS NULL AND orphaned_at IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("luks: mark orphaned volumes: %w", err)
	}
	return res.RowsAffected()
}

// Summary computes the fleet StatCard counts.
func (r *LuksRepository) Summary(ctx context.Context) (*models.DiskEncryptionSummary, error) {
	s := &models.DiskEncryptionSummary{}
	err := r.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM luks_volumes),
		  (SELECT COUNT(DISTINCT node_id) FROM luks_volumes WHERE node_id IS NOT NULL),
		  (SELECT COALESCE(SUM(jsonb_array_length(unencrypted_system_mounts)), 0) FROM node_disk_encryption),
		  (SELECT COUNT(*) FROM luks_recovery_keys WHERE status = 'active'),
		  (SELECT COUNT(*) FROM luks_volumes WHERE rotation_requested_at IS NOT NULL),
		  (SELECT COUNT(*) FROM luks_volumes WHERE COALESCE(state_json->>'externallyManagedBy', '') <> ''),
		  (SELECT COUNT(*) FROM (
		     SELECT luks_uuid FROM luks_volumes WHERE node_id IS NOT NULL
		     GROUP BY luks_uuid HAVING COUNT(DISTINCT node_id) > 1) c)`).
		Scan(&s.Volumes, &s.EncryptedNodes, &s.UnencryptedMounts, &s.EscrowedActive,
			&s.RotationsOverdue, &s.ExternallyManaged, &s.CloneSuspects)
	if err != nil {
		return nil, fmt.Errorf("luks: summary: %w", err)
	}
	return s, nil
}

// ─── Node platform facts ─────────────────────────────────────────────────

// UpsertNodePlatform stores a node's platform facts.
func (r *LuksRepository) UpsertNodePlatform(ctx context.Context, p *models.NodeDiskEncryption) error {
	unencrypted := p.UnencryptedSystemMounts
	if len(unencrypted) == 0 {
		unencrypted = json.RawMessage("[]")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO node_disk_encryption
		  (node_id, tpm2_present, secure_boot, initramfs_generator, systemd_version,
		   cryptsetup_version, clevis_version, unencrypted_system_mounts, reported_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (node_id) DO UPDATE SET
		  tpm2_present = EXCLUDED.tpm2_present,
		  secure_boot = EXCLUDED.secure_boot,
		  initramfs_generator = EXCLUDED.initramfs_generator,
		  systemd_version = EXCLUDED.systemd_version,
		  cryptsetup_version = EXCLUDED.cryptsetup_version,
		  clevis_version = EXCLUDED.clevis_version,
		  unencrypted_system_mounts = EXCLUDED.unencrypted_system_mounts,
		  reported_at = NOW()`,
		p.NodeID, p.TPM2Present, p.SecureBoot, p.InitramfsGenerator, p.SystemdVersion,
		p.CryptsetupVersion, p.ClevisVersion, []byte(unencrypted))
	if err != nil {
		return fmt.Errorf("luks: upsert node platform: %w", err)
	}
	return nil
}

// GetNodePlatform returns a node's platform facts, or nil when the node has
// never reported.
func (r *LuksRepository) GetNodePlatform(ctx context.Context, nodeID string) (*models.NodeDiskEncryption, error) {
	p := &models.NodeDiskEncryption{}
	var tpm sql.NullBool
	var unencrypted []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT node_id, tpm2_present, secure_boot, initramfs_generator, systemd_version,
		       cryptsetup_version, clevis_version, unencrypted_system_mounts, reported_at
		FROM node_disk_encryption WHERE node_id = $1`, nodeID).
		Scan(&p.NodeID, &tpm, &p.SecureBoot, &p.InitramfsGenerator, &p.SystemdVersion,
			&p.CryptsetupVersion, &p.ClevisVersion, &unencrypted, &p.ReportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("luks: get node platform: %w", err)
	}
	if tpm.Valid {
		b := tpm.Bool
		p.TPM2Present = &b
	}
	p.UnencryptedSystemMounts = unencrypted
	return p, nil
}

// ─── Recovery keys ───────────────────────────────────────────────────────

const recoveryKeyMetaColumns = `k.id, k.volume_id, k.status, k.reason, k.keyslot, k.keyslot_fingerprint,
	k.created_at, k.confirmed_at, k.retired_at, k.destroy_after, k.reveal_count,
	k.last_revealed_at, k.last_revealed_by, k.released_at`

func scanRecoveryKeyMeta(s luksRowScanner) (*models.LuksRecoveryKeyMeta, error) {
	var (
		m           models.LuksRecoveryKeyMeta
		keyslot     sql.NullInt64
		fingerprint sql.NullString
		confirmedAt sql.NullTime
		retiredAt   sql.NullTime
		destroyAt   sql.NullTime
		revealedAt  sql.NullTime
		revealedBy  sql.NullString
		releasedAt  sql.NullTime
	)
	if err := s.Scan(&m.ID, &m.VolumeID, &m.Status, &m.Reason, &keyslot, &fingerprint,
		&m.CreatedAt, &confirmedAt, &retiredAt, &destroyAt, &m.RevealCount,
		&revealedAt, &revealedBy, &releasedAt); err != nil {
		return nil, err
	}
	if keyslot.Valid {
		slot := int(keyslot.Int64)
		m.Keyslot = &slot
	}
	m.KeyslotFingerprint = fingerprint.String
	if confirmedAt.Valid {
		t := confirmedAt.Time
		m.ConfirmedAt = &t
	}
	if retiredAt.Valid {
		t := retiredAt.Time
		m.RetiredAt = &t
	}
	if destroyAt.Valid {
		t := destroyAt.Time
		m.DestroyAfter = &t
	}
	if revealedAt.Valid {
		t := revealedAt.Time
		m.LastRevealedAt = &t
	}
	m.LastRevealedBy = revealedBy.String
	if releasedAt.Valid {
		t := releasedAt.Time
		m.ReleasedAt = &t
	}
	return &m, nil
}

// InsertPendingKey stores a freshly escrowed key in PENDING state. The id is
// the agent-chosen escrow id; re-sending the same id is idempotent (the
// insert is skipped when the row already exists for the same volume).
func (r *LuksRepository) InsertPendingKey(ctx context.Context, escrowID, volumeID, reason, kekID string, wrappedDEK, ciphertext []byte) error {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO luks_recovery_keys (id, volume_id, status, reason, kek_id, wrapped_dek, ciphertext)
		VALUES ($1, $2, 'pending', $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING`,
		escrowID, volumeID, reason, kekID, wrappedDEK, ciphertext)
	if err != nil {
		return fmt.Errorf("luks: insert pending key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Idempotent retry: accept only when the existing row belongs to
		// the same volume (otherwise an id collision across volumes).
		var existingVolume string
		if err := r.db.QueryRowContext(ctx,
			`SELECT volume_id FROM luks_recovery_keys WHERE id = $1`, escrowID).Scan(&existingVolume); err != nil {
			return fmt.Errorf("luks: check existing escrow id: %w", err)
		}
		if existingVolume != volumeID {
			return fmt.Errorf("luks: escrow id %s already used by another volume", escrowID)
		}
	}
	return nil
}

// CountKeysCreatedSince counts a volume's keys created after the cut-off
// (escrow rate limit).
func (r *LuksRepository) CountKeysCreatedSince(ctx context.Context, volumeID string, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM luks_recovery_keys WHERE volume_id = $1 AND created_at > $2`,
		volumeID, since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("luks: count keys created since: %w", err)
	}
	return n, nil
}

// GetKeyMeta returns one key's metadata.
func (r *LuksRepository) GetKeyMeta(ctx context.Context, escrowID string) (*models.LuksRecoveryKeyMeta, error) {
	m, err := scanRecoveryKeyMeta(r.db.QueryRowContext(ctx,
		`SELECT `+recoveryKeyMetaColumns+` FROM luks_recovery_keys k WHERE k.id = $1`, escrowID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecoveryKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("luks: get key meta: %w", err)
	}
	return m, nil
}

// ListKeysByVolume returns a volume's keys, newest first, without secrets.
func (r *LuksRepository) ListKeysByVolume(ctx context.Context, volumeID string) ([]*models.LuksRecoveryKeyMeta, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+recoveryKeyMetaColumns+` FROM luks_recovery_keys k
		 WHERE k.volume_id = $1 ORDER BY k.created_at DESC`, volumeID)
	if err != nil {
		return nil, fmt.Errorf("luks: list keys by volume: %w", err)
	}
	defer func() { _ = rows.Close() }()
	keys := []*models.LuksRecoveryKeyMeta{}
	for rows.Next() {
		m, err := scanRecoveryKeyMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("luks: scan key: %w", err)
		}
		keys = append(keys, m)
	}
	return keys, rows.Err()
}

// getKeyRow loads a full key row (with sealed secrets) by a WHERE clause.
func (r *LuksRepository) getKeyRow(ctx context.Context, where string, args ...interface{}) (*RecoveryKeyRow, error) {
	row := &RecoveryKeyRow{}
	var (
		kekID       sql.NullString
		keyslot     sql.NullInt64
		fingerprint sql.NullString
		confirmedAt sql.NullTime
		releasedAt  sql.NullTime
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT k.id, k.volume_id, k.status, k.reason, k.kek_id, k.wrapped_dek, k.ciphertext,
		       k.keyslot, k.keyslot_fingerprint, k.created_at, k.confirmed_at, k.reveal_count,
		       k.released_at, v.luks_uuid, v.node_id
		FROM luks_recovery_keys k
		JOIN luks_volumes v ON v.id = k.volume_id
		WHERE `+where, args...).
		Scan(&row.ID, &row.VolumeID, &row.Status, &row.Reason, &kekID, &row.WrappedDEK, &row.Ciphertext,
			&keyslot, &fingerprint, &row.CreatedAt, &confirmedAt, &row.RevealCount,
			&releasedAt, &row.VolumeUUID, &row.NodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecoveryKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("luks: get key row: %w", err)
	}
	row.KEKID = kekID.String
	if keyslot.Valid {
		slot := int(keyslot.Int64)
		row.Keyslot = &slot
	}
	row.KeyslotFingerprint = fingerprint.String
	if confirmedAt.Valid {
		t := confirmedAt.Time
		row.ConfirmedAt = &t
	}
	if releasedAt.Valid {
		t := releasedAt.Time
		row.ReleasedAt = &t
	}
	return row, nil
}

// GetKeyRowByID loads a full key row by escrow id.
func (r *LuksRepository) GetKeyRowByID(ctx context.Context, escrowID string) (*RecoveryKeyRow, error) {
	return r.getKeyRow(ctx, "k.id = $1", escrowID)
}

// GetActiveKeyRow loads the ACTIVE key of a volume.
func (r *LuksRepository) GetActiveKeyRow(ctx context.Context, volumeID string) (*RecoveryKeyRow, error) {
	return r.getKeyRow(ctx, "k.volume_id = $1 AND k.status = 'active'", volumeID)
}

// ConfirmKey promotes a PENDING key to ACTIVE, moving the previous ACTIVE
// key (if any) to SUPERSEDED, in one transaction.
func (r *LuksRepository) ConfirmKey(ctx context.Context, escrowID, volumeID string, keyslot int, fingerprint string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("luks: begin confirm tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, supErr := tx.ExecContext(ctx, `
		UPDATE luks_recovery_keys SET status = 'superseded'
		WHERE volume_id = $1 AND status = 'active'`, volumeID); supErr != nil {
		return fmt.Errorf("luks: supersede active key: %w", supErr)
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE luks_recovery_keys
		SET status = 'active', keyslot = $3, keyslot_fingerprint = $4, confirmed_at = NOW()
		WHERE id = $1 AND volume_id = $2 AND status = 'pending'`,
		escrowID, volumeID, keyslot, fingerprint)
	if err != nil {
		return fmt.Errorf("luks: confirm key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRecoveryKeyNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("luks: commit confirm tx: %w", err)
	}
	return nil
}

// MarkReleased records a server-assisted release of the ACTIVE key.
func (r *LuksRepository) MarkReleased(ctx context.Context, escrowID, rotationID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE luks_recovery_keys SET released_at = NOW(), release_rotation_id = $2 WHERE id = $1`,
		escrowID, rotationID)
	if err != nil {
		return fmt.Errorf("luks: mark released: %w", err)
	}
	return nil
}

// RecordReveal increments the reveal counter and stores the actor.
func (r *LuksRepository) RecordReveal(ctx context.Context, escrowID, username string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE luks_recovery_keys
		SET reveal_count = reveal_count + 1, last_revealed_at = NOW(), last_revealed_by = $2
		WHERE id = $1`, escrowID, username)
	if err != nil {
		return fmt.Errorf("luks: record reveal: %w", err)
	}
	return nil
}

// RetireSupersededKeys retires a volume's SUPERSEDED keys whose keyslot
// fingerprint is no longer present on the disk. Returns the retired ids.
func (r *LuksRepository) RetireSupersededKeys(ctx context.Context, volumeID string, presentFingerprints []string, destroyAfter time.Time) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE luks_recovery_keys
		SET status = 'retired', retired_at = NOW(), destroy_after = $3
		WHERE volume_id = $1 AND status = 'superseded'
		  AND (keyslot_fingerprint IS NULL OR NOT (keyslot_fingerprint = ANY($2)))
		RETURNING id`, volumeID, pq.Array(presentFingerprints), destroyAfter)
	if err != nil {
		return nil, fmt.Errorf("luks: retire superseded keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("luks: scan retired id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ExpirePendingKeys expires PENDING keys older than the cut-off and
// crypto-shreds them (a key that never had a slot has nothing to recover).
// Returns the expired ids.
func (r *LuksRepository) ExpirePendingKeys(ctx context.Context, cutoff time.Time) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE luks_recovery_keys
		SET status = 'expired', kek_id = NULL, wrapped_dek = NULL, ciphertext = NULL
		WHERE status = 'pending' AND created_at < $1
		RETURNING id`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("luks: expire pending keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("luks: scan expired id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DestroyExpiredKeys crypto-shreds RETIRED keys past destroy_after and every
// key of a volume orphaned longer than orphanCutoff. Returns the ids.
func (r *LuksRepository) DestroyExpiredKeys(ctx context.Context, now, orphanCutoff time.Time) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE luks_recovery_keys k
		SET status = 'destroyed', kek_id = NULL, wrapped_dek = NULL, ciphertext = NULL
		WHERE k.status NOT IN ('destroyed', 'expired')
		  AND (
		    (k.status = 'retired' AND k.destroy_after IS NOT NULL AND k.destroy_after < $1)
		    OR k.volume_id IN (
		      SELECT id FROM luks_volumes WHERE orphaned_at IS NOT NULL AND orphaned_at < $2)
		  )
		RETURNING k.id`, now, orphanCutoff)
	if err != nil {
		return nil, fmt.Errorf("luks: destroy keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("luks: scan destroyed id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListUnconfirmedReleases returns ACTIVE keys released before the cut-off
// whose rotation never completed. The release marker is cleared so each
// release alerts once; the audit log keeps the full history.
func (r *LuksRepository) ListUnconfirmedReleases(ctx context.Context, cutoff time.Time) ([]*RecoveryKeyRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		UPDATE luks_recovery_keys k
		SET release_rotation_id = NULL
		FROM luks_volumes v
		WHERE v.id = k.volume_id
		  AND k.status = 'active' AND k.release_rotation_id IS NOT NULL AND k.released_at < $1
		RETURNING k.id, k.volume_id, v.luks_uuid, v.node_name`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("luks: list unconfirmed releases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*RecoveryKeyRow
	for rows.Next() {
		row := &RecoveryKeyRow{}
		var nodeName string
		if err := rows.Scan(&row.ID, &row.VolumeID, &row.VolumeUUID, &nodeName); err != nil {
			return nil, fmt.Errorf("luks: scan unconfirmed release: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListKeysForRewrap returns up to limit keys wrapped with a KEK other than
// currentKEKID, including their sealed secrets.
func (r *LuksRepository) ListKeysForRewrap(ctx context.Context, currentKEKID string, limit int) ([]*RecoveryKeyRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT k.id, k.volume_id, k.status, k.kek_id, k.wrapped_dek, k.ciphertext, v.luks_uuid
		FROM luks_recovery_keys k
		JOIN luks_volumes v ON v.id = k.volume_id
		WHERE k.kek_id IS NOT NULL AND k.kek_id <> $1 AND k.ciphertext IS NOT NULL
		LIMIT $2`, currentKEKID, limit)
	if err != nil {
		return nil, fmt.Errorf("luks: list keys for rewrap: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*RecoveryKeyRow
	for rows.Next() {
		row := &RecoveryKeyRow{}
		if err := rows.Scan(&row.ID, &row.VolumeID, &row.Status, &row.KEKID, &row.WrappedDEK, &row.Ciphertext, &row.VolumeUUID); err != nil {
			return nil, fmt.Errorf("luks: scan rewrap row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateKeyWrapping stores a rewrapped key.
func (r *LuksRepository) UpdateKeyWrapping(ctx context.Context, escrowID, kekID string, wrappedDEK, ciphertext []byte) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE luks_recovery_keys SET kek_id = $2, wrapped_dek = $3, ciphertext = $4 WHERE id = $1`,
		escrowID, kekID, wrappedDEK, ciphertext)
	if err != nil {
		return fmt.Errorf("luks: update key wrapping: %w", err)
	}
	return nil
}

// ListRotationCandidates returns volumes with an ACTIVE key and no pending
// rotation, with the node id and the key's confirmation time. The caller
// applies each node's effective rotation interval.
func (r *LuksRepository) ListRotationCandidates(ctx context.Context) ([]*RecoveryKeyRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT k.id, k.volume_id, v.luks_uuid, v.node_id, k.confirmed_at
		FROM luks_recovery_keys k
		JOIN luks_volumes v ON v.id = k.volume_id
		WHERE k.status = 'active' AND k.confirmed_at IS NOT NULL
		  AND v.rotation_requested_at IS NULL AND v.node_id IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("luks: list rotation candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*RecoveryKeyRow
	for rows.Next() {
		row := &RecoveryKeyRow{}
		var confirmedAt sql.NullTime
		if err := rows.Scan(&row.ID, &row.VolumeID, &row.VolumeUUID, &row.NodeID, &confirmedAt); err != nil {
			return nil, fmt.Errorf("luks: scan rotation candidate: %w", err)
		}
		if confirmedAt.Valid {
			t := confirmedAt.Time
			row.ConfirmedAt = &t
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountVolumesByEscrowed counts volumes with and without an ACTIVE escrowed
// key (for the Prometheus collector).
func (r *LuksRepository) CountVolumesByEscrowed(ctx context.Context) (escrowed, notEscrowed int, err error) {
	err = r.db.QueryRowContext(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE ak.id IS NOT NULL),
		  COUNT(*) FILTER (WHERE ak.id IS NULL)
		FROM luks_volumes v
		LEFT JOIN luks_recovery_keys ak ON ak.volume_id = v.id AND ak.status = 'active'`).
		Scan(&escrowed, &notEscrowed)
	if err != nil {
		return 0, 0, fmt.Errorf("luks: count volumes by escrowed: %w", err)
	}
	return escrowed, notEscrowed, nil
}

// CountRotationsPending counts volumes with a pending rotation task.
func (r *LuksRepository) CountRotationsPending(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM luks_volumes WHERE rotation_requested_at IS NOT NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("luks: count rotations pending: %w", err)
	}
	return n, nil
}

// ─── Tang servers ────────────────────────────────────────────────────────

const tangServerColumns = `id, name, url, trusted_thumbprints, advertised_thumbprints, advertisement,
	last_check_at, last_check_status, last_check_error, created_by, created_at, updated_at`

func scanTangServer(s luksRowScanner) (*models.TangServer, error) {
	var (
		t          models.TangServer
		trusted    pq.StringArray
		advertised pq.StringArray
		adv        []byte
		checkAt    sql.NullTime
		checkErr   sql.NullString
	)
	if err := s.Scan(&t.ID, &t.Name, &t.URL, &trusted, &advertised, &adv,
		&checkAt, &t.LastCheckStatus, &checkErr, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.TrustedThumbprints = []string(trusted)
	t.AdvertisedThumbprints = []string(advertised)
	if t.AdvertisedThumbprints == nil {
		t.AdvertisedThumbprints = []string{}
	}
	t.Advertisement = adv
	if checkAt.Valid {
		at := checkAt.Time
		t.LastCheckAt = &at
	}
	t.LastCheckError = checkErr.String
	return &t, nil
}

// ListTangServers returns every registered Tang server.
func (r *LuksRepository) ListTangServers(ctx context.Context) ([]*models.TangServer, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+tangServerColumns+` FROM tang_servers ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("luks: list tang servers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	servers := []*models.TangServer{}
	for rows.Next() {
		t, err := scanTangServer(rows)
		if err != nil {
			return nil, fmt.Errorf("luks: scan tang server: %w", err)
		}
		servers = append(servers, t)
	}
	return servers, rows.Err()
}

// GetTangServer returns one Tang server by ID.
func (r *LuksRepository) GetTangServer(ctx context.Context, id string) (*models.TangServer, error) {
	t, err := scanTangServer(r.db.QueryRowContext(ctx,
		`SELECT `+tangServerColumns+` FROM tang_servers WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTangServerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("luks: get tang server: %w", err)
	}
	return t, nil
}

// CreateTangServer inserts a Tang server and fills ID and timestamps.
func (r *LuksRepository) CreateTangServer(ctx context.Context, t *models.TangServer) error {
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO tang_servers
		  (name, url, trusted_thumbprints, advertised_thumbprints, advertisement,
		   last_check_at, last_check_status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, created_at, updated_at`,
		t.Name, t.URL, pq.Array(t.TrustedThumbprints), pq.Array(t.AdvertisedThumbprints),
		nullableBytes(t.Advertisement), t.LastCheckAt, t.LastCheckStatus, t.CreatedBy,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrTangServerConflict
	}
	if err != nil {
		return fmt.Errorf("luks: create tang server: %w", err)
	}
	return nil
}

// UpdateTangServer updates a Tang server's editable fields.
func (r *LuksRepository) UpdateTangServer(ctx context.Context, t *models.TangServer) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE tang_servers SET
		  name = $1, url = $2, trusted_thumbprints = $3, updated_at = NOW()
		WHERE id = $4`,
		t.Name, t.URL, pq.Array(t.TrustedThumbprints), t.ID)
	if isUniqueViolation(err) {
		return ErrTangServerConflict
	}
	if err != nil {
		return fmt.Errorf("luks: update tang server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTangServerNotFound
	}
	return nil
}

// UpdateTangCheck stores the result of an advertisement check.
func (r *LuksRepository) UpdateTangCheck(ctx context.Context, id, status, checkErr string, advertised []string, advertisement []byte) error {
	if advertised == nil {
		advertised = []string{}
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE tang_servers SET
		  advertised_thumbprints = $2, advertisement = COALESCE($3, advertisement),
		  last_check_at = NOW(), last_check_status = $4, last_check_error = $5, updated_at = NOW()
		WHERE id = $1`,
		id, pq.Array(advertised), nullableBytes(advertisement), status, checkErr)
	if err != nil {
		return fmt.Errorf("luks: update tang check: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTangServerNotFound
	}
	return nil
}

// DeleteTangServer removes a Tang server.
func (r *LuksRepository) DeleteTangServer(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM tang_servers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("luks: delete tang server: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTangServerNotFound
	}
	return nil
}

// CountBoundVolumesByThumbprint counts, per Tang signing thumbprint, the
// volumes with a Clevis binding made against it (from inventory reports).
// This is the fleet view Tang itself lacks: it tells the admin when an old
// key's hidden files can be deleted.
func (r *LuksRepository) CountBoundVolumesByThumbprint(ctx context.Context) (map[string]int, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT thp, COUNT(DISTINCT v.id)
		FROM luks_volumes v,
		     jsonb_array_elements(COALESCE(v.state_json->'keyslots', '[]'::jsonb)) ks,
		     jsonb_array_elements_text(COALESCE(ks->'tangSigningThumbprints', '[]'::jsonb)) thp
		GROUP BY thp`)
	if err != nil {
		return nil, fmt.Errorf("luks: count bound volumes by thumbprint: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var thp string
		var n int
		if err := rows.Scan(&thp, &n); err != nil {
			return nil, fmt.Errorf("luks: scan thumbprint count: %w", err)
		}
		out[thp] = n
	}
	return out, rows.Err()
}
