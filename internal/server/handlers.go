package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/query"
)

// HandleStatus writes the current server status as JSON.
// Accepts optional ?pattern= to return pattern-scoped stats.
func (m *ClientManager) HandleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	info := m.Status()

	patternName := r.URL.Query().Get("pattern")
	if patternName == "" {
		patterns, _ := m.redisReader.GetPatterns(r.Context())
		if len(patterns) > 0 {
			patternName = patterns[0]
		}
	}
	if patternName != "" {
		stats, err := m.redisReader.GetStats(r.Context(), patternName)
		if err == nil {
			info.Messages = uint64(stats.MessageCount)
			info.BufferCapacity = int(stats.BufferCapacity)
		}
		count, err := m.redisReader.GetMessageCount(r.Context(), patternName)
		if err == nil {
			info.BufferUsed = int(count)
		}
	}

	if err := json.NewEncoder(w).Encode(info); err != nil {
		log.WithError(err).Warn("HandleStatus: encode error")
	}
}

// HandleLoadRange returns messages from the Redis sorted set.
// Query params: start (int, default 0), count (int, default 100), pattern (string, optional)
func (m *ClientManager) HandleLoadRange(w http.ResponseWriter, r *http.Request) {
	startStr := r.URL.Query().Get("start")
	countStr := r.URL.Query().Get("count")
	start, _ := strconv.Atoi(startStr)
	if start < 0 {
		start = 0
	}
	count, _ := strconv.Atoi(countStr)
	if count <= 0 {
		count = 100
	}
	if count > 1000 {
		count = 1000
	}

	var msgs []models.LogMessage

	patternName := r.URL.Query().Get("pattern")
	if patternName == "" {
		patterns, _ := m.redisReader.GetPatterns(r.Context())
		if len(patterns) > 0 {
			patternName = patterns[0]
		}
	}
	if patternName != "" {
		var err error
		msgs, err = m.redisReader.GetMessages(r.Context(), patternName, start, count)
		if err != nil {
			log.WithError(err).Warn("HandleLoadRange: redis read error")
		}
	}

	// Apply pre-filter if provided.
	preFilter := query.ParseLabels(r.URL.Query().Get("filter"))
	if len(preFilter) > 0 {
		filtered := make([]models.LogMessage, 0, len(msgs))
		for _, m := range msgs {
			if preFilter.Matches(m) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}

	if msgs == nil {
		msgs = []models.LogMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": msgs,
		"total":    m.MessageCount(),
	}); err != nil {
		log.WithError(err).Warn("HandleLoadRange: encode error")
	}
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
	patterns, err := m.redisReader.GetPatterns(r.Context())
	if err != nil {
		log.WithError(err).Warn("HandlePatterns: redis error")
		http.Error(w, `{"error":"failed to retrieve patterns"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Count local subscribers per pattern.
	m.mu.RLock()
	subCounts := make(map[string]int)
	for _, c := range m.clients {
		c.mu.Lock()
		pn := c.pattern
		c.mu.Unlock()
		if pn != "" {
			subCounts[pn]++
		}
	}
	m.mu.RUnlock()

	result := make([]PatternInfo, 0, len(patterns))
	for _, name := range patterns {
		stats, _ := m.redisReader.GetStats(r.Context(), name)
		count, _ := m.redisReader.GetMessageCount(r.Context(), name)
		result = append(result, PatternInfo{
			Name:            name,
			BufferUsed:      int(count),
			BufferCapacity:  int(stats.BufferCapacity),
			MessageCount:    uint64(stats.MessageCount),
			SubscriberCount: subCounts[name],
		})
	}
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.WithError(err).Warn("HandlePatterns: encode error")
	}
}
