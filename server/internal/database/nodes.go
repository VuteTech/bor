// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/VuteTech/Bor/server/internal/models"
)

// NodeRepository handles node database operations
type NodeRepository struct {
	db *DB
}

// NewNodeRepository creates a new NodeRepository
func NewNodeRepository(db *DB) *NodeRepository {
	return &NodeRepository{db: db}
}

// nodeSelect is the column list for all node SELECT queries.
const nodeSelect = `
	n.id, n.name, n.fqdn, n.machine_id, n.ip_address, n.os_name, n.os_version, n.desktop_env,
	n.agent_version, n.status_cached, n.status_reason, n.groups, n.notes,
	n.last_seen, n.created_at, n.updated_at`

const nodeFrom = `FROM nodes n`

func scanNode(row interface{ Scan(dest ...interface{}) error }) (*models.Node, error) {
	node := &models.Node{}
	err := row.Scan(
		&node.ID, &node.Name, &node.FQDN, &node.MachineID,
		&node.IPAddress, &node.OSName, &node.OSVersion, &node.DesktopEnv,
		&node.AgentVersion, &node.StatusCached, &node.StatusReason,
		&node.Groups, &node.Notes,
		&node.LastSeen, &node.CreatedAt, &node.UpdatedAt,
	)
	return node, err
}

// populateGroups loads group memberships for a slice of nodes in one query.
func (r *NodeRepository) populateGroups(ctx context.Context, nodes []*models.Node) error {
	if len(nodes) == 0 {
		return nil
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	query := fmt.Sprintf(`
		SELECT ngm.node_id, CAST(ngm.node_group_id AS TEXT), ng.name
		FROM node_group_members ngm
		JOIN node_groups ng ON ng.id = ngm.node_group_id
		WHERE ngm.node_id IN (%s)
		ORDER BY ngm.created_at`, strings.Join(placeholders, ","))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to load node group memberships: %w", err)
	}
	defer rows.Close()
	nodeMap := make(map[string]*models.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}
	for rows.Next() {
		var nodeID, groupID, groupName string
		if err := rows.Scan(&nodeID, &groupID, &groupName); err != nil {
			return fmt.Errorf("failed to scan group membership: %w", err)
		}
		if n, ok := nodeMap[nodeID]; ok {
			n.NodeGroupIDs = append(n.NodeGroupIDs, groupID)
			n.NodeGroupNames = append(n.NodeGroupNames, groupName)
		}
	}
	return rows.Err()
}

// Create inserts a new node into the database
func (r *NodeRepository) Create(ctx context.Context, node *models.Node) error {
	query := `
		INSERT INTO nodes (name, fqdn, machine_id, ip_address, os_version, desktop_env,
			agent_version, status_cached, status_reason, groups, notes, last_seen, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id`

	now := time.Now()
	node.CreatedAt = now
	node.UpdatedAt = now
	if node.StatusCached == "" {
		node.StatusCached = models.NodeStatusUnknown
	}

	err := r.db.QueryRowContext(ctx, query,
		node.Name, node.FQDN, node.MachineID, node.IPAddress,
		node.OSVersion, node.DesktopEnv, node.AgentVersion,
		node.StatusCached, node.StatusReason, node.Groups, node.Notes,
		node.LastSeen, node.CreatedAt, node.UpdatedAt,
	).Scan(&node.ID)
	if err != nil {
		return fmt.Errorf("failed to create node: %w", err)
	}

	return nil
}

// GetByID retrieves a node by ID
func (r *NodeRepository) GetByID(ctx context.Context, id string) (*models.Node, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE n.id = $1`, nodeSelect, nodeFrom)

	node, err := scanNode(r.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node by id: %w", err)
	}

	if err := r.populateGroups(ctx, []*models.Node{node}); err != nil {
		return nil, err
	}

	return node, nil
}

// GetByMachineID retrieves a node by machine ID
func (r *NodeRepository) GetByMachineID(ctx context.Context, machineID string) (*models.Node, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE n.machine_id = $1`, nodeSelect, nodeFrom)

	node, err := scanNode(r.db.QueryRowContext(ctx, query, machineID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node by machine_id: %w", err)
	}

	if err := r.populateGroups(ctx, []*models.Node{node}); err != nil {
		return nil, err
	}

	return node, nil
}

// ListAll returns all nodes ordered by last_seen descending
func (r *NodeRepository) ListAll(ctx context.Context) ([]*models.Node, error) {
	query := fmt.Sprintf(`SELECT %s %s ORDER BY n.last_seen DESC NULLS LAST`, nodeSelect, nodeFrom)

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*models.Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.populateGroups(ctx, nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

// ListByStatus returns nodes with a given status
func (r *NodeRepository) ListByStatus(ctx context.Context, status string) ([]*models.Node, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE n.status_cached = $1 ORDER BY n.last_seen DESC NULLS LAST`, nodeSelect, nodeFrom)

	rows, err := r.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes by status: %w", err)
	}
	defer rows.Close()

	var nodes []*models.Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.populateGroups(ctx, nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

// Search searches nodes by name, FQDN, IP address, or groups
func (r *NodeRepository) Search(ctx context.Context, term string) ([]*models.Node, error) {
	query := fmt.Sprintf(`SELECT %s %s
		WHERE n.name ILIKE $1 OR n.fqdn ILIKE $1 OR n.ip_address ILIKE $1 OR n.groups ILIKE $1
		ORDER BY n.last_seen DESC NULLS LAST`, nodeSelect, nodeFrom)

	pattern := "%" + term + "%"
	rows, err := r.db.QueryContext(ctx, query, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to search nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*models.Node
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.populateGroups(ctx, nodes); err != nil {
		return nil, err
	}

	return nodes, nil
}

// Update updates an existing node
func (r *NodeRepository) Update(ctx context.Context, node *models.Node) error {
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if node.Name != "" {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, node.Name)
		argIdx++
	}

	// Always update updated_at
	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	node.UpdatedAt = time.Now()
	args = append(args, node.UpdatedAt)
	argIdx++

	if len(setClauses) == 1 {
		// Only updated_at, nothing to change
		return nil
	}

	args = append(args, node.ID)
	query := fmt.Sprintf("UPDATE nodes SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	return nil
}

// UpdateFields updates specific fields of a node
func (r *NodeRepository) UpdateFields(ctx context.Context, id string, req *models.UpdateNodeRequest) error {
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Groups != nil {
		setClauses = append(setClauses, fmt.Sprintf("groups = $%d", argIdx))
		args = append(args, *req.Groups)
		argIdx++
	}
	if req.Notes != nil {
		setClauses = append(setClauses, fmt.Sprintf("notes = $%d", argIdx))
		args = append(args, *req.Notes)
		argIdx++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", argIdx))
	args = append(args, time.Now())
	argIdx++

	args = append(args, id)
	query := fmt.Sprintf("UPDATE nodes SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check affected rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("node not found")
	}

	return nil
}

// UpdateStatus updates the cached status of a node
func (r *NodeRepository) UpdateStatus(ctx context.Context, id, status, reason string) error {
	query := `UPDATE nodes SET status_cached = $1, status_reason = $2, updated_at = $3 WHERE id = $4`

	_, err := r.db.ExecContext(ctx, query, status, reason, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update node status: %w", err)
	}

	return nil
}

// UpdateHeartbeat updates the last_seen timestamp and optionally facts
func (r *NodeRepository) UpdateHeartbeat(ctx context.Context, id string, facts map[string]string) error {
	setClauses := []string{"last_seen = $1", "updated_at = $1"}
	args := []interface{}{time.Now()}
	argIdx := 2

	if v, ok := facts["ip_address"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("ip_address = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}
	if v, ok := facts["os_version"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("os_version = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}
	if v, ok := facts["desktop_env"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("desktop_env = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}
	if v, ok := facts["agent_version"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("agent_version = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}
	if v, ok := facts["os_name"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("os_name = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}
	if v, ok := facts["fqdn"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("fqdn = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}
	if v, ok := facts["machine_id"]; ok {
		setClauses = append(setClauses, fmt.Sprintf("machine_id = $%d", argIdx))
		args = append(args, v)
		argIdx++
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE nodes SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argIdx)

	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	return nil
}

// CountByStatus returns the count of nodes per status
func (r *NodeRepository) CountByStatus(ctx context.Context) (map[string]int, error) {
	query := `SELECT status_cached, COUNT(*) FROM nodes GROUP BY status_cached`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to count nodes by status: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count: %w", err)
		}
		counts[status] = count
	}

	return counts, rows.Err()
}

// GetByName retrieves a node by name (returns the first match)
func (r *NodeRepository) GetByName(ctx context.Context, name string) (*models.Node, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE n.name = $1 ORDER BY n.created_at DESC LIMIT 1`, nodeSelect, nodeFrom)

	node, err := scanNode(r.db.QueryRowContext(ctx, query, name))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node by name: %w", err)
	}

	if err := r.populateGroups(ctx, []*models.Node{node}); err != nil {
		return nil, err
	}

	return node, nil
}

// Delete removes a node by ID
func (r *NodeRepository) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete node: %w", err)
	}
	return nil
}

// AddToGroup adds a node to a node group (no-op if already a member).
func (r *NodeRepository) AddToGroup(ctx context.Context, nodeID, groupID string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO node_group_members (node_id, node_group_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		nodeID, groupID)
	if err != nil {
		return fmt.Errorf("failed to add node to group: %w", err)
	}
	return nil
}

// RemoveFromGroup removes a node from a specific node group.
func (r *NodeRepository) RemoveFromGroup(ctx context.Context, nodeID, groupID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM node_group_members WHERE node_id = $1 AND node_group_id = $2`,
		nodeID, groupID)
	if err != nil {
		return fmt.Errorf("failed to remove node from group: %w", err)
	}
	return nil
}

// ListGroupIDs returns all group IDs for a given node.
func (r *NodeRepository) ListGroupIDs(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT CAST(node_group_id AS TEXT) FROM node_group_members WHERE node_id = $1 ORDER BY created_at`,
		nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list node group IDs: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
