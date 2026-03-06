# Client Pre-Filters & Auth Callback Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add immutable server-enforced pre-filters on WebSocket clients (via query params) and an optional auth callback that gates WebSocket upgrades.

**Architecture:** Pre-filters are parsed from `/ws?filter=key:val,key:val` on connect, stored immutably on the Client struct, and enforced in the subscriber fan-out filter function alongside user-set label filters. An optional `--auth-url` flag enables a POST-based auth callback before the WS upgrade completes. The frontend reads `filter` from `window.location.search` and passes it to WS and all API calls.

**Tech Stack:** Go (backend), Vue 3 + Pinia (frontend), existing `query.ParseLabels` for filter parsing

---

### Task 1: Add pre-filter to `client_joined` response type

**Files:**
- Modify: `internal/server/client.go:41-46`

**Step 1: Add `PreFilters` field to `clientJoinedData`**

In `internal/server/client.go`, add to the `clientJoinedData` struct:

```go
type clientJoinedData struct {
	ClientID       string            `json:"client_id"`
	BufferSize     int               `json:"buffer_size"`
	Patterns       []string          `json:"patterns,omitempty"`
	DefaultPattern string            `json:"default_pattern,omitempty"`
	PreFilters     map[string]string `json:"pre_filters,omitempty"`
}
```

**Step 2: Run tests to verify no regression**

Run: `cd /home/paulo/OwnCloud/developaulo/personal/work/interpt/tech/devs/flume && go test ./internal/server/ -v -count=1`
Expected: All existing tests PASS

**Step 3: Commit**

```bash
git add internal/server/client.go
git commit -m "feat: add pre_filters field to client_joined WS message"
```

---

### Task 2: Parse pre-filter from WS query params and apply to Client

**Files:**
- Modify: `internal/server/websocket.go:66-114`
- Modify: `internal/server/client.go:96-104`
- Test: `internal/server/server_test.go`

**Step 1: Write the failing test**

Add to `internal/server/server_test.go`:

```go
func TestWebSocketPreFilter(t *testing.T) {
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	// Connect with pre-filter.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?filter=ns:prod,app:web"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	defer conn.Close()

	msg := readWSMessage(t, conn, 2*time.Second)
	if msg.Type != "client_joined" {
		t.Fatalf("expected client_joined, got %q", msg.Type)
	}

	var data clientJoinedData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if data.PreFilters == nil {
		t.Fatal("expected pre_filters in client_joined")
	}
	if data.PreFilters["ns"] != "prod" {
		t.Errorf("expected pre_filters[ns]=prod, got %q", data.PreFilters["ns"])
	}
	if data.PreFilters["app"] != "web" {
		t.Errorf("expected pre_filters[app]=web, got %q", data.PreFilters["app"])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestWebSocketPreFilter -v -count=1`
Expected: FAIL — `pre_filters` is nil

**Step 3: Implement pre-filter parsing in HandleWS**

In `internal/server/websocket.go`, modify `HandleWS` to parse the filter query param and set it on the client before registration. Add import of `"github.com/interpt-co/flume/internal/query"`.

```go
func (m *ClientManager) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	id := uuid.New().String()
	c := newClient(id, conn, m)

	// Parse pre-filter from query params (immutable for this connection).
	if filterStr := r.URL.Query().Get("filter"); filterStr != "" {
		c.preFilter = map[string]string(query.ParseLabels(filterStr))
	}

	// Subscribe to pattern: from query param, or default to first available.
	if m.registry != nil {
		patternName := r.URL.Query().Get("pattern")
		if patternName == "" {
			names := m.registry.Names()
			if len(names) > 0 {
				patternName = names[0]
			}
		}
		if patternName != "" {
			c.subscribeToPattern(patternName)
		}
	}

	m.mu.Lock()
	m.clients[id] = c
	m.mu.Unlock()

	// Build client_joined data.
	joined := clientJoinedData{
		ClientID:   id,
		BufferSize: m.bufferCap(),
		PreFilters: c.preFilter,
	}
	if m.registry != nil {
		joined.Patterns = m.registry.Names()
		if c.pattern != "" {
			joined.DefaultPattern = c.pattern
		} else if len(joined.Patterns) > 0 {
			joined.DefaultPattern = joined.Patterns[0]
		}
	}

	c.writeJSON(wsMessage{
		Type: "client_joined",
		Data: mustMarshal(joined),
	})

	go c.readPump()
	go c.writePump()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestWebSocketPreFilter -v -count=1`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./internal/server/ -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/server/websocket.go internal/server/server_test.go
git commit -m "feat: parse pre-filter from WS query params"
```

---

### Task 3: Enforce pre-filter in message delivery

**Files:**
- Modify: `internal/server/client.go:106-156` (subscribeToPattern, handleSetPattern, Send)
- Test: `internal/server/server_test.go`

**Step 1: Write the failing test**

Add to `internal/server/server_test.go`:

```go
func TestPreFilterBlocksMessages(t *testing.T) {
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	// Connect with pre-filter: only ns=prod.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?filter=ns:prod"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	defer conn.Close()
	readWSMessage(t, conn, 2*time.Second) // consume client_joined

	ch := make(chan models.LogMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go mgr.ConsumeLoop(ctx, ch)

	// Send a message that matches the pre-filter.
	ch <- models.LogMessage{
		ID:      "match-1",
		Content: "should arrive",
		Source:  models.SourceStdin,
		Labels:  map[string]string{"ns": "prod"},
	}

	// Send a message that does NOT match.
	ch <- models.LogMessage{
		ID:      "nomatch-1",
		Content: "should not arrive",
		Source:  models.SourceStdin,
		Labels:  map[string]string{"ns": "staging"},
	}

	// Wait for flush.
	msg := readWSMessage(t, conn, 500*time.Millisecond)
	if msg.Type != "log_bulk" {
		t.Fatalf("expected log_bulk, got %q", msg.Type)
	}

	var data logBulkData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	for _, m := range data.Messages {
		if m.ID == "nomatch-1" {
			t.Error("received message that should have been blocked by pre-filter")
		}
	}

	found := false
	for _, m := range data.Messages {
		if m.ID == "match-1" {
			found = true
		}
	}
	if !found {
		t.Error("did not receive message that matches pre-filter")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestPreFilterBlocksMessages -v -count=1`
Expected: FAIL — `nomatch-1` arrives because `Send()` doesn't check `preFilter`

**Step 3: Implement pre-filter enforcement in Client.Send()**

In `internal/server/client.go`, modify `Send()` to check `preFilter` before `labelFilter`:

```go
func (c *Client) Send(msg models.LogMessage) {
	c.mu.Lock()
	st := c.status
	filter := c.labelFilter
	pre := c.preFilter
	c.mu.Unlock()

	if st == StatusStopped {
		return
	}

	if len(pre) > 0 && !query.LabelMatcher(pre).Matches(msg) {
		return
	}

	if len(filter) > 0 && !query.LabelMatcher(filter).Matches(msg) {
		return
	}

	select {
	case c.send <- msg:
	default:
	}
}
```

Also update `subscribeToPattern` and `handleSetPattern` filter functions to include `preFilter`:

```go
// In the Filter function used by both subscribeToPattern and handleSetPattern:
Filter: func(msg models.LogMessage) bool {
	c.mu.Lock()
	st := c.status
	filter := c.labelFilter
	pre := c.preFilter
	c.mu.Unlock()
	if st == StatusStopped {
		return false
	}
	if len(pre) > 0 && !query.LabelMatcher(pre).Matches(msg) {
		return false
	}
	if len(filter) > 0 {
		return query.LabelMatcher(filter).Matches(msg)
	}
	return true
},
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestPreFilterBlocksMessages -v -count=1`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./internal/server/ -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/server/client.go internal/server/server_test.go
git commit -m "feat: enforce pre-filter in message delivery"
```

---

### Task 4: Auth callback — backend

**Files:**
- Modify: `internal/server/websocket.go`
- Create: `internal/server/auth.go`
- Test: `internal/server/auth_test.go`

**Step 1: Write the test**

Create `internal/server/auth_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthCallbackAllowed(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body authRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.Filters["ns"] != "prod" {
			t.Errorf("expected filter ns=prod, got %v", body.Filters)
		}
		// Check forwarded auth header.
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected forwarded auth header")
		}
		json.NewEncoder(w).Encode(authResponse{Allowed: true})
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000} // 5s
	filters := map[string]string{"ns": "prod"}
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)
	fakeReq.Header.Set("Authorization", "Bearer test-token")

	allowed, reason := ac.Check(fakeReq, filters, "all")
	if !allowed {
		t.Errorf("expected allowed, got denied: %s", reason)
	}
}

func TestAuthCallbackDenied(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(authResponse{Allowed: false, Reason: "no access"})
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000}
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)

	allowed, reason := ac.Check(fakeReq, nil, "all")
	if allowed {
		t.Error("expected denied")
	}
	if reason != "no access" {
		t.Errorf("expected reason 'no access', got %q", reason)
	}
}

func TestAuthCallbackServerError(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer authServer.Close()

	ac := &AuthConfig{URL: authServer.URL, Timeout: 5_000_000_000}
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)

	allowed, _ := ac.Check(fakeReq, nil, "all")
	if allowed {
		t.Error("expected denied on server error (fail closed)")
	}
}

func TestAuthCallbackNilConfig(t *testing.T) {
	// When no auth is configured, Check should always allow.
	var ac *AuthConfig
	fakeReq, _ := http.NewRequest("GET", "/ws", nil)
	allowed, _ := ac.Check(fakeReq, nil, "all")
	if !allowed {
		t.Error("expected allowed when auth is not configured")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestAuthCallback -v -count=1`
Expected: FAIL — `authRequest`, `authResponse`, `AuthConfig` not defined

**Step 3: Implement auth.go**

Create `internal/server/auth.go`:

```go
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

// AuthConfig configures the optional auth callback for WebSocket upgrades.
type AuthConfig struct {
	URL     string        // POST target URL
	Timeout time.Duration // request timeout (default 5s)
}

// authRequest is the JSON body sent to the auth URL.
type authRequest struct {
	Filters map[string]string `json:"filters"`
	Pattern string            `json:"pattern"`
}

// authResponse is the expected JSON response from the auth URL.
type authResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Check calls the auth callback. Returns (true, "") if allowed or if auth is not configured.
// Returns (false, reason) if denied. Fails closed on errors.
func (ac *AuthConfig) Check(r *http.Request, filters map[string]string, pattern string) (bool, string) {
	if ac == nil || ac.URL == "" {
		return true, ""
	}

	body, _ := json.Marshal(authRequest{
		Filters: filters,
		Pattern: pattern,
	})

	timeout := ac.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest("POST", ac.URL, bytes.NewReader(body))
	if err != nil {
		return false, "auth request failed"
	}
	req.Header.Set("Content-Type", "application/json")

	// Forward auth-related headers from the original WS upgrade request.
	if v := r.Header.Get("Authorization"); v != "" {
		req.Header.Set("Authorization", v)
	}
	if v := r.Header.Get("Cookie"); v != "" {
		req.Header.Set("Cookie", v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "auth service unreachable"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, "unauthorized"
	}
	if resp.StatusCode != http.StatusOK {
		return false, "auth service error"
	}

	var result authResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "invalid auth response"
	}

	if !result.Allowed {
		reason := result.Reason
		if reason == "" {
			reason = "access denied"
		}
		return false, reason
	}

	return true, ""
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestAuthCallback -v -count=1`
Expected: All 4 PASS

**Step 5: Commit**

```bash
git add internal/server/auth.go internal/server/auth_test.go
git commit -m "feat: add auth callback for WebSocket upgrade"
```

---

### Task 5: Wire auth callback into HandleWS

**Files:**
- Modify: `internal/server/websocket.go:26-33,66-114`
- Test: `internal/server/server_test.go`

**Step 1: Write the failing test**

Add to `internal/server/server_test.go`:

```go
func TestWebSocketAuthDenied(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			Allowed bool   `json:"allowed"`
			Reason  string `json:"reason"`
		}{false, "forbidden"})
	}))
	defer authServer.Close()

	mgr := newTestManager(1000)
	mgr.SetAuthConfig(&AuthConfig{URL: authServer.URL, Timeout: 5 * time.Second})
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?filter=ns:prod"
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestWebSocketAuthDenied -v -count=1`
Expected: FAIL — `SetAuthConfig` not defined

**Step 3: Add auth field to ClientManager and gate HandleWS**

In `internal/server/websocket.go`:

Add `auth *AuthConfig` field to `ClientManager` struct and a setter:

```go
// In ClientManager struct, add:
	auth *AuthConfig // optional auth callback

// Add method:
func (m *ClientManager) SetAuthConfig(ac *AuthConfig) {
	m.auth = ac
}
```

Modify `HandleWS` to check auth before upgrading. The auth check must happen before `upgrader.Upgrade`. Move the filter parsing before the upgrade too:

```go
func (m *ClientManager) HandleWS(w http.ResponseWriter, r *http.Request) {
	// Parse pre-filter from query params.
	var preFilter map[string]string
	if filterStr := r.URL.Query().Get("filter"); filterStr != "" {
		preFilter = map[string]string(query.ParseLabels(filterStr))
	}

	patternName := r.URL.Query().Get("pattern")

	// Auth callback (before WebSocket upgrade).
	if allowed, reason := m.auth.Check(r, preFilter, patternName); !allowed {
		http.Error(w, reason, http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	id := uuid.New().String()
	c := newClient(id, conn, m)
	c.preFilter = preFilter

	// Subscribe to pattern: from query param, or default to first available.
	if m.registry != nil {
		if patternName == "" {
			names := m.registry.Names()
			if len(names) > 0 {
				patternName = names[0]
			}
		}
		if patternName != "" {
			c.subscribeToPattern(patternName)
		}
	}

	m.mu.Lock()
	m.clients[id] = c
	m.mu.Unlock()

	joined := clientJoinedData{
		ClientID:   id,
		BufferSize: m.bufferCap(),
		PreFilters: c.preFilter,
	}
	if m.registry != nil {
		joined.Patterns = m.registry.Names()
		if c.pattern != "" {
			joined.DefaultPattern = c.pattern
		} else if len(joined.Patterns) > 0 {
			joined.DefaultPattern = joined.Patterns[0]
		}
	}

	c.writeJSON(wsMessage{
		Type: "client_joined",
		Data: mustMarshal(joined),
	})

	go c.readPump()
	go c.writePump()
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestWebSocketAuthDenied -v -count=1`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./internal/server/ -v -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
git add internal/server/websocket.go internal/server/server_test.go
git commit -m "feat: wire auth callback into WebSocket upgrade"
```

---

### Task 6: Add CLI flags for auth

**Files:**
- Modify: `cmd/flume/main.go:47-70`
- Modify: `internal/aggregator/aggregator.go:17-30,57-115`

**Step 1: Add auth fields to aggregator Config**

In `internal/aggregator/aggregator.go`, add to `Config`:

```go
	AuthURL     string
	AuthTimeout time.Duration
```

In `Run()`, after creating the ClientManager, wire auth:

```go
	if a.cfg.AuthURL != "" {
		timeout := a.cfg.AuthTimeout
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		manager.SetAuthConfig(&server.AuthConfig{
			URL:     a.cfg.AuthURL,
			Timeout: timeout,
		})
		log.WithField("url", a.cfg.AuthURL).Info("auth callback enabled")
	}
```

**Step 2: Add CLI flags**

In `cmd/flume/main.go`, add to `aggregatorCmd.RunE`:

```go
		cfg.AuthURL = getStringFlag(cmd, "auth-url", "FLUME_AUTH_URL", "")
		cfg.AuthTimeout = getDurationFlag(cmd, "auth-timeout", "FLUME_AUTH_TIMEOUT", 5*time.Second)
```

In `init()`, add flags:

```go
	aggregatorCmd.Flags().String("auth-url", "", "Auth callback URL (POST) for WebSocket upgrade")
	aggregatorCmd.Flags().String("auth-timeout", "5s", "Timeout for auth callback requests")
```

**Step 3: Build to verify compilation**

Run: `cd /home/paulo/OwnCloud/developaulo/personal/work/interpt/tech/devs/flume && go build ./cmd/flume/`
Expected: Success

**Step 4: Commit**

```bash
git add cmd/flume/main.go internal/aggregator/aggregator.go
git commit -m "feat: add --auth-url and --auth-timeout aggregator flags"
```

---

### Task 7: Frontend — read pre-filter from URL and pass to WS/API

**Files:**
- Modify: `web/src/App.vue`
- Modify: `web/src/composables/useWebSocket.ts`
- Modify: `web/src/stores/connection.ts`
- Modify: `web/src/stores/labels.ts`
- Create: `web/src/stores/prefilter.ts`
- Modify: `web/src/types/index.ts`

**Step 1: Create pre-filter store**

Create `web/src/stores/prefilter.ts`:

```typescript
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const usePrefilterStore = defineStore('prefilter', () => {
  const filters = ref<Record<string, string>>({})

  const filterParam = computed(() => {
    const entries = Object.entries(filters.value)
    if (entries.length === 0) return ''
    return entries.map(([k, v]) => `${k}:${v}`).join(',')
  })

  const queryString = computed(() => {
    if (!filterParam.value) return ''
    return `filter=${encodeURIComponent(filterParam.value)}`
  })

  function loadFromURL() {
    const params = new URLSearchParams(window.location.search)
    const raw = params.get('filter')
    if (!raw) return

    const parsed: Record<string, string> = {}
    for (const pair of raw.split(',')) {
      const [key, ...rest] = pair.split(':')
      const val = rest.join(':')
      if (key && val) {
        parsed[key.trim()] = val.trim()
      }
    }
    filters.value = parsed
  }

  function setFromServer(preFilters: Record<string, string>) {
    if (preFilters && Object.keys(preFilters).length > 0) {
      filters.value = preFilters
    }
  }

  return {
    filters,
    filterParam,
    queryString,
    loadFromURL,
    setFromServer,
  }
})
```

**Step 2: Update types to include `pre_filters` in `ClientJoinedData`**

In `web/src/types/index.ts`, add to `ClientJoinedData`:

```typescript
export interface ClientJoinedData {
  client_id: string
  buffer_size: number
  patterns?: string[]
  default_pattern?: string
  pre_filters?: Record<string, string>
}
```

**Step 3: Update `useWebSocket.ts` to include filter in WS URL and handle `pre_filters`**

In `web/src/composables/useWebSocket.ts`:

- Import `usePrefilterStore`
- The `url` parameter is now constructed with the filter query string
- On `client_joined`, call `prefilterStore.setFromServer(data.pre_filters)`

```typescript
import { usePrefilterStore } from '../stores/prefilter'

// Inside useWebSocket(url):
const prefilterStore = usePrefilterStore()

// In handleMessage, case 'client_joined':
if (data.pre_filters) {
  prefilterStore.setFromServer(data.pre_filters)
}
```

**Step 4: Update `App.vue` to include filter in WS URL**

In `web/src/App.vue`:

```typescript
import { usePrefilterStore } from './stores/prefilter'

const prefilterStore = usePrefilterStore()
prefilterStore.loadFromURL()

const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
const filterQS = prefilterStore.queryString
const wsUrl = `${wsProtocol}//${window.location.host}/ws${filterQS ? '?' + filterQS : ''}`
```

**Step 5: Update `connection.ts` to include filter in API calls**

In `web/src/stores/connection.ts`:

- Import `usePrefilterStore`
- In `patternParam()`, also append filter param
- Rename to `queryParams()` for clarity

```typescript
import { usePrefilterStore } from './prefilter'

function queryParams(): string {
  const patternsStore = usePatternsStore()
  const prefilterStore = usePrefilterStore()
  const parts: string[] = []
  if (patternsStore.current) parts.push(`pattern=${encodeURIComponent(patternsStore.current)}`)
  if (prefilterStore.filterParam) parts.push(`filter=${encodeURIComponent(prefilterStore.filterParam)}`)
  return parts.length > 0 ? '?' + parts.join('&') : ''
}
```

Then replace all uses of `patternParam()` and the manual query string building with `queryParams()`.

**Step 6: Update `labels.ts` to include filter in API call**

In `web/src/stores/labels.ts`:

```typescript
import { usePrefilterStore } from './prefilter'

// In fetchLabels():
const prefilterStore = usePrefilterStore()
const parts: string[] = []
if (patternsStore.current) parts.push(`pattern=${encodeURIComponent(patternsStore.current)}`)
if (prefilterStore.filterParam) parts.push(`filter=${encodeURIComponent(prefilterStore.filterParam)}`)
const qs = parts.length > 0 ? '?' + parts.join('&') : ''
const res = await fetch(`/api/labels${qs}`)
```

**Step 7: Build frontend to verify compilation**

Run: `cd /home/paulo/OwnCloud/developaulo/personal/work/interpt/tech/devs/flume/web && npm run build`
Expected: Success

**Step 8: Commit**

```bash
git add web/src/stores/prefilter.ts web/src/types/index.ts web/src/composables/useWebSocket.ts web/src/App.vue web/src/stores/connection.ts web/src/stores/labels.ts
git commit -m "feat: frontend pre-filter support — URL parsing, WS, API calls"
```

---

### Task 8: Frontend — pre-filter badges and label bar exclusion

**Files:**
- Modify: `web/src/components/LabelFilter.vue`

**Step 1: Add pre-filter badges and hide pre-filtered keys**

In `web/src/components/LabelFilter.vue`:

- Import `usePrefilterStore`
- Filter out pre-filtered keys from `sortedKeys`
- Show non-removable scope badges for pre-filter entries

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useLabelsStore } from '../stores/labels'
import { usePrefilterStore } from '../stores/prefilter'

const labels = useLabelsStore()
const prefilterStore = usePrefilterStore()

const hasPrefilters = computed(() => Object.keys(prefilterStore.filters).length > 0)
const hasLabels = computed(() => Object.keys(labels.availableLabels).length > 0)
const hasActiveLabels = computed(() => Object.keys(labels.activeLabels).length > 0)
const showBar = computed(() => hasPrefilters.value || hasLabels.value)

// Exclude pre-filtered keys from the interactive label pills.
const sortedKeys = computed(() =>
  Object.keys(labels.availableLabels)
    .filter(k => !(k in prefilterStore.filters))
    .sort()
)

function toggleLabel(key: string, value: string) {
  if (labels.activeLabels[key] === value) {
    labels.removeLabel(key)
  } else {
    labels.setLabel(key, value)
  }
}
</script>

<template>
  <div v-if="showBar" class="label-filter">
    <div class="label-filter__pills">
      <!-- Pre-filter scope badges (non-removable) -->
      <template v-for="(val, key) in prefilterStore.filters" :key="`pre-${key}`">
        <span class="label-filter__scope-badge">{{ key }}:{{ val }}</span>
      </template>

      <!-- Interactive label pills -->
      <template v-for="key in sortedKeys" :key="key">
        <div class="label-filter__group">
          <span class="label-filter__key">{{ key }}:</span>
          <button
            v-for="val in labels.availableLabels[key]"
            :key="`${key}:${val}`"
            class="label-filter__pill"
            :class="{ 'label-filter__pill--active': labels.activeLabels[key] === val }"
            @click="toggleLabel(key, val)"
          >
            {{ val }}
          </button>
        </div>
      </template>
      <button
        v-if="hasActiveLabels"
        class="label-filter__clear"
        @click="labels.clearLabels()"
      >
        clear
      </button>
    </div>
  </div>
</template>
```

Add CSS for the scope badge:

```css
.label-filter__scope-badge {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 3px;
  background-color: var(--flume-accent);
  color: var(--flume-bg);
  font-weight: 600;
  user-select: none;
  opacity: 0.8;
}
```

**Step 2: Build frontend**

Run: `cd /home/paulo/OwnCloud/developaulo/personal/work/interpt/tech/devs/flume/web && npm run build`
Expected: Success

**Step 3: Commit**

```bash
git add web/src/components/LabelFilter.vue
git commit -m "feat: pre-filter scope badges and label bar exclusion"
```

---

### Task 9: Apply pre-filter to `/api/labels` response

**Files:**
- Modify: `internal/server/history.go:161-219`

The `/api/labels` endpoint should exclude labels that are part of the pre-filter from the response (since the user can't change them). The filter is passed as `?filter=key:val,key:val` query param.

**Step 1: Modify HandleLabels to filter out pre-filter keys**

In `internal/server/history.go`, in `HandleLabels`, after building the `result` map, remove any keys present in the `filter` query param:

```go
func (m *ClientManager) HandleLabels(w http.ResponseWriter, r *http.Request) {
	// ... existing ring buffer selection code stays the same ...

	// After building result map, remove pre-filter keys.
	preFilter := query.ParseLabels(r.URL.Query().Get("filter"))
	if len(preFilter) > 0 {
		for k := range preFilter {
			delete(result, k)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
```

**Step 2: Run all server tests**

Run: `go test ./internal/server/ -v -count=1`
Expected: All PASS

**Step 3: Commit**

```bash
git add internal/server/history.go
git commit -m "feat: exclude pre-filter keys from /api/labels response"
```

---

### Task 10: Rebuild embedded frontend and final verification

**Step 1: Rebuild everything**

Run: `cd /home/paulo/OwnCloud/developaulo/personal/work/interpt/tech/devs/flume && make build`
Expected: Frontend builds, Go binary compiles

**Step 2: Run all Go tests**

Run: `go test ./... -count=1`
Expected: All PASS

**Step 3: Commit the embedded dist**

```bash
git add internal/server/dist/
git commit -m "chore: rebuild embedded frontend with pre-filter support"
```
