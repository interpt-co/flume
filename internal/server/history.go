package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/query"
)

// Storage is the interface required by the history handler. It mirrors the
// ReadBefore method from internal/storage.Storage to avoid a circular import.
type Storage interface {
	ReadBefore(ctx context.Context, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error)
}

// HistoryHandler serves the /api/history endpoint backed by persistent storage.
type HistoryHandler struct {
	storage Storage
}

// historyResponse is the JSON envelope returned by /api/history.
type historyResponse struct {
	Messages []models.LogMessage `json:"messages"`
	HasMore  bool                `json:"has_more"`
}

// HandleHistory returns historical log messages from persistent storage.
//
//	GET /api/history?before={ISO_timestamp}&count=500&labels=key:val,key:val
//
// Parameters:
//   - before: RFC3339 timestamp (default: now)
//   - count:  number of messages to return (default 500, max 1000)
//   - labels: comma-separated key:value label filter
func (h *HistoryHandler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	// Parse "before" parameter.
	beforeStr := r.URL.Query().Get("before")
	var before time.Time
	if beforeStr != "" {
		var err error
		before, err = time.Parse(time.RFC3339Nano, beforeStr)
		if err != nil {
			// Try RFC3339 (without nanos) as fallback.
			before, err = time.Parse(time.RFC3339, beforeStr)
			if err != nil {
				http.Error(w, `{"error":"invalid 'before' timestamp, use RFC3339 format"}`, http.StatusBadRequest)
				return
			}
		}
	} else {
		before = time.Now()
	}

	// Parse "count" parameter.
	count := 500
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if v, err := strconv.Atoi(countStr); err == nil && v > 0 {
			count = v
		}
	}
	if count > 1000 {
		count = 1000
	}

	// Parse "labels" filter.
	filter := query.ParseLabels(r.URL.Query().Get("labels"))

	msgs, err := h.storage.ReadBefore(r.Context(), before, count, map[string]string(filter))
	if err != nil {
		http.Error(w, `{"error":"failed to read history"}`, http.StatusInternalServerError)
		return
	}

	hasMore := len(msgs) == count

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(historyResponse{
		Messages: msgs,
		HasMore:  hasMore,
	})
}

// HandleLabels returns distinct label keys and their values from the ring buffer.
func (m *ClientManager) HandleLabels(w http.ResponseWriter, r *http.Request) {
	msgs := m.ring.GetAll()
	labels := make(map[string]map[string]bool)

	for _, msg := range msgs {
		// Include level as a virtual label.
		if msg.Level != "" {
			if labels["level"] == nil {
				labels["level"] = make(map[string]bool)
			}
			labels["level"][msg.Level] = true
		}
		for k, v := range msg.Labels {
			if labels[k] == nil {
				labels[k] = make(map[string]bool)
			}
			labels[k][v] = true
		}
	}

	// Convert sets to sorted slices.
	result := make(map[string][]string, len(labels))
	for k, vals := range labels {
		list := make([]string, 0, len(vals))
		for v := range vals {
			list = append(list, v)
		}
		sort.Strings(list)
		result[k] = list
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
