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
	n.last_seen, n.created_at, n.updated_at, n.cert_serial, n.cert_not_after`

const nodeFrom = `FROM nodes n`

func scanNode(row interface {
	Scan(dest ...interface{}) error
}) (*models.Node, error) {
	node := &models.Node{}
	err := row.Scan(
		&node.ID, &node.Name, &node.FQDN, &node.MachineID,
		&node.IPAddress, &node.OSName, &node.OSVersion, &node.DesktopEnv,
		&node.AgentVersion, &node.StatusCached, &node.StatusReason,
		&node.Groups, &node.Notes,
		&node.LastSeen, &node.CreatedAt, &node.UpdatedAt,
		&node.CertSerial, &node.CertNotAfter,
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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

// nodeSortColumns is the allowlist mapping API sort fields to SQL columns.
// ORDER BY cannot be parameterized, so the sort column MUST come from this map
// — never from raw request input — to prevent SQL injection.
var nodeSortColumns = map[string]string{
	"name":          "n.name",
	"status":        "n.status_cached",
	"os":            "n.os_name",
	"agent_version": "n.agent_version",
	"last_seen":     "n.last_seen",
}

// nodeOrderBy returns a safe ORDER BY clause from an allowlisted field and a
// direction. Unknown fields fall back to last_seen; unknown directions to DESC.
func nodeOrderBy(field, order string) string {
	col, ok := nodeSortColumns[field]
	if !ok {
		col = "n.last_seen"
	}
	dir := "DESC"
	if strings.EqualFold(order, "asc") {
		dir = "ASC"
	}
	return fmt.Sprintf("ORDER BY %s %s NULLS LAST", col, dir)
}

// buildNodeFilter builds the WHERE clause and args for the status + search
// filters. All values are passed as bind parameters.
func buildNodeFilter(req *models.NodeListRequest) (where string, args []interface{}) {
	var conds []string
	if req.Status != "" {
		args = append(args, req.Status)
		conds = append(conds, fmt.Sprintf("n.status_cached = $%d", len(args)))
	}
	if req.OS != "" {
		args = append(args, req.OS)
		conds = append(conds, fmt.Sprintf("n.os_name = $%d", len(args)))
	}
	if req.Desktop != "" {
		args = append(args, req.Desktop)
		conds = append(conds, fmt.Sprintf("n.desktop_env = $%d", len(args)))
	}
	if req.AgentVersion != "" {
		args = append(args, req.AgentVersion)
		conds = append(conds, fmt.Sprintf("n.agent_version = $%d", len(args)))
	}
	if s := strings.TrimSpace(req.Search); s != "" {
		args = append(args, "%"+s+"%")
		p := len(args)
		conds = append(conds, fmt.Sprintf(
			"(n.name ILIKE $%d OR n.fqdn ILIKE $%d OR n.ip_address ILIKE $%d OR n.groups ILIKE $%d)",
			p, p, p, p))
	}
	if len(conds) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// DistinctFilterValues returns the distinct non-empty os_name, desktop_env, and
// agent_version values, for populating filter dropdowns.
func (r *NodeRepository) DistinctFilterValues(ctx context.Context) (*models.NodeFilterOptions, error) {
	opts := &models.NodeFilterOptions{OS: []string{}, Desktops: []string{}, AgentVersions: []string{}}
	cols := []struct {
		col  string
		dest *[]string
	}{
		{"os_name", &opts.OS},
		{"desktop_env", &opts.Desktops},
		{"agent_version", &opts.AgentVersions},
	}
	for _, c := range cols {
		// Column name is from a fixed local list, never request input.
		query := fmt.Sprintf(
			"SELECT DISTINCT %s FROM nodes WHERE %s IS NOT NULL AND %s <> '' ORDER BY %s",
			c.col, c.col, c.col, c.col)
		rows, err := r.db.QueryContext(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("failed to load distinct %s: %w", c.col, err)
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("failed to scan %s: %w", c.col, err)
			}
			*c.dest = append(*c.dest, v)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return opts, nil
}

// CountFiltered returns the number of nodes matching the filter.
func (r *NodeRepository) CountFiltered(ctx context.Context, req *models.NodeListRequest) (int, error) {
	where, args := buildNodeFilter(req)
	query := fmt.Sprintf(`SELECT COUNT(*) %s %s`, nodeFrom, where)
	var count int
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count nodes: %w", err)
	}
	return count, nil
}

// ListPaged returns a page of nodes matching the filter, ordered by the
// allowlisted sort field, with group memberships populated.
func (r *NodeRepository) ListPaged(ctx context.Context, req *models.NodeListRequest) ([]*models.Node, error) {
	where, args := buildNodeFilter(req)
	orderBy := nodeOrderBy(req.SortField, req.SortOrder)
	page, perPage := models.ClampPagination(req.Page, req.PerPage)
	offset := (page - 1) * perPage
	args = append(args, perPage, offset)
	query := fmt.Sprintf(`SELECT %s %s %s %s LIMIT $%d OFFSET $%d`,
		nodeSelect, nodeFrom, where, orderBy, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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

// UpdateCertificate persists the serial hex and notAfter time for a node's mTLS certificate.
func (r *NodeRepository) UpdateCertificate(ctx context.Context, nodeID, serial string, notAfter time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE nodes SET cert_serial = $1, cert_not_after = $2, updated_at = $3 WHERE id = $4`,
		serial, notAfter, time.Now(), nodeID)
	if err != nil {
		return fmt.Errorf("failed to update node certificate: %w", err)
	}
	return nil
}

// ListExpiringCerts returns all nodes whose mTLS certificate expires within
// the given number of days, ordered by cert_not_after ascending.
func (r *NodeRepository) ListExpiringCerts(ctx context.Context, withinDays int) ([]*models.Node, error) {
	cutoff := time.Now().Add(time.Duration(withinDays) * 24 * time.Hour)
	query := fmt.Sprintf(`SELECT %s %s WHERE n.cert_not_after IS NOT NULL AND n.cert_not_after <= $1 ORDER BY n.cert_not_after ASC`, nodeSelect, nodeFrom)
	rows, err := r.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to list expiring certs: %w", err)
	}
	defer func() { _ = rows.Close() }()
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

// NodeMetricRow holds the minimal node fields needed for Prometheus metrics.
type NodeMetricRow struct {
	Name         string // inventory name — unique within Bor
	FQDN         string // hostname reported by the agent (may not be unique)
	CertNotAfter *time.Time
	LastSeen     *time.Time
}

// ListForMetrics returns lightweight rows for every node, used only by the
// Prometheus collector to emit per-node cert-expiry and last-seen gauges.
// Each row is identified by the Bor inventory name, which is unique.
func (r *NodeRepository) ListForMetrics(ctx context.Context) ([]NodeMetricRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, COALESCE(fqdn, ''), cert_not_after, last_seen
		FROM   nodes
		ORDER  BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes for metrics: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NodeMetricRow
	for rows.Next() {
		var row NodeMetricRow
		if err := rows.Scan(&row.Name, &row.FQDN, &row.CertNotAfter, &row.LastSeen); err != nil {
			return nil, fmt.Errorf("failed to scan node metric row: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListGroupIDs returns all group IDs for a given node.
func (r *NodeRepository) ListGroupIDs(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT CAST(node_group_id AS TEXT) FROM node_group_members WHERE node_id = $1 ORDER BY created_at`,
		nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list node group IDs: %w", err)
	}
	defer func() { _ = rows.Close() }()
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
