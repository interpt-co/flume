package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseHourFromPrefix_Unpartitioned(t *testing.T) {
	got, err := ParseHourFromPrefix("logs/2026/03/05/14/", "logs/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHourFromPrefix_Partitioned(t *testing.T) {
	got, err := ParseHourFromPrefix("logs/prod/2026/03/05/14/", "logs/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseHourFromPrefix_TooFewComponents(t *testing.T) {
	_, err := ParseHourFromPrefix("logs/2026/03/", "logs/")
	if err == nil {
		t.Error("expected error for too few components")
	}
}

func TestRetention_DeletesExpiredPrefixes(t *testing.T) {
	mock := newMockS3Client()
	ctx := context.Background()

	now := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	retention := 2 * time.Hour

	// Create chunks at hours 10, 11, 12, 13, 14 (hour 14 is "now").
	// With 2h retention, cutoff = 12:00. Hours whose end <= cutoff are expired:
	// hour 10 (ends 11:00) — expired
	// hour 11 (ends 12:00) — expired (end == cutoff)
	// hour 12 (ends 13:00) — NOT expired
	// hour 13 (ends 14:00) — NOT expired
	// hour 14 (ends 15:00) — NOT expired
	for hour := 10; hour <= 14; hour++ {
		ts := time.Date(2026, 3, 5, hour, 30, 0, 0, time.UTC)
		key := ChunkKey("logs", ts)
		data, _ := MarshalGzip(nil)
		mock.objects[key] = data
		// Also add a manifest.
		mKey := ManifestKey("logs", "", ts)
		mock.objects[mKey] = []byte(`{"chunks":[],"updated_at":"2026-03-05T10:00:00Z"}`)
	}

	cutoff := now.Add(-retention)
	err := runRetentionUnpartitioned(ctx, mock, "bucket", "logs/", cutoff)
	if err != nil {
		t.Fatalf("runRetention: %v", err)
	}

	// Hours 10 and 11 should be deleted. Hours 12, 13, 14 should remain.
	mock.mu.Lock()
	remaining := len(mock.objects)
	var remainingKeys []string
	for k := range mock.objects {
		remainingKeys = append(remainingKeys, k)
	}
	mock.mu.Unlock()

	// 3 hours * 2 objects each (chunk + manifest) = 6
	if remaining != 6 {
		t.Errorf("expected 6 remaining objects (hours 12-14), got %d", remaining)
		for _, k := range remainingKeys {
			t.Logf("  remaining: %s", k)
		}
	}

	// Verify no hour 10 or 11 keys remain.
	for _, k := range remainingKeys {
		if strings.Contains(k, "/05/10/") || strings.Contains(k, "/05/11/") {
			t.Errorf("expired key should have been deleted: %s", k)
		}
	}
}

func TestRetention_Partitioned(t *testing.T) {
	mock := newMockS3Client()
	ctx := context.Background()

	now := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	retention := 2 * time.Hour

	// Create chunks in two partitions at hours 10 and 14.
	for _, partition := range []string{"prod", "staging"} {
		for _, hour := range []int{10, 14} {
			ts := time.Date(2026, 3, 5, hour, 30, 0, 0, time.UTC)
			key := PartitionedChunkKey("logs", partition, ts)
			data, _ := MarshalGzip(nil)
			mock.objects[key] = data
		}
	}

	cutoff := now.Add(-retention)
	err := runRetentionPartitioned(ctx, mock, "bucket", "logs/", cutoff)
	if err != nil {
		t.Fatalf("runRetention partitioned: %v", err)
	}

	mock.mu.Lock()
	remaining := len(mock.objects)
	var remainingKeys []string
	for k := range mock.objects {
		remainingKeys = append(remainingKeys, k)
	}
	mock.mu.Unlock()

	// Only hour 14 in both partitions should remain = 2 objects.
	if remaining != 2 {
		t.Errorf("expected 2 remaining objects, got %d", remaining)
		for _, k := range remainingKeys {
			t.Logf("  remaining: %s", k)
		}
	}
}

func TestRetention_NothingToDelete(t *testing.T) {
	mock := newMockS3Client()
	ctx := context.Background()

	// Create a chunk at the current hour.
	now := time.Date(2026, 3, 5, 14, 30, 0, 0, time.UTC)
	key := ChunkKey("logs", now)
	data, _ := MarshalGzip(nil)
	mock.objects[key] = data

	cutoff := now.Add(-time.Hour) // retention = 1h, cutoff = 13:30
	// Hour 14 ends at 15:00 which is after cutoff, so nothing deleted.
	err := runRetentionUnpartitioned(ctx, mock, "bucket", "logs/", cutoff)
	if err != nil {
		t.Fatalf("runRetention: %v", err)
	}

	mock.mu.Lock()
	remaining := len(mock.objects)
	mock.mu.Unlock()

	if remaining != 1 {
		t.Errorf("expected 1 remaining object, got %d", remaining)
	}
}
