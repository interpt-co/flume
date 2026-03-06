package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	goredis "github.com/redis/go-redis/v9"

	"github.com/interpt-co/flume/internal/models"
	flumeredis "github.com/interpt-co/flume/internal/redis"
)

func newTestRedis(t *testing.T) (*flumeredis.Client, *flumeredis.Writer, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	client := flumeredis.NewClientFromRedis(rdb, "flume")
	writer := flumeredis.NewWriter(client)
	return client, writer, mr
}

func newTestManager(t *testing.T) (*ClientManager, *flumeredis.Writer, *miniredis.Miniredis) {
	t.Helper()
	client, writer, mr := newTestRedis(t)
	reader := flumeredis.NewReader(client)
	subscriber := flumeredis.NewSubscriber(client)
	mgr := NewClientManagerWithRedis(reader, subscriber, 50) // 50ms flush for faster tests
	return mgr, writer, mr
}

func newTestHTTPServer(t *testing.T, mgr *ClientManager) *httptest.Server {
	t.Helper()
	srv := NewServer("127.0.0.1", 0, mgr)
	return httptest.NewServer(srv.Handler())
}

func dialWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	return conn
}

func readWSMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) wsMessage {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	var msg wsMessage
	err := conn.ReadJSON(&msg)
	if err != nil {
		t.Fatalf("failed to read ws message: %v", err)
	}
	return msg
}

func TestStatusEndpoint(t *testing.T) {
	mgr, writer, _ := newTestManager(t)

	// Seed a pattern with some data.
	ctx := context.Background()
	writer.WriteBatch(ctx, "default", []models.LogMessage{
		{ID: "1", Content: "hello", Timestamp: time.Now(), Source: models.SourceStdin},
	}, 1000)

	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/status?pattern=default")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var status StatusInfo
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}

	if status.BufferCapacity != 1000 {
		t.Errorf("expected buffer_capacity=1000, got %d", status.BufferCapacity)
	}
	if status.BufferUsed != 1 {
		t.Errorf("expected buffer_used=1, got %d", status.BufferUsed)
	}
}

func TestRootEndpoint(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "Flume") {
		t.Errorf("expected body to contain 'Flume', got %q", body)
	}
}

func TestWebSocketClientJoined(t *testing.T) {
	mgr, writer, _ := newTestManager(t)
	ctx := context.Background()
	writer.WriteBatch(ctx, "default", []models.LogMessage{
		{ID: "1", Content: "seed", Timestamp: time.Now(), Source: models.SourceStdin},
	}, 1000)

	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	conn := dialWS(t, ts)
	defer conn.Close()

	msg := readWSMessage(t, conn, 2*time.Second)

	if msg.Type != "client_joined" {
		t.Fatalf("expected type 'client_joined', got %q", msg.Type)
	}

	var data clientJoinedData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal client_joined data: %v", err)
	}
	if data.ClientID == "" {
		t.Error("expected non-empty client_id")
	}
	if len(data.Patterns) == 0 {
		t.Error("expected at least one pattern")
	}
}

func TestPingPong(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	conn := dialWS(t, ts)
	defer conn.Close()
	readWSMessage(t, conn, 2*time.Second) // consume client_joined

	pingMsg := `{"type":"ping"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(pingMsg)); err != nil {
		t.Fatalf("failed to send ping: %v", err)
	}

	msg := readWSMessage(t, conn, 2*time.Second)
	if msg.Type != "pong" {
		t.Errorf("expected type 'pong', got %q", msg.Type)
	}
}

func TestLoadRangeEndpoint(t *testing.T) {
	mgr, writer, _ := newTestManager(t)
	ctx := context.Background()

	// Seed Redis with messages.
	now := time.Now()
	msgs := make([]models.LogMessage, 10)
	for i := 0; i < 10; i++ {
		msgs[i] = models.LogMessage{
			ID:        "msg-" + string(rune('0'+i)),
			Content:   "message",
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			Source:    models.SourceStdin,
		}
	}
	writer.WriteBatch(ctx, "default", msgs, 1000)

	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/client/load?start=0&count=5&pattern=default")
	if err != nil {
		t.Fatalf("GET /api/client/load: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Messages []models.LogMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result.Messages))
	}
}

func TestPatternsEndpoint(t *testing.T) {
	mgr, writer, _ := newTestManager(t)
	ctx := context.Background()

	writer.WriteBatch(ctx, "alpha", []models.LogMessage{
		{ID: "1", Content: "a", Timestamp: time.Now(), Source: models.SourceStdin},
	}, 500)
	writer.WriteBatch(ctx, "beta", []models.LogMessage{
		{ID: "2", Content: "b", Timestamp: time.Now(), Source: models.SourceStdin},
	}, 1000)

	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/patterns")
	if err != nil {
		t.Fatalf("GET /api/patterns: %v", err)
	}
	defer resp.Body.Close()

	var patterns []PatternInfo
	if err := json.NewDecoder(resp.Body).Decode(&patterns); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(patterns) != 2 {
		t.Fatalf("expected 2 patterns, got %d", len(patterns))
	}
}

func TestLabelsEndpoint(t *testing.T) {
	mgr, writer, _ := newTestManager(t)
	ctx := context.Background()

	writer.WriteBatch(ctx, "default", []models.LogMessage{
		{ID: "1", Content: "a", Timestamp: time.Now(), Source: models.SourceStdin, Labels: map[string]string{"app": "web", "env": "prod"}},
		{ID: "2", Content: "b", Timestamp: time.Now().Add(time.Millisecond), Source: models.SourceStdin, Labels: map[string]string{"app": "api", "env": "prod"}},
	}, 1000)

	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/labels?pattern=default")
	if err != nil {
		t.Fatalf("GET /api/labels: %v", err)
	}
	defer resp.Body.Close()

	var labels map[string][]string
	if err := json.NewDecoder(resp.Body).Decode(&labels); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(labels["app"]) != 2 {
		t.Errorf("expected 2 app values, got %d", len(labels["app"]))
	}
}

func TestStatusEndpointReflectsClients(t *testing.T) {
	mgr, _, _ := newTestManager(t)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	conn := dialWS(t, ts)
	defer conn.Close()
	readWSMessage(t, conn, 2*time.Second)

	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()

	var status StatusInfo
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}

	if status.Clients != 1 {
		t.Errorf("expected clients=1, got %d", status.Clients)
	}
}

func TestWebSocketAuthDenied(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(struct {
			Allowed bool   `json:"allowed"`
			Reason  string `json:"reason"`
		}{false, "forbidden"})
	}))
	defer authServer.Close()

	mgr, _, _ := newTestManager(t)
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
