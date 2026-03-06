package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/interpt-co/flume/internal/buffer"
	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/query"
)

// Storage is the interface required by the history handler. It mirrors the
// ReadBefore method from internal/storage.Storage to avoid a circular import.
type Storage interface {
	ReadBefore(ctx context.Context, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error)
}

// CrossNodeStorage extends Storage with cross-node read capability.
type CrossNodeStorage interface {
	Storage
	CrossNodeReadBefore(ctx context.Context, basePrefix, patternName string, nodes []string, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error)
	DiscoverNodes(ctx context.Context) ([]string, error)
}

// HistoryHandler serves the /api/history endpoint backed by persistent storage.
type HistoryHandler struct {
	storage  Storage
	manager  *ClientManager
	xnStore  CrossNodeStorage // optional cross-node storage for aggregator mode
	s3Prefix string           // base S3 prefix for cross-node reads
}

// HistoryOption configures the HistoryHandler.
type HistoryOption func(*HistoryHandler)

// WithCrossNodeStorage enables cross-node S3 reads for the aggregator.
func WithCrossNodeStorage(store CrossNodeStorage, prefix string) HistoryOption {
	return func(h *HistoryHandler) {
		h.xnStore = store
		h.s3Prefix = prefix
	}
}

// WithManager attaches a ClientManager for buffer-first history reads.
func WithManager(m *ClientManager) HistoryOption {
	return func(h *HistoryHandler) {
		h.manager = m
	}
}

// historyResponse is the JSON envelope returned by /api/history.
type historyResponse struct {
	Messages []models.LogMessage `json:"messages"`
	HasMore  bool                `json:"has_more"`
}

// HandleHistory returns historical log messages.
//
//	GET /api/history?before=RFC3339&count=500&labels=key:val,key:val&pattern=X
//
// In aggregator mode with a pattern param, it reads from the pattern's ring
// buffer first, then falls back to cross-node S3 reads.
func (h *HistoryHandler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	before := parseBefore(r)
	if before.IsZero() {
		http.Error(w, `{"error":"invalid 'before' timestamp, use RFC3339 format"}`, http.StatusBadRequest)
		return
	}

	count := parseCount(r)
	filter := query.ParseLabels(r.URL.Query().Get("labels"))
	preFilter := query.ParseLabels(r.URL.Query().Get("filter"))
	// Merge pre-filter into filter (pre-filter keys take precedence).
	if len(preFilter) > 0 {
		if filter == nil {
			filter = preFilter
		} else {
			for k, v := range preFilter {
				filter[k] = v
			}
		}
	}
	patternName := r.URL.Query().Get("pattern")

	// Aggregator mode: unified buffer → S3 history.
	if patternName != "" && h.manager != nil && h.manager.registry != nil {
		msgs := h.unifiedHistory(r.Context(), patternName, before, count, map[string]string(filter))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(historyResponse{
			Messages: msgs,
			HasMore:  len(msgs) == count,
		})
		return
	}

	// Standalone mode: direct S3 read.
	if h.storage != nil {
		msgs, err := h.storage.ReadBefore(r.Context(), before, count, map[string]string(filter))
		if err != nil {
			http.Error(w, `{"error":"failed to read history"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(historyResponse{
			Messages: msgs,
			HasMore:  len(msgs) == count,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(historyResponse{})
}

// unifiedHistory reads from the pattern's ring buffer first, then S3 for older data.
func (h *HistoryHandler) unifiedHistory(ctx context.Context, patternName string, before time.Time, count int, filter map[string]string) []models.LogMessage {
	var result []models.LogMessage
	matcher := query.LabelMatcher(filter)

	// 1. Read from ring buffer.
	if p := h.manager.registry.Get(patternName); p != nil {
		msgs := p.Ring().GetAll()
		// Scan newest-first for messages before the cursor.
		for i := len(msgs) - 1; i >= 0 && len(result) < count; i-- {
			if msgs[i].Timestamp.Before(before) && matcher.Matches(msgs[i]) {
				result = append(result, msgs[i])
			}
		}
	}

	if len(result) >= count {
		return result[:count]
	}

	// 2. Fall back to S3 cross-node reads.
	if h.xnStore != nil {
		s3Before := before
		if len(result) > 0 {
			s3Before = result[len(result)-1].Timestamp
		}

		remaining := count - len(result)
		nodes := h.discoverNodes(ctx, patternName)
		if len(nodes) > 0 {
			s3Msgs, err := h.xnStore.CrossNodeReadBefore(ctx, h.s3Prefix, patternName, nodes, s3Before, remaining, filter)
			if err == nil {
				result = append(result, s3Msgs...)
			}
		}
	}

	return result
}

// discoverNodes returns nodes from the gRPC tracker + S3 fallback.
func (h *HistoryHandler) discoverNodes(ctx context.Context, patternName string) []string {
	// Primary: gRPC tracker nodes.
	// (Tracker is accessed via registry metadata — for now use S3 discovery)
	nodes, err := h.xnStore.DiscoverNodes(ctx)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	return nodes
}

// HandleLabels returns distinct label keys and their values from the ring buffer.
// Supports optional pattern query param for aggregator mode.
func (m *ClientManager) HandleLabels(w http.ResponseWriter, r *http.Request) {
	var ring *buffer.Ring[models.LogMessage]

	// Use pattern-scoped ring if in aggregator mode.
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
	if ring == nil {
		ring = m.ring
	}

	if ring == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string][]string{})
		return
	}

	msgs := ring.GetAll()
	labels := make(map[string]map[string]bool)

	for _, msg := range msgs {
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

	result := make(map[string][]string, len(labels))
	for k, vals := range labels {
		list := make([]string, 0, len(vals))
		for v := range vals {
			list = append(list, v)
		}
		sort.Strings(list)
		result[k] = list
	}

	// Remove pre-filter keys from the response.
	preFilter := query.ParseLabels(r.URL.Query().Get("filter"))
	if len(preFilter) > 0 {
		for k := range preFilter {
			delete(result, k)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func parseBefore(r *http.Request) time.Time {
	beforeStr := r.URL.Query().Get("before")
	if beforeStr == "" {
		return time.Now()
	}
	t, err := time.Parse(time.RFC3339Nano, beforeStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, beforeStr)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func parseCount(r *http.Request) int {
	count := 500
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if v, err := strconv.Atoi(countStr); err == nil && v > 0 {
			count = v
		}
	}
	if count > 1000 {
		count = 1000
	}
	return count
}
