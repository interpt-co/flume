# flume

[![Go 1.24](https://img.shields.io/badge/go-1.24-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/interpt-co/flume/actions/workflows/ci.yml/badge.svg)](https://github.com/interpt-co/flume/actions)

Real-time log viewer and explorer: pipe any log stream into a browser-based UI over WebSocket, with search, JSON highlighting, and optional S3 persistence.

| Light | Dark |
|-------|------|
| ![Light theme](screenshots/light.png) | ![Dark theme](screenshots/dark.png) |
| ![Catppuccin Latte](screenshots/catppuccin-latte.png) | ![Catppuccin Mocha](screenshots/catppuccin-mocha.png) |

## Features

- **Multiple log sources** — stdin, file follow (`tail -f`), TCP socket, Fluent Bit Forward protocol, and a built-in demo generator
- **Real-time streaming** — logs delivered to the browser via WebSocket with configurable batched flush (default 100 ms)
- **Ring buffer** — configurable in-memory history (default 10 000 messages); scroll up to load older entries without reconnecting
- **S3 persistence** — optional time-partitioned gzipped JSON storage; scroll past the ring buffer to load from S3 history
- **Search and filter** — text substring or regex filter applied client-side in real time
- **JSON detection and highlighting** — structured log records are parsed and rendered as a collapsible JSON tree
- **Column configuration** — show/hide and reorder displayed fields
- **Four themes** — Default Light, Default Dark, Catppuccin Latte, and Catppuccin Mocha; base (light/dark) and palette (Default/Catppuccin) toggled independently via the UI
- **Auto-follow with pause/resume** — follows the tail automatically; scrolling up pauses, clicking the button or scrolling to the bottom resumes
- **Expandable log detail panel** — click any row for the full record, raw content, and all extracted fields
- **Kubernetes native** — designed for per-namespace Fluent Bit sidecar deployment with the included Helm chart
- **Single binary** — frontend assets embedded via `go:embed`; deploy one file

## Inspiration

flume was inspired by two projects:

- **[logdy](https://github.com/logdyhq/logdy-core)** — the single-binary, embedded-frontend, WebSocket-streaming architecture is directly modelled on logdy-core. If you want a more mature tool in the same space, check it out.
- **[Grafana Loki](https://grafana.com/oss/loki/)** — the label-based log aggregation concept and the project name. flume is *not* a Loki datasource; it is a lightweight alternative for teams that do not run a full Loki stack.

## Quick Start

### Install from source

```bash
git clone https://github.com/interpt-co/flume.git
cd flume
make build            # builds frontend, then backend
./bin/flume demo  # generates fake logs at 10 msg/s
```

Open `http://localhost:8080` in a browser.

### Docker

```bash
docker run --rm -p 8080:8080 interpt-co/flume demo --rate=50
```

## Source Modes

### stdin

Reads newline-delimited log lines from standard input. Works with any tool that writes to stdout.

```bash
kubectl logs -f my-pod | flume stdin
tail -f /var/log/app.log | flume stdin
journalctl -f | flume stdin
```

### follow

Follows one or more files, equivalent to `tail -f`. Accepts glob patterns via the shell.

```bash
flume follow --file=/var/log/app.log
flume follow --file=/var/log/app.log --file=/var/log/worker.log
flume follow --file="/var/log/*.log"
```

### socket

Listens on a TCP port for newline-delimited log lines. Any application that can write to a TCP socket can ship logs here.

```bash
flume socket --listen=:9999
# Send logs from another process:
echo '{"level":"info","msg":"hello"}' | nc localhost 9999
```

### forward

Accepts logs from [Fluent Bit](https://fluentbit.io/) using the Forward protocol (msgpack over TCP). Supports Message, Forward, and PackedForward modes, including Kubernetes metadata enrichment.

```bash
flume forward --listen=:24224
```

Fluent Bit configuration to send to flume:

```ini
[OUTPUT]
    Name    forward
    Match   *
    Host    flume
    Port    24224
```

### demo

Generates synthetic log messages at a configurable rate. Useful for development and UI testing.

```bash
flume demo --rate=100   # 100 messages per second
```

## Configuration

All flags can be set via environment variables using the `FLUME_` prefix. Flags take precedence over environment variables.

### Global flags

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--port` | `FLUME_PORT` | `8080` | HTTP server port |
| `--host` | `FLUME_HOST` | `0.0.0.0` | HTTP server bind address |
| `--max-messages` | `FLUME_MAX_MESSAGES` | `10000` | Ring buffer capacity (messages) |
| `--bulk-window-ms` | `FLUME_BULK_WINDOW_MS` | `100` | WebSocket flush interval in milliseconds |
| `--verbose` | `FLUME_VERBOSE` | `false` | Enable debug logging |

### Source-specific flags

| Command | Flag | Env var | Default | Description |
|---------|------|---------|---------|-------------|
| `follow` | `--file` | — | required | File path to follow; repeatable |
| `socket` | `--listen` | `FLUME_SOCKET_ADDR` | `:9999` | TCP address to listen on |
| `forward` | `--listen` | `FLUME_FORWARD_LISTEN` | `:24224` | TCP address for Fluent Bit Forward |
| `demo` | `--rate` | `FLUME_DEMO_RATE` | `10` | Messages per second |

### S3 storage flags

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--s3-bucket` | `FLUME_S3_BUCKET` | — | S3 bucket name (enables persistence when set) |
| `--s3-prefix` | `FLUME_S3_PREFIX` | — | Key prefix (e.g. namespace name) |
| `--s3-region` | `FLUME_S3_REGION` | — | AWS region |
| `--s3-endpoint` | `FLUME_S3_ENDPOINT` | — | Custom endpoint for MinIO or localstack |
| `--s3-flush-interval` | `FLUME_S3_FLUSH_INTERVAL` | `10s` | Maximum time between flushes |
| `--s3-flush-count` | `FLUME_S3_FLUSH_COUNT` | `1000` | Flush when this many messages are buffered |

## S3 Persistence

When `--s3-bucket` is set, flume writes log chunks to S3-compatible object storage as time-partitioned gzipped JSON files. The ring buffer still serves as the fast recent-history cache; the UI transparently falls back to S3 when the user scrolls past the ring buffer boundary.

**Key layout:**

```
{prefix}/{YYYY}/{MM}/{DD}/{HH}/chunk-{unix_ms}.json.gz
```

**AWS credentials** are resolved via the standard AWS SDK chain: environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`), `~/.aws/credentials`, or an IAM role (IRSA on EKS).

**Example — AWS S3:**

```bash
flume forward \
  --s3-bucket=my-logs \
  --s3-prefix=production \
  --s3-region=eu-west-1 \
  --s3-flush-interval=30s
```

**Example — MinIO:**

```bash
flume forward \
  --s3-bucket=logs \
  --s3-endpoint=http://minio:9000 \
  --s3-region=us-east-1
```

**History API** (only available when S3 is configured):

```
GET /api/history?before=2025-01-15T12:00:00Z&count=500
```

Returns up to `count` messages (max 1000) with timestamps before the given RFC3339 timestamp, ordered newest-first.

## Kubernetes Deployment

The Helm chart under `deploy/helm/flume/` deploys flume as a per-namespace pod, with a service that exposes both the web UI (port 8080) and the Fluent Bit Forward receiver (port 24224).

### Install with Helm

```bash
helm install flume ./deploy/helm/flume/ \
  --namespace my-app \
  --set flume.s3.bucket=my-logs \
  --set flume.s3.prefix=my-app \
  --set flume.s3.region=eu-west-1
```

### Helm values

Key values from `values.yaml`:

```yaml
image:
  repository: interpt-co/flume
  tag: ""           # defaults to chart appVersion

service:
  type: ClusterIP
  httpPort: 8080
  forwardPort: 24224

flume:
  maxMessages: 10000
  bulkWindowMs: 100
  verbose: false
  s3:
    bucket: ""
    prefix: ""
    region: ""
    endpoint: ""      # set for MinIO
    flushInterval: "10s"
    flushCount: 1000

serviceAccount:
  create: true
  annotations: {}   # use for EKS IRSA: eks.amazonaws.com/role-arn: arn:aws:iam::...
```

### Fluent Bit sidecar

Add a Fluent Bit sidecar to your application pods and point it at the flume service in the same namespace:

```yaml
# In your Pod spec, alongside your application container:
- name: fluent-bit
  image: fluent/fluent-bit:3
  args:
    - /fluent-bit/bin/fluent-bit
    - -c
    - /fluent-bit/etc/fluent-bit.conf
  volumeMounts:
    - name: varlog
      mountPath: /var/log
    - name: fluent-bit-config
      mountPath: /fluent-bit/etc
```

```ini
# fluent-bit.conf
[SERVICE]
    Flush         1
    Daemon        Off
    Log_Level     info

[INPUT]
    Name          tail
    Path          /var/log/containers/*.log
    Parser        docker
    Tag           kube.*
    Refresh_Interval 5

[FILTER]
    Name          kubernetes
    Match         kube.*
    Kube_URL      https://kubernetes.default.svc:443
    Merge_Log     On

[OUTPUT]
    Name          forward
    Match         *
    Host          flume
    Port          24224
```

### IRSA (EKS IAM Roles for Service Accounts)

To allow S3 writes via IRSA, annotate the service account:

```yaml
serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/flume-s3-role
```

The IAM role needs `s3:PutObject`, `s3:GetObject`, and `s3:ListObjectsV2` on your bucket.

## Web Component

The `<flume-viewer>` custom element lets you embed the log viewer in any web page. It runs in a shadow DOM with self-contained styles and manages its own WebSocket connection.

Build the web component:

```bash
cd web && npm run build:wc   # outputs to web/dist-wc/
```

Embed in a page:

```html
<script type="module" src="dist-wc/flume-viewer.js"></script>

<flume-viewer
  ws-url="ws://localhost:8080/ws"
  theme="dark"
  auto-follow="true"
  height="600px"
></flume-viewer>
```

| Attribute | Values | Default | Description |
|-----------|--------|---------|-------------|
| `ws-url` | WebSocket URL | required | flume WebSocket endpoint |
| `theme` | `light`, `dark` | `dark` | Initial color theme |
| `auto-follow` | `true`, `false` | `true` | Start in auto-follow mode |
| `show-search` | `true`, `false` | `true` | Show the filter bar |
| `height` | CSS length | `400px` | Component height |

**JS API** (via element reference):

```js
const viewer = document.querySelector('flume-viewer')
viewer.connect()
viewer.disconnect()
viewer.pause()
viewer.resume()
viewer.clear()
viewer.setTheme('light')
viewer.setFilter('error')
```

**Custom events:** `flume:connected`, `flume:disconnected`, `flume:message`, `flume:error`, `flume:row-click`.

## Architecture

```
                         flume process
 ┌─────────────────────────────────────────────────────────┐
 │                                                         │
 │  Source                Pipeline              Server     │
 │  ──────                ────────              ──────     │
 │  forward ──┐                                            │
 │  stdin   ──┤  <-chan    ┌─parse──┐  <-chan   WebSocket  │
 │  file    ──┼──LogMsg──> │enrich  │──LogMsg──> clients   │
 │  socket  ──┤           └────────┘     │                 │
 │  demo    ──┘                          │    Ring Buffer  │
 │                                       ├──> (in-memory)  │
 │                                       │                 │
 │                                       └──> S3 Storage   │
 │                                            (optional)   │
 └─────────────────────────────────────────────────────────┘
            WebSocket (ws://host/ws)
                      │
            ┌─────────┴─────────┐
            │  Browser (Vue 3)  │
            │  - log viewer     │
            │  - search/filter  │
            │  - JSON highlight │
            │  - history scroll │
            └───────────────────┘
```

**Data flow:**

1. A source produces `LogMessage` values on a channel
2. The processing pipeline runs two stages: JSON detection (`fastjson`) and enrichment (UUID, timestamp extraction, log level)
3. Processed messages are pushed to a thread-safe ring buffer and optionally teed to the S3 writer
4. Connected WebSocket clients receive batched message bursts flushed every `--bulk-window-ms` milliseconds
5. On connect, the client fetches recent history from the ring buffer via `GET /api/client/load`; scrolling past the buffer boundary triggers `GET /api/history` to load from S3

## REST API

| Endpoint | Description |
|----------|-------------|
| `GET /ws` | WebSocket connection for live log streaming |
| `GET /api/status` | Server status: connected clients, buffer used/capacity |
| `GET /api/client/load?start=N&count=N` | Paginate the ring buffer |
| `GET /api/history?before=RFC3339&count=N` | Historical messages from S3 (requires `--s3-bucket`) |

## Development

### Prerequisites

- Go 1.24+
- Node.js 22+

### Commands

```bash
make build            # build frontend + backend (output: bin/flume)
make build-frontend   # build Vue app only (output: web/dist/, internal/server/dist/)
make build-backend    # build Go binary only
make test             # run Go tests
make lint             # go vet + golangci-lint
make dev              # run backend without building frontend
make clean            # remove bin/, web/dist/, internal/server/dist/
```

### Frontend development server

```bash
cd web
npm install
npm run dev           # Vite dev server on http://localhost:5173
                      # proxies /ws and /api/* to a running flume backend
```

### Running a local backend for frontend dev

```bash
./bin/flume demo --rate=20
```

### Project layout

```
cmd/flume/       CLI entry point (Cobra)
internal/
  config/            Config struct + defaults
  models/            LogMessage type
  source/            Source implementations (stdin, file, socket, forward, demo)
  processing/        Pipeline stages (parse JSON, enrich with ID/ts/level)
  buffer/            Thread-safe generic ring buffer
  storage/           S3 backend + Storage interface
  server/            HTTP server, WebSocket, REST handlers, go:embed
  app/               Orchestrator wiring all components
web/                 Vue 3 + Vite + TypeScript + Pinia frontend
deploy/
  helm/flume/    Helm chart
```

## License

MIT — see [LICENSE](LICENSE).
