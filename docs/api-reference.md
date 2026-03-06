# Flume API Reference

## REST Endpoints

All endpoints are served by the Dispatcher on the configured HTTP port (default `8080`).

---

### `GET /api/status`

Returns current server status.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `pattern` | string | Pattern name for scoped stats. Defaults to first pattern. |

**Response:**

```json
{
  "clients": 3,
  "messages": 154230,
  "buffer_used": 10000,
  "buffer_capacity": 50000
}
```

---

### `GET /api/client/load`

Returns a range of messages from the Redis sorted set.

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `start` | int | `0` | Logical index (0 = oldest) |
| `count` | int | `100` | Number of messages (max 1000) |
| `pattern` | string | first pattern | Pattern name |
| `filter` | string | | Pre-filter labels, e.g. `namespace:prod,app:web` |

**Response:**

```json
{
  "messages": [
    {
      "id": "abc123",
      "content": "Starting server on :8080",
      "ts": "2026-03-06T12:30:45.123Z",
      "source": "container",
      "level": "info",
      "is_json": false,
      "labels": {
        "namespace": "default",
        "pod": "api-server-abc123",
        "container": "api",
        "app": "api-server"
      },
      "origin": {
        "name": "node-1"
      }
    }
  ],
  "total": 154230
}
```

---

### `GET /api/labels`

Returns distinct label keys and their values from Redis.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `pattern` | string | Pattern name. Defaults to first pattern. |
| `filter` | string | Pre-filter labels to exclude from the response. |

**Response:**

```json
{
  "namespace": ["default", "kube-system", "monitoring"],
  "pod": ["api-server-abc123", "worker-def456"],
  "level": ["info", "warn", "error"],
  "app": ["api-server", "worker"]
}
```

---

### `GET /api/patterns`

Returns available patterns and their stats.

**Response:**

```json
[
  {
    "name": "all",
    "buffer_used": 10000,
    "buffer_capacity": 50000,
    "message_count": 154230,
    "subscriber_count": 2
  },
  {
    "name": "production",
    "buffer_used": 5000,
    "buffer_capacity": 50000,
    "message_count": 82100,
    "subscriber_count": 1
  }
]
```

---

### `GET /api/history`

Returns historical log messages, reading from Redis first and falling back to S3 for older data.

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `before` | RFC3339 | now | Return messages older than this timestamp |
| `count` | int | `500` | Number of messages (max 1000) |
| `labels` | string | | Label filter, e.g. `level:error,namespace:prod` |
| `filter` | string | | Pre-filter labels (merged into label filter) |
| `pattern` | string | | Pattern name (required for cross-node S3 reads) |

**Response:**

```json
{
  "messages": [...],
  "has_more": true
}
```

---

### `GET /ws`

Upgrades to a WebSocket connection.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `filter` | string | Pre-filter labels applied server-side for the lifetime of the connection |
| `pattern` | string | Initial pattern to subscribe to |
| `token` | string | Bearer token for auth (alternative to Authorization header) |

---

## WebSocket Protocol

Messages are JSON objects with `type` and optional `data` fields.

### Server → Client Messages

#### `client_joined`

Sent immediately after connection.

```json
{
  "type": "client_joined",
  "data": {
    "client_id": "550e8400-e29b-41d4-a716-446655440000",
    "buffer_size": 50000,
    "patterns": ["all", "production", "staging"],
    "default_pattern": "all",
    "pre_filters": {"namespace": "production"}
  }
}
```

#### `log_bulk`

Batched log messages, flushed at the configured interval (default 100ms).

```json
{
  "type": "log_bulk",
  "data": {
    "messages": [
      {
        "id": "msg-uuid",
        "content": "Request processed in 42ms",
        "ts": "2026-03-06T12:30:45.123Z",
        "source": "container",
        "level": "info",
        "is_json": true,
        "json_content": {"method": "GET", "path": "/api/health", "duration_ms": 42},
        "labels": {"namespace": "default", "pod": "api-abc", "app": "api"},
        "origin": {"name": "node-1"}
      }
    ],
    "total": 154231
  }
}
```

#### `pattern_changed`

Sent after a successful `set_pattern` request.

```json
{
  "type": "pattern_changed",
  "data": {
    "pattern": "production",
    "buffer_size": 50000,
    "buffer_used": 12345
  }
}
```

#### `pong`

Response to a client `ping`.

```json
{"type": "pong"}
```

### Client → Server Messages

#### `set_status`

Pause or resume live message delivery.

```json
{"type": "set_status", "data": {"status": "stopped"}}
{"type": "set_status", "data": {"status": "following"}}
```

#### `set_filter`

Set label filter for server-side filtering. Keys that overlap with pre-filter keys are silently stripped.

```json
{"type": "set_filter", "data": {"labels": {"level": "error", "app": "api"}}}
```

#### `set_pattern`

Switch to a different pattern. Clears the client's buffer and triggers a `pattern_changed` response.

```json
{"type": "set_pattern", "data": {"pattern": "production"}}
```

#### `load_range`

Request a range of messages from the Redis sorted set.

```json
{"type": "load_range", "data": {"start": 0, "count": 100}}
```

#### `ping`

Keep-alive ping.

```json
{"type": "ping"}
```

---

## Log Message Schema

```json
{
  "id": "string",
  "content": "string",
  "ts": "RFC3339Nano string",
  "source": "container | forward | socket | demo | loki | stdin | file",
  "level": "debug | info | warn | error | fatal",
  "is_json": true,
  "json_content": {},
  "labels": {"key": "value"},
  "origin": {"name": "node-name", "meta": {}},
  "kube": {
    "namespace": "string",
    "pod": "string",
    "container": "string",
    "pod_uid": "string",
    "node_name": "string"
  }
}
```

Note: `origin.meta` and `kube` fields are optional and omitted when empty.
