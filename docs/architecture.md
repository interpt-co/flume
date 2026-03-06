# Flume Architecture

Flume is a real-time Kubernetes log collector and aggregator. It runs as two components — a **Collector** DaemonSet on every node and a central **Aggregator** Deployment — and serves logs to browser clients via WebSocket.

## System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│  Kubernetes Cluster                                             │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   Node A      │  │   Node B      │  │   Node C      │        │
│  │  ┌──────────┐ │  │  ┌──────────┐ │  │  ┌──────────┐ │        │
│  │  │Collector │ │  │  │Collector │ │  │  │Collector │ │        │
│  │  │DaemonSet │ │  │  │DaemonSet │ │  │  │DaemonSet │ │        │
│  │  └────┬─────┘ │  │  └────┬─────┘ │  │  └────┬─────┘ │        │
│  └───────┼───────┘  └───────┼───────┘  └───────┼───────┘        │
│          │ gRPC              │ gRPC              │ gRPC           │
│          └──────────────────┼──────────────────┘                │
│                             ▼                                   │
│                   ┌─────────────────┐       ┌─────────┐         │
│                   │   Aggregator     │──────▶│   S3    │         │
│                   │   Deployment     │       │(optional)│        │
│                   └────────┬────────┘       └─────────┘         │
│                            │ HTTP/WS                            │
└────────────────────────────┼────────────────────────────────────┘
                             ▼
                    ┌─────────────────┐
                    │  Browser Clients │
                    │  (Vue.js SPA)    │
                    └─────────────────┘
```

## Components

### Collector (`cmd/flume collector`)

The Collector runs as a DaemonSet on every Kubernetes node. It:

1. **Discovers** container log files in `/var/log/containers/` using `fsnotify`
2. **Tails** each file with `nxadm/tail`, handling log rotation (rename + create)
3. **Parses** CRI log format lines (timestamp, stream, partial flag, content)
4. **Assembles** multi-line partial logs via the CRI Assembler
5. **Enriches** messages with pod labels from the Kubernetes API
6. **Dispatches** each message through a processing pipeline (JSON parse, level extraction)
7. **Routes** messages to per-pattern destinations: ring buffer, S3, and gRPC stream

#### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/collector` | Orchestration: wiring discovery → tail → pipeline → dispatch |
| `internal/collector/discovery` | `fsnotify`-based log file discovery |
| `internal/collector/cri` | CRI log format parser and multi-line assembler |
| `internal/collector/podwatch` | Kubernetes API pod label cache |
| `internal/collector/fanout` | Pattern-based message routing to multiple destinations |

### Aggregator (`cmd/flume aggregator`)

The Aggregator receives log streams from all Collectors and serves them to browser clients. It:

1. **Accepts** gRPC streams from Collectors (one stream per node×pattern pair)
2. **Ingests** log batches into per-pattern ring buffers
3. **Fans out** messages to subscribed WebSocket clients
4. **Serves** the embedded Vue.js frontend
5. **Provides** REST endpoints for status, labels, patterns, and history
6. **Reads** historical logs from S3 for the `/api/history` endpoint (optional)

#### Key Packages

| Package | Purpose |
|---------|---------|
| `internal/aggregator` | Orchestration: gRPC server + HTTP server wiring |
| `internal/grpc` | gRPC client (collector-side) and server (aggregator-side) |
| `internal/pattern` | Pattern registry, per-pattern ring buffer + subscriber fan-out |
| `internal/server` | HTTP/WebSocket server, client management, auth callback |
| `internal/storage` | S3 persistence: write chunks, read history, retention sweep |

## Data Flow

### Collector Pipeline

```
Log File → tail → CRI Parse → Assembler → Pipeline(Parse → Enrich) → Dispatcher
                                                                        ├─→ Ring Buffer
                                                                        ├─→ S3 Writer
                                                                        └─→ gRPC Stream
```

### Aggregator Pipeline

```
gRPC Stream → Ingester → Pattern.Ingest() → Ring Buffer
                                            └─→ Subscriber Fan-out → WebSocket Clients
```

## Pattern System

Patterns are label-based routing rules that group log messages. Each pattern:

- Has a **selector** with `matchLabels` (e.g., `app: web, env: production`)
- Maintains its own **ring buffer** (in-memory, configurable capacity)
- Has independent **S3 partitioning** (one S3 key prefix per pattern)
- Tracks **subscribers** (WebSocket clients watching this pattern)

The Collector evaluates patterns locally via the Dispatcher. A single log message can match multiple patterns and be routed to all of them.

## WebSocket Protocol

The frontend communicates with the Aggregator via a JSON-over-WebSocket protocol:

### Server → Client

| Message Type | Description |
|-------------|-------------|
| `client_joined` | Connection established; includes client ID, buffer size, available patterns, pre-filters |
| `log_bulk` | Batch of log messages (flushed at configurable interval, default 100ms) |
| `pattern_changed` | Confirmation of pattern switch with new buffer stats |
| `pong` | Response to client ping |

### Client → Server

| Message Type | Description |
|-------------|-------------|
| `set_status` | Pause (`stopped`) or resume (`following`) live message delivery |
| `set_filter` | Set label filter for server-side message filtering |
| `set_pattern` | Switch to a different pattern |
| `load_range` | Request a range of buffered messages |
| `ping` | Keep-alive ping |

## S3 Storage Layout

```
{prefix}/
  {node}/
    {pattern}/
      {YYYY}/{MM}/{DD}/{HH}/
        chunk-{unix_ms}.json.gz      # Gzipped JSON array of LogMessage
        manifest.json                 # Per-hour index of chunks with metadata
```

When `PartitionLabel` is configured, messages are further grouped:

```
{prefix}/
  {partition_value}/
    {YYYY}/{MM}/{DD}/{HH}/
      chunk-{unix_ms}.json.gz
      manifest.json
```

## Auth Callback

The Aggregator supports an optional auth callback for WebSocket upgrades:

1. Client connects to `/ws?filter=namespace:prod&pattern=all`
2. Before upgrading, the server POSTs to the configured auth URL with the filter and pattern
3. The auth service returns `{"allowed": true}` or `{"allowed": false, "reason": "..."}`
4. `Authorization` and `Cookie` headers are forwarded from the original request

## Frontend

The Vue.js SPA (`web/src/`) provides:

- **Real-time log viewer** with virtual scrolling (handles thousands of rows)
- **Pattern selector** for switching between pattern views
- **Label-based filtering** (server-side + client-side)
- **Text/regex search** across log content
- **Pre-filter scoping** via URL query params (`?filter=namespace:prod`)
- **History loading** — infinite scroll up loads older logs from S3
- **JSON syntax highlighting** for structured log messages
- **Dark/light theme** with multiple color palettes
- **Column configuration** (show/hide timestamp, level, source)
