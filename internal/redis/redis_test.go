package redis

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/interpt-co/flume/internal/models"
)

func setupTest(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	client := NewClientFromRedis(rdb, "flume")
	return client, mr
}

func makeMsg(id, content string, ts time.Time, labels map[string]string) models.LogMessage {
	return models.LogMessage{
		ID:        id,
		Content:   content,
		Timestamp: ts,
		Source:    models.SourceContainer,
		Labels:    labels,
		Level:     "info",
	}
}

func TestWriteAndReadRoundTrip(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	now := time.Now().Truncate(time.Millisecond)
	msgs := []models.LogMessage{
		makeMsg("1", "hello", now, map[string]string{"app": "web"}),
		makeMsg("2", "world", now.Add(time.Millisecond), map[string]string{"app": "web", "env": "prod"}),
	}

	if err := writer.WriteBatch(ctx, "default", msgs, 100); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// Read back
	got, err := reader.GetMessages(ctx, "default", 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].ID != "1" || got[1].ID != "2" {
		t.Errorf("wrong order: got IDs %s, %s", got[0].ID, got[1].ID)
	}
}

func TestRingTrim(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	now := time.Now().Truncate(time.Millisecond)
	// Write 5 messages with bufCap=3
	for i := 0; i < 5; i++ {
		msg := makeMsg(
			string(rune('a'+i)),
			"msg",
			now.Add(time.Duration(i)*time.Millisecond),
			nil,
		)
		if err := writer.WriteBatch(ctx, "default", []models.LogMessage{msg}, 3); err != nil {
			t.Fatalf("WriteBatch %d: %v", i, err)
		}
	}

	count, err := reader.GetMessageCount(ctx, "default")
	if err != nil {
		t.Fatalf("GetMessageCount: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 messages after trim, got %d", count)
	}

	// Should have the newest 3 messages (indices 2,3,4 → IDs c, d, e)
	got, err := reader.GetMessages(ctx, "default", 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].ID != string(rune('c')) {
		t.Errorf("expected oldest remaining ID 'c', got %q", got[0].ID)
	}
}

func TestLabelIndex(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	now := time.Now()
	msgs := []models.LogMessage{
		makeMsg("1", "a", now, map[string]string{"app": "web", "env": "prod"}),
		makeMsg("2", "b", now.Add(time.Millisecond), map[string]string{"app": "api", "env": "prod"}),
	}

	if err := writer.WriteBatch(ctx, "default", msgs, 100); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	labels, err := reader.GetLabels(ctx, "default")
	if err != nil {
		t.Fatalf("GetLabels: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected 2 label keys, got %d", len(labels))
	}

	appVals := labels["app"]
	if len(appVals) != 2 {
		t.Errorf("expected 2 app values, got %d: %v", len(appVals), appVals)
	}
	envVals := labels["env"]
	if len(envVals) != 1 || envVals[0] != "prod" {
		t.Errorf("expected env=[prod], got %v", envVals)
	}
}

func TestStats(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	now := time.Now()
	msgs := []models.LogMessage{
		makeMsg("1", "a", now, nil),
		makeMsg("2", "b", now.Add(time.Millisecond), nil),
	}

	if err := writer.WriteBatch(ctx, "default", msgs, 1000); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	stats, err := reader.GetStats(ctx, "default")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.MessageCount != 2 {
		t.Errorf("expected message_count=2, got %d", stats.MessageCount)
	}
	if stats.BufferCapacity != 1000 {
		t.Errorf("expected buffer_capacity=1000, got %d", stats.BufferCapacity)
	}
}

func TestGetPatterns(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	now := time.Now()
	writer.WriteBatch(ctx, "alpha", []models.LogMessage{makeMsg("1", "a", now, nil)}, 100)
	writer.WriteBatch(ctx, "beta", []models.LogMessage{makeMsg("2", "b", now, nil)}, 100)

	patterns, err := reader.GetPatterns(ctx)
	if err != nil {
		t.Fatalf("GetPatterns: %v", err)
	}
	if len(patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d", len(patterns))
	}
}

func TestGetMessagesBefore(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	msgs := []models.LogMessage{
		makeMsg("1", "a", base, nil),
		makeMsg("2", "b", base.Add(time.Second), nil),
		makeMsg("3", "c", base.Add(2*time.Second), nil),
		makeMsg("4", "d", base.Add(3*time.Second), nil),
	}
	if err := writer.WriteBatch(ctx, "default", msgs, 100); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// Get messages before msg "3" timestamp (2s after base)
	beforeNano := base.Add(2 * time.Second).UnixNano()
	got, err := reader.GetMessagesBefore(ctx, "default", beforeNano, 10)
	if err != nil {
		t.Fatalf("GetMessagesBefore: %v", err)
	}
	// Should get msgs 1 and 2, newest-first
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].ID != "2" || got[1].ID != "1" {
		t.Errorf("expected newest-first [2,1], got [%s,%s]", got[0].ID, got[1].ID)
	}
}

func TestTimestampOrdering(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)
	reader := NewReader(client)

	// Write messages out of order
	base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	msgs := []models.LogMessage{
		makeMsg("3", "third", base.Add(2*time.Second), nil),
		makeMsg("1", "first", base, nil),
		makeMsg("2", "second", base.Add(time.Second), nil),
	}
	if err := writer.WriteBatch(ctx, "default", msgs, 100); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	got, err := reader.GetMessages(ctx, "default", 0, 10)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	// Should be sorted by timestamp regardless of insertion order
	if got[0].ID != "1" || got[1].ID != "2" || got[2].ID != "3" {
		t.Errorf("expected timestamp-ordered [1,2,3], got [%s,%s,%s]", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestPubSubDelivery(t *testing.T) {
	client, mr := setupTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	writer := NewWriter(client)
	sub := NewSubscriber(client)

	// Subscribe first
	ch := sub.Subscribe(ctx, "live")

	// Drain the backfill (empty since nothing written yet)
	// Give subscriber time to connect
	time.Sleep(50 * time.Millisecond)

	// Write a batch
	now := time.Now()
	msgs := []models.LogMessage{
		makeMsg("x", "live-msg", now, nil),
	}
	if err := writer.WriteBatch(ctx, "live", msgs, 100); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	// miniredis needs manual message propagation
	mr.SetTime(time.Now())

	// Read from subscriber channel — may get backfill first, then live
	var received []models.LogMessage
	timeout := time.After(2 * time.Second)
	for {
		select {
		case batch, ok := <-ch:
			if !ok {
				t.Fatal("channel closed unexpectedly")
			}
			received = append(received, batch...)
			// Check if we got our message
			for _, m := range received {
				if m.ID == "x" {
					return // success
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for pub/sub message, received %d messages", len(received))
		}
	}
}

func TestWriteBatchEmpty(t *testing.T) {
	client, _ := setupTest(t)
	ctx := context.Background()
	writer := NewWriter(client)

	// Writing empty batch should be a no-op
	if err := writer.WriteBatch(ctx, "default", nil, 100); err != nil {
		t.Fatalf("WriteBatch with nil: %v", err)
	}
	if err := writer.WriteBatch(ctx, "default", []models.LogMessage{}, 100); err != nil {
		t.Fatalf("WriteBatch with empty: %v", err)
	}
}

func TestPubSubPayloadFormat(t *testing.T) {
	// Verify that the payload published is a JSON array of LogMessage objects
	client, mr := setupTest(t)
	ctx := context.Background()

	// Subscribe to the raw Redis channel
	pubsub := client.rdb.Subscribe(ctx, client.channelKey("fmt"))
	defer pubsub.Close()
	_, err := pubsub.Receive(ctx)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	writer := NewWriter(client)
	now := time.Now().Truncate(time.Millisecond)
	msgs := []models.LogMessage{
		makeMsg("p1", "payload-test", now, map[string]string{"k": "v"}),
	}
	if err := writer.WriteBatch(ctx, "fmt", msgs, 100); err != nil {
		t.Fatalf("WriteBatch: %v", err)
	}

	mr.SetTime(time.Now())

	// Read one message
	msgCh := pubsub.Channel()
	select {
	case redisMsg := <-msgCh:
		var batch []json.RawMessage
		if err := json.Unmarshal([]byte(redisMsg.Payload), &batch); err != nil {
			t.Fatalf("payload is not JSON array: %v", err)
		}
		if len(batch) != 1 {
			t.Fatalf("expected 1 item in batch, got %d", len(batch))
		}
		var msg models.LogMessage
		if err := json.Unmarshal(batch[0], &msg); err != nil {
			t.Fatalf("batch item not a LogMessage: %v", err)
		}
		if msg.ID != "p1" {
			t.Errorf("expected ID p1, got %s", msg.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pub/sub message")
	}
}
