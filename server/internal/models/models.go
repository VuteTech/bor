// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

package models

import (
	"time"
)

// Policy state constants (artifact lifecycle)
const (
	PolicyStateDraft    = "draft"
	PolicyStateReleased = "released"
	PolicyStateArchived = "archived"
)

// Binding state constants (enforcement lifecycle)
const (
	BindingStateEnabled  = "enabled"
	BindingStateDisabled = "disabled"
)

// Policy represents a desktop policy
type Policy struct {
	ID                  string     `json:"id" db:"id"`
	Name                string     `json:"name" db:"name"`
	Description         string     `json:"description" db:"description"`
	Type                string     `json:"type" db:"type"`
	Content             string     `json:"content" db:"content"` // JSON string
	Version             int        `json:"version" db:"version"`
	State               string     `json:"state" db:"state"`
	DeprecatedAt        *time.Time `json:"deprecated_at,omitempty" db:"deprecated_at"`
	DeprecationMessage  *string    `json:"deprecation_message,omitempty" db:"deprecation_message"`
	ReplacementPolicyID *string    `json:"replacement_policy_id,omitempty" db:"replacement_policy_id"`
	CreatedBy           string     `json:"created_by" db:"created_by"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
}

// CreatePolicyRequest represents a request to create a policy
type CreatePolicyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Content     string `json:"content"`
}

// UpdatePolicyRequest represents a request to update a policy (only allowed in DRAFT state)
type UpdatePolicyRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Type        *string `json:"type,omitempty"`
	Content     *string `json:"content,omitempty"`
}

// SetPolicyStateRequest represents a request to change policy state
type SetPolicyStateRequest struct {
	State string `json:"state"`
}

// DeprecatePolicyRequest represents a request to mark a policy as deprecated
type DeprecatePolicyRequest struct {
	Message             *string `json:"message,omitempty"`
	ReplacementPolicyID *string `json:"replacement_policy_id,omitempty"`
}

// Node status constants
const (
	NodeStatusOnline   = "online"
	NodeStatusDegraded = "degraded"
	NodeStatusOffline  = "offline"
	NodeStatusUnknown  = "unknown"
)

// Client represents a desktop client/agent (legacy, kept for migration compatibility)
type Client struct {
	ID           string     `json:"id" db:"id"`
	Hostname     string     `json:"hostname" db:"hostname"`
	MACAddress   *string    `json:"mac_address,omitempty" db:"mac_address"`
	IPAddress    *string    `json:"ip_address,omitempty" db:"ip_address"`
	OSVersion    *string    `json:"os_version,omitempty" db:"os_version"`
	AgentVersion *string    `json:"agent_version,omitempty" db:"agent_version"`
	LastSeen     *time.Time `json:"last_seen,omitempty" db:"last_seen"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
}

// Node represents a managed desktop node/agent
type Node struct {
	ID             string     `json:"id" db:"id"`
	Name           string     `json:"name" db:"name"`
	FQDN           *string    `json:"fqdn,omitempty" db:"fqdn"`
	MachineID      *string    `json:"machine_id,omitempty" db:"machine_id"`
	IPAddress      *string    `json:"ip_address,omitempty" db:"ip_address"`
	OSName         *string    `json:"os_name,omitempty" db:"os_name"`
	OSVersion      *string    `json:"os_version,omitempty" db:"os_version"`
	DesktopEnv     *string    `json:"desktop_env,omitempty" db:"desktop_env"`
	AgentVersion   *string    `json:"agent_version,omitempty" db:"agent_version"`
	StatusCached   string     `json:"status" db:"status_cached"`
	StatusReason   *string    `json:"status_reason,omitempty" db:"status_reason"`
	Groups         *string    `json:"groups,omitempty" db:"groups"`
	Notes          *string    `json:"notes,omitempty" db:"notes"`
	NodeGroupIDs   []string   `json:"node_group_ids"`
	NodeGroupNames []string   `json:"node_group_names"`
	LastSeen       *time.Time `json:"last_seen,omitempty" db:"last_seen"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// UpdateNodeRequest represents a request to update a node
type UpdateNodeRequest struct {
	Name   *string `json:"name,omitempty"`
	Groups *string `json:"groups,omitempty"`
	Notes  *string `json:"notes,omitempty"`
}

// ComplianceReport represents a policy compliance report from a client
type ComplianceReport struct {
	ID         string    `json:"id" db:"id"`
	ClientID   string    `json:"client_id" db:"client_id"`
	PolicyID   string    `json:"policy_id" db:"policy_id"`
	Compliant  bool      `json:"compliant" db:"compliant"`
	Message    *string   `json:"message,omitempty" db:"message"`
	ReportedAt time.Time `json:"reported_at" db:"reported_at"`
}

// User represents a system user for authentication
type User struct {
	ID           string    `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Email        string    `json:"email" db:"email"`
	FullName     string    `json:"full_name" db:"full_name"`
	Source       string    `json:"source" db:"source"` // "local" or "ldap"
	Enabled      bool      `json:"enabled" db:"enabled"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// Legacy role constants (kept for backward compatibility during migration)
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Scope type constants for user role bindings
const (
	ScopeGlobal       = "global"
	ScopeOrganization = "organization"
	ScopeGroup        = "group"
)

// Role represents an RBAC role
type Role struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Permission represents an RBAC permission (resource + action)
type Permission struct {
	ID       string `json:"id" db:"id"`
	Resource string `json:"resource" db:"resource"`
	Action   string `json:"action" db:"action"`
}

// UserRoleBinding represents a binding between a user, a role, and a scope
type UserRoleBinding struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	RoleID    string    `json:"role_id" db:"role_id"`
	ScopeType string    `json:"scope_type" db:"scope_type"`
	ScopeID   *string   `json:"scope_id,omitempty" db:"scope_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Default role name constants
const (
	RoleSuperAdmin      = "Super Admin"
	RoleOrgAdmin        = "Org Admin"
	RolePolicyEditor    = "Policy Editor"
	RolePolicyReviewer  = "Policy Reviewer"
	RoleComplianceViewer = "Compliance Viewer"
	RoleAuditor         = "Auditor"
)

// UserSource constants
const (
	SourceLocal = "local"
	SourceLDAP  = "ldap"
)

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// MeResponse represents the response for GET /api/v1/auth/me
type MeResponse struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	FullName    string   `json:"full_name"`
	Permissions []string `json:"permissions"`
}

// CreateUserRequest represents a request to create a user
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	RoleName string `json:"role_name,omitempty"` // RBAC role name to assign (e.g. "Super Admin")
}

// UpdateUserRequest represents a request to update a user
type UpdateUserRequest struct {
	Email    *string `json:"email,omitempty"`
	FullName *string `json:"full_name,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
}

// CreateRoleRequest represents a request to create a role
type CreateRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateRoleRequest represents a request to update a role
type UpdateRoleRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// SetRolePermissionsRequest represents a request to set role permissions
type SetRolePermissionsRequest struct {
	PermissionIDs []string `json:"permission_ids"`
}

// UserGroup represents a logical grouping of users (identity domain)
type UserGroup struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// CreateUserGroupRequest represents a request to create a user group
type CreateUserGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateUserGroupRequest represents a request to update a user group
type UpdateUserGroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UserGroupMember represents a user's membership in a user group (identity domain)
type UserGroupMember struct {
	ID        string    `json:"id" db:"id"`
	GroupID   string    `json:"group_id" db:"group_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// AddGroupMemberRequest represents a request to add a user to a group
type AddGroupMemberRequest struct {
	UserID string `json:"user_id"`
}

// UserGroupRoleBinding represents a role binding for a user group (identity domain)
type UserGroupRoleBinding struct {
	ID        string    `json:"id" db:"id"`
	GroupID   string    `json:"group_id" db:"group_id"`
	RoleID    string    `json:"role_id" db:"role_id"`
	ScopeType string    `json:"scope_type" db:"scope_type"`
	ScopeID   *string   `json:"scope_id,omitempty" db:"scope_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// CreateGroupRoleBindingRequest represents a request to assign a role to a group
type CreateGroupRoleBindingRequest struct {
	RoleID    string  `json:"role_id"`
	ScopeType string  `json:"scope_type"`
	ScopeID   *string `json:"scope_id,omitempty"`
}

// NodeGroup represents a logical grouping of nodes (infrastructure domain)
type NodeGroup struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// CreateNodeGroupRequest represents a request to create a node group
type CreateNodeGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdateNodeGroupRequest represents a request to update a node group
type UpdateNodeGroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// EnrollmentToken represents a short-lived, single-use enrollment token
type EnrollmentToken struct {
	Token       string    `json:"token"`
	NodeGroupID string    `json:"node_group_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	Used        bool      `json:"used"`
}

// AgentNotificationSettings holds the notification configuration for agents
type AgentNotificationSettings struct {
	NotifyUsers          bool   `json:"notify_users"`
	NotifyCooldown       int    `json:"notify_cooldown"`
	NotifyMessage        string `json:"notify_message"`
	NotifyMessageFirefox string `json:"notify_message_firefox"`
	NotifyMessageChrome  string `json:"notify_message_chrome"`
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID           string    `json:"id" db:"id"`
	UserID       *string   `json:"user_id,omitempty" db:"user_id"`
	Username     string    `json:"username" db:"username"`
	Action       string    `json:"action" db:"action"`
	ResourceType string    `json:"resource_type" db:"resource_type"`
	ResourceID   string    `json:"resource_id" db:"resource_id"`
	Details      string    `json:"details" db:"details"`
	IPAddress    string    `json:"ip_address" db:"ip_address"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// AuditLogListRequest represents query parameters for listing audit logs
type AuditLogListRequest struct {
	Page         int    `json:"page"`
	PerPage      int    `json:"per_page"`
	ResourceType string `json:"resource_type,omitempty"`
	Action       string `json:"action,omitempty"`
	Username     string `json:"username,omitempty"`
}

// AuditLogListResponse represents a paginated list of audit logs
type AuditLogListResponse struct {
	Items      []*AuditLog `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PerPage    int         `json:"per_page"`
	TotalPages int         `json:"total_pages"`
}

// PolicyBinding represents a binding between a policy and a node group
type PolicyBinding struct {
	ID        string    `json:"id" db:"id"`
	PolicyID  string    `json:"policy_id" db:"policy_id"`
	GroupID   string    `json:"group_id" db:"group_id"`
	State     string    `json:"state" db:"state"`
	Priority  int       `json:"priority" db:"priority"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// PolicyBindingWithDetails includes related policy and group information
type PolicyBindingWithDetails struct {
	PolicyBinding
	PolicyName  string `json:"policy_name"`
	PolicyState string `json:"policy_state"`
	GroupName   string `json:"group_name"`
	NodeCount   int    `json:"node_count"`
}

// CreatePolicyBindingRequest represents a request to create a policy binding
type CreatePolicyBindingRequest struct {
	PolicyID string `json:"policy_id"`
	GroupID  string `json:"group_id"`
	Priority int    `json:"priority"`
}

// UpdatePolicyBindingRequest represents a request to update a policy binding
type UpdatePolicyBindingRequest struct {
	State    *string `json:"state,omitempty"`
	Priority *int    `json:"priority,omitempty"`
}

// NodeHeartbeatInfo contains metadata reported by an agent during heartbeat.
type NodeHeartbeatInfo struct {
	FQDN         string
	IPAddress    string
	OSName       string
	OSVersion    string
	DesktopEnvs  []string
	AgentVersion string
	MachineID    string
}
