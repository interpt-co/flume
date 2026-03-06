# Client Pre-Filters + Auth Callback

## Overview

Clients connect to the aggregator with immutable pre-filters that scope what logs they can see. An optional auth callback validates that the client is authorized for the requested filters before the WebSocket upgrade completes.

## Connection Flow

1. Browser requests `/ws?filter=namespace:production,app:odoo&pattern=all` with `Authorization`/`Cookie` headers
2. If `--auth-url` is configured, aggregator POSTs to it with filters + forwarded headers
3. Auth service returns allow/deny
4. If allowed (or no auth configured): upgrade to WebSocket, apply pre-filters
5. If denied: respond with HTTP 403, no upgrade

## Filter Syntax

Query param: `filter=key:value,key:value`

Same `matchLabels` semantics as collector patterns — all key-value pairs must match for a message to pass.

Examples:
- `/ws?filter=namespace:production,app:odoo`
- `/ws?filter=team:platform`
- No filter param = see everything (subject to auth)

## Server-Side Enforcement

Pre-filters are immutable on the Client — set at connect, cannot be changed via `set_filter`. They stack with user-set label filters:

    message passes = preFilter.Matches(msg) AND labelFilter.Matches(msg)

Applied in:
- Subscriber fan-out (live streaming)
- `/api/client/load` (ring buffer reads)
- `/api/history` (S3 reads)
- `/api/labels` (excludes pre-filtered keys from response)

## Auth Callback

### Config

Aggregator flags:
- `--auth-url URL` — POST target for auth checks (empty = disabled)
- `--auth-timeout DURATION` — timeout for auth request (default 5s)

### Request

POST to the configured URL:

```json
{
  "filters": {"namespace": "production", "app": "odoo"},
  "pattern": "all"
}
```

Headers forwarded from WS upgrade: `Authorization`, `Cookie`.

### Response

| Response | Result |
|----------|--------|
| 200 `{"allowed": true}` | Proceed |
| 200 `{"allowed": false, "reason": "..."}` | Reject 403 |
| 401 / 403 | Reject |
| Timeout / 5xx | Reject (fail closed) |

## Frontend

- Reads `filter` from `window.location.search` on load
- Passes to WS connection URL and all API calls
- `client_joined` includes `pre_filters` so frontend knows locked dimensions
- Label bar hides keys in pre_filters
- Non-removable scope badges shown for active pre-filters

## Unchanged

- Collector patterns, fan-out, gRPC — untouched
- Pattern selector — works within pre-filter scope
- User label filters (`set_filter`) — additive on top of pre-filters
