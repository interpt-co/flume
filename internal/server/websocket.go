package server

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/interpt-co/flume/internal/query"
	flumeredis "github.com/interpt-co/flume/internal/redis"
)

// StatusInfo holds runtime status information.
type StatusInfo struct {
	Clients        int    `json:"clients"`
	Messages       uint64 `json:"messages"`
	BufferUsed     int    `json:"buffer_used"`
	BufferCapacity int    `json:"buffer_capacity"`
}

// ClientManager manages connected WebSocket clients and distributes log
// messages from Redis to browser clients.
type ClientManager struct {
	mu              sync.RWMutex
	clients         map[string]*Client
	redisReader     *flumeredis.Reader
	redisSubscriber *flumeredis.Subscriber
	bulkMS          int    // flush interval in milliseconds
	msgCount        uint64 // total messages received (atomic)
	auth            *AuthConfig
}

// NewClientManagerWithRedis creates a Redis-backed ClientManager for the dispatcher.
func NewClientManagerWithRedis(reader *flumeredis.Reader, subscriber *flumeredis.Subscriber, bulkWindowMS int) *ClientManager {
	if bulkWindowMS <= 0 {
		bulkWindowMS = 100
	}
	return &ClientManager{
		clients:         make(map[string]*Client),
		redisReader:     reader,
		redisSubscriber: subscriber,
		bulkMS:          bulkWindowMS,
	}
}

// RedisReader returns the Redis reader.
func (m *ClientManager) RedisReader() *flumeredis.Reader {
	return m.redisReader
}

// SetAuthConfig sets the auth callback configuration for WebSocket upgrades.
func (m *ClientManager) SetAuthConfig(ac *AuthConfig) {
	m.auth = ac
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleWS upgrades an HTTP connection to a WebSocket and registers the client.
func (m *ClientManager) HandleWS(w http.ResponseWriter, r *http.Request) {
	// Parse pre-filter from query params.
	var preFilter map[string]string
	if filterStr := r.URL.Query().Get("filter"); filterStr != "" {
		preFilter = map[string]string(query.ParseLabels(filterStr))
	}

	patternName := r.URL.Query().Get("pattern")

	// Auth callback (before WebSocket upgrade).
	if allowed, reason := m.auth.Check(r, preFilter, patternName); !allowed {
		http.Error(w, reason, http.StatusForbidden)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	id := uuid.New().String()
	c := newClient(id, conn, m)
	c.preFilter = preFilter

	// Subscribe to pattern: from query param, or default to first available.
	if patternName == "" {
		patterns, _ := m.redisReader.GetPatterns(r.Context())
		if len(patterns) > 0 {
			patternName = patterns[0]
		}
	}
	if patternName != "" {
		c.subscribeToPatternRedis(patternName)
	}

	m.mu.Lock()
	m.clients[id] = c
	m.mu.Unlock()

	// Build client_joined data.
	joined := clientJoinedData{
		ClientID:   id,
		PreFilters: c.preFilter,
	}
	patterns, _ := m.redisReader.GetPatterns(r.Context())
	joined.Patterns = patterns
	if c.pattern != "" {
		joined.DefaultPattern = c.pattern
	} else if len(patterns) > 0 {
		joined.DefaultPattern = patterns[0]
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
		if c.redisCancel != nil {
			c.redisCancel()
		}
		c.closed.Store(true)
		close(c.send)
		delete(m.clients, id)
	}
	m.mu.Unlock()
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
		Clients:  clientCount,
		Messages: m.MessageCount(),
	}
}
