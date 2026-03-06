package collector

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nxadm/tail"
	"github.com/paulofilip3/pipeline"
	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/collector/cri"
	"github.com/interpt-co/flume/internal/collector/discovery"
	"github.com/interpt-co/flume/internal/collector/fanout"
	"github.com/interpt-co/flume/internal/collector/podwatch"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/processing"
	flumeredis "github.com/interpt-co/flume/internal/redis"
	"github.com/interpt-co/flume/internal/storage"
)

// Collector is the DaemonSet component that collects container logs from the
// local node, enriches them with K8s metadata, and dispatches to per-pattern
// Redis sorted sets and S3 storage.
type Collector struct {
	cfg      *Config
	nodeName string
}

// New creates a new Collector.
func New(cfg *Config) *Collector {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		hostname, _ := os.Hostname()
		nodeName = hostname
	}
	return &Collector{cfg: cfg, nodeName: nodeName}
}

// Run starts all collector components and blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	if c.cfg.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	log.WithFields(log.Fields{
		"node":     c.nodeName,
		"logDir":   c.cfg.LogDir,
		"patterns": len(c.cfg.Patterns),
	}).Info("collector starting")

	// 1. Start pod watcher.
	podWatcher := podwatch.NewWatcher()

	// Load static labels for local testing.
	for key, labels := range c.cfg.StaticLabels {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) == 2 {
			podWatcher.SetLabels(parts[0], parts[1], labels)
			log.WithFields(log.Fields{
				"pod": key, "labels": len(labels),
			}).Debug("collector: loaded static labels")
		}
	}

	go func() {
		if err := podWatcher.Start(ctx, c.nodeName); err != nil {
			if len(c.cfg.StaticLabels) > 0 {
				log.Info("collector: pod watcher unavailable, using static labels")
			} else {
				log.WithError(err).Warn("collector: pod watcher failed (labels will be empty)")
			}
		}
	}()

	// 2. Create Redis writer (if configured).
	var redisWriter *flumeredis.Writer
	if c.cfg.Redis.Addr != "" {
		redisClient := flumeredis.NewClient(flumeredis.Config{
			Addr:     c.cfg.Redis.Addr,
			Password: c.cfg.Redis.Password,
			DB:       c.cfg.Redis.DB,
		})
		if err := redisClient.Ping(ctx); err != nil {
			return fmt.Errorf("redis ping: %w", err)
		}
		defer redisClient.Close()
		redisWriter = flumeredis.NewWriter(redisClient)
		log.WithField("addr", c.cfg.Redis.Addr).Info("collector: connected to Redis")
	}

	// 3. Create per-pattern resources.
	var dests []fanout.Destination
	for _, pDef := range c.cfg.Patterns {
		dest := fanout.Destination{
			Pattern: pDef,
		}

		// S3 writer per pattern.
		if c.cfg.S3.Bucket != "" {
			prefix := storage.NodePatternPrefix(c.cfg.S3.Prefix, c.nodeName, pDef.Name)
			s3cfg := storage.S3Config{
				Bucket:        c.cfg.S3.Bucket,
				Prefix:        prefix,
				Region:        c.cfg.S3.Region,
				Endpoint:      c.cfg.S3.Endpoint,
				FlushInterval: c.cfg.S3.FlushInterval,
				FlushCount:    c.cfg.S3.FlushCount,
				Retention:     c.cfg.S3.Retention,
			}
			s3store, err := storage.NewS3Storage(ctx, s3cfg)
			if err != nil {
				return fmt.Errorf("S3 storage for pattern %s: %w", pDef.Name, err)
			}
			go func() {
				if err := s3store.Start(ctx); err != nil {
					log.WithError(err).WithField("pattern", pDef.Name).Error("S3 storage loop error")
				}
			}()
			dest.S3 = s3store.Writer()
		}

		// Redis channel per pattern.
		if redisWriter != nil {
			redisCh := make(chan models.LogMessage, 1000)
			dest.Redis = redisCh
			go redisBatcher(ctx, pDef.Name, redisCh, redisWriter, c.cfg.BufferSize)
		}

		dests = append(dests, dest)
	}

	dispatcher := fanout.NewDispatcher(dests)

	// 4. Start file discovery watcher.
	fileEvents, err := discovery.WatchDir(ctx, c.cfg.LogDir)
	if err != nil {
		return fmt.Errorf("watching %s: %w", c.cfg.LogDir, err)
	}

	// 5. Merged channel for all tailer output.
	merged := make(chan models.LogMessage, 4096)
	assembler := cri.NewAssembler()

	// Track active tailers for cleanup.
	type tailerCancel struct {
		cancel context.CancelFunc
	}
	tailers := make(map[string]tailerCancel)

	// 6. Handle file events.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-fileEvents:
				if !ok {
					return
				}
				switch event.Type {
				case discovery.FileAdded:
					if _, exists := tailers[event.Path]; exists {
						continue
					}
					tCtx, tCancel := context.WithCancel(ctx)
					tailers[event.Path] = tailerCancel{cancel: tCancel}
					go tailFile(tCtx, event, podWatcher, assembler, merged)

				case discovery.FileRemoved:
					if tc, ok := tailers[event.Path]; ok {
						tc.cancel()
						delete(tailers, event.Path)
					}
				}
			}
		}
	}()

	// 7. Build processing pipeline.
	stages := []pipeline.Stage[models.LogMessage]{
		{Name: "parse", Worker: processing.ParseWorker, Concurrency: 1},
		{Name: "enrich", Worker: processing.EnrichWorker, Concurrency: 1},
	}

	pipe, err := pipeline.New(stages...)
	if err != nil {
		return fmt.Errorf("creating pipeline: %w", err)
	}

	pipeOut, errs := pipe.Run(ctx, merged)

	// 8. Error drain.
	go func() {
		for err := range errs {
			log.WithError(err).Warn("collector: pipeline error")
		}
	}()

	// 9. Dispatch pipeline output.
	for msg := range pipeOut {
		dispatcher.Dispatch(msg)
	}

	return nil
}

// tailFile tails a single container log file, parses CRI lines, enriches with
// pod labels, and sends to the merged channel.
func tailFile(ctx context.Context, event discovery.FileEvent, podWatcher *podwatch.Watcher, assembler *cri.Assembler, out chan<- models.LogMessage) {
	ref := event.Ref

	log.WithFields(log.Fields{
		"path":      event.Path,
		"pod":       ref.Pod,
		"namespace": ref.Namespace,
		"container": ref.Container,
	}).Debug("collector: starting tailer")

	// Use nxadm/tail to follow the file.
	tailCfg := tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: false,
		Location:  &tail.SeekInfo{Offset: 0, Whence: 2}, // seek to end
	}

	t, err := tail.TailFile(event.Path, tailCfg)
	if err != nil {
		log.WithError(err).WithField("path", event.Path).Warn("collector: failed to tail file")
		return
	}

	defer func() {
		t.Stop()
		t.Cleanup()
	}()
	defer assembler.Remove(ref.ID)

	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			if line.Err != nil {
				continue
			}

			ts, stream, partial, content, err := cri.ParseLine(line.Text)
			if err != nil {
				continue
			}

			assembled := assembler.Process(ref.ID, ts, stream, partial, content)
			if assembled == nil {
				continue // still buffering partials
			}

			// Build LogMessage.
			msg := models.LogMessage{
				Content:   assembled.Content,
				Timestamp: assembled.Timestamp,
				Source:    models.SourceContainer,
				Kube: &models.KubeMeta{
					Namespace: ref.Namespace,
					Pod:       ref.Pod,
					Container: ref.Container,
					PodUID:    ref.ID,
				},
			}

			// Enrich with pod labels.
			podLabels := podWatcher.GetLabels(ref.Namespace, ref.Pod)
			if podLabels != nil {
				msg.Labels = make(map[string]string, len(podLabels))
				for k, v := range podLabels {
					msg.Labels[k] = v
				}
			}

			// Add K8s metadata and stream as labels for filtering.
			if msg.Labels == nil {
				msg.Labels = make(map[string]string)
			}
			msg.Labels["namespace"] = ref.Namespace
			msg.Labels["pod"] = ref.Pod
			msg.Labels["container"] = ref.Container
			msg.Labels["stream"] = assembled.Stream

			select {
			case out <- msg:
			case <-ctx.Done():
				return
			}
		}
	}
}
