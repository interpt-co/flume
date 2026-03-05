package app

import (
	"context"
	"fmt"

	"github.com/paulofilip3/pipeline"
	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/buffer"
	"github.com/interpt-co/flume/internal/config"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/processing"
	"github.com/interpt-co/flume/internal/server"
	"github.com/interpt-co/flume/internal/storage"
)

// App wires all flume components together: source -> pipeline -> server.
type App struct {
	cfg config.Config
}

// New creates a new App with the given configuration.
func New(cfg config.Config) *App {
	if cfg.Verbose {
		log.SetLevel(log.DebugLevel)
	}
	return &App{cfg: cfg}
}

// Run starts all components and blocks until ctx is cancelled.
// input is the channel of raw log messages from the source(s).
func (a *App) Run(ctx context.Context, input <-chan models.LogMessage) error {
	// 1. Create ring buffer.
	ring := buffer.NewRing[models.LogMessage](a.cfg.MaxMessages)

	// 2. Create client manager.
	manager := server.NewClientManager(ring, a.cfg.BulkWindowMS)

	// 3. Build pipeline stages.
	stages := []pipeline.Stage[models.LogMessage]{
		{Name: "parse", Worker: processing.ParseWorker, Concurrency: 1},
		{Name: "enrich", Worker: processing.EnrichWorker, Concurrency: 1},
		{Name: "buffer", Worker: bufferStage(ring), Concurrency: 1},
	}

	// 4. Optionally add S3 storage stage.
	var store storage.Storage
	if a.cfg.S3Bucket != "" {
		s3cfg := storage.S3Config{
			Bucket:         a.cfg.S3Bucket,
			Prefix:         a.cfg.S3Prefix,
			Region:         a.cfg.S3Region,
			Endpoint:       a.cfg.S3Endpoint,
			FlushInterval:  a.cfg.S3FlushInterval,
			FlushCount:     a.cfg.S3FlushCount,
			PartitionLabel: a.cfg.S3PartitionLabel,
			Retention:      a.cfg.S3Retention,
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
		stages = append(stages, pipeline.Stage[models.LogMessage]{
			Name: "store", Worker: storeStage(store.Writer()), Concurrency: 1,
		})
		log.WithField("bucket", a.cfg.S3Bucket).Info("S3 storage enabled")
	}

	// 5. Create and run the pipeline.
	pipe, err := pipeline.New(stages...)
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %w", err)
	}
	out, errs := pipe.Run(ctx, input)

	// 6. Start ConsumeLoop (broadcasts to WS clients).
	go manager.ConsumeLoop(ctx, out)

	// 7. Start error drain goroutine.
	go func() {
		for err := range errs {
			log.WithError(err).Warn("pipeline error")
		}
	}()

	// 8. Create and start HTTP server.
	var serverOpts []server.ServerOption
	if store != nil {
		serverOpts = append(serverOpts, server.WithStorage(store))
	}
	srv := server.NewServer(a.cfg.Host, a.cfg.Port, manager, serverOpts...)

	// 9. Print startup message.
	addr := fmt.Sprintf("http://%s:%d", a.cfg.Host, a.cfg.Port)
	fmt.Printf("flume listening on %s\n", addr)
	log.WithField("addr", addr).Info("server starting")

	// 10. Block until server returns.
	return srv.Start(ctx)
}

// bufferStage returns a pipeline worker that pushes each message into the
// ring buffer and passes it through unchanged.
func bufferStage(ring *buffer.Ring[models.LogMessage]) pipeline.WorkerFunc[models.LogMessage] {
	return func(_ context.Context, msg models.LogMessage) (models.LogMessage, error) {
		ring.Push(msg)
		return msg, nil
	}
}

// storeStage returns a pipeline worker that sends each message to the S3
// storage writer channel (non-blocking) and passes it through unchanged.
func storeStage(writer chan<- models.LogMessage) pipeline.WorkerFunc[models.LogMessage] {
	return func(_ context.Context, msg models.LogMessage) (models.LogMessage, error) {
		select {
		case writer <- msg:
		default: // don't block if storage is slow
		}
		return msg, nil
	}
}
