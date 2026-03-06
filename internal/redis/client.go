package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

// Config holds Redis connection parameters.
type Config struct {
	Addr      string `yaml:"addr"`
	Password  string `yaml:"password"`
	DB        int    `yaml:"db"`
	KeyPrefix string `yaml:"key_prefix"`
}

// Client wraps a go-redis client and provides key helpers.
type Client struct {
	rdb    *goredis.Client
	prefix string
}

// NewClient creates a new Redis client from the given config.
func NewClient(cfg Config) *Client {
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "flume"
	}
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return &Client{rdb: rdb, prefix: prefix}
}

// NewClientFromRedis creates a Client wrapping an existing go-redis client.
// Useful for testing with miniredis.
func NewClientFromRedis(rdb *goredis.Client, prefix string) *Client {
	if prefix == "" {
		prefix = "flume"
	}
	return &Client{rdb: rdb, prefix: prefix}
}

// Ping verifies the connection to Redis.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close closes the underlying Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Underlying returns the raw go-redis client.
func (c *Client) Underlying() *goredis.Client {
	return c.rdb
}

// Key helpers — all keys are prefixed.

func (c *Client) patternsKey() string {
	return fmt.Sprintf("%s:patterns", c.prefix)
}

func (c *Client) msgsKey(pattern string) string {
	return fmt.Sprintf("%s:%s:msgs", c.prefix, pattern)
}

func (c *Client) channelKey(pattern string) string {
	return fmt.Sprintf("%s:%s:stream", c.prefix, pattern)
}

func (c *Client) labelKeysKey(pattern string) string {
	return fmt.Sprintf("%s:%s:label_keys", c.prefix, pattern)
}

func (c *Client) labelsBaseKey(pattern string) string {
	return fmt.Sprintf("%s:%s:labels", c.prefix, pattern)
}

func (c *Client) labelValuesKey(pattern, key string) string {
	return fmt.Sprintf("%s:%s:labels:%s", c.prefix, pattern, key)
}

func (c *Client) statsKey(pattern string) string {
	return fmt.Sprintf("%s:%s:stats", c.prefix, pattern)
}
