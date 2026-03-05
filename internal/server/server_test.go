package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/interpt-co/flume/internal/buffer"
	"github.com/interpt-co/flume/internal/models"
)

// helper: create a ClientManager with a ring buffer of the given capacity.
func newTestManager(ringCap int) *ClientManager {
	ring := buffer.NewRing[models.LogMessage](ringCap)
	return NewClientManager(ring, 50) // 50ms flush for faster tests
}

// helper: create an httptest.Server from a Server's Handler.
func newTestHTTPServer(t *testing.T, mgr *ClientManager) *httptest.Server {
	t.Helper()
	srv := NewServer("127.0.0.1", 0, mgr)
	return httptest.NewServer(srv.Handler())
}

// helper: dial a WebSocket on the given httptest.Server.
func dialWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	return conn
}

// readWSMessage reads a single wsMessage from the connection with a timeout.
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
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/status")
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
	if status.Clients != 0 {
		t.Errorf("expected clients=0, got %d", status.Clients)
	}
}

func TestRootEndpoint(t *testing.T) {
	mgr := newTestManager(100)
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
	mgr := newTestManager(1000)
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
	if data.BufferSize != 1000 {
		t.Errorf("expected buffer_size=1000, got %d", data.BufferSize)
	}
}

func TestMessageDistribution(t *testing.T) {
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Read the client_joined message first.
	readWSMessage(t, conn, 2*time.Second)

	// Feed messages through ConsumeLoop.
	ch := make(chan models.LogMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go mgr.ConsumeLoop(ctx, ch)

	// Send a test message.
	ch <- models.LogMessage{
		ID:      "test-1",
		Content: "hello world",
		Source:  models.SourceStdin,
	}

	// Wait for the flush interval to deliver the batch.
	msg := readWSMessage(t, conn, 500*time.Millisecond)

	if msg.Type != "log_bulk" {
		t.Fatalf("expected type 'log_bulk', got %q", msg.Type)
	}

	var data logBulkData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal log_bulk data: %v", err)
	}
	if len(data.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(data.Messages))
	}
	if data.Messages[0].ID != "test-1" {
		t.Errorf("expected message ID 'test-1', got %q", data.Messages[0].ID)
	}
	if data.Total != 1 {
		t.Errorf("expected total=1, got %d", data.Total)
	}
}

func TestPauseResume(t *testing.T) {
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	conn := dialWS(t, ts)
	defer conn.Close()

	// Read client_joined.
	readWSMessage(t, conn, 2*time.Second)

	ch := make(chan models.LogMessage, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go mgr.ConsumeLoop(ctx, ch)

	// Pause the client.
	pauseMsg := `{"type":"set_status","data":{"status":"stopped"}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(pauseMsg)); err != nil {
		t.Fatalf("failed to send set_status: %v", err)
	}

	// Give the readPump time to process the command.
	time.Sleep(50 * time.Millisecond)

	// Send a message while paused — client should NOT receive it.
	ch <- models.LogMessage{
		ID:      "paused-1",
		Content: "should not arrive",
		Source:  models.SourceStdin,
	}

	// Wait past the flush interval.
	time.Sleep(100 * time.Millisecond)

	// Resume the client.
	resumeMsg := `{"type":"set_status","data":{"status":"following"}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(resumeMsg)); err != nil {
		t.Fatalf("failed to send set_status: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Send a message after resume — client SHOULD receive it.
	ch <- models.LogMessage{
		ID:      "resumed-1",
		Content: "should arrive",
		Source:  models.SourceStdin,
	}

	msg := readWSMessage(t, conn, 500*time.Millisecond)

	if msg.Type != "log_bulk" {
		t.Fatalf("expected type 'log_bulk', got %q", msg.Type)
	}

	var data logBulkData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal log_bulk data: %v", err)
	}

	// The batch should contain only the resumed message.
	for _, m := range data.Messages {
		if m.ID == "paused-1" {
			t.Error("received message that was sent while paused")
		}
	}

	found := false
	for _, m := range data.Messages {
		if m.ID == "resumed-1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("did not receive message sent after resume")
	}
}

func TestStatusEndpointReflectsClients(t *testing.T) {
	mgr := newTestManager(100)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	// Connect a WebSocket client.
	conn := dialWS(t, ts)
	defer conn.Close()
	readWSMessage(t, conn, 2*time.Second) // consume client_joined

	// Give the manager time to register the client.
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
		t.Errorf("expected clients=1 after WS connect, got %d", status.Clients)
	}
}

func TestPingPong(t *testing.T) {
	mgr := newTestManager(100)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	conn := dialWS(t, ts)
	defer conn.Close()
	readWSMessage(t, conn, 2*time.Second) // consume client_joined

	// Send a ping message.
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
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	// Pre-fill the ring buffer with 10 messages.
	for i := 0; i < 10; i++ {
		mgr.ring.Push(models.LogMessage{
			ID:      fmt.Sprintf("msg-%d", i),
			Content: fmt.Sprintf("message %d", i),
			Source:  models.SourceStdin,
		})
	}

	resp, err := http.Get(ts.URL + "/api/client/load?start=0&count=5")
	if err != nil {
		t.Fatalf("GET /api/client/load: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Messages []models.LogMessage `json:"messages"`
		Total    uint64              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].ID != "msg-0" {
		t.Errorf("expected first message ID 'msg-0', got %q", result.Messages[0].ID)
	}
	if result.Messages[4].ID != "msg-4" {
		t.Errorf("expected last message ID 'msg-4', got %q", result.Messages[4].ID)
	}
}

func TestLoadRangeEndpointDefaults(t *testing.T) {
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	// Pre-fill with 3 messages.
	for i := 0; i < 3; i++ {
		mgr.ring.Push(models.LogMessage{
			ID:      fmt.Sprintf("def-%d", i),
			Content: fmt.Sprintf("default message %d", i),
			Source:  models.SourceStdin,
		})
	}

	// Call without params — defaults should work (start=0, count=100).
	resp, err := http.Get(ts.URL + "/api/client/load")
	if err != nil {
		t.Fatalf("GET /api/client/load: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result struct {
		Messages []models.LogMessage `json:"messages"`
		Total    uint64              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Should return all 3 messages since count defaults to 100.
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages with default params, got %d", len(result.Messages))
	}
}

func TestLoadRange(t *testing.T) {
	mgr := newTestManager(1000)
	ts := newTestHTTPServer(t, mgr)
	defer ts.Close()

	// Pre-fill the ring buffer.
	for i := 0; i < 5; i++ {
		mgr.ring.Push(models.LogMessage{
			ID:      "buffered-" + string(rune('0'+i)),
			Content: "buffered message",
			Source:  models.SourceStdin,
		})
	}

	conn := dialWS(t, ts)
	defer conn.Close()
	readWSMessage(t, conn, 2*time.Second) // consume client_joined

	// Request a range from the buffer.
	loadMsg := `{"type":"load_range","data":{"start":1,"count":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(loadMsg)); err != nil {
		t.Fatalf("failed to send load_range: %v", err)
	}

	msg := readWSMessage(t, conn, 2*time.Second)
	if msg.Type != "log_bulk" {
		t.Fatalf("expected type 'log_bulk', got %q", msg.Type)
	}

	var data logBulkData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("failed to unmarshal log_bulk data: %v", err)
	}
	if len(data.Messages) != 2 {
		t.Fatalf("expected 2 messages from load_range, got %d", len(data.Messages))
	}
}
