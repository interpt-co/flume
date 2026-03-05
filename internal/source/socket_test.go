package source

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestSocketSourceName(t *testing.T) {
	src := NewSocketSource(":0")
	if src.Name() != "socket" {
		t.Errorf("Name() = %q, want %q", src.Name(), "socket")
	}
}

func TestSocketSourceImplementsSource(t *testing.T) {
	var _ Source = (*SocketSource)(nil)
}

func TestSocketSourceBasicConnection(t *testing.T) {
	// Create a listener on a random port for testability.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewSocketSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Connect and send lines.
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	lines := []string{"hello\n", "world\n"}
	for _, line := range lines {
		_, err := fmt.Fprint(conn, line)
		if err != nil {
			t.Fatalf("failed to write: %v", err)
		}
	}

	// Collect messages.
	var messages []models.LogMessage
	timeout := time.After(5 * time.Second)
	for len(messages) < 2 {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-timeout:
			t.Fatalf("timed out waiting for messages; got %d, want 2", len(messages))
		}
	}

	expected := []string{"hello", "world"}
	for i, want := range expected {
		if messages[i].Content != want {
			t.Errorf("message[%d].Content = %q, want %q", i, messages[i].Content, want)
		}
		if messages[i].Source != models.SourceSocket {
			t.Errorf("message[%d].Source = %q, want %q", i, messages[i].Source, models.SourceSocket)
		}
		if messages[i].Timestamp.IsZero() {
			t.Errorf("message[%d].Timestamp is zero", i)
		}
	}
}

func TestSocketSourceMultipleConnections(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewSocketSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Open two connections.
	conn1, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("conn1 dial failed: %v", err)
	}
	defer conn1.Close()

	conn2, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("conn2 dial failed: %v", err)
	}
	defer conn2.Close()

	fmt.Fprint(conn1, "from conn1\n")
	fmt.Fprint(conn2, "from conn2\n")

	// Collect messages.
	var messages []models.LogMessage
	timeout := time.After(5 * time.Second)
	for len(messages) < 2 {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-timeout:
			t.Fatalf("timed out; got %d messages, want 2", len(messages))
		}
	}

	contents := map[string]bool{}
	for _, m := range messages {
		contents[m.Content] = true
	}
	if !contents["from conn1"] {
		t.Error("missing message from conn1")
	}
	if !contents["from conn2"] {
		t.Error("missing message from conn2")
	}
}

func TestSocketSourceOriginName(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewSocketSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	fmt.Fprint(conn, "test\n")

	select {
	case msg := <-ch:
		// Origin.Name should be the remote address (ip:port).
		if msg.Origin.Name == "" {
			t.Error("Origin.Name is empty")
		}
		// Parse to verify it looks like an address.
		_, _, err := net.SplitHostPort(msg.Origin.Name)
		if err != nil {
			t.Errorf("Origin.Name %q is not a valid host:port: %v", msg.Origin.Name, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestSocketSourceContextCancellation(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewSocketSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel immediately.
	cancel()

	// The channel should close promptly.
	select {
	case _, ok := <-ch:
		if ok {
			// Draining any in-flight message is fine.
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel to close after cancel")
	}
}

func TestSocketSourceAddr(t *testing.T) {
	// Before Start, Addr should be nil for address-based constructor.
	src := NewSocketSource(":0")
	if src.Addr() != nil {
		t.Error("Addr() should be nil before Start()")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if src.Addr() == nil {
		t.Error("Addr() should not be nil after Start()")
	}
}
