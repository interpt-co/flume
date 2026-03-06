package server

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/interpt-co/flume/internal/buffer"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/pattern"
)

// StatusInfo holds runtime status information.
type StatusInfo struct {
	Clients        int    `json:"clients"`
	Messages       uint64 `json:"messages"`
	BufferUsed     int    `json:"buffer_used"`
	BufferCapacity int    `json:"buffer_capacity"`
}

// ClientManager manages connected WebSocket clients and distributes log
// messages received from the processing pipeline.
type ClientManager struct {
	mu       sync.RWMutex
	clients  map[string]*Client
	ring     *buffer.Ring[models.LogMessage] // used in standalone mode
	registry *pattern.Registry               // used in aggregator mode
	bulkMS   int                             // flush interval in milliseconds
	msgCount uint64                          // total messages received (atomic)
}

// NewClientManager creates a new ClientManager backed by a single ring buffer.
// bulkWindowMS controls how often batched messages are flushed to clients
// (default: 100ms if zero).
func NewClientManager(ring *buffer.Ring[models.LogMessage], bulkWindowMS int) *ClientManager {
	if bulkWindowMS <= 0 {
		bulkWindowMS = 100
	}
	return &ClientManager{
		clients: make(map[string]*Client),
		ring:    ring,
		bulkMS:  bulkWindowMS,
	}
}

// NewClientManagerWithRegistry creates a pattern-aware ClientManager for the aggregator.
func NewClientManagerWithRegistry(registry *pattern.Registry, bulkWindowMS int) *ClientManager {
	if bulkWindowMS <= 0 {
		bulkWindowMS = 100
	}
	return &ClientManager{
		clients:  make(map[string]*Client),
		registry: registry,
		bulkMS:   bulkWindowMS,
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleWS upgrades an HTTP connection to a WebSocket and registers the client.
func (m *ClientManager) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	id := uuid.New().String()
	c := newClient(id, conn, m)

	// Subscribe to pattern: from query param, or default to first available.
	if m.registry != nil {
		patternName := r.URL.Query().Get("pattern")
		if patternName == "" {
			names := m.registry.Names()
			if len(names) > 0 {
				patternName = names[0]
			}
		}
		if patternName != "" {
			c.subscribeToPattern(patternName)
		}
	}

	m.mu.Lock()
	m.clients[id] = c
	m.mu.Unlock()

	// Build client_joined data.
	joined := clientJoinedData{
		ClientID:   id,
		BufferSize: m.bufferCap(),
	}
	if m.registry != nil {
		joined.Patterns = m.registry.Names()
		if c.pattern != "" {
			joined.DefaultPattern = c.pattern
		} else if len(joined.Patterns) > 0 {
			joined.DefaultPattern = joined.Patterns[0]
		}
	}

	c.writeJSON(wsMessage{
		Type: "client_joined",
		Data: mustMarshal(joined),
	})

	go c.readPump()
	go c.writePump()
}

// removeClient unregisters a client and closes its send channel.
func (m *ClientManager) removeClient(id string) {
	m.mu.Lock()
	c, ok := m.clients[id]
	if ok {
		// Unsubscribe from pattern if subscribed.
		if c.pattern != "" && m.registry != nil {
			if p := m.registry.Get(c.pattern); p != nil {
				p.Unsubscribe(id)
			}
		}
		close(c.send)
		delete(m.clients, id)
	}
	m.mu.Unlock()
}

// ConsumeLoop reads messages from the pipeline output channel and distributes
// them to all connected clients. Used in standalone mode (not aggregator).
// It blocks until ctx is cancelled or the messages channel is closed.
func (m *ClientManager) ConsumeLoop(ctx context.Context, messages <-chan models.LogMessage) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			atomic.AddUint64(&m.msgCount, 1)

			m.mu.RLock()
			for _, c := range m.clients {
				c.Send(msg)
			}
			m.mu.RUnlock()
		}
	}
}

// MessageCount returns the total number of messages received.
func (m *ClientManager) MessageCount() uint64 {
	return atomic.LoadUint64(&m.msgCount)
}

// Status returns the current runtime status.
func (m *ClientManager) Status() StatusInfo {
	m.mu.RLock()
	clientCount := len(m.clients)
	m.mu.RUnlock()

	return StatusInfo{
		Clients:        clientCount,
		Messages:       m.MessageCount(),
		BufferUsed:     m.bufferLen(),
		BufferCapacity: m.bufferCap(),
	}
}

// bufferCap returns the ring buffer capacity (standalone or first pattern).
func (m *ClientManager) bufferCap() int {
	if m.ring != nil {
		return m.ring.Cap()
	}
	return 0
}

// bufferLen returns the ring buffer usage (standalone mode).
func (m *ClientManager) bufferLen() int {
	if m.ring != nil {
		return m.ring.Len()
	}
	return 0
}

// Registry returns the pattern registry, or nil in standalone mode.
func (m *ClientManager) Registry() *pattern.Registry {
	return m.registry
}
