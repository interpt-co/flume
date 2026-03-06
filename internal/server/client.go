package server

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/query"
)

const (
	StatusFollowing = "following"
	StatusStopped   = "stopped"

	sendBufferSize = 256
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = (pongWait * 9) / 10
)

// wsMessage represents a message exchanged over the WebSocket.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// clientJoinedData is sent to the client upon connection.
type clientJoinedData struct {
	ClientID       string            `json:"client_id"`
	BufferSize     int               `json:"buffer_size"`
	Patterns       []string          `json:"patterns,omitempty"`
	DefaultPattern string            `json:"default_pattern,omitempty"`
	PreFilters     map[string]string `json:"pre_filters,omitempty"`
}

// logBulkData is a batch of log messages sent to the client.
type logBulkData struct {
	Messages []models.LogMessage `json:"messages"`
	Total    uint64              `json:"total"`
}

type setStatusData struct {
	Status string `json:"status"`
}

type setFilterData struct {
	Labels map[string]string `json:"labels"`
}

type loadRangeData struct {
	Start int `json:"start"`
	Count int `json:"count"`
}

type setPatternData struct {
	Pattern string `json:"pattern"`
}

type patternChangedData struct {
	Pattern    string `json:"pattern"`
	BufferSize int    `json:"buffer_size"`
	BufferUsed int    `json:"buffer_used"`
}

// Client represents a single connected WebSocket client.
type Client struct {
	id          string
	conn        *websocket.Conn
	manager     *ClientManager
	send        chan models.LogMessage
	closed      atomic.Bool
	status      string
	pattern     string
	labelFilter map[string]string
	preFilter   map[string]string
	redisCancel context.CancelFunc
	mu          sync.Mutex
	writeMu     sync.Mutex
	closeOnce   sync.Once
}

func newClient(id string, conn *websocket.Conn, manager *ClientManager) *Client {
	return &Client{
		id:      id,
		conn:    conn,
		manager: manager,
		send:    make(chan models.LogMessage, sendBufferSize),
		status:  StatusFollowing,
	}
}

// subscribeToPatternRedis subscribes the client to a Redis-backed pattern.
func (c *Client) subscribeToPatternRedis(patternName string) {
	if c.manager.redisSubscriber == nil {
		return
	}

	if c.redisCancel != nil {
		c.redisCancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.redisCancel = cancel
	c.pattern = patternName

	ch := c.manager.redisSubscriber.Subscribe(ctx, patternName)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case batch, ok := <-ch:
				if !ok {
					return
				}
				for _, msg := range batch {
					if c.closed.Load() {
						return
					}
					c.mu.Lock()
					st := c.status
					pre := c.preFilter
					filter := c.labelFilter
					c.mu.Unlock()

					if st == StatusStopped {
						continue
					}
					if len(pre) > 0 && !query.LabelMatcher(pre).Matches(msg) {
						continue
					}
					if len(filter) > 0 && !query.LabelMatcher(filter).Matches(msg) {
						continue
					}

					select {
					case c.send <- msg:
					default:
					}
				}
			}
		}
	}()
}

// Send enqueues a message for delivery. Non-blocking.
func (c *Client) Send(msg models.LogMessage) {
	if c.closed.Load() {
		return
	}

	c.mu.Lock()
	st := c.status
	filter := c.labelFilter
	pre := c.preFilter
	c.mu.Unlock()

	if st == StatusStopped {
		return
	}
	if len(pre) > 0 && !query.LabelMatcher(pre).Matches(msg) {
		return
	}
	if len(filter) > 0 && !query.LabelMatcher(filter).Matches(msg) {
		return
	}

	select {
	case c.send <- msg:
	default:
	}
}

// readPump reads messages from the WebSocket connection.
func (c *Client) readPump() {
	defer func() {
		c.manager.removeClient(c.id)
		c.closeOnce.Do(func() { c.conn.Close() })
	}()

	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "set_status":
			var data setStatusData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}
			if data.Status == StatusFollowing || data.Status == StatusStopped {
				c.mu.Lock()
				c.status = data.Status
				c.mu.Unlock()
			}

		case "load_range":
			var data loadRangeData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}
			c.mu.Lock()
			pn := c.pattern
			pre := c.preFilter
			filter := c.labelFilter
			c.mu.Unlock()
			if pn != "" {
				messages, err := c.manager.redisReader.GetMessages(context.Background(), pn, data.Start, data.Count)
				if err != nil {
					log.WithError(err).Warn("load_range: redis read error")
					continue
				}
				if len(pre) > 0 || len(filter) > 0 {
					filtered := make([]models.LogMessage, 0, len(messages))
					for _, m := range messages {
						if len(pre) > 0 && !query.LabelMatcher(pre).Matches(m) {
							continue
						}
						if len(filter) > 0 && !query.LabelMatcher(filter).Matches(m) {
							continue
						}
						filtered = append(filtered, m)
					}
					messages = filtered
				}
				c.writeLogBulk(messages)
			}

		case "set_filter":
			var data setFilterData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}
			c.mu.Lock()
			for k := range c.preFilter {
				delete(data.Labels, k)
			}
			if len(data.Labels) == 0 {
				c.labelFilter = nil
			} else {
				c.labelFilter = data.Labels
			}
			c.mu.Unlock()

		case "set_pattern":
			var data setPatternData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}
			c.handleSetPatternRedis(data.Pattern)

		case "ping":
			c.writeJSON(wsMessage{Type: "pong"})
		}
	}
}

// handleSetPatternRedis switches the client to a new pattern using Redis.
func (c *Client) handleSetPatternRedis(patternName string) {
	c.subscribeToPatternRedis(patternName)

	stats, _ := c.manager.redisReader.GetStats(context.Background(), patternName)
	count, _ := c.manager.redisReader.GetMessageCount(context.Background(), patternName)

	c.writeJSON(wsMessage{
		Type: "pattern_changed",
		Data: mustMarshal(patternChangedData{
			Pattern:    patternName,
			BufferSize: int(stats.BufferCapacity),
			BufferUsed: int(count),
		}),
	})
}

// writePump batches messages from the send channel and flushes them to the
// WebSocket at regular intervals.
func (c *Client) writePump() {
	flushInterval := time.Duration(c.manager.bulkMS) * time.Millisecond
	ticker := time.NewTicker(flushInterval)
	pingTicker := time.NewTicker(pingInterval)

	defer func() {
		ticker.Stop()
		pingTicker.Stop()
		c.closeOnce.Do(func() { c.conn.Close() })
	}()

	var batch []models.LogMessage

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.writeMu.Lock()
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				c.writeMu.Unlock()
				return
			}
			batch = append(batch, msg)

		case <-ticker.C:
			if len(batch) > 0 {
				c.writeLogBulk(batch)
				batch = nil
			}

		case <-pingTicker.C:
			c.writeMu.Lock()
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) writeLogBulk(messages []models.LogMessage) {
	total := c.manager.MessageCount()
	data := logBulkData{
		Messages: messages,
		Total:    total,
	}
	c.writeJSON(wsMessage{
		Type: "log_bulk",
		Data: mustMarshal(data),
	})
}

func (c *Client) writeJSON(msg wsMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteJSON(msg)
}

func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("server: failed to marshal: " + err.Error())
	}
	return b
}
