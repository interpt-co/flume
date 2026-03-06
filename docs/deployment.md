# Flume Deployment Guide

## Prerequisites

- Kubernetes cluster (1.24+)
- Helm 3
- Redis (standalone or cluster)
- (Optional) S3-compatible storage for log persistence

## Helm Installation

```bash
helm install flume deploy/helm/flume \
  --namespace flume \
  --create-namespace \
  --set redis.addr=redis:6379
```

## Configuration

All configuration is done via Helm values. See `deploy/helm/flume/values.yaml` for the full reference.

### Collector Settings

| Value | Default | Description |
|-------|---------|-------------|
| `collector.logDir` | `/var/log/containers` | Directory to watch for container log files |
| `collector.bufferSize` | `10000` | Buffer size for log batching |
| `collector.resources` | `50m/64Mi` | CPU/memory requests |
| `collector.tolerations` | `[]` | Kubernetes tolerations for scheduling on all nodes |
| `collector.nodeSelector` | `{}` | Node selector constraints |

### Dispatcher Settings

| Value | Default | Description |
|-------|---------|-------------|
| `dispatcher.replicas` | `2` | Number of dispatcher replicas |
| `dispatcher.port` | `8080` | HTTP server port |
| `dispatcher.bulkWindowMS` | `100` | WebSocket flush interval (ms) |
| `dispatcher.verbose` | `false` | Enable debug logging |
| `dispatcher.authUrl` | `""` | Auth callback URL for WebSocket authorization |
| `dispatcher.authTimeout` | `5s` | Timeout for auth callback requests |

### Redis Settings

| Value | Default | Description |
|-------|---------|-------------|
| `redis.addr` | `redis:6379` | Redis address |
| `redis.password` | `""` | Redis password |
| `redis.db` | `""` | Redis database number |

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

Patterns define label-based routing rules. Each pattern gets its own Redis sorted set and S3 partition.

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

### Dispatcher

```bash
flume dispatcher [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `0.0.0.0` | Bind address |
| `--port` | `8080` | HTTP port |
| `--redis-addr` | `localhost:6379` | Redis address |
| `--redis-password` | | Redis password |
| `--redis-db` | `0` | Redis database number |
| `--bulk-window-ms` | `100` | WebSocket flush interval (ms) |
| `--s3-bucket` | | S3 bucket for history |
| `--s3-prefix` | | S3 key prefix |
| `--s3-region` | | AWS region |
| `--s3-endpoint` | | Custom S3 endpoint |
| `--auth-url` | | Auth callback URL |
| `--auth-timeout` | `5s` | Auth callback timeout |
| `--verbose` | `false` | Debug logging |

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

  redis:
    addr: redis:6379
    password: ""
    db: 0

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

| Deployment | Log Volume | Dispatcher Replicas | Memory (per replica) |
|------------|-----------|---------------------|----------------------|
| Small | < 100 msgs/s | 1 | 128Mi |
| Medium | 100-1000 msgs/s | 2 | 256Mi |
| Large | 1000+ msgs/s | 3+ | 512Mi |

Redis memory depends on buffer capacity (configurable per pattern). S3 storage grows at approximately 1 KB per log message (gzip compressed).
