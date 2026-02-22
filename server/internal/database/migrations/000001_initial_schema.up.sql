-- ═══════════════════════════════════════════════
-- Policies
-- ═══════════════════════════════════════════════
CREATE TABLE policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    type VARCHAR(50) NOT NULL,
    content JSONB NOT NULL,
    version INT NOT NULL DEFAULT 1,
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    deprecated_at TIMESTAMP,
    deprecation_message TEXT,
    replacement_policy_id UUID REFERENCES policies(id),
    created_by VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_policies_type ON policies(type);
CREATE INDEX idx_policies_status ON policies(status);

-- ═══════════════════════════════════════════════
-- Users
-- ═══════════════════════════════════════════════
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL DEFAULT '',
    email VARCHAR(255) NOT NULL DEFAULT '',
    full_name VARCHAR(255) NOT NULL DEFAULT '',
    source VARCHAR(50) NOT NULL DEFAULT 'local',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_source ON users(source);

-- ═══════════════════════════════════════════════
-- Node Groups
-- ═══════════════════════════════════════════════
CREATE TABLE node_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

-- ═══════════════════════════════════════════════
-- Nodes
-- ═══════════════════════════════════════════════
CREATE TABLE nodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    fqdn VARCHAR(255),
    machine_id VARCHAR(255),
    ip_address VARCHAR(45),
    os_name VARCHAR(100),
    os_version VARCHAR(100),
    desktop_env VARCHAR(100),
    agent_version VARCHAR(50),
    status_cached VARCHAR(20) NOT NULL DEFAULT 'unknown',
    status_reason TEXT,
    groups TEXT,
    notes TEXT,
    last_seen TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_nodes_name ON nodes(name);
CREATE INDEX idx_nodes_fqdn ON nodes(fqdn);
CREATE INDEX idx_nodes_machine_id ON nodes(machine_id);
CREATE INDEX idx_nodes_status ON nodes(status_cached);
CREATE INDEX idx_nodes_last_seen ON nodes(last_seen DESC);
CREATE INDEX idx_nodes_os_version ON nodes(os_version);
CREATE INDEX idx_nodes_agent_version ON nodes(agent_version);

-- ═══════════════════════════════════════════════
-- Node Group Members (many-to-many)
-- ═══════════════════════════════════════════════
CREATE TABLE node_group_members (
    node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    node_group_id UUID NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (node_id, node_group_id)
);

CREATE INDEX idx_node_group_members_node_id ON node_group_members(node_id);
CREATE INDEX idx_node_group_members_group_id ON node_group_members(node_group_id);

-- ═══════════════════════════════════════════════
-- Policy Bindings
-- ═══════════════════════════════════════════════
CREATE TABLE policy_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES policies(id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    state VARCHAR(20) NOT NULL DEFAULT 'disabled',
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(policy_id, group_id)
);

CREATE INDEX idx_policy_bindings_policy_id ON policy_bindings(policy_id);
CREATE INDEX idx_policy_bindings_group_id ON policy_bindings(group_id);
CREATE INDEX idx_policy_bindings_state ON policy_bindings(state);
CREATE INDEX idx_policy_bindings_policy_state ON policy_bindings(policy_id, state);

-- ═══════════════════════════════════════════════
-- RBAC: Roles, Permissions, Bindings
-- ═══════════════════════════════════════════════
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource TEXT NOT NULL,
    action TEXT NOT NULL,
    UNIQUE(resource, action)
);

CREATE TABLE role_permissions (
    role_id UUID REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX idx_role_permissions_role_id ON role_permissions(role_id);

CREATE TABLE user_role_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID REFERENCES roles(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL,
    scope_id UUID NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_role_bindings_user_id ON user_role_bindings(user_id);
CREATE INDEX idx_user_role_bindings_scope ON user_role_bindings(scope_type, scope_id);

-- ═══════════════════════════════════════════════
-- User Groups (identity domain)
-- ═══════════════════════════════════════════════
CREATE TABLE user_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE user_group_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(group_id, user_id)
);

CREATE INDEX idx_user_group_members_group_id ON user_group_members(group_id);
CREATE INDEX idx_user_group_members_user_id ON user_group_members(user_id);

CREATE TABLE user_group_role_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL DEFAULT 'global',
    scope_id UUID NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_group_role_bindings_group_id ON user_group_role_bindings(group_id);

-- ═══════════════════════════════════════════════
-- Audit Logs
-- ═══════════════════════════════════════════════
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    username TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    details TEXT NOT NULL DEFAULT '',
    ip_address TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_resource_type ON audit_logs(resource_type);
CREATE INDEX idx_audit_logs_action ON audit_logs(action);

-- ═══════════════════════════════════════════════
-- Agent Settings
-- ═══════════════════════════════════════════════
CREATE TABLE agent_settings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key TEXT NOT NULL UNIQUE,
    value TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
