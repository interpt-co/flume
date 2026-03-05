package source

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

// StdinSource reads lines from an io.Reader and emits them as LogMessages.
// Although named "stdin", it accepts any reader so it can be tested easily.
// The CLI wires it with os.Stdin.
type StdinSource struct {
	reader io.Reader
}

// NewStdinSource creates a new StdinSource that reads from the given reader.
func NewStdinSource(reader io.Reader) *StdinSource {
	return &StdinSource{reader: reader}
}

// Name returns "stdin".
func (s *StdinSource) Name() string {
	return "stdin"
}

// Start launches a goroutine that reads lines from the reader and sends them
// as LogMessages on the returned channel. The channel is closed when the
// reader reaches EOF or the context is cancelled.
func (s *StdinSource) Start(ctx context.Context) (<-chan models.LogMessage, error) {
	ch := make(chan models.LogMessage)

	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(s.reader)
		for scanner.Scan() {
			msg := models.LogMessage{
				Content:   scanner.Text(),
				Source:    models.SourceStdin,
				Origin:    models.Origin{Name: "stdin"},
				Timestamp: time.Now(),
			}

			select {
			case <-ctx.Done():
				return
			case ch <- msg:
			}
		}
		// scanner.Err() is intentionally not checked here; EOF is the
		// normal termination and other errors simply end the stream.
	}()

	return ch, nil
}
