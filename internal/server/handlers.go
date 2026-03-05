package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// HandleStatus writes the current server status as JSON.
func (m *ClientManager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m.Status())
}

// HandleLoadRange returns messages from the ring buffer.
// Query params: start (int, default 0), count (int, default 100)
func (m *ClientManager) HandleLoadRange(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start")
	countStr := r.URL.Query().Get("count")
	start, _ := strconv.Atoi(startStr) // defaults to 0 on parse error
	count, _ := strconv.Atoi(countStr)
	if count <= 0 {
		count = 100
	}
	if count > 1000 {
		count = 1000
	}

	messages := m.ring.GetRange(start, count)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"total":    m.MessageCount(),
	})
}
