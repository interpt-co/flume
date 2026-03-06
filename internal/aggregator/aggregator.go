package aggregator

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	flumegrpc "github.com/interpt-co/flume/internal/grpc"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/pattern"
	"github.com/interpt-co/flume/internal/server"
	"github.com/interpt-co/flume/internal/storage"
)

// Config holds aggregator configuration.
type Config struct {
	Host         string
	Port         int
	GRPCPort     int
	BufferSize   int
	BulkWindowMS int
	Verbose      bool

	// S3 storage settings (read-only for history).
	S3Bucket   string
	S3Prefix   string
	S3Region   string
	S3Endpoint string

	// Auth callback settings.
	AuthURL     string
	AuthTimeout time.Duration
}

// Aggregator is the central component that receives logs from collectors
// and serves them to browser clients.
type Aggregator struct {
	cfg Config
}

// New creates a new Aggregator.
func New(cfg Config) *Aggregator {
	return &Aggregator{cfg: cfg}
}

// ingesterAdapter adapts pattern.Registry to the flumegrpc.Ingester interface.
type ingesterAdapter struct {
	registry *pattern.Registry
}

func (a *ingesterAdapter) Ingest(patternName string, entries []flumegrpc.LogEntry) {
	p := a.registry.GetOrCreate(patternName)
	msgs := make([]models.LogMessage, len(entries))
	for i, e := range entries {
		msgs[i] = e.ToLogMessage()
	}
	p.Ingest(msgs)
}

// Run starts all aggregator components and blocks until ctx is cancelled.
func (a *Aggregator) Run(ctx context.Context) error {
	if a.cfg.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	// 1. Create pattern registry.
	registry := pattern.NewRegistry(a.cfg.BufferSize)

	// 2. Create gRPC tracker and server.
	tracker := flumegrpc.NewTracker()
	ingester := &ingesterAdapter{registry: registry}
	grpcSrv := flumegrpc.NewServer(ingester, tracker)

	go func() {
		addr := fmt.Sprintf(":%d", a.cfg.GRPCPort)
		if err := grpcSrv.Serve(addr); err != nil {
			log.WithError(err).Error("gRPC server error")
		}
	}()
	go func() {
		<-ctx.Done()
		grpcSrv.GracefulStop()
	}()

	// 3. Create pattern-aware ClientManager.
	manager := server.NewClientManagerWithRegistry(registry, a.cfg.BulkWindowMS)

	// 4. Optionally configure auth callback.
	if a.cfg.AuthURL != "" {
		timeout := a.cfg.AuthTimeout
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		manager.SetAuthConfig(&server.AuthConfig{
			URL:     a.cfg.AuthURL,
			Timeout: timeout,
		})
		log.WithField("url", a.cfg.AuthURL).Info("auth callback enabled")
	}

	// 5. Optionally create S3 reader for cross-node history.
	var serverOpts []server.ServerOption
	if a.cfg.S3Bucket != "" {
		s3cfg := storage.S3Config{
			Bucket:   a.cfg.S3Bucket,
			Prefix:   a.cfg.S3Prefix,
			Region:   a.cfg.S3Region,
			Endpoint: a.cfg.S3Endpoint,
		}
		s3store, err := storage.NewS3Storage(ctx, s3cfg)
		if err != nil {
			return fmt.Errorf("failed to create S3 reader: %w", err)
		}
		serverOpts = append(serverOpts,
			server.WithCrossNodeStorageOption(s3store, a.cfg.S3Prefix),
		)
		log.WithField("bucket", a.cfg.S3Bucket).Info("S3 history reader enabled")
	}

	// 6. Start HTTP server.
	srv := server.NewServer(a.cfg.Host, a.cfg.Port, manager, serverOpts...)

	addr := fmt.Sprintf("http://%s:%d", a.cfg.Host, a.cfg.Port)
	fmt.Printf("flume aggregator listening on %s (gRPC :%d)\n", addr, a.cfg.GRPCPort)
	log.WithFields(log.Fields{
		"http": addr,
		"grpc": a.cfg.GRPCPort,
	}).Info("aggregator starting")

	return srv.Start(ctx)
}
