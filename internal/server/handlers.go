package server

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// HandleStatus writes the current server status as JSON.
// In aggregator mode, accepts optional ?pattern= to return pattern-scoped stats.
func (m *ClientManager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	info := m.Status()

	// In aggregator mode, override buffer stats with pattern-scoped data.
	if m.registry != nil {
		patternName := r.URL.Query().Get("pattern")
		if patternName == "" {
			names := m.registry.Names()
			if len(names) > 0 {
				patternName = names[0]
			}
		}
		if patternName != "" {
			if p := m.registry.Get(patternName); p != nil {
				info.BufferUsed = p.Ring().Len()
				info.BufferCapacity = p.Ring().Cap()
			}
		}
	}

	json.NewEncoder(w).Encode(info)
}

// HandleLoadRange returns messages from the ring buffer.
// Query params: start (int, default 0), count (int, default 100), pattern (string, optional)
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

	// In aggregator mode, use pattern-scoped ring buffer.
	ring := m.ring
	if m.registry != nil {
		patternName := r.URL.Query().Get("pattern")
		if patternName == "" {
			names := m.registry.Names()
			if len(names) > 0 {
				patternName = names[0]
			}
		}
		if patternName != "" {
			if p := m.registry.Get(patternName); p != nil {
				ring = p.Ring()
			}
		}
	}

	var messages interface{}
	if ring != nil {
		messages = ring.GetRange(start, count)
	} else {
		messages = []struct{}{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"total":    m.MessageCount(),
	})
}

// PatternInfo holds per-pattern stats for the /api/patterns endpoint.
type PatternInfo struct {
	Name            string `json:"name"`
	BufferUsed      int    `json:"buffer_used"`
	BufferCapacity  int    `json:"buffer_capacity"`
	MessageCount    uint64 `json:"message_count"`
	SubscriberCount int    `json:"subscriber_count"`
}

// HandlePatterns returns available pattern names and stats.
func (m *ClientManager) HandlePatterns(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if m.registry == nil {
		json.NewEncoder(w).Encode([]PatternInfo{})
		return
	}

	patterns := m.registry.All()
	result := make([]PatternInfo, 0, len(patterns))
	for name, p := range patterns {
		result = append(result, PatternInfo{
			Name:            name,
			BufferUsed:      p.Ring().Len(),
			BufferCapacity:  p.Ring().Cap(),
			MessageCount:    p.MessageCount(),
			SubscriberCount: p.SubscriberCount(),
		})
	}
	json.NewEncoder(w).Encode(result)
}
