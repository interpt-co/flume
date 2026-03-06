package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"runtime"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Config holds Redis connection parameters.
type Config struct {
	Addr      string `yaml:"addr"`
	Password  string `yaml:"password"`
	DB        int    `yaml:"db"`
	KeyPrefix string `yaml:"key_prefix"`

	// TLS settings. Enable TLS by setting UseTLS to true.
	UseTLS         bool   `yaml:"use_tls"`
	TLSCertFile    string `yaml:"tls_cert_file"`
	TLSKeyFile     string `yaml:"tls_key_file"`
	TLSCACertFile  string `yaml:"tls_ca_cert_file"`
	TLSSkipVerify  bool   `yaml:"tls_skip_verify"`
}

// Client wraps a go-redis client and provides key helpers.
type Client struct {
	rdb    *goredis.Client
	prefix string
}

// NewClient creates a new Redis client from the given config.
func NewClient(cfg Config) (*Client, error) {
	prefix := cfg.KeyPrefix
	if prefix == "" {
		prefix = "flume"
	}

	poolSize := 10 * runtime.GOMAXPROCS(0)
	if poolSize < 20 {
		poolSize = 20
	}

	opts := &goredis.Options{
		Addr:            cfg.Addr,
		Password:        cfg.Password,
		DB:              cfg.DB,
		PoolSize:        poolSize,
		MinIdleConns:    4,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		ConnMaxIdleTime: 5 * time.Minute,
	}

	if cfg.UseTLS {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("redis TLS config: %w", err)
		}
		opts.TLSConfig = tlsCfg
	}

	rdb := goredis.NewClient(opts)
	return &Client{rdb: rdb, prefix: prefix}, nil
}

// buildTLSConfig constructs a *tls.Config from the Redis config.
func buildTLSConfig(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.TLSSkipVerify,
	}

	// Mutual TLS: client certificate.
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	// Custom CA certificate.
	if cfg.TLSCACertFile != "" {
		caCert, err := os.ReadFile(cfg.TLSCACertFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA cert: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert from %s", cfg.TLSCACertFile)
		}
		tlsCfg.RootCAs = pool
	}

	return tlsCfg, nil
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
