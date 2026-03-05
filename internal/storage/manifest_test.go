package storage

import (
	"context"
	"testing"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

func TestManifestKey_Unpartitioned(t *testing.T) {
	ts := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	got := ManifestKey("logs", "", ts)
	want := "logs/2026/03/05/14/manifest.json"
	if got != want {
		t.Errorf("ManifestKey = %q, want %q", got, want)
	}
}

func TestManifestKey_Partitioned(t *testing.T) {
	ts := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	got := ManifestKey("logs", "prod", ts)
	want := "logs/prod/2026/03/05/14/manifest.json"
	if got != want {
		t.Errorf("ManifestKey = %q, want %q", got, want)
	}
}

func TestManifest_RoundTrip(t *testing.T) {
	mock := newMockS3Client()
	ctx := context.Background()

	manifest := &HourManifest{
		Chunks: []ChunkMeta{
			{
				Key:   "test/chunk-1.json.gz",
				Count: 10,
				MinTS: time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC),
				MaxTS: time.Date(2026, 3, 5, 14, 9, 0, 0, time.UTC),
				Labels: map[string][]string{
					"level": {"error", "info"},
				},
			},
		},
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}

	key := "test/2026/03/05/14/manifest.json"
	err := WriteManifest(ctx, mock, "bucket", key, manifest)
	if err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	got, err := ReadManifest(ctx, mock, "bucket", key)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil manifest")
	}
	if len(got.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got.Chunks))
	}
	if got.Chunks[0].Count != 10 {
		t.Errorf("count = %d, want 10", got.Chunks[0].Count)
	}
}

func TestReadManifest_NotFound(t *testing.T) {
	mock := newMockS3Client()
	got, err := ReadManifest(context.Background(), mock, "bucket", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent manifest")
	}
}

func TestAppendChunk_CreatesThenAppends(t *testing.T) {
	mock := newMockS3Client()
	ctx := context.Background()
	key := "test/manifest.json"

	// First append creates the manifest.
	meta1 := ChunkMeta{Key: "chunk-1", Count: 5}
	if err := AppendChunk(ctx, mock, "bucket", key, meta1); err != nil {
		t.Fatalf("AppendChunk 1: %v", err)
	}

	m, _ := ReadManifest(ctx, mock, "bucket", key)
	if len(m.Chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(m.Chunks))
	}

	// Second append adds to existing.
	meta2 := ChunkMeta{Key: "chunk-2", Count: 3}
	if err := AppendChunk(ctx, mock, "bucket", key, meta2); err != nil {
		t.Fatalf("AppendChunk 2: %v", err)
	}

	m, _ = ReadManifest(ctx, mock, "bucket", key)
	if len(m.Chunks) != 2 {
		t.Fatalf("expected 2 chunks after append, got %d", len(m.Chunks))
	}
}

func TestBuildChunkMeta(t *testing.T) {
	now := time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	msgs := []models.LogMessage{
		{ID: "1", Timestamp: now, Level: "error", Labels: map[string]string{"ns": "prod"}},
		{ID: "2", Timestamp: now.Add(5 * time.Minute), Level: "info", Labels: map[string]string{"ns": "prod"}},
		{ID: "3", Timestamp: now.Add(10 * time.Minute), Level: "error", Labels: map[string]string{"ns": "staging"}},
	}

	meta := BuildChunkMeta("test-key", msgs)

	if meta.Key != "test-key" {
		t.Errorf("Key = %q, want %q", meta.Key, "test-key")
	}
	if meta.Count != 3 {
		t.Errorf("Count = %d, want 3", meta.Count)
	}
	if !meta.MinTS.Equal(now) {
		t.Errorf("MinTS = %v, want %v", meta.MinTS, now)
	}
	if !meta.MaxTS.Equal(now.Add(10 * time.Minute)) {
		t.Errorf("MaxTS = %v, want %v", meta.MaxTS, now.Add(10*time.Minute))
	}
	if len(meta.Labels["level"]) != 2 {
		t.Errorf("expected 2 level values, got %d", len(meta.Labels["level"]))
	}
	if len(meta.Labels["ns"]) != 2 {
		t.Errorf("expected 2 ns values, got %d", len(meta.Labels["ns"]))
	}
}

func TestManifestMatchesFilter(t *testing.T) {
	meta := ChunkMeta{
		Labels: map[string][]string{
			"level": {"error", "info"},
			"ns":    {"prod"},
		},
	}

	tests := []struct {
		name   string
		filter map[string]string
		want   bool
	}{
		{"empty filter", nil, true},
		{"matching level", map[string]string{"level": "error"}, true},
		{"non-matching level", map[string]string{"level": "debug"}, false},
		{"matching ns", map[string]string{"ns": "prod"}, true},
		{"missing label", map[string]string{"app": "web"}, false},
		{"multi match", map[string]string{"level": "error", "ns": "prod"}, true},
		{"multi partial fail", map[string]string{"level": "error", "ns": "staging"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := manifestMatchesFilter(meta, tc.filter)
			if got != tc.want {
				t.Errorf("manifestMatchesFilter = %v, want %v", got, tc.want)
			}
		})
	}
}
