package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/config"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/server"
)

// getFreePort asks the OS for an available port.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestAppRunAndMessageFlow(t *testing.T) {
	port := getFreePort(t)

	cfg := config.Config{
		Host:         "127.0.0.1",
		Port:         port,
		MaxMessages:  100,
		BulkWindowMS: 50,
		Verbose:      false,
	}

	input := make(chan models.LogMessage, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run the app in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- New(cfg).Run(ctx, input)
	}()

	// Wait for the server to be ready.
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := http.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not become ready in time")
	}

	// Send a message through the input channel.
	input <- models.LogMessage{
		Content:   "hello from test",
		Source:    models.SourceStdin,
		Origin:    models.Origin{Name: "test"},
		Timestamp: time.Now(),
	}

	// Give the pipeline time to process the message and push it to the ring.
	time.Sleep(200 * time.Millisecond)

	// Check the status endpoint to verify the message was received.
	resp, err := http.Get(baseURL + "/api/status")
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var status server.StatusInfo
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}

	if status.Messages != 1 {
		t.Errorf("expected 1 message, got %d", status.Messages)
	}
	if status.BufferUsed != 1 {
		t.Errorf("expected buffer_used 1, got %d", status.BufferUsed)
	}
	if status.BufferCapacity != 100 {
		t.Errorf("expected buffer_capacity 100, got %d", status.BufferCapacity)
	}

	// Shut down.
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("app.Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("app.Run did not return in time")
	}
}
