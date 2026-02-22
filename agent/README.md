# Bor Agent

Go daemon that enforces desktop policies managed by the Bor server.

## Structure

- `cmd/agent/` - Daemon entry point
- `internal/config/` - YAML configuration loading
- `internal/policyclient/` - gRPC client for the PolicyService
- `internal/policy/` - Policy application logic (Firefox, file_create)
- `config.yaml.example` - Example configuration file

## Prerequisites

- Go 1.24+
- Access to a running Bor server (default port 8080)

## Building

```bash
# From the repository root
make agent

# Or directly
cd agent && go build -o bor-agent ./cmd/agent
```

## Running

```bash
# Copy and edit configuration
sudo mkdir -p /etc/bor
sudo cp config.yaml.example /etc/bor/config.yaml
sudo chmod 0600 /etc/bor/config.yaml

# Run the agent
sudo ./bor-agent --config /etc/bor/config.yaml
```

## Features

- Connects to the Bor server via gRPC
- Polls for enabled policies on a configurable interval
- Applies Firefox policies by merging multiple server policies into a single `/etc/firefox/policies/policies.json`
- Applies `file_create` policies (restricted to `/tmp/` for safety)
- Reports compliance status (Deployed / Error) back to the server for each policy
- Graceful shutdown on SIGINT / SIGTERM

## Configuration

Default config path: `/etc/bor/config.yaml`

```yaml
server:
  address: "localhost:8080"   # same address as the web UI

agent:
  client_id: ""          # defaults to hostname
  poll_interval: 60      # seconds (minimum 5)

firefox:
  policies_path: "/etc/firefox/policies/policies.json"
```

## Testing

```bash
cd agent && go test ./...
```
