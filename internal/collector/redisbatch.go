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

	flush := func(c context.Context) {
		if len(batch) == 0 {
			return
		}
		if err := writer.WriteBatch(c, pattern, batch, bufCap); err != nil {
			log.WithError(err).WithField("pattern", pattern).Warn("collector: redis write error")
		}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			flush(drainCtx)
			cancel()
			return
		case msg, ok := <-ch:
			if !ok {
				flush(ctx)
				return
			}
			batch = append(batch, msg)
			if len(batch) >= redisBatchSize {
				flush(ctx)
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(redisBatchTimeout)
			}
		case <-timer.C:
			flush(ctx)
			timer.Reset(redisBatchTimeout)
		}
	}
}
