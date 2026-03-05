package source

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestDemoSourceName(t *testing.T) {
	src := NewDemoSource(10)
	if src.Name() != "demo" {
		t.Errorf("Name() = %q, want %q", src.Name(), "demo")
	}
}

func TestDemoSourceImplementsSource(t *testing.T) {
	var _ Source = (*DemoSource)(nil)
}

func TestDemoSourceRate(t *testing.T) {
	rate := 100
	src := NewDemoSource(rate)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Collect messages for 500ms.
	var count int
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()

loop:
	for {
		select {
		case <-ch:
			count++
		case <-timer.C:
			cancel()
			break loop
		}
	}

	// At 100/s for 500ms we expect ~50 messages. Allow generous range
	// because CI can be slow.
	if count < 20 || count > 80 {
		t.Errorf("expected ~50 messages in 500ms at rate %d, got %d", rate, count)
	}
}

func TestDemoSourceDefaultRate(t *testing.T) {
	// Rate <= 0 should default to 10.
	src := NewDemoSource(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Should produce at least one message within 500ms (default rate = 10/s).
	select {
	case msg := <-ch:
		if msg.Content == "" {
			t.Error("got empty content")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for first message with default rate")
	}
}

func TestDemoSourceVariedContent(t *testing.T) {
	src := NewDemoSource(200)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Collect 50 messages.
	var messages []models.LogMessage
	for len(messages) < 50 {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatalf("timed out; got %d messages", len(messages))
		}
	}
	cancel()

	// Verify not all the same content.
	unique := map[string]bool{}
	for _, m := range messages {
		unique[m.Content] = true
	}
	if len(unique) < 5 {
		t.Errorf("expected varied content, but only got %d unique messages out of 50", len(unique))
	}
}

func TestDemoSourceVariedLevels(t *testing.T) {
	src := NewDemoSource(500)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Collect 100 messages.
	var messages []models.LogMessage
	for len(messages) < 100 {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatalf("timed out; got %d messages", len(messages))
		}
	}
	cancel()

	// Check that we see at least 2 different level indicators.
	levelIndicators := []string{"DEBUG", "INFO", "WARN", "ERROR", `"debug"`, `"info"`, `"warn"`, `"error"`}
	found := map[string]bool{}
	for _, m := range messages {
		for _, ind := range levelIndicators {
			if strings.Contains(m.Content, ind) {
				found[strings.ToLower(strings.Trim(ind, `"`))] = true
			}
		}
	}
	if len(found) < 2 {
		t.Errorf("expected at least 2 different log levels, found %d: %v", len(found), found)
	}
}

func TestDemoSourceSomeJSON(t *testing.T) {
	src := NewDemoSource(500)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var jsonCount int
	var total int
	for total < 100 {
		select {
		case msg := <-ch:
			total++
			var js map[string]interface{}
			if json.Unmarshal([]byte(msg.Content), &js) == nil {
				jsonCount++
			}
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatalf("timed out; got %d messages", total)
		}
	}
	cancel()

	if jsonCount == 0 {
		t.Error("expected some JSON messages, got none")
	}
	if jsonCount == total {
		t.Error("expected some non-JSON messages, but all were JSON")
	}
}

func TestDemoSourceContextCancellation(t *testing.T) {
	src := NewDemoSource(10)

	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel immediately.
	cancel()

	// The channel should close promptly.
	drained := 0
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				// Channel closed — success.
				return
			}
			drained++
			if drained > 5 {
				t.Fatal("too many messages after cancel")
			}
		case <-timeout:
			t.Fatal("timed out waiting for channel to close after cancel")
		}
	}
}

func TestDemoSourceMessageFields(t *testing.T) {
	src := NewDemoSource(100)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	select {
	case msg := <-ch:
		if msg.Source != models.SourceDemo {
			t.Errorf("Source = %q, want %q", msg.Source, models.SourceDemo)
		}
		if msg.Origin.Name != "demo" {
			t.Errorf("Origin.Name = %q, want %q", msg.Origin.Name, "demo")
		}
		if msg.Timestamp.IsZero() {
			t.Error("Timestamp is zero")
		}
		if msg.Content == "" {
			t.Error("Content is empty")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}
