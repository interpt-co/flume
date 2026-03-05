package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestLogMessageJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)
	original := LogMessage{
		ID:          "abc-123",
		Content:     "hello world",
		JsonContent: json.RawMessage(`{"key":"value"}`),
		IsJson:      true,
		Timestamp:   ts,
		Source:      SourceStdin,
		Origin: Origin{
			Name: "stdin",
			Meta: map[string]string{"pid": "42"},
		},
		Labels: map[string]string{"env": "dev"},
		Level:  "info",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded LogMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Content != original.Content {
		t.Errorf("Content mismatch: got %q, want %q", decoded.Content, original.Content)
	}
	if decoded.IsJson != original.IsJson {
		t.Errorf("IsJson mismatch: got %v, want %v", decoded.IsJson, original.IsJson)
	}
	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Errorf("Timestamp mismatch: got %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source mismatch: got %q, want %q", decoded.Source, original.Source)
	}
	if decoded.Origin.Name != original.Origin.Name {
		t.Errorf("Origin.Name mismatch: got %q, want %q", decoded.Origin.Name, original.Origin.Name)
	}
	if decoded.Origin.Meta["pid"] != "42" {
		t.Errorf("Origin.Meta[pid] mismatch: got %q, want %q", decoded.Origin.Meta["pid"], "42")
	}
	if decoded.Labels["env"] != "dev" {
		t.Errorf("Labels[env] mismatch: got %q, want %q", decoded.Labels["env"], "dev")
	}
	if decoded.Level != original.Level {
		t.Errorf("Level mismatch: got %q, want %q", decoded.Level, original.Level)
	}
	if string(decoded.JsonContent) != string(original.JsonContent) {
		t.Errorf("JsonContent mismatch: got %s, want %s", decoded.JsonContent, original.JsonContent)
	}
}

func TestLogMessageJSONOmitsEmptyFields(t *testing.T) {
	msg := LogMessage{
		Content:   "minimal",
		Source:    SourceFile,
		Timestamp: time.Now(),
		Origin:    Origin{Name: "test"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	// These fields have omitempty and should not be present.
	for _, key := range []string{"json_content", "labels", "level"} {
		if _, ok := raw[key]; ok {
			t.Errorf("expected field %q to be omitted, but it was present", key)
		}
	}

	// Origin.Meta should also be omitted.
	var origin map[string]json.RawMessage
	if err := json.Unmarshal(raw["origin"], &origin); err != nil {
		t.Fatalf("Unmarshal origin failed: %v", err)
	}
	if _, ok := origin["meta"]; ok {
		t.Error("expected Origin.Meta to be omitted, but it was present")
	}
}

func TestSourceTypeConstants(t *testing.T) {
	cases := []struct {
		st   SourceType
		want string
	}{
		{SourceLoki, "loki"},
		{SourceStdin, "stdin"},
		{SourceFile, "file"},
		{SourceSocket, "socket"},
		{SourceDemo, "demo"},
	}
	for _, tc := range cases {
		if string(tc.st) != tc.want {
			t.Errorf("SourceType %v: got %q, want %q", tc.st, string(tc.st), tc.want)
		}
	}
}
