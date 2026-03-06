package cri

import (
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantTS  time.Time
		wantStr string
		wantP   bool
		wantC   string
		wantErr bool
	}{
		{
			name:    "standard stdout full line",
			input:   "2026-03-06T12:34:56.789Z stdout F hello world",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 789000000, time.UTC),
			wantStr: "stdout",
			wantP:   false,
			wantC:   "hello world",
		},
		{
			name:    "stderr full line",
			input:   "2026-03-06T12:34:56.789Z stderr F error message here",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 789000000, time.UTC),
			wantStr: "stderr",
			wantP:   false,
			wantC:   "error message here",
		},
		{
			name:    "partial line",
			input:   "2026-03-06T12:34:56.789Z stdout P partial content",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 789000000, time.UTC),
			wantStr: "stdout",
			wantP:   true,
			wantC:   "partial content",
		},
		{
			name:    "nanosecond timestamp",
			input:   "2026-03-06T12:34:56.123456789Z stdout F nano",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 123456789, time.UTC),
			wantStr: "stdout",
			wantP:   false,
			wantC:   "nano",
		},
		{
			name:    "content with spaces",
			input:   "2026-03-06T12:34:56.789Z stdout F content with   multiple   spaces",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 789000000, time.UTC),
			wantStr: "stdout",
			wantP:   false,
			wantC:   "content with   multiple   spaces",
		},
		{
			name:    "empty content",
			input:   "2026-03-06T12:34:56.789Z stdout F ",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 789000000, time.UTC),
			wantStr: "stdout",
			wantP:   false,
			wantC:   "",
		},
		{
			name:    "flag only no content",
			input:   "2026-03-06T12:34:56.789Z stdout F",
			wantTS:  time.Date(2026, 3, 6, 12, 34, 56, 789000000, time.UTC),
			wantStr: "stdout",
			wantP:   false,
			wantC:   "",
		},
		{
			name:    "no separator",
			input:   "garbage",
			wantErr: true,
		},
		{
			name:    "bad timestamp",
			input:   "not-a-time stdout F hello",
			wantErr: true,
		},
		{
			name:    "no stream separator",
			input:   "2026-03-06T12:34:56.789Z stdout",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, stream, partial, content, err := ParseLine(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ts.Equal(tt.wantTS) {
				t.Errorf("timestamp: got %v, want %v", ts, tt.wantTS)
			}
			if stream != tt.wantStr {
				t.Errorf("stream: got %q, want %q", stream, tt.wantStr)
			}
			if partial != tt.wantP {
				t.Errorf("partial: got %v, want %v", partial, tt.wantP)
			}
			if content != tt.wantC {
				t.Errorf("content: got %q, want %q", content, tt.wantC)
			}
		})
	}
}

func TestAssembler(t *testing.T) {
	a := NewAssembler()

	// Full line — should return immediately.
	ts := time.Date(2026, 3, 6, 12, 0, 0, 0, time.UTC)
	line := a.Process("c1", ts, "stdout", false, "hello")
	if line == nil {
		t.Fatal("expected line for full message")
	}
	if line.Content != "hello" {
		t.Errorf("got %q, want %q", line.Content, "hello")
	}

	// Partial then full — should assemble.
	line = a.Process("c2", ts, "stdout", true, "part1-")
	if line != nil {
		t.Fatal("expected nil for partial")
	}
	line = a.Process("c2", ts, "stdout", true, "part2-")
	if line != nil {
		t.Fatal("expected nil for second partial")
	}
	line = a.Process("c2", ts, "stdout", false, "end")
	if line == nil {
		t.Fatal("expected assembled line")
	}
	if line.Content != "part1-part2-end" {
		t.Errorf("assembled: got %q, want %q", line.Content, "part1-part2-end")
	}

	// After assembly, buffer should be reset — next full line is standalone.
	line = a.Process("c2", ts, "stdout", false, "standalone")
	if line == nil || line.Content != "standalone" {
		t.Errorf("expected standalone line, got %v", line)
	}

	// Remove cleans up.
	a.Process("c3", ts, "stdout", true, "buffered")
	a.Remove("c3")
	// Next full line should not include old buffer.
	line = a.Process("c3", ts, "stdout", false, "fresh")
	if line == nil || line.Content != "fresh" {
		t.Errorf("expected fresh after remove, got %v", line)
	}
}
