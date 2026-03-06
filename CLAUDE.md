# Flume

Kubernetes log collector and dispatcher.

## Architecture

- **Backend:** Go (module: `github.com/interpt-co/flume`)
- **Frontend:** Vue.js (in `web/`)
- **Two modes:** `flume collector` (DaemonSet per node) and `flume dispatcher` (scalable Deployment)
- **Data plane:** Redis (sorted sets for buffering, pub/sub for live dispatch)

## Directory Layout

- `cmd/flume/` - CLI entry point (collector + dispatcher subcommands)
- `internal/collector/` - Collector: CRI parser, file discovery, pod watcher, fan-out
- `internal/dispatcher/` - Dispatcher: wiring for Redis + HTTP/WS server
- `internal/redis/` - Redis client, writer (Lua scripts), reader, subscriber
- `internal/pattern/` - Pattern matching (config, selector, matcher)
- `internal/models/` - Data models (LogMessage, KubeMeta)
- `internal/processing/` - Log processing and transformation
- `internal/server/` - HTTP/WebSocket server
- `internal/storage/` - S3 persistence and cross-node reads
- `internal/query/` - Label filtering
- `internal/config/` - Configuration loading
- `deploy/helm/flume/` - Helm chart
- `web/` - Vue.js frontend application

## Build

```bash
make build            # Build frontend then backend
make build-frontend   # Build frontend only
make build-backend    # Build backend only
make build-collector  # Build collector binary (CGO_ENABLED=0)
make build-dispatcher # Build dispatcher binary (CGO_ENABLED=0)
make dev-dispatcher   # Run dispatcher in dev mode
make dev-collector    # Run collector in dev mode (requires test-config.yaml)
make test             # Run tests
make clean            # Remove build artifacts
make lint             # Run linters
```
