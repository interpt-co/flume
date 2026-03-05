package processing

import (
	"context"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestNewPipeline_Integration(t *testing.T) {
	p, err := NewPipeline()
	if err != nil {
		t.Fatalf("NewPipeline() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	in := make(chan models.LogMessage, 3)
	in <- models.LogMessage{Content: `{"level":"error","msg":"boom"}`, Source: models.SourceStdin}
	in <- models.LogMessage{Content: "plain text log line", Source: models.SourceStdin}
	in <- models.LogMessage{Content: "2025-01-01 [WARN] disk full", Source: models.SourceFile}
	close(in)

	out, errs := p.Run(ctx, in)

	var results []models.LogMessage
	for msg := range out {
		results = append(results, msg)
	}
	// Drain errors.
	for err := range errs {
		t.Errorf("unexpected pipeline error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First message: JSON with level "error".
	r0 := results[0]
	if !r0.IsJson {
		t.Error("result[0]: expected IsJson=true")
	}
	if r0.Level != "error" {
		t.Errorf("result[0]: Level = %q, want %q", r0.Level, "error")
	}
	if r0.ID == "" {
		t.Error("result[0]: expected non-empty ID")
	}
	if r0.Timestamp.IsZero() {
		t.Error("result[0]: expected non-zero Timestamp")
	}

	// Second message: plain text, no level.
	r1 := results[1]
	if r1.IsJson {
		t.Error("result[1]: expected IsJson=false")
	}
	if r1.Level != "" {
		t.Errorf("result[1]: Level = %q, want empty", r1.Level)
	}
	if r1.ID == "" {
		t.Error("result[1]: expected non-empty ID")
	}

	// Third message: bracket level.
	r2 := results[2]
	if r2.IsJson {
		t.Error("result[2]: expected IsJson=false")
	}
	if r2.Level != "warn" {
		t.Errorf("result[2]: Level = %q, want %q", r2.Level, "warn")
	}
}

func TestNewPipeline_EmptyInput(t *testing.T) {
	p, err := NewPipeline()
	if err != nil {
		t.Fatalf("NewPipeline() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	in := make(chan models.LogMessage)
	close(in)

	out, errs := p.Run(ctx, in)

	var count int
	for range out {
		count++
	}
	for err := range errs {
		t.Errorf("unexpected pipeline error: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 results from empty input, got %d", count)
	}
}

func TestNewPipeline_PreservesOrder(t *testing.T) {
	p, err := NewPipeline()
	if err != nil {
		t.Fatalf("NewPipeline() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const n = 20
	in := make(chan models.LogMessage, n)
	for i := 0; i < n; i++ {
		in <- models.LogMessage{
			Content: "msg",
			Source:  models.SourceStdin,
			Labels:  map[string]string{"seq": string(rune('A' + i))},
		}
	}
	close(in)

	out, errs := p.Run(ctx, in)

	var results []models.LogMessage
	for msg := range out {
		results = append(results, msg)
	}
	for err := range errs {
		t.Errorf("unexpected pipeline error: %v", err)
	}

	if len(results) != n {
		t.Fatalf("expected %d results, got %d", n, len(results))
	}
	for i, r := range results {
		want := string(rune('A' + i))
		if r.Labels["seq"] != want {
			t.Errorf("result[%d]: seq = %q, want %q", i, r.Labels["seq"], want)
		}
	}
}
