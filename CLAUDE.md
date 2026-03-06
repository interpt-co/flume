# Flume

Kubernetes log collector and aggregator.

## Architecture

- **Backend:** Go (module: `github.com/interpt-co/flume`)
- **Frontend:** Vue.js (in `web/`)
- **Two modes:** `flume collector` (DaemonSet per node) and `flume aggregator` (central Deployment)

## Directory Layout

- `cmd/flume/` - CLI entry point (collector + aggregator subcommands)
- `internal/collector/` - Collector: CRI parser, file discovery, pod watcher, fan-out
- `internal/aggregator/` - Aggregator: wiring for gRPC server + HTTP/WS
- `internal/pattern/` - Pattern matching, registry, per-pattern state
- `internal/grpc/` - gRPC transport (client, server, tracker)
- `internal/models/` - Data models (LogMessage, KubeMeta)
- `internal/processing/` - Log processing and transformation
- `internal/buffer/` - In-memory ring buffer
- `internal/server/` - HTTP/WebSocket server
- `internal/storage/` - S3 persistence and cross-node reads
- `internal/query/` - Label filtering
- `internal/config/` - Configuration loading
- `api/proto/` - gRPC proto definitions
- `deploy/helm/flume/` - Helm chart
- `web/` - Vue.js frontend application

## Build

```bash
make build            # Build frontend then backend
make build-frontend   # Build frontend only
make build-backend    # Build backend only
make build-collector  # Build collector binary (CGO_ENABLED=0)
make build-aggregator # Build aggregator binary (CGO_ENABLED=0)
make dev-aggregator   # Run aggregator in dev mode
make dev-collector    # Run collector in dev mode
make test             # Run tests
make clean            # Remove build artifacts
make lint             # Run linters
```
