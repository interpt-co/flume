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
	ring     *buffer.Ring[models.LogMessage]
	bulkMS   int    // flush interval in milliseconds
	msgCount uint64 // total messages received (atomic)
}

// NewClientManager creates a new ClientManager.
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

	m.mu.Lock()
	m.clients[id] = c
	m.mu.Unlock()

	// Send client_joined message.
	c.writeJSON(wsMessage{
		Type: "client_joined",
		Data: mustMarshal(clientJoinedData{
			ClientID:   id,
			BufferSize: m.ring.Cap(),
		}),
	})

	go c.readPump()
	go c.writePump()
}

// removeClient unregisters a client and closes its send channel.
func (m *ClientManager) removeClient(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[id]; ok {
		close(c.send)
		delete(m.clients, id)
	}
}

// ConsumeLoop reads messages from the pipeline output channel and distributes
// them to all connected clients. The ring buffer is populated by a pipeline
// stage, so ConsumeLoop only handles broadcasting.
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
		BufferUsed:     m.ring.Len(),
		BufferCapacity: m.ring.Cap(),
	}
}
