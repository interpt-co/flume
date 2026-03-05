package app

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/buffer"
	"github.com/interpt-co/flume/internal/config"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/processing"
	"github.com/interpt-co/flume/internal/server"
	"github.com/interpt-co/flume/internal/source"
	"github.com/interpt-co/flume/internal/storage"
)

// App wires all flume components together: source -> pipeline -> buffer -> server.
type App struct {
	cfg config.Config
	src source.Source
}

// New creates a new App with the given configuration and log source.
func New(cfg config.Config, src source.Source) (*App, error) {
	if cfg.Verbose {
		log.SetLevel(log.DebugLevel)
	}
	return &App{cfg: cfg, src: src}, nil
}

// Run starts all components and blocks until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	// 1. Create ring buffer.
	ring := buffer.NewRing[models.LogMessage](a.cfg.MaxMessages)

	// 2. Create client manager.
	manager := server.NewClientManager(ring, a.cfg.BulkWindowMS)

	// 3. Create processing pipeline.
	pipe, err := processing.NewPipeline()
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}

	// 4. Optionally create S3 storage.
	var store storage.Storage
	if a.cfg.S3Bucket != "" {
		s3cfg := storage.S3Config{
			Bucket:        a.cfg.S3Bucket,
			Prefix:        a.cfg.S3Prefix,
			Region:        a.cfg.S3Region,
			Endpoint:      a.cfg.S3Endpoint,
			FlushInterval: a.cfg.S3FlushInterval,
			FlushCount:    a.cfg.S3FlushCount,
		}
		s3store, err := storage.NewS3Storage(ctx, s3cfg)
		if err != nil {
			return fmt.Errorf("failed to create S3 storage: %w", err)
		}
		store = s3store
		go func() {
			if err := store.Start(ctx); err != nil {
				log.WithError(err).Error("S3 storage loop exited with error")
			}
		}()
		log.WithField("bucket", a.cfg.S3Bucket).Info("S3 storage enabled")
	}

	// 5. Start the source.
	log.WithField("source", a.src.Name()).Info("starting source")
	sourceCh, err := a.src.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start source %q: %w", a.src.Name(), err)
	}

	// 6. Run the pipeline.
	out, errs := pipe.Run(ctx, sourceCh)

	// 7. Tee pipeline output to both client manager and storage.
	if store != nil {
		teedCh := make(chan models.LogMessage, 256)
		go func() {
			defer close(teedCh)
			writerCh := store.Writer()
			for msg := range out {
				teedCh <- msg
				select {
				case writerCh <- msg:
				default: // don't block if storage is slow
				}
			}
		}()
		out = teedCh
	}

	// 8. Start ConsumeLoop in a goroutine.
	go manager.ConsumeLoop(ctx, out)

	// 9. Start error drain goroutine.
	go func() {
		for err := range errs {
			log.WithError(err).Warn("pipeline error")
		}
	}()

	// 10. Create and start HTTP server.
	var serverOpts []server.ServerOption
	if store != nil {
		serverOpts = append(serverOpts, server.WithStorage(store))
	}
	srv := server.NewServer(a.cfg.Host, a.cfg.Port, manager, serverOpts...)

	// 11. Print startup message.
	addr := fmt.Sprintf("http://%s:%d", a.cfg.Host, a.cfg.Port)
	fmt.Printf("flume listening on %s\n", addr)
	log.WithField("addr", addr).Info("server starting")

	// 12. Block until server returns.
	return srv.Start(ctx)
}
