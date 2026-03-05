package source

import (
	"bufio"
	"context"
	"net"
	"sync"
	"time"

	"github.com/interpt-co/flume/internal/models"
)

// SocketSource accepts TCP connections and reads newline-delimited log
// messages from each one.
type SocketSource struct {
	addr     string
	listener net.Listener
}

// NewSocketSource creates a SocketSource that will listen on the given address.
// The listener is created during Start().
func NewSocketSource(addr string) *SocketSource {
	return &SocketSource{addr: addr}
}

// NewSocketSourceFromListener creates a SocketSource using a pre-existing
// net.Listener. This is useful for testing with a random port (:0).
func NewSocketSourceFromListener(l net.Listener) *SocketSource {
	return &SocketSource{listener: l}
}

// Name returns "socket".
func (s *SocketSource) Name() string {
	return "socket"
}

// Addr returns the listener's address. It is nil before Start() is called.
func (s *SocketSource) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Start begins accepting TCP connections and reading lines from each one.
// All messages from all connections are merged into a single output channel.
// The channel is closed when the context is cancelled and all connection
// goroutines have finished.
func (s *SocketSource) Start(ctx context.Context) (<-chan models.LogMessage, error) {
	if s.listener == nil {
		l, err := net.Listen("tcp", s.addr)
		if err != nil {
			return nil, err
		}
		s.listener = l
	}

	ch := make(chan models.LogMessage)
	var connWg sync.WaitGroup

	// Close the listener when the context is cancelled to break Accept().
	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()

	go func() {
		defer func() {
			connWg.Wait()
			close(ch)
		}()

		for {
			conn, err := s.listener.Accept()
			if err != nil {
				// Listener closed (context cancelled) or transient error.
				// If context is done, exit gracefully.
				select {
				case <-ctx.Done():
					return
				default:
					// Transient error; keep accepting.
					continue
				}
			}

			connWg.Add(1)
			go func(c net.Conn) {
				defer connWg.Done()
				defer c.Close()

				remoteAddr := c.RemoteAddr().String()
				scanner := bufio.NewScanner(c)
				for scanner.Scan() {
					msg := models.LogMessage{
						Content:   scanner.Text(),
						Source:    models.SourceSocket,
						Origin:    models.Origin{Name: remoteAddr},
						Timestamp: time.Now(),
					}
					select {
					case <-ctx.Done():
						return
					case ch <- msg:
					}
				}
			}(conn)
		}
	}()

	return ch, nil
}
