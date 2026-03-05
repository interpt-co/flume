package source

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/vmihailenco/msgpack/v5"

	"github.com/interpt-co/flume/internal/models"
)

func TestForwardSourceName(t *testing.T) {
	src := NewForwardSource(":0")
	if src.Name() != "forward" {
		t.Errorf("Name() = %q, want %q", src.Name(), "forward")
	}
}

func TestForwardSourceImplementsSource(t *testing.T) {
	var _ Source = (*ForwardSource)(nil)
}

// TestForwardMessageMode tests decoding a single [tag, time, record] message.
func TestForwardMessageMode(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Encode a Message mode message: [tag, time, record]
	now := time.Now().Unix()
	record := map[string]interface{}{
		"log":    "hello world",
		"stream": "stdout",
	}

	enc := msgpack.NewEncoder(conn)
	enc.EncodeArrayLen(3)
	enc.EncodeString("kube.var.log.containers.myapp")
	enc.EncodeInt(now)
	enc.EncodeMap(record)

	// Read the message from the channel.
	select {
	case msg := <-ch:
		if msg.Source != models.SourceForward {
			t.Errorf("Source = %q, want %q", msg.Source, models.SourceForward)
		}
		if msg.Origin.Name != "kube.var.log.containers.myapp" {
			t.Errorf("Origin.Name = %q, want %q", msg.Origin.Name, "kube.var.log.containers.myapp")
		}
		if msg.Timestamp.Unix() != now {
			t.Errorf("Timestamp = %v, want epoch %d", msg.Timestamp, now)
		}
		if !msg.IsJson {
			t.Error("IsJson should be true")
		}

		// Verify the content is valid JSON containing our record.
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(msg.Content), &parsed); err != nil {
			t.Fatalf("Content is not valid JSON: %v", err)
		}
		if parsed["log"] != "hello world" {
			t.Errorf("Content[log] = %v, want %q", parsed["log"], "hello world")
		}
		if parsed["stream"] != "stdout" {
			t.Errorf("Content[stream] = %v, want %q", parsed["stream"], "stdout")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

// TestForwardForwardMode tests decoding a batch [tag, [[time, record], ...]] message.
func TestForwardForwardMode(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Encode a Forward mode message: [tag, [[time, record], [time, record]]]
	now := time.Now().Unix()
	enc := msgpack.NewEncoder(conn)
	enc.EncodeArrayLen(2) // [tag, entries]
	enc.EncodeString("app.logs")

	// Entries array with 2 items.
	enc.EncodeArrayLen(2)

	// Entry 1: [time, record]
	enc.EncodeArrayLen(2)
	enc.EncodeInt(now)
	enc.EncodeMap(map[string]interface{}{"log": "line one"})

	// Entry 2: [time, record]
	enc.EncodeArrayLen(2)
	enc.EncodeInt(now + 1)
	enc.EncodeMap(map[string]interface{}{"log": "line two"})

	// Read both messages from the channel.
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

	// Verify first message.
	if messages[0].Origin.Name != "app.logs" {
		t.Errorf("message[0].Origin.Name = %q, want %q", messages[0].Origin.Name, "app.logs")
	}
	var rec0 map[string]interface{}
	json.Unmarshal([]byte(messages[0].Content), &rec0)
	if rec0["log"] != "line one" {
		t.Errorf("message[0] log = %v, want %q", rec0["log"], "line one")
	}

	// Verify second message.
	var rec1 map[string]interface{}
	json.Unmarshal([]byte(messages[1].Content), &rec1)
	if rec1["log"] != "line two" {
		t.Errorf("message[1] log = %v, want %q", rec1["log"], "line two")
	}

	// Verify timestamps differ by 1 second.
	diff := messages[1].Timestamp.Unix() - messages[0].Timestamp.Unix()
	if diff != 1 {
		t.Errorf("timestamp diff = %d, want 1", diff)
	}
}

// TestForwardPackedForwardMode tests decoding a PackedForward [tag, msgpack_bin] message.
func TestForwardPackedForwardMode(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Build the packed binary: concatenated msgpack arrays of [time, record].
	now := time.Now().Unix()
	var packed []byte
	for i, logLine := range []string{"packed one", "packed two", "packed three"} {
		entry, _ := msgpack.Marshal([]interface{}{now + int64(i), map[string]interface{}{"log": logLine}})
		packed = append(packed, entry...)
	}

	// Encode PackedForward: [tag, bin]
	enc := msgpack.NewEncoder(conn)
	enc.EncodeArrayLen(2)
	enc.EncodeString("packed.tag")
	enc.EncodeBytes(packed)

	// Read 3 messages.
	var messages []models.LogMessage
	timeout := time.After(5 * time.Second)
	for len(messages) < 3 {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-timeout:
			t.Fatalf("timed out; got %d, want 3", len(messages))
		}
	}

	expectedLogs := []string{"packed one", "packed two", "packed three"}
	for i, want := range expectedLogs {
		var rec map[string]interface{}
		json.Unmarshal([]byte(messages[i].Content), &rec)
		if rec["log"] != want {
			t.Errorf("message[%d] log = %v, want %q", i, rec["log"], want)
		}
		if messages[i].Origin.Name != "packed.tag" {
			t.Errorf("message[%d].Origin.Name = %q, want %q", i, messages[i].Origin.Name, "packed.tag")
		}
	}
}

// TestForwardKubernetesLabels tests that Kubernetes metadata is extracted
// from the record into Labels.
func TestForwardKubernetesLabels(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	now := time.Now().Unix()
	record := map[string]interface{}{
		"log": "some log line",
		"kubernetes": map[string]interface{}{
			"pod_name":       "my-pod-abc123",
			"namespace_name": "default",
			"container_name": "app",
			"host":           "node-1",
			"labels": map[string]interface{}{
				"app": "myapp",
			},
		},
	}

	enc := msgpack.NewEncoder(conn)
	enc.EncodeArrayLen(3)
	enc.EncodeString("kube.logs")
	enc.EncodeInt(now)
	enc.EncodeMap(record)

	select {
	case msg := <-ch:
		if msg.Labels == nil {
			t.Fatal("Labels should not be nil when kubernetes metadata is present")
		}
		expected := map[string]string{
			"kubernetes.pod_name":       "my-pod-abc123",
			"kubernetes.namespace_name": "default",
			"kubernetes.container_name": "app",
			"kubernetes.host":           "node-1",
			"kubernetes.labels.app":     "myapp",
		}
		for k, want := range expected {
			got, ok := msg.Labels[k]
			if !ok {
				t.Errorf("Labels[%q] missing", k)
				continue
			}
			if got != want {
				t.Errorf("Labels[%q] = %q, want %q", k, got, want)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

// TestForwardNoKubernetesLabels tests that Labels is nil when no kubernetes
// metadata is present in the record.
func TestForwardNoKubernetesLabels(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	now := time.Now().Unix()
	record := map[string]interface{}{
		"log":    "plain log",
		"stream": "stderr",
	}

	enc := msgpack.NewEncoder(conn)
	enc.EncodeArrayLen(3)
	enc.EncodeString("plain.tag")
	enc.EncodeInt(now)
	enc.EncodeMap(record)

	select {
	case msg := <-ch:
		if msg.Labels != nil {
			t.Errorf("Labels should be nil, got %v", msg.Labels)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

// TestForwardAckChunk tests that the source responds with an ack when the
// options map contains a "chunk" key.
func TestForwardAckChunk(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	now := time.Now().Unix()
	chunkID := "test-chunk-123"

	// Message mode with options: [tag, time, record, {chunk: "..."}]
	enc := msgpack.NewEncoder(conn)
	enc.EncodeArrayLen(4)
	enc.EncodeString("ack.test")
	enc.EncodeInt(now)
	enc.EncodeMap(map[string]interface{}{"log": "ack me"})
	enc.EncodeMap(map[string]interface{}{"chunk": chunkID})

	// Drain the log message.
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	// Read the ack response from the connection.
	dec := msgpack.NewDecoder(conn)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	raw, err := dec.DecodeInterface()
	if err != nil {
		t.Fatalf("failed to decode ack response: %v", err)
	}

	ackMap := toStringKeyMap(raw)
	if ackMap == nil {
		t.Fatal("ack response is not a map")
	}
	if ackMap["ack"] != chunkID {
		t.Errorf("ack = %v, want %q", ackMap["ack"], chunkID)
	}
}

// TestForwardContextCancellation tests that the source shuts down gracefully.
func TestForwardContextCancellation(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	src := NewForwardSourceFromListener(l)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

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
