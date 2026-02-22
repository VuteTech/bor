# Server

Go backend server for Bor Policy Management System.

## Structure

- `cmd/server/` - Main application entry point
- `internal/` - Private application code
  - `api/` - HTTP/REST API handlers
  - `config/` - Configuration management
  - `database/` - Database layer and migrations
  - `models/` - Data models
  - `services/` - Business logic
- `pkg/` - Public libraries
  - `grpc/` - gRPC service implementations
  - `auth/` - Authentication and authorization
- `web/frontend/` - PatternFly frontend application

## Development

### Prerequisites

- Go 1.21 or higher
- PostgreSQL 14 or higher
- Protocol Buffers compiler (protoc)

### Building

```bash
go build -o server ./cmd/server
```

### Running

```bash
./server
```

### Testing

```bash
go test ./...
```

## Configuration

Configuration is managed through environment variables and/or config files.

See `internal/config/` for available options.

## API

The server exposes two APIs:

1. **gRPC API** - For agent communication (policy delivery)
2. **REST API** - For web UI and administration

See `proto/` for gRPC service definitions.
