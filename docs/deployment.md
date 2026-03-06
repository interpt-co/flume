# Flume Deployment Guide

## Prerequisites

- Kubernetes cluster (1.24+)
- Helm 3
- (Optional) S3-compatible storage for log persistence

## Helm Installation

```bash
helm install flume deploy/helm/flume \
  --namespace flume \
  --create-namespace
```

## Configuration

All configuration is done via Helm values. See `deploy/helm/flume/values.yaml` for the full reference.

### Collector Settings

| Value | Default | Description |
|-------|---------|-------------|
| `collector.logDir` | `/var/log/containers` | Directory to watch for container log files |
| `collector.bufferSize` | `10000` | Per-pattern ring buffer capacity |
| `collector.resources` | `50m/64Mi` | CPU/memory requests |
| `collector.tolerations` | `[]` | Kubernetes tolerations for scheduling on all nodes |
| `collector.nodeSelector` | `{}` | Node selector constraints |

### Aggregator Settings

| Value | Default | Description |
|-------|---------|-------------|
| `aggregator.replicas` | `1` | Number of aggregator replicas |
| `aggregator.port` | `8080` | HTTP server port |
| `aggregator.grpcPort` | `9090` | gRPC server port (collectors connect here) |
| `aggregator.maxMessages` | `50000` | Per-pattern ring buffer capacity |
| `aggregator.bulkWindowMS` | `100` | WebSocket flush interval (ms) |
| `aggregator.verbose` | `false` | Enable debug logging |
| `aggregator.authUrl` | `""` | Auth callback URL for WebSocket authorization |
| `aggregator.authTimeout` | `5s` | Timeout for auth callback requests |

### S3 Storage

| Value | Default | Description |
|-------|---------|-------------|
| `s3.enabled` | `false` | Enable S3 persistence |
| `s3.bucket` | `""` | S3 bucket name |
| `s3.prefix` | `flume` | Key prefix for all objects |
| `s3.region` | `""` | AWS region |
| `s3.endpoint` | `""` | Custom S3 endpoint (for MinIO) |
| `s3.flushInterval` | `10s` | How often to flush buffered messages to S3 |
| `s3.flushCount` | `1000` | Max messages per flush batch |
| `s3.retention` | `168h` | How long to keep S3 data (0 = forever) |

### Patterns

Patterns define label-based routing rules. Each pattern gets its own ring buffer and S3 partition.

```yaml
patterns:
  - name: all
    selector:
      matchLabels: {}          # matches everything

  - name: production
    selector:
      matchLabels:
        env: production

  - name: api
    selector:
      matchLabels:
        app: api-server
        env: production
```

A message matching multiple patterns is routed to all of them.

### Ingress

```yaml
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: flume.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - hosts:
        - flume.example.com
      secretName: flume-tls
```

## CLI Usage

### Aggregator

```bash
flume aggregator [flags]
```

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--host` | `FLUME_HOST` | `0.0.0.0` | Bind address |
| `--port` | `FLUME_PORT` | `8080` | HTTP port |
| `--grpc-port` | `FLUME_GRPC_PORT` | `9090` | gRPC port |
| `--max-messages` | `FLUME_MAX_MESSAGES` | `10000` | Ring buffer capacity |
| `--bulk-window-ms` | `FLUME_BULK_WINDOW_MS` | `100` | WebSocket flush interval (ms) |
| `--s3-bucket` | `FLUME_S3_BUCKET` | | S3 bucket for history |
| `--s3-prefix` | `FLUME_S3_PREFIX` | | S3 key prefix |
| `--s3-region` | `FLUME_S3_REGION` | | AWS region |
| `--s3-endpoint` | `FLUME_S3_ENDPOINT` | | Custom S3 endpoint |
| `--auth-url` | `FLUME_AUTH_URL` | | Auth callback URL |
| `--auth-timeout` | `FLUME_AUTH_TIMEOUT` | `5s` | Auth callback timeout |
| `--verbose` | `FLUME_VERBOSE` | `false` | Debug logging |

### Collector

```bash
flume collector [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--config` | `/etc/flume/config.yaml` | Path to collector config YAML |
| `--verbose` | `false` | Debug logging |

#### Collector Config YAML

```yaml
collector:
  logDir: /var/log/containers
  bufferSize: 10000
  verbose: false

  aggregator:
    addr: flume-aggregator:9090

  patterns:
    - name: all
      selector:
        matchLabels: {}

  s3:
    bucket: my-log-bucket
    prefix: flume
    region: us-east-1
    flushInterval: 10s
    flushCount: 1000
    retention: 168h

  # Optional: static labels for local testing (no K8s API needed)
  staticLabels:
    default/my-pod:
      app: my-app
      env: dev
```

## Auth Callback Integration

When `authUrl` is configured, every WebSocket upgrade triggers a POST to the auth service:

```
POST {authUrl}
Content-Type: application/json
Authorization: Bearer {forwarded from client}
Cookie: {forwarded from client}

{
  "filters": {"namespace": "production"},
  "pattern": "all"
}
```

Expected response:

```json
{"allowed": true}
```

Or:

```json
{"allowed": false, "reason": "Insufficient permissions"}
```

The auth service can also receive a bearer token via the `?token=` query parameter on the WebSocket URL, which is useful since browsers cannot set custom headers on WebSocket connections.

## Pre-Filtering

Pre-filters scope a WebSocket connection to a subset of logs. They are set via URL query parameters and cannot be changed after connection:

```
wss://flume.example.com/ws?filter=namespace:production,app:api
```

Pre-filtered keys are excluded from the label filter dropdown in the UI and cannot be overridden by the `set_filter` WebSocket command.

## Resource Sizing

| Deployment | Log Volume | Buffer Size | Memory |
|------------|-----------|-------------|--------|
| Small | < 100 msgs/s | 10,000 | 128Mi |
| Medium | 100-1000 msgs/s | 50,000 | 512Mi |
| Large | 1000+ msgs/s | 100,000 | 1Gi+ |

S3 storage grows at approximately 1 KB per log message (gzip compressed).
