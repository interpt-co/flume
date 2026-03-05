package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestFileSourceName(t *testing.T) {
	src := NewFileSource([]string{"/tmp/test.log"})
	if src.Name() != "file" {
		t.Errorf("Name() = %q, want %q", src.Name(), "file")
	}
}

func TestFileSourceImplementsSource(t *testing.T) {
	var _ Source = (*FileSource)(nil)
}

func TestFileSourceSingleFile(t *testing.T) {
	// Create a temp file.
	f, err := os.CreateTemp("", "flume-file-test-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := NewFileSource([]string{f.Name()})
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give the tailer a moment to start.
	time.Sleep(100 * time.Millisecond)

	// Write lines to the file.
	lines := []string{"first line\n", "second line\n", "third line\n"}
	for _, line := range lines {
		_, err := f.WriteString(line)
		if err != nil {
			t.Fatalf("failed to write to temp file: %v", err)
		}
	}
	f.Sync()

	// Collect messages with a timeout.
	var messages []models.LogMessage
	timeout := time.After(5 * time.Second)
	for len(messages) < 3 {
		select {
		case msg := <-ch:
			messages = append(messages, msg)
		case <-timeout:
			t.Fatalf("timed out waiting for messages; got %d, want 3", len(messages))
		}
	}

	expected := []string{"first line", "second line", "third line"}
	for i, want := range expected {
		if messages[i].Content != want {
			t.Errorf("message[%d].Content = %q, want %q", i, messages[i].Content, want)
		}
		if messages[i].Source != models.SourceFile {
			t.Errorf("message[%d].Source = %q, want %q", i, messages[i].Source, models.SourceFile)
		}
		if messages[i].Timestamp.IsZero() {
			t.Errorf("message[%d].Timestamp is zero", i)
		}
	}
}

func TestFileSourceMultipleFiles(t *testing.T) {
	f1, err := os.CreateTemp("", "flume-file-test-a-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f1.Name())
	defer f1.Close()

	f2, err := os.CreateTemp("", "flume-file-test-b-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f2.Name())
	defer f2.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := NewFileSource([]string{f1.Name(), f2.Name()})
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	f1.WriteString("from file one\n")
	f1.Sync()
	f2.WriteString("from file two\n")
	f2.Sync()

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

	// Both messages should have arrived (order is not guaranteed).
	contents := map[string]bool{}
	for _, m := range messages {
		contents[m.Content] = true
	}
	if !contents["from file one"] {
		t.Error("missing message from file one")
	}
	if !contents["from file two"] {
		t.Error("missing message from file two")
	}
}

func TestFileSourceOriginMeta(t *testing.T) {
	f, err := os.CreateTemp("", "flume-file-test-meta-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := NewFileSource([]string{f.Name()})
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	f.WriteString("test line\n")
	f.Sync()

	select {
	case msg := <-ch:
		if msg.Origin.Name != filepath.Base(f.Name()) {
			t.Errorf("Origin.Name = %q, want %q", msg.Origin.Name, filepath.Base(f.Name()))
		}
		if msg.Origin.Meta == nil {
			t.Fatal("Origin.Meta is nil")
		}
		if msg.Origin.Meta["path"] != f.Name() {
			t.Errorf("Origin.Meta[\"path\"] = %q, want %q", msg.Origin.Meta["path"], f.Name())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestFileSourceContextCancellation(t *testing.T) {
	f, err := os.CreateTemp("", "flume-file-test-cancel-*.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())

	src := NewFileSource([]string{f.Name()})
	ch, err := src.Start(ctx)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Cancel the context.
	cancel()

	// The channel should close promptly.
	select {
	case _, ok := <-ch:
		if ok {
			// Draining any in-flight message is acceptable.
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel to close after cancel")
	}
}
