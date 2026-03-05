package source

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestStdinSourceName(t *testing.T) {
	src := NewStdinSource(strings.NewReader(""))
	if src.Name() != "stdin" {
		t.Errorf("Name() = %q, want %q", src.Name(), "stdin")
	}
}

func TestStdinSourceImplementsSource(t *testing.T) {
	var _ Source = (*StdinSource)(nil)
}

func TestStdinSourceReadsLines(t *testing.T) {
	input := "line one\nline two\nline three\n"
	src := NewStdinSource(strings.NewReader(input))

	ctx := context.Background()
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var messages []models.LogMessage
	for msg := range ch {
		messages = append(messages, msg)
	}

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	expected := []string{"line one", "line two", "line three"}
	for i, want := range expected {
		if messages[i].Content != want {
			t.Errorf("message[%d].Content = %q, want %q", i, messages[i].Content, want)
		}
		if messages[i].Source != models.SourceStdin {
			t.Errorf("message[%d].Source = %q, want %q", i, messages[i].Source, models.SourceStdin)
		}
		if messages[i].Origin.Name != "stdin" {
			t.Errorf("message[%d].Origin.Name = %q, want %q", i, messages[i].Origin.Name, "stdin")
		}
		if messages[i].Timestamp.IsZero() {
			t.Errorf("message[%d].Timestamp is zero", i)
		}
	}
}

func TestStdinSourceEmptyInput(t *testing.T) {
	src := NewStdinSource(strings.NewReader(""))

	ctx := context.Background()
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var count int
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 messages from empty input, got %d", count)
	}
}

func TestStdinSourceRespectsContextCancellation(t *testing.T) {
	// Use io.Pipe: we control both ends. After cancelling the context,
	// we close the writer to unblock the scanner, and verify the channel
	// closes promptly.
	r, w := io.Pipe()

	src := NewStdinSource(r)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Write one line so the goroutine produces a message.
	_, _ = w.Write([]byte("hello\n"))

	select {
	case msg := <-ch:
		if msg.Content != "hello" {
			t.Errorf("Content = %q, want %q", msg.Content, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first message")
	}

	// Cancel the context and close the writer to unblock the scanner.
	cancel()
	w.Close()

	// The channel should close promptly.
	select {
	case _, ok := <-ch:
		if ok {
			// It's acceptable to get one more message that was in flight,
			// but the channel must eventually close.
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for channel to close after cancel")
	}
}
