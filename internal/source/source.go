package source

import (
	"context"

	"github.com/interpt-co/flume/internal/models"
)

// Source is the interface that all log sources must implement.
type Source interface {
	// Name returns a human-readable name for this source.
	Name() string
	// Start begins producing log messages. It returns a channel that will
	// receive messages until the source is exhausted or the context is
	// cancelled. The channel is closed when no more messages will be sent.
	Start(ctx context.Context) (<-chan models.LogMessage, error)
}
