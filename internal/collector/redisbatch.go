package collector

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
	flumeredis "github.com/interpt-co/flume/internal/redis"
)

const (
	redisBatchSize    = 100
	redisBatchTimeout = 200 * time.Millisecond
)

// redisBatcher accumulates messages and flushes them to Redis in batches.
func redisBatcher(ctx context.Context, pattern string, ch <-chan models.LogMessage, writer *flumeredis.Writer, bufCap int) {
	batch := make([]models.LogMessage, 0, redisBatchSize)
	timer := time.NewTimer(redisBatchTimeout)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := writer.WriteBatch(ctx, pattern, batch, bufCap); err != nil {
			log.WithError(err).WithField("pattern", pattern).Warn("collector: redis write error")
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case msg, ok := <-ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, msg)
			if len(batch) >= redisBatchSize {
				flush()
				timer.Reset(redisBatchTimeout)
			}
		case <-timer.C:
			flush()
			timer.Reset(redisBatchTimeout)
		}
	}
}
