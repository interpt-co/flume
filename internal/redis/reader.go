package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	goredis "github.com/redis/go-redis/v9"
	"github.com/interpt-co/flume/internal/models"
)

// Reader reads log messages and metadata from Redis.
type Reader struct {
	client *Client
}

// NewReader creates a new Reader.
func NewReader(client *Client) *Reader {
	return &Reader{client: client}
}

// GetMessages returns up to count messages starting from the given offset
// (0 = oldest in the sorted set). Messages are ordered oldest-first.
func (r *Reader) GetMessages(ctx context.Context, pattern string, start, count int) ([]models.LogMessage, error) {
	if count <= 0 {
		return nil, nil
	}
	key := r.client.msgsKey(pattern)
	vals, err := r.client.rdb.ZRange(ctx, key, int64(start), int64(start+count-1)).Result()
	if err != nil {
		return nil, err
	}
	return deserializeMessages(vals)
}

// GetMessagesBefore returns up to count messages with timestamps strictly before
// the given UnixNano timestamp, ordered newest-first.
func (r *Reader) GetMessagesBefore(ctx context.Context, pattern string, beforeNano int64, count int) ([]models.LogMessage, error) {
	key := r.client.msgsKey(pattern)
	// ZREVRANGEBYSCORE: from (beforeNano-1) down to -inf, LIMIT 0 count
	vals, err := r.client.rdb.ZRevRangeByScore(ctx, key, &goredis.ZRangeBy{
		Min:    "-inf",
		Max:    "(" + strconv.FormatInt(beforeNano, 10),
		Offset: 0,
		Count:  int64(count),
	}).Result()
	if err != nil {
		return nil, err
	}
	return deserializeMessages(vals)
}

// GetMessageCount returns the number of messages stored for a pattern.
func (r *Reader) GetMessageCount(ctx context.Context, pattern string) (int64, error) {
	return r.client.rdb.ZCard(ctx, r.client.msgsKey(pattern)).Result()
}

// PatternStats holds per-pattern statistics from Redis.
type PatternStats struct {
	MessageCount   int64
	BufferCapacity int64
}

// GetStats returns the stats hash for a pattern.
func (r *Reader) GetStats(ctx context.Context, pattern string) (PatternStats, error) {
	vals, err := r.client.rdb.HGetAll(ctx, r.client.statsKey(pattern)).Result()
	if err != nil {
		return PatternStats{}, err
	}
	var stats PatternStats
	if v, ok := vals["message_count"]; ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return PatternStats{}, fmt.Errorf("parsing message_count %q: %w", v, err)
		}
		stats.MessageCount = n
	}
	if v, ok := vals["buffer_capacity"]; ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return PatternStats{}, fmt.Errorf("parsing buffer_capacity %q: %w", v, err)
		}
		stats.BufferCapacity = n
	}
	return stats, nil
}

// GetLabels returns a map of label keys to their distinct values for a pattern.
func (r *Reader) GetLabels(ctx context.Context, pattern string) (map[string][]string, error) {
	keys, err := r.client.rdb.SMembers(ctx, r.client.labelKeysKey(pattern)).Result()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return make(map[string][]string), nil
	}

	// Pipeline all SMembers calls into a single round trip.
	pipe := r.client.rdb.Pipeline()
	cmds := make(map[string]*goredis.StringSliceCmd, len(keys))
	for _, k := range keys {
		cmds[k] = pipe.SMembers(ctx, r.client.labelValuesKey(pattern, k))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}

	result := make(map[string][]string, len(keys))
	for k, cmd := range cmds {
		vals, err := cmd.Result()
		if err != nil {
			return nil, err
		}
		result[k] = vals
	}
	return result, nil
}

// GetPatterns returns all known pattern names.
func (r *Reader) GetPatterns(ctx context.Context) ([]string, error) {
	return r.client.rdb.SMembers(ctx, r.client.patternsKey()).Result()
}

func deserializeMessages(vals []string) ([]models.LogMessage, error) {
	msgs := make([]models.LogMessage, 0, len(vals))
	for _, v := range vals {
		var msg models.LogMessage
		if err := json.Unmarshal([]byte(v), &msg); err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
