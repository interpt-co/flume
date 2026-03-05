package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
)

// ChunkMeta holds metadata about a single chunk stored in S3.
type ChunkMeta struct {
	Key    string              `json:"key"`
	Count  int                 `json:"count"`
	MinTS  time.Time           `json:"min_ts"`
	MaxTS  time.Time           `json:"max_ts"`
	Labels map[string][]string `json:"labels,omitempty"`
}

// HourManifest is a per-hour index of all chunks in a partition/hour.
type HourManifest struct {
	Chunks    []ChunkMeta `json:"chunks"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// ManifestKey returns the S3 key for the manifest of a given hour.
// If partition is empty, uses unpartitioned layout.
func ManifestKey(prefix, partition string, t time.Time) string {
	u := t.UTC()
	if partition == "" {
		return fmt.Sprintf("%s/%04d/%02d/%02d/%02d/manifest.json",
			strings.TrimRight(prefix, "/"),
			u.Year(), u.Month(), u.Day(), u.Hour(),
		)
	}
	return fmt.Sprintf("%s/%s/%04d/%02d/%02d/%02d/manifest.json",
		strings.TrimRight(prefix, "/"),
		partition,
		u.Year(), u.Month(), u.Day(), u.Hour(),
	)
}

// ReadManifest fetches and parses a manifest from S3. Returns nil, nil if
// the manifest does not exist.
func ReadManifest(ctx context.Context, client S3Client, bucket, key string) (*HourManifest, error) {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, nil
		}
		// Also handle non-typed not-found (e.g. from mocks or other S3-compatible stores).
		if strings.Contains(err.Error(), "NoSuchKey") {
			return nil, nil
		}
		return nil, fmt.Errorf("GetObject manifest %s: %w", key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("reading manifest %s: %w", key, err)
	}

	var m HourManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", key, err)
	}
	return &m, nil
}

// WriteManifest serializes and writes a manifest to S3.
func WriteManifest(ctx context.Context, client S3Client, bucket, key string, m *HourManifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("PutObject manifest %s: %w", key, err)
	}
	return nil
}

// AppendChunk performs a read-modify-write of the manifest to add a new chunk.
// If the manifest doesn't exist yet, it creates one.
func AppendChunk(ctx context.Context, client S3Client, bucket, manifestKey string, meta ChunkMeta) error {
	manifest, err := ReadManifest(ctx, client, bucket, manifestKey)
	if err != nil {
		return err
	}
	if manifest == nil {
		manifest = &HourManifest{}
	}
	manifest.Chunks = append(manifest.Chunks, meta)
	manifest.UpdatedAt = time.Now().UTC()
	return WriteManifest(ctx, client, bucket, manifestKey, manifest)
}

// BuildChunkMeta creates a ChunkMeta from a set of messages and their S3 key.
func BuildChunkMeta(key string, msgs []models.LogMessage) ChunkMeta {
	meta := ChunkMeta{
		Key:   key,
		Count: len(msgs),
	}
	if len(msgs) == 0 {
		return meta
	}

	meta.MinTS = msgs[0].Timestamp
	meta.MaxTS = msgs[0].Timestamp
	labels := make(map[string]map[string]bool)

	for _, m := range msgs {
		if m.Timestamp.Before(meta.MinTS) {
			meta.MinTS = m.Timestamp
		}
		if m.Timestamp.After(meta.MaxTS) {
			meta.MaxTS = m.Timestamp
		}
		if m.Level != "" {
			if labels["level"] == nil {
				labels["level"] = make(map[string]bool)
			}
			labels["level"][m.Level] = true
		}
		for k, v := range m.Labels {
			if labels[k] == nil {
				labels[k] = make(map[string]bool)
			}
			labels[k][v] = true
		}
	}

	if len(labels) > 0 {
		meta.Labels = make(map[string][]string, len(labels))
		for k, vals := range labels {
			list := make([]string, 0, len(vals))
			for v := range vals {
				list = append(list, v)
			}
			meta.Labels[k] = list
		}
	}

	return meta
}

// manifestMatchesFilter checks if a chunk's label metadata intersects the filter.
func manifestMatchesFilter(meta ChunkMeta, filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	for k, v := range filter {
		chunkVals, ok := meta.Labels[k]
		if !ok {
			return false
		}
		found := false
		for _, cv := range chunkVals {
			if strings.EqualFold(cv, v) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// manifestTimeOverlaps checks if a chunk's time range overlaps with "before" cutoff.
func manifestTimeOverlaps(meta ChunkMeta, before time.Time) bool {
	return meta.MinTS.Before(before)
}

// appendManifestAfterFlush is called after each chunk is written to update the
// hour's manifest. Errors are logged but don't fail the flush.
func (s *S3Storage) appendManifestAfterFlush(ctx context.Context, key string, msgs []models.LogMessage, partition string) {
	meta := BuildChunkMeta(key, msgs)

	// Determine the hour from the first message's timestamp.
	var t time.Time
	if len(msgs) > 0 {
		t = msgs[0].Timestamp
	} else {
		t = time.Now()
	}

	mKey := ManifestKey(s.cfg.Prefix, partition, t)
	if err := AppendChunk(ctx, s.client, s.cfg.Bucket, mKey, meta); err != nil {
		log.WithError(err).Warn("storage: failed to update manifest")
	}
}
