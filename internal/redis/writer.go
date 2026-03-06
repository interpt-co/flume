package redis

import (
	"context"
	"encoding/json"

	goredis "github.com/redis/go-redis/v9"
	"github.com/interpt-co/flume/internal/models"
)

// luaWriteBatch atomically: SADD patterns, ZADD msgs, ZREMRANGEBYRANK trim,
// SADD label_keys, SADD label values, HINCRBY stats, PUBLISH batch.
//
// KEYS: patternsKey, msgsKey, statsKey, channelKey, labelKeysKey, labelsBaseKey
// ARGV: pattern, bufCap, msgCount, member1, score1, ..., memberN, scoreN,
//       labelKeysCount, labelKey1, labelVal1, ..., publishPayload
var luaWriteBatch = goredis.NewScript(`
local patternsKey = KEYS[1]
local msgsKey     = KEYS[2]
local statsKey    = KEYS[3]
local channelKey  = KEYS[4]
local lkKey       = KEYS[5]
local labelsBase  = KEYS[6]

local pattern  = ARGV[1]
local bufCap   = tonumber(ARGV[2])
local msgCount = tonumber(ARGV[3])

-- Register pattern
redis.call('SADD', patternsKey, pattern)

-- ZADD messages
local idx = 4
for i = 1, msgCount do
    local member = ARGV[idx]
    local score  = ARGV[idx + 1]
    redis.call('ZADD', msgsKey, score, member)
    idx = idx + 2
end

-- Trim to bufCap (keep newest)
local total = redis.call('ZCARD', msgsKey)
if total > bufCap then
    redis.call('ZREMRANGEBYRANK', msgsKey, 0, total - bufCap - 1)
end

-- Label indexing
local labelCount = tonumber(ARGV[idx])
idx = idx + 1
for i = 1, labelCount do
    local lk = ARGV[idx]
    local lv = ARGV[idx + 1]
    redis.call('SADD', lkKey, lk)
    redis.call('SADD', labelsBase .. ':' .. lk, lv)
    idx = idx + 2
end

-- Stats
redis.call('HINCRBY', statsKey, 'message_count', msgCount)
redis.call('HSET', statsKey, 'buffer_capacity', bufCap)

-- Publish
local payload = ARGV[idx]
redis.call('PUBLISH', channelKey, payload)

return 1
`)

// Writer writes log message batches to Redis.
type Writer struct {
	client *Client
}

// NewWriter creates a new Writer.
func NewWriter(client *Client) *Writer {
	return &Writer{client: client}
}

// WriteBatch atomically writes a batch of messages to Redis for the given pattern.
func (w *Writer) WriteBatch(ctx context.Context, pattern string, msgs []models.LogMessage, bufCap int) error {
	if len(msgs) == 0 {
		return nil
	}

	// Collect unique labels across all messages.
	labelPairs := collectLabels(msgs)

	// Serialize messages for ZADD and PUBLISH.
	members := make([]interface{}, 0, len(msgs)*2)
	pubMsgs := make([]json.RawMessage, 0, len(msgs))
	for _, msg := range msgs {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		score := float64(msg.Timestamp.UnixNano())
		members = append(members, string(data), score)
		pubMsgs = append(pubMsgs, data)
	}

	pubPayload, err := json.Marshal(pubMsgs)
	if err != nil {
		return err
	}

	// Build ARGV: pattern, bufCap, msgCount, members..., labelCount, labels..., pubPayload
	// The Lua script uses KEYS[6] (labelsBase) to construct per-label-key set keys
	// as labelsBase .. ':' .. labelKey. This is dynamic key access inside the script
	// but works on standalone Redis and Cluster with hash-tag routing.
	argv := make([]interface{}, 0, 4+len(members)+1+len(labelPairs)*2+1)
	argv = append(argv, pattern, bufCap, len(msgs))
	argv = append(argv, members...)
	argv = append(argv, len(labelPairs))
	for _, lp := range labelPairs {
		argv = append(argv, lp[0], lp[1])
	}
	argv = append(argv, string(pubPayload))

	keys := []string{
		w.client.patternsKey(),
		w.client.msgsKey(pattern),
		w.client.statsKey(pattern),
		w.client.channelKey(pattern),
		w.client.labelKeysKey(pattern),
		w.client.labelsBaseKey(pattern),
	}

	return luaWriteBatch.Run(ctx, w.client.rdb, keys, argv...).Err()
}

// collectLabels returns deduplicated [key, value] pairs across all messages.
func collectLabels(msgs []models.LogMessage) [][2]string {
	seen := make(map[[2]string]struct{})
	var pairs [][2]string
	for _, msg := range msgs {
		for k, v := range msg.Labels {
			p := [2]string{k, v}
			if _, ok := seen[p]; !ok {
				seen[p] = struct{}{}
				pairs = append(pairs, p)
			}
		}
	}
	return pairs
}
