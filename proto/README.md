# Protocol Buffers Definitions

This directory contains the Protocol Buffer (.proto) definitions for the Bor project.

## Structure

- `policy/` - Policy service definitions

## Generating Code

### Go

```bash
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    proto/policy/*.proto
```

### C++

```bash
protoc --cpp_out=agent/src/generated \
    --grpc_out=agent/src/generated \
    --plugin=protoc-gen-grpc=`which grpc_cpp_plugin` \
    proto/policy/*.proto
```

## API Documentation

See the individual .proto files for detailed API documentation.

## Versioning

We follow semantic versioning for our APIs:
- Major version changes indicate breaking changes
- Minor version changes add new features
- Patch version changes are bug fixes

The API version is encoded in the package name (e.g., `bor.policy.v1`).
