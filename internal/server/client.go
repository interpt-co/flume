package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/interpt-co/flume/internal/models"
)

const (
	// StatusFollowing means the client receives live log messages.
	StatusFollowing = "following"
	// StatusStopped means the client has paused live message delivery.
	StatusStopped = "stopped"

	// sendBufferSize is the capacity of the per-client send channel.
	sendBufferSize = 256

	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the time allowed to read the next pong message.
	pongWait = 60 * time.Second

	// pingInterval must be less than pongWait.
	pingInterval = (pongWait * 9) / 10
)

// wsMessage represents a message exchanged over the WebSocket.
type wsMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// clientJoinedData is sent to the client upon connection.
type clientJoinedData struct {
	ClientID   string `json:"client_id"`
	BufferSize int    `json:"buffer_size"`
}

// logBulkData is a batch of log messages sent to the client.
type logBulkData struct {
	Messages []models.LogMessage `json:"messages"`
	Total    uint64              `json:"total"`
}

// setStatusData is received from the client to change status.
type setStatusData struct {
	Status string `json:"status"`
}

// loadRangeData is received from the client to request buffered messages.
type loadRangeData struct {
	Start int `json:"start"`
	Count int `json:"count"`
}

// Client represents a single connected WebSocket client.
type Client struct {
	id      string
	conn    *websocket.Conn
	manager *ClientManager
	send    chan models.LogMessage
	status  string
	mu      sync.Mutex // guards status
	writeMu sync.Mutex // guards WebSocket writes
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

// Send enqueues a message for delivery. Non-blocking: drops the message
// if the client's send buffer is full.
func (c *Client) Send(msg models.LogMessage) {
	c.mu.Lock()
	st := c.status
	c.mu.Unlock()

	if st == StatusStopped {
		return
	}

	select {
	case c.send <- msg:
	default:
		// drop message if buffer is full
	}
}

// readPump reads messages from the WebSocket connection. It handles
// client commands (set_status, load_range, ping) and removes the client
// when the connection is closed.
func (c *Client) readPump() {
	defer func() {
		c.manager.removeClient(c.id)
		c.conn.Close()
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
			messages := c.manager.ring.GetRange(data.Start, data.Count)
			c.writeLogBulk(messages)

		case "ping":
			c.writeJSON(wsMessage{Type: "pong"})
		}
	}
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
		c.conn.Close()
	}()

	var batch []models.LogMessage

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// Channel closed — manager removed us.
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

// writeLogBulk sends a log_bulk message with the given messages.
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

// writeJSON sends a JSON message to the WebSocket. Thread-safe via writeMu.
func (c *Client) writeJSON(msg wsMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(writeWait))
	return c.conn.WriteJSON(msg)
}

// mustMarshal marshals v to json.RawMessage, panicking on error (should never happen
// with known types).
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("server: failed to marshal: " + err.Error())
	}
	return b
}
