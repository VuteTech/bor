.PHONY: help server agent frontend proto proto-go proto-ts clean test lint install-deps dev \
        packages packages-agent packages-server

# Versioning — override with: make packages VERSION=1.2.3
VERSION ?= 0.1.0
# Target architecture — override with: make packages ARCH=arm64
ARCH    ?= amd64

# Default target
help:
	@echo "Bor Policy Management System - Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  help               - Show this help message"
	@echo "  server             - Build the Go server"
	@echo "  agent              - Build the Go agent"
	@echo "  frontend           - Build the React frontend"
	@echo "  proto              - Generate code from Protocol Buffers"
	@echo "  test               - Run all tests"
	@echo "  test-server        - Run server tests"
	@echo "  test-agent         - Run agent tests"
	@echo "  lint               - Run linters"
	@echo "  lint-server        - Lint Go server code"
	@echo "  lint-agent         - Lint Go agent code"
	@echo "  clean              - Clean build artifacts"
	@echo "  install-deps       - Install development dependencies"
	@echo "  dev                - Start development environment"
	@echo "  docker             - Build Docker images"
	@echo "  packages           - Build all packages (deb/rpm/apk/archlinux)"
	@echo "  packages-agent     - Build agent packages only"
	@echo "  packages-server    - Build server packages only"
	@echo ""
	@echo "Package versioning:  make packages VERSION=1.2.3 ARCH=amd64"

# Build server
server:
	@echo "Building server..."
	cd server && go build -o server ./cmd/server

# Build frontend
frontend:
	@echo "Building frontend..."
	cd server/web/frontend && npm install && npm run build

# Build agent
agent:
	@echo "Building agent..."
	cd agent && go build -o bor-agent ./cmd/agent

# Generate Protocol Buffers (Go + TypeScript)
proto: proto-go proto-ts

proto-go:
	@echo "Generating Go protobuf code..."
	mkdir -p server/pkg/grpc/policy
	protoc --go_out=server/pkg/grpc/policy --go_opt=paths=source_relative \
		--go-grpc_out=server/pkg/grpc/policy --go-grpc_opt=paths=source_relative \
		-I proto/policy proto/policy/*.proto

proto-ts:
	@echo "Generating TypeScript protobuf types..."
	mkdir -p server/web/frontend/src/generated/proto
	cd server/web/frontend && \
	protoc \
		--plugin=node_modules/.bin/protoc-gen-ts_proto \
		--ts_proto_out=src/generated/proto \
		--ts_proto_opt=onlyTypes=true,snakeToCamel=false,outputServices=false \
		-I ../../../proto/policy \
		../../../proto/policy/*.proto

# Run all tests
test: test-server test-agent

# Run server tests
test-server:
	@echo "Running server tests..."
	cd server && go test -v ./...

# Run agent tests
test-agent:
	@echo "Running agent tests..."
	cd agent && go test -v ./...

# Run all linters
lint: lint-server lint-agent

# Lint server code
lint-server:
	@echo "Linting server code..."
	cd server && golangci-lint run ./...

# Lint agent code
lint-agent:
	@echo "Linting agent code..."
	cd agent && golangci-lint run ./...

# Build packages (deb, rpm, apk, archlinux) using nfpm
packages: packages-agent packages-server

packages-agent: agent
	@echo "Building bor-agent packages (version=$(VERSION) arch=$(ARCH))..."
	@mkdir -p builds
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-agent.yaml --packager deb        --target builds/
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-agent.yaml --packager rpm        --target builds/
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-agent.yaml --packager apk        --target builds/
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-agent.yaml --packager archlinux  --target builds/
	@echo "Agent packages written to builds/"

packages-server: server frontend
	@echo "Building bor-server packages (version=$(VERSION) arch=$(ARCH))..."
	@mkdir -p builds
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-server.yaml --packager deb        --target builds/
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-server.yaml --packager rpm        --target builds/
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-server.yaml --packager apk        --target builds/
	VERSION=$(VERSION) ARCH=$(ARCH) nfpm package --config packaging/nfpm-server.yaml --packager archlinux  --target builds/
	@echo "Server packages written to builds/"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf server/server
	rm -rf agent/bor-agent
	rm -rf server/pkg/grpc/policy/*.pb.go
	rm -rf server/web/frontend/src/generated/proto/
	rm -rf builds/

# Install development dependencies
install-deps:
	@echo "Installing development dependencies..."
	@echo "Installing Go tools..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
	@echo "Installing TypeScript proto plugin..."
	cd server/web/frontend && npm install
	@echo ""
	@echo "Note: Please ensure you have installed:"
	@echo "  - Protocol Buffers compiler (protoc)"

# Start development environment
dev:
	@echo "Starting development environment..."
	podman-compose up -d postgres
	@echo "Database started on localhost:5432"
	@echo ""
	@echo "To run server: make server && cd server && ./server"
	@echo "To run agent: make agent && cd agent && ./bor-agent"

# Build Docker images
docker:
	@echo "Building Docker images..."
	podman build -t bor-server:latest -f server/Containerfile .

# Format code
fmt:
	@echo "Formatting code..."
	cd server && go fmt ./...
	cd agent && go fmt ./...

# Run server in development mode
run-server:
	cd server && go run ./cmd/server

# Database migrations
migrate-up:
	@echo "Running database migrations..."
	cd server && go run ./cmd/migrate up

migrate-down:
	@echo "Rolling back database migrations..."
	cd server && go run ./cmd/migrate down

# Coverage
coverage:
	@echo "Generating coverage report..."
	cd server && go test -coverprofile=coverage.out ./...
	cd server && go tool cover -html=coverage.out -o coverage.html
	cd agent && go test -coverprofile=coverage.out ./...
	cd agent && go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage reports generated"
