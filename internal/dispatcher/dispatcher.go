package dispatcher

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	flumeredis "github.com/interpt-co/flume/internal/redis"
	"github.com/interpt-co/flume/internal/server"
	"github.com/interpt-co/flume/internal/storage"
)

// Config holds dispatcher configuration.
type Config struct {
	Host         string
	Port         int
	BulkWindowMS int
	Verbose      bool

	// Redis connection.
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// S3 storage settings (read-only for history).
	S3Bucket   string
	S3Prefix   string
	S3Region   string
	S3Endpoint string

	// Auth callback settings.
	AuthURL     string
	AuthTimeout time.Duration
}

// Dispatcher is the client-facing component that reads from Redis and serves
// browsers via HTTP/WebSocket.
type Dispatcher struct {
	cfg Config
}

// New creates a new Dispatcher.
func New(cfg Config) *Dispatcher {
	return &Dispatcher{cfg: cfg}
}

// Run starts all dispatcher components and blocks until ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) error {
	if d.cfg.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	// 1. Connect to Redis.
	redisClient := flumeredis.NewClient(flumeredis.Config{
		Addr:     d.cfg.RedisAddr,
		Password: d.cfg.RedisPassword,
		DB:       d.cfg.RedisDB,
	})
	if err := redisClient.Ping(ctx); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	defer redisClient.Close()

	reader := flumeredis.NewReader(redisClient)
	subscriber := flumeredis.NewSubscriber(redisClient)

	log.WithField("addr", d.cfg.RedisAddr).Info("dispatcher: connected to Redis")

	// 2. Create Redis-backed ClientManager.
	manager := server.NewClientManagerWithRedis(reader, subscriber, d.cfg.BulkWindowMS)

	// 3. Optionally configure auth callback.
	if d.cfg.AuthURL != "" {
		timeout := d.cfg.AuthTimeout
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		manager.SetAuthConfig(&server.AuthConfig{
			URL:     d.cfg.AuthURL,
			Timeout: timeout,
		})
		log.WithField("url", d.cfg.AuthURL).Info("auth callback enabled")
	}

	// 4. Optionally create S3 reader for history fallback.
	var serverOpts []server.ServerOption
	if d.cfg.S3Bucket != "" {
		s3cfg := storage.S3Config{
			Bucket:   d.cfg.S3Bucket,
			Prefix:   d.cfg.S3Prefix,
			Region:   d.cfg.S3Region,
			Endpoint: d.cfg.S3Endpoint,
		}
		s3store, err := storage.NewS3Storage(ctx, s3cfg)
		if err != nil {
			return fmt.Errorf("failed to create S3 reader: %w", err)
		}
		serverOpts = append(serverOpts,
			server.WithCrossNodeStorageOption(s3store, d.cfg.S3Prefix),
		)
		log.WithField("bucket", d.cfg.S3Bucket).Info("S3 history reader enabled")
	}

	// 5. Start HTTP server.
	srv := server.NewServer(d.cfg.Host, d.cfg.Port, manager, serverOpts...)

	addr := fmt.Sprintf("http://%s:%d", d.cfg.Host, d.cfg.Port)
	fmt.Printf("flume dispatcher listening on %s\n", addr)
	log.WithField("http", addr).Info("dispatcher starting")

	return srv.Start(ctx)
}
