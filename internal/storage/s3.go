package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/query"
)

// S3Client abstracts the S3 operations we need, making the storage testable.
type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

// S3Config holds configuration for the S3 storage backend.
type S3Config struct {
	Bucket         string        // S3 bucket name
	Prefix         string        // Key prefix (typically namespace name)
	Region         string        // AWS region
	Endpoint       string        // Custom endpoint (MinIO, localstack)
	FlushInterval  time.Duration // Default 10s
	FlushCount     int           // Default 1000
	PartitionLabel string        // Partition S3 keys by this label (empty = no partitioning)
	Retention      time.Duration // TTL for S3 data (0 = disabled)
}

// S3Storage implements Storage backed by S3-compatible object storage.
type S3Storage struct {
	client S3Client
	cfg    S3Config
	ch     chan models.LogMessage
	mu     sync.Mutex
	buf    []models.LogMessage
}

// NewS3Storage creates a new S3Storage from the given config. It initialises
// the AWS SDK client. If cfg.Endpoint is non-empty it is used as a custom
// endpoint (for MinIO / localstack).
func NewS3Storage(ctx context.Context, cfg S3Config) (*S3Storage, error) {
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 10 * time.Second
	}
	if cfg.FlushCount == 0 {
		cfg.FlushCount = 1000
	}

	var opts []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	var s3Opts []func(*s3.Options)
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return NewS3StorageWithClient(client, cfg), nil
}

// NewS3StorageWithClient creates an S3Storage using an already-constructed S3
// client. This is useful for testing with a mock client.
func NewS3StorageWithClient(client S3Client, cfg S3Config) *S3Storage {
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 10 * time.Second
	}
	if cfg.FlushCount == 0 {
		cfg.FlushCount = 1000
	}
	return &S3Storage{
		client: client,
		cfg:    cfg,
		ch:     make(chan models.LogMessage, cfg.FlushCount),
	}
}

// Writer returns the write channel for ingesting messages.
func (s *S3Storage) Writer() chan<- models.LogMessage {
	return s.ch
}

// Start runs the background flush loop. It blocks until ctx is cancelled,
// then flushes any remaining buffered messages.
func (s *S3Storage) Start(ctx context.Context) error {
	if s.cfg.Retention > 0 {
		go RunRetentionLoop(ctx, s.client, s.cfg)
	}

	ticker := time.NewTicker(s.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-s.ch:
			if !ok {
				// Channel closed — flush remaining.
				return s.flush(context.Background())
			}
			s.mu.Lock()
			s.buf = append(s.buf, msg)
			full := len(s.buf) >= s.cfg.FlushCount
			s.mu.Unlock()

			if full {
				if err := s.flush(ctx); err != nil {
					// Log but keep going; best-effort persistence.
					log.WithError(err).Warn("storage: flush error")
				}
			}

		case <-ticker.C:
			if err := s.flush(ctx); err != nil {
				log.WithError(err).Warn("storage: flush error")
			}

		case <-ctx.Done():
			// Drain remaining messages from channel with a deadline.
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer drainCancel()
			for {
				select {
				case msg := <-s.ch:
					s.mu.Lock()
					s.buf = append(s.buf, msg)
					s.mu.Unlock()
				default:
					return s.flush(drainCtx)
				}
			}
		}
	}
}

// flush writes the current buffer to S3 and resets it. If PartitionLabel is
// set, messages are grouped by label value and each group is written to its
// own partitioned key.
func (s *S3Storage) flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.buf) == 0 {
		s.mu.Unlock()
		return nil
	}
	msgs := s.buf
	s.buf = nil
	s.mu.Unlock()

	now := time.Now()

	if s.cfg.PartitionLabel == "" {
		key := ChunkKey(s.cfg.Prefix, now)
		if err := s.writeChunk(ctx, msgs, key); err != nil {
			return err
		}
		s.appendManifestAfterFlush(ctx, key, msgs, "", now)
		return nil
	}

	// Group messages by partition label value.
	groups := make(map[string][]models.LogMessage)
	for _, m := range msgs {
		partition := "_default"
		if m.Labels != nil {
			if v, ok := m.Labels[s.cfg.PartitionLabel]; ok && v != "" {
				partition = v
			}
		}
		groups[partition] = append(groups[partition], m)
	}

	var errs []error
	for partition, group := range groups {
		key := PartitionedChunkKey(s.cfg.Prefix, partition, now)
		if err := s.writeChunk(ctx, group, key); err != nil {
			errs = append(errs, fmt.Errorf("partition %s: %w", partition, err))
			continue
		}
		s.appendManifestAfterFlush(ctx, key, group, partition, now)
	}
	return errors.Join(errs...)
}

// writeChunk marshals messages to gzipped JSON and writes them to S3 at the given key.
func (s *S3Storage) writeChunk(ctx context.Context, msgs []models.LogMessage, key string) error {
	data, err := MarshalGzip(msgs)
	if err != nil {
		return fmt.Errorf("marshal+gzip: %w", err)
	}

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(s.cfg.Bucket),
		Key:             aws.String(key),
		Body:            bytes.NewReader(data),
		ContentType: aws.String("application/gzip"),
	})
	if err != nil {
		return fmt.Errorf("PutObject %s: %w", key, err)
	}
	return nil
}

// ReadBefore returns up to `count` messages with timestamps before `before`,
// ordered newest-first. If filter is non-nil, only messages matching all
// filter labels are returned. When PartitionLabel is configured and the filter
// contains that label, reads are scoped to that partition only.
func (s *S3Storage) ReadBefore(ctx context.Context, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error) {
	var result []models.LogMessage

	matcher := query.LabelMatcher(filter)

	// Determine which partitions to scan.
	partitions := s.resolvePartitions(ctx, filter)

	// Walk backwards hour by hour starting from the hour containing `before`.
	cur := before.UTC()
	limit := cur.Add(-7 * 24 * time.Hour)

	for cur.After(limit) && len(result) < count {
		for _, partition := range partitions {
			if len(result) >= count {
				break
			}

			chunkKeys, err := s.resolveChunkKeys(ctx, partition, cur, before, filter)
			if err != nil {
				return nil, err
			}

			for _, key := range chunkKeys {
				if len(result) >= count {
					break
				}
				msgs, err := s.readChunk(ctx, key)
				if err != nil {
					return nil, fmt.Errorf("reading chunk %s: %w", key, err)
				}
				sort.Slice(msgs, func(i, j int) bool {
					return msgs[i].Timestamp.After(msgs[j].Timestamp)
				})
				for _, m := range msgs {
					if m.Timestamp.Before(before) && matcher.Matches(m) {
						result = append(result, m)
						if len(result) >= count {
							break
						}
					}
				}
			}
		}

		cur = cur.Truncate(time.Hour).Add(-time.Hour)
	}

	return result, nil
}

// resolvePartitions determines which partition prefixes to scan.
// - No partitioning configured: returns [""] (scan unpartitioned prefix)
// - Partitioning + filter contains partition label: returns [filterValue] (scoped)
// - Partitioning + no filter match: discovers partitions via listing
func (s *S3Storage) resolvePartitions(ctx context.Context, filter map[string]string) []string {
	if s.cfg.PartitionLabel == "" {
		return []string{""}
	}

	// If filter specifies the partition label, scope to that partition only.
	if filter != nil {
		if v, ok := filter[s.cfg.PartitionLabel]; ok {
			return []string{v}
		}
	}

	// Discover partitions by listing at the prefix level.
	partitions, err := s.listPartitions(ctx)
	if err != nil {
		return []string{""}
	}
	if len(partitions) == 0 {
		return []string{""}
	}
	return partitions
}

// listPartitions discovers partition directories under the configured prefix
// using a delimiter-based listing.
func (s *S3Storage) listPartitions(ctx context.Context) ([]string, error) {
	prefix := strings.TrimRight(s.cfg.Prefix, "/") + "/"
	delimiter := "/"

	var partitions []string
	var continuationToken *string

	for {
		out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.cfg.Bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String(delimiter),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		for _, cp := range out.CommonPrefixes {
			p := aws.ToString(cp.Prefix)
			p = strings.TrimPrefix(p, prefix)
			p = strings.TrimSuffix(p, "/")
			if p != "" {
				partitions = append(partitions, p)
			}
		}

		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil {
			break
		}
		continuationToken = out.NextContinuationToken
	}
	return partitions, nil
}

// resolveChunkKeys returns chunk keys for a given partition+hour, using the
// manifest to prune irrelevant chunks when available. Falls back to listing.
func (s *S3Storage) resolveChunkKeys(ctx context.Context, partition string, hour, before time.Time, filter map[string]string) ([]string, error) {
	mKey := ManifestKey(s.cfg.Prefix, partition, hour)
	manifest, err := ReadManifest(ctx, s.client, s.cfg.Bucket, mKey)
	if err != nil {
		// Log and fall through to listing.
		log.WithError(err).Debug("storage: failed to read manifest, falling back to listing")
	}

	if manifest != nil && len(manifest.Chunks) > 0 {
		var keys []string
		for _, chunk := range manifest.Chunks {
			if !manifestTimeOverlaps(chunk, before) {
				continue
			}
			if !manifestMatchesFilter(chunk, filter) {
				continue
			}
			keys = append(keys, chunk.Key)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(keys)))
		return keys, nil
	}

	// Fall back to listing.
	var prefix string
	if partition == "" {
		prefix = HourPrefix(s.cfg.Prefix, hour)
	} else {
		prefix = PartitionedHourPrefix(s.cfg.Prefix, partition, hour)
	}
	keys, err := s.listKeys(ctx, prefix)
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	return keys, nil
}

// listKeys lists all object keys under the given prefix.
func (s *S3Storage) listKeys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var continuationToken *string

	for {
		out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.cfg.Bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("ListObjectsV2 prefix=%s: %w", prefix, err)
		}
		for _, obj := range out.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil {
			break
		}
		continuationToken = out.NextContinuationToken
	}

	return keys, nil
}

// readChunk downloads an S3 object, decompresses it, and parses the JSON
// array of LogMessage.
func (s *S3Storage) readChunk(ctx context.Context, key string) ([]models.LogMessage, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	return UnmarshalGzip(out.Body)
}

// ChunkKey builds the S3 key for a chunk written at time t.
// Layout: {prefix}/{YYYY}/{MM}/{DD}/{HH}/chunk-{unix_ms}.json.gz
func ChunkKey(prefix string, t time.Time) string {
	u := t.UTC()
	return fmt.Sprintf("%s/%04d/%02d/%02d/%02d/chunk-%d.json.gz",
		strings.TrimRight(prefix, "/"),
		u.Year(), u.Month(), u.Day(), u.Hour(),
		u.UnixMilli(),
	)
}

// HourPrefix returns the S3 prefix for the hour containing t.
// Layout: {prefix}/{YYYY}/{MM}/{DD}/{HH}/
func HourPrefix(prefix string, t time.Time) string {
	u := t.UTC()
	return fmt.Sprintf("%s/%04d/%02d/%02d/%02d/",
		strings.TrimRight(prefix, "/"),
		u.Year(), u.Month(), u.Day(), u.Hour(),
	)
}

// PartitionedChunkKey builds the S3 key for a partitioned chunk.
// Layout: {prefix}/{partition}/{YYYY}/{MM}/{DD}/{HH}/chunk-{unix_ms}.json.gz
func PartitionedChunkKey(prefix, partition string, t time.Time) string {
	u := t.UTC()
	return fmt.Sprintf("%s/%s/%04d/%02d/%02d/%02d/chunk-%d.json.gz",
		strings.TrimRight(prefix, "/"),
		partition,
		u.Year(), u.Month(), u.Day(), u.Hour(),
		u.UnixMilli(),
	)
}

// PartitionedHourPrefix returns the S3 prefix for a partition's hour.
// Layout: {prefix}/{partition}/{YYYY}/{MM}/{DD}/{HH}/
func PartitionedHourPrefix(prefix, partition string, t time.Time) string {
	u := t.UTC()
	return fmt.Sprintf("%s/%s/%04d/%02d/%02d/%02d/",
		strings.TrimRight(prefix, "/"),
		partition,
		u.Year(), u.Month(), u.Day(), u.Hour(),
	)
}

// NodePatternPrefix returns the S3 prefix for a specific node and pattern.
// Layout: {prefix}/{node}/{pattern}
func NodePatternPrefix(prefix, node, pattern string) string {
	return fmt.Sprintf("%s/%s/%s",
		strings.TrimRight(prefix, "/"),
		node, pattern,
	)
}

// NodePatternHourPrefix returns the S3 prefix for a node+pattern's hour.
// Layout: {prefix}/{node}/{pattern}/{YYYY}/{MM}/{DD}/{HH}/
func NodePatternHourPrefix(prefix, node, pattern string, t time.Time) string {
	return HourPrefix(NodePatternPrefix(prefix, node, pattern), t)
}

// CrossNodeReadBefore reads historical logs across multiple nodes for a given
// pattern. For each hour (walking backward), it fans out reads to all nodes
// in parallel, merges results, and returns up to count messages newest-first.
func (s *S3Storage) CrossNodeReadBefore(ctx context.Context, basePrefix, patternName string, nodes []string, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error) {
	matcher := query.LabelMatcher(filter)
	var result []models.LogMessage

	cur := before.UTC()
	limit := cur.Add(-7 * 24 * time.Hour)

	for cur.After(limit) && len(result) < count {
		type nodeResult struct {
			msgs []models.LogMessage
			err  error
		}
		ch := make(chan nodeResult, len(nodes))

		for _, node := range nodes {
			node := node
			go func() {
				prefix := NodePatternHourPrefix(basePrefix, node, patternName, cur)
				keys, err := s.listKeys(ctx, prefix)
				if err != nil {
					ch <- nodeResult{err: err}
					return
				}
				sort.Sort(sort.Reverse(sort.StringSlice(keys)))

				var msgs []models.LogMessage
				for _, key := range keys {
					chunk, err := s.readChunk(ctx, key)
					if err != nil {
						log.WithError(err).WithField("key", key).Warn("storage: cross-node read chunk error")
						continue
					}
					for _, m := range chunk {
						if m.Timestamp.Before(before) && matcher.Matches(m) {
							msgs = append(msgs, m)
						}
					}
				}
				ch <- nodeResult{msgs: msgs}
			}()
		}

		// Collect all node results.
		var hourMsgs []models.LogMessage
		for range nodes {
			nr := <-ch
			if nr.err != nil {
				log.WithError(nr.err).Warn("storage: cross-node hour scan error")
				continue
			}
			hourMsgs = append(hourMsgs, nr.msgs...)
		}

		// Sort newest-first and append.
		sort.Slice(hourMsgs, func(i, j int) bool {
			return hourMsgs[i].Timestamp.After(hourMsgs[j].Timestamp)
		})
		for _, m := range hourMsgs {
			if len(result) >= count {
				break
			}
			result = append(result, m)
		}

		cur = cur.Truncate(time.Hour).Add(-time.Hour)
	}

	return result, nil
}

// DiscoverNodes lists node directories under {prefix}/ in S3.
func (s *S3Storage) DiscoverNodes(ctx context.Context) ([]string, error) {
	prefix := strings.TrimRight(s.cfg.Prefix, "/") + "/"
	delimiter := "/"

	var nodes []string
	var continuationToken *string

	for {
		out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(s.cfg.Bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String(delimiter),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		for _, cp := range out.CommonPrefixes {
			p := aws.ToString(cp.Prefix)
			p = strings.TrimPrefix(p, prefix)
			p = strings.TrimSuffix(p, "/")
			if p != "" {
				nodes = append(nodes, p)
			}
		}

		if !aws.ToBool(out.IsTruncated) || out.NextContinuationToken == nil {
			break
		}
		continuationToken = out.NextContinuationToken
	}
	return nodes, nil
}

// MarshalGzip serializes messages to a gzipped JSON array.
func MarshalGzip(msgs []models.LogMessage) ([]byte, error) {
	jsonData, err := json.Marshal(msgs)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(jsonData); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// UnmarshalGzip decompresses gzipped data from r and parses a JSON array of
// LogMessage. Decompressed data is limited to 50 MB to prevent memory exhaustion.
func UnmarshalGzip(r io.Reader) ([]models.LogMessage, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	const maxDecompressed = 50 << 20 // 50 MB
	data, err := io.ReadAll(io.LimitReader(gz, maxDecompressed+1))
	if err != nil {
		return nil, fmt.Errorf("reading gzip: %w", err)
	}
	if len(data) > maxDecompressed {
		return nil, fmt.Errorf("decompressed chunk exceeds %d bytes limit", maxDecompressed)
	}

	var msgs []models.LogMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	return msgs, nil
}

