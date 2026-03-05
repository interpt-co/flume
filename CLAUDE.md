# Flume

Open-source logs viewer, explorer, and analyzer.

## Architecture

- **Backend:** Go (module: `github.com/interpt-co/flume`)
- **Frontend:** Vue.js (in `web/`)
- **Monorepo** with backend and frontend in a single repository

## Directory Layout

- `cmd/flume/` - Main application entry point
- `internal/config/` - Configuration loading and management
- `internal/models/` - Data models and types
- `internal/source/` - Log source connectors (Loki, files, etc.)
- `internal/processing/` - Log processing and transformation
- `internal/buffer/` - In-memory log buffering
- `internal/server/` - HTTP/WebSocket server
- `internal/app/` - Application wiring and lifecycle
- `web/` - Vue.js frontend application

## Build

```bash
make build          # Build frontend then backend
make build-frontend # Build frontend only
make build-backend  # Build backend only
make dev            # Run in development mode
make clean          # Remove build artifacts
make lint           # Run linters
```
