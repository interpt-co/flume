package processing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestEnrichWorker_AssignsUUID(t *testing.T) {
	msg := models.LogMessage{Content: "hello"}
	out, err := EnrichWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID == "" {
		t.Error("expected a non-empty ID")
	}
	// UUIDs are 36 characters: 8-4-4-4-12
	if len(out.ID) != 36 {
		t.Errorf("expected UUID length 36, got %d: %q", len(out.ID), out.ID)
	}
}

func TestEnrichWorker_UniqueUUIDs(t *testing.T) {
	msg := models.LogMessage{Content: "hello"}
	out1, _ := EnrichWorker(context.Background(), msg)
	out2, _ := EnrichWorker(context.Background(), msg)
	if out1.ID == out2.ID {
		t.Error("expected different UUIDs for different calls")
	}
}

func TestEnrichWorker_FillsZeroTimestamp(t *testing.T) {
	msg := models.LogMessage{Content: "hello"}
	before := time.Now()
	out, err := EnrichWorker(context.Background(), msg)
	after := time.Now()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Timestamp.Before(before) || out.Timestamp.After(after) {
		t.Errorf("timestamp %v not in expected range [%v, %v]", out.Timestamp, before, after)
	}
}

func TestEnrichWorker_PreservesExistingTimestamp(t *testing.T) {
	ts := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	msg := models.LogMessage{Content: "hello", Timestamp: ts}
	out, err := EnrichWorker(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Timestamp.Equal(ts) {
		t.Errorf("timestamp changed: got %v, want %v", out.Timestamp, ts)
	}
}

func TestExtractLevel_JSONLevel(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"level field", `{"level":"error","msg":"boom"}`, "error"},
		{"severity field", `{"severity":"WARNING","msg":"hmm"}`, "warn"},
		{"info level", `{"level":"INFO","message":"ok"}`, "info"},
		{"debug level", `{"level":"debug","data":123}`, "debug"},
		{"unknown level", `{"level":"custom","msg":"x"}`, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := models.LogMessage{
				Content:     tc.content,
				IsJson:      true,
				JsonContent: json.RawMessage(tc.content),
			}
			got := extractLevel(msg)
			if got != tc.want {
				t.Errorf("extractLevel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractLevel_BracketPatterns(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"2025-01-01 [ERROR] something failed", "error"},
		{"[WARN] disk space low", "warn"},
		{"[INFO] server started on :8080", "info"},
		{"[DEBUG] query took 5ms", "debug"},
		{"[FATAL] cannot open database", "fatal"},
		{"[TRACE] entering function foo", "trace"},
		{"[WARNING] deprecated API", "warn"},
	}

	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			msg := models.LogMessage{Content: tc.content}
			got := extractLevel(msg)
			if got != tc.want {
				t.Errorf("extractLevel(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestExtractLevel_BareKeywords(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"2025-01-01T12:00:00Z ERROR connection refused", "error"},
		{"2025-01-01 12:00:00 WARN timeout approaching", "warn"},
		{"2025-01-01 INFO startup complete", "info"},
	}

	for _, tc := range cases {
		t.Run(tc.content, func(t *testing.T) {
			msg := models.LogMessage{Content: tc.content}
			got := extractLevel(msg)
			if got != tc.want {
				t.Errorf("extractLevel(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestExtractLevel_NoLevel(t *testing.T) {
	msg := models.LogMessage{Content: "just a regular message with no level"}
	got := extractLevel(msg)
	if got != "" {
		t.Errorf("expected empty level, got %q", got)
	}
}
