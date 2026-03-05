package storage

import (
	"context"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

// Storage defines the interface for persisting and reading log messages.
type Storage interface {
	// Writer returns a channel that accepts messages to be persisted.
	// The storage implementation batches and flushes them.
	Writer() chan<- models.LogMessage

	// Start begins the background flush loop. Blocks until ctx is cancelled.
	// On shutdown, flushes any remaining buffered messages.
	Start(ctx context.Context) error

	// ReadBefore returns up to `count` messages with timestamps before `before`,
	// ordered newest-first (reverse chronological). If filter is non-nil, only
	// messages matching all filter labels are returned.
	ReadBefore(ctx context.Context, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error)
}
