# Architecture

## Overview

Bor is an Enterprise Linux Desktop Policy Management system consisting of:

1. **Server** - Central policy management and delivery (Go backend + PatternFly frontend)
2. **Agent** - Desktop client daemon for policy enforcement (Go binary)

## Components

### Server

**Technology Stack:**

- **Backend**: Go 1.21+
- **Frontend**: PatternFly (React-based UI framework)
- **Database**: PostgreSQL
- **Communication**: 
  - gRPC with mTLS for agent communication
  - REST API for web UI
- **Authentication**: 
  - JWT-based authentication for web UI
  - mTLS certificate-based authentication for agents
- **Deployment**: UBI (Red Hat Universal Base Image) containers, non-root user (1001)

**Architecture Layers:**

```
┌─────────────────────────────────────┐
│     PatternFly Web UI (React)       │
│     - Policy Management             │
│     - Node Groups Management        │
│     - User/Role Management          │
│     - Enrollment Token Generation   │
├─────────────────────────────────────┤
│     REST API (HTTP/JSON)            │
│     - JWT Authentication            │
│     - RBAC Authorization            │
├─────────────────────────────────────┤
│     Business Logic (Services)       │
│     - PolicyService                 │
│     - EnrollmentService             │
│     - NodeService                   │
│     - AuthService                   │
├─────────────────────────────────────┤
│     Data Access Layer (Repos)       │
├─────────────────────────────────────┤
│     PostgreSQL Database             │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│     gRPC API (mTLS secured)         │
│     - PolicyService (agents)        │
│     - EnrollmentService (bootstrap) │
├─────────────────────────────────────┤
│     PolicyHub (streaming updates)   │
│     - Server-side streaming         │
│     - Event distribution            │
└─────────────────────────────────────┘
```

**Components:**

- `cmd/server/` - Application entry point and initialization
- `internal/api/` - REST API handlers for web UI
- `internal/config/` - Configuration management
- `internal/database/` - Database layer, migrations, repositories
- `internal/models/` - Data models and DTOs
- `internal/services/` - Business logic (policy, enrollment, node, user management)
- `internal/grpc/` - gRPC service implementations (PolicyServer, EnrollmentServer)
- `internal/pki/` - Certificate management and internal CA
- `internal/authz/` - RBAC authorization
- `pkg/grpc/` - Protocol Buffer generated code
- `web/frontend/` - PatternFly React application
- `web/static/` - Embedded static assets

### Agent

**Technology Stack:**
- **Language**: Go 1.21+
- **Communication**: gRPC client with mTLS
- **Deployment**: System daemon (systemd service)
- **Platform**: Linux (Go binary)

**Architecture:**
```
┌─────────────────────────────────────┐
│     Agent Main Loop                 │
│     - Enrollment check              │
│     - mTLS connection               │
│     - Stream reconnection           │
├─────────────────────────────────────┤
│     Policy Client (gRPC)            │
│     - SubscribePolicyUpdates        │
│     - ReportCompliance              │
│     - Certificate management        │
├─────────────────────────────────────┤
│     Policy Enforcement              │
│     - Firefox policy merger         │
│     - Atomic file writes            │
│     - Compliance reporting          │
└─────────────────────────────────────┘
```

**Components:**

- `cmd/agent/` - Agent entry point and main loop
- `internal/config/` - Configuration management
- `internal/policyclient/` - gRPC client and enrollment logic
- `internal/policy/` - Policy application and enforcement (Firefox, etc.)


## Communication Flow

### Agent Enrollment (Bootstrap)

```
┌────────┐                           ┌────────┐
│ Agent  │                           │ Server │
└───┬────┘                           └───┬────┘
    │                                    │
    │ 1. Admin generates token via UI    │
    │    (Node Groups page)              │
    │◄───────────────────────────────────│
    │                                    │
    │ 2. Agent starts with --token flag  │
    │    (no client cert yet)            │
    │    - Generate RSA 2048 key pair    │
    │    - Create CSR                    │
    │                                    │
    │ 3. Enroll RPC (TLS only)          │
    │    - token + CSR                   │
    ├───────────────────────────────────►│
    │                                    │
    │                                    │ 4. Validate token
    │                                    │ 5. Sign CSR with CA
    │                                    │ 6. Create Node record
    │                                    │
    │ 7. EnrollResponse                  │
    │    - Signed client certificate     │
    │    - CA certificate                │
    │    - Assigned node group ID        │
    │◄───────────────────────────────────│
    │                                    │
    │ 8. Persist credentials:            │
    │    - agent.crt (signed cert)       │
    │    - agent.key (private key)       │
    │    - ca.crt (verify server)        │
    │                                    │
```

### Policy Delivery (Streaming with mTLS)

```
┌────────┐                           ┌────────┐
│ Agent  │                           │ Server │
└───┬────┘                           └───┬────┘
    │                                    │
    │ 1. Connect with mTLS               │
    │    (client cert required)          │
    ├───────────────────────────────────►│
    │                                    │
    │                                    │ 2. Verify client cert
    │                                    │ 3. Look up Node & Group
    │                                    │
    │ 4. SubscribePolicyUpdates          │
    │    (last_known_revision)           │
    ├───────────────────────────────────►│
    │                                    │
    │                                    │ 5. Send snapshot or delta
    │                                    │
    │ 6. Stream: PolicyUpdate events     │
    │◄───────────────────────────────────│
    │    - SNAPSHOT (initial sync)       │
    │    - CREATED (new policy)          │
    │    - UPDATED (modified policy)     │
    │    - DELETED (removed policy)      │
    │                                    │
    │ 7. Apply policy locally            │
    │    (Firefox policies.json)         │
    │                                    │
    │ 8. ReportCompliance RPC            │
    │    (success/failure + message)     │
    ├───────────────────────────────────►│
    │                                    │
    │    ... stream stays open ...       │
    │                                    │
    │ 9. Admin changes policy binding    │
    │◄───────────────────────────────────│
    │                                    │
    │10. Stream: New snapshot            │
    │◄───────────────────────────────────│
    │    (full resync on binding change) │
    │                                    │
```

### Reconnection & Delta Sync

- Agent persists `last_known_revision` 
- On reconnect, sends last revision to server
- Server attempts delta (events since revision)
- If delta unavailable (too old), sends full snapshot
- Agent applies updates and continues streaming

## Data Flow

1. **Policy Creation & Assignment**:
   - Admin creates policy via Web UI (REST API)
   - Policy stored in PostgreSQL with state (Draft/Released)
   - Admin creates policy binding to Node Group
   - PolicyHub broadcasts resync signal to connected agents
   - Agents receive new snapshot via streaming RPC

2. **Policy Enforcement**:
   - Agent receives policy update via gRPC stream
   - Agent validates policy content
   - Agent merges multiple policies (for Firefox)
   - Agent writes atomically to target location (e.g., `/usr/lib64/firefox/distribution/policies.json`)
   - Agent reports compliance status back to server

3. **Policy Monitoring**:
   - Agents report compliance via ReportCompliance RPC
   - Server logs compliance reports (future: store in database)
   - Admin views node status in Web UI
   - Audit logging for all policy changes

4. **Node Management**:
   - Enrollment creates Node record linked to Node Group
   - Node Group defines policy bindings
   - Admin can reassign nodes to different groups
   - Node group change triggers policy resync

## Database Schema

### Core Entities

- **users** - Web UI users (username, password hash, LDAP link)
- **user_groups** - User organization (e.g., "Engineering", "IT")
- **roles** - RBAC roles (name, permissions)
- **permissions** - Granular permissions (resource, action)
- **user_role_bindings** - User-to-Role assignments
- **user_group_role_bindings** - Group-to-Role assignments
- **user_group_members** - User membership in groups

- **policies** - Policy definitions (name, type, content JSON, state, version)
- **node_groups** - Logical groupings of nodes (name, description)
- **nodes** - Enrolled agents (name, node_group_id, last_seen)
- **policy_bindings** - Many-to-Many (policy ↔ node_group)

### Key Relationships

- Policy ↔ Node Group (many-to-many via policy_bindings)
- Node → Node Group (many-to-one)
- User ↔ Role (many-to-many via user_role_bindings)
- User Group ↔ Role (many-to-many via user_group_role_bindings)
- User ↔ User Group (many-to-many via user_group_members)


## Security

### Authentication

#### Web UI
- JWT tokens for session management
- LDAP integration supported (optional)
- Password hashing with bcrypt
- Session timeout and refresh

#### Agent-Server Communication
- **Enrollment Phase**: One-time token exchange
  - Admin generates short-lived (5 min) enrollment token via UI
  - Agent uses token to authenticate Enroll RPC (TLS only, no client cert)
  - Token is single-use and tied to a Node Group
  
- **Operational Phase**: Mutual TLS (mTLS)
  - All gRPC RPCs (except Enroll) require verified client certificate
  - Agent presents client certificate signed by internal CA
  - Server validates certificate chain
  - Certificate CN matches node name
  - Interceptors enforce mTLS at gRPC layer

### Internal PKI

- **Internal Certificate Authority (CA)**
  - Auto-generated on first startup if not provided
  - Used to sign agent client certificates
  - Used to sign server certificate (for Development  HTTPS)
  - Stored in `/var/lib/bor/pki/ca/`
  
- **Agent Certificates**
  - Generated during enrollment (RSA 2048)
  - Signed by internal CA
  - Long-lived (10 years default)
  - Stored locally in `/var/lib/bor/agent/` on agent
  
- **Server Certificate**
  - Auto-generated for HTTPS if not provided
  - Signed by internal CA (self-signed for dev)
  - Production: use proper certificates from trusted CA

### Authorization

#### Web UI (RBAC)
- Role-based access control
- Granular permissions (resource:action)
- User Groups for bulk role assignment
- Permission checks at API layer

#### gRPC
- Client certificate verification (authentication)
- Node group scoping (agents only see their group's policies)
- Enrollment token validation (authorization for bootstrap)

### Data Protection

- **In Transit**:
  - TLS 1.2+ for all connections (agents, web UI)
  - mTLS for agent-server gRPC
  - HTTPS for web UI
  
- **At Rest**:
  - Database credentials via environment variables
  - No hardcoded secrets
  - Private keys protected with file permissions (0600)
  - CA key protected in production deployments
  
- **Audit**:
  - All policy changes logged
  - User actions tracked (future: audit table)
  - Compliance reports logged

## Deployment

### Server Deployment

**Container-based (Recommended)**:
- Built on UBI 10 (Red Hat Universal Base Image)
- Multi-stage build (Node.js for frontend, Go for backend)
- Non-root user (UID 1001)
- Read-only filesystem support
- Podman/Docker Compose configuration provided

**Components**:
- Bor server container (Go binary + embedded frontend)
- PostgreSQL container (separate)
- Named volumes:
  - `postgres_data` - Database persistence
  - `bor_pki` - CA and certificates (chowned to UID 1001 with :U flag)

**Configuration**:
- Environment variables for all settings
- Automatic PKI bootstrap (CA + server cert)
- HTTPS on port 8443 by default
- gRPC multiplexed on same port (HTTP/2)

### Agent Deployment

**Native Packages**:
- RPM packages for RHEL/Fedora (future)
- DEB packages for Debian/Ubuntu (future)
- Go binary deployment (current)

**Installation**:
```bash
# Copy binary
cp bor-agent /usr/local/bin/

# Create config
mkdir -p /etc/bor
cp config.yaml.example /etc/bor/config.yaml

# Enroll agent
bor-agent --token <ENROLLMENT_TOKEN>

# Start service (systemd)
systemctl enable --now bor-agent
```

**Configuration**:
- Config file: `/etc/bor/config.yaml`
- Data directory: `/var/lib/bor/agent/` (certificates)
- System service (systemd unit file)
- Automatic reconnection with exponential backoff

### Production Considerations

- **Database**: Use managed PostgreSQL service or dedicated instance
- **TLS**: Provide proper certificates (not self-signed)
- **Secrets**: Use secrets management (Vault, Kubernetes secrets)
- **Monitoring**: Enable structured logging, metrics endpoints
- **Backup**: Borr database backups, PKI key backup
- **High Availability**: Multiple server replicas (requires session affinity for gRPC streams)

## Scalability

### Current Architecture
- Single server instance with streaming gRPC
- PostgreSQL database (can scale vertically)
- Long-lived gRPC streams per agent
- PolicyHub for in-memory event distribution

### Scaling Considerations

**Vertical Scaling (Recommended for MVP)**:
- Increase server CPU/memory
- PostgreSQL tuning (connection pooling, indexes)
- Single server can handle 1000s of concurrent streams

**Horizontal Scaling (Future)**:
- Multiple server replicas with sticky sessions
- Challenge: gRPC streams require session affinity
- Options:
  - Load balancer with gRPC-aware routing
  - Service mesh (Istio, Linkerd)
  - Polling fallback for agents
  
**Database Optimization**:
- Connection pooling (already implemented)
- Read replicas for reporting queries
- Indexes on frequently queried columns
- Policy content caching (future enhancement)

**Stream Optimization**:
- Delta updates reduce bandwidth
- Snapshot compaction
- Event buffer limits in PolicyHub
- Agent reconnection backoff

## Monitoring

### Logging
- Structured logging with standard log package
- JSON format for production (future)
- Log levels: INFO, WARN, ERROR
- Key events logged:
  - Enrollment events
  - Policy updates
  - Compliance reports
  - Connection events
  - Errors and warnings

### Metrics (Future)
- Prometheus-compatible metrics endpoint
- Key metrics:
  - Active agent connections
  - Policy update latency
  - Enrollment success/failure rate
  - Database query performance
  - gRPC request rates

### Health Checks
- Database connectivity
- Certificate validity
- gRPC server status
- HTTP endpoint: `/healthz` (future)

### Observability
- Correlation IDs for request tracking
- Distributed tracing support (OpenTelemetry - future)
- Agent-side logging for compliance issues
- Policy application audit trail

## Technology Stack Summary

| Component | Technology | Version |
|-----------|-----------|---------|
| Server Backend | Go | 1.21+ |
| Agent | Go | 1.21+ |
| Database | PostgreSQL | 17+ |
| Frontend | React | 18.3+ |
| UI Framework | PatternFly | 5.3+ |
| RPC | gRPC | protobuf v3 |
| Build | Webpack | 5.x |
| Container Base | UBI 10 | Latest |
| TLS | TLS 1.2+ | - |

## Protocol Buffers

### Service Definitions

**PolicyService** (`proto/policy/policy.proto`):
- `GetPolicy` - Fetch single policy by ID
- `ListPolicies` - List policies for agent's node group
- `SubscribePolicyUpdates` - Server-streaming RPC for real-time updates
- `ReportCompliance` - Agent compliance reporting

**EnrollmentService** (`proto/enrollment/enrollment.proto` - implied):
- `Enroll` - Bootstrap enrollment with token + CSR
- `CreateEnrollmentToken` - Admin API to generate tokens

### Message Types
- `Policy` - Policy metadata and content
- `PolicyUpdate` - Stream event (SNAPSHOT, CREATED, UPDATED, DELETED)
- `EnrollRequest/Response` - Enrollment flow messages

## Development Workflow

1. **Local Development**:
   ```bash
   # Start infrastructure
   podman-compose up -d postgres
   
   # Run server locally
   cd server
   go run cmd/server/main.go
   
   # Run agent locally
   cd agent
   go run cmd/agent/main.go --token <TOKEN>
   ```

2. **Build Frontend**:
   ```bash
   cd server/web/frontend
   npm install
   npm run build  # Production  OR
   npm run dev    # Development with watch
   ```

3. **Generate Protocol Buffers**:
   ```bash
   protoc --go_out=. --go-grpc_out=. proto/policy/policy.proto
   ```

4. **Run Tests**:
   ```bash
   # Server tests
   cd server && go test ./...
   
   # Agent tests
   cd agent && go test ./...
   ```

## Future Enhancements

### High Priority
- [ ] Persistent compliance reporting (database storage)
- [ ] Web UI for viewing node compliance status
- [ ] Agent auto-update mechanism
- [ ] Additional policy types (systemd, network, packages)
- [ ] Policy templates library

### Medium Priority
- [ ] Policy validation framework
- [ ] Policy rollback capability
- [ ] Scheduled policy deployment
- [ ] Multi-tenancy support
- [ ] Enhanced audit logging

### Low Priority
- [ ] Prometheus metrics export
- [ ] Grafana dashboards
- [ ] CLI tool for server management
- [ ] Policy simulation/dry-run mode
- [ ] Agent health telemetry
