package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/query"
)

// CrossNodeStorage is the interface for cross-node S3 reads in the history handler.
type CrossNodeStorage interface {
	ReadBefore(ctx context.Context, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error)
	CrossNodeReadBefore(ctx context.Context, basePrefix, patternName string, nodes []string, before time.Time, count int, filter map[string]string) ([]models.LogMessage, error)
	DiscoverNodes(ctx context.Context) ([]string, error)
}

// HistoryHandler serves the /api/history endpoint backed by persistent storage.
type HistoryHandler struct {
	manager  *ClientManager
	xnStore  CrossNodeStorage
	s3Prefix string
}

type historyResponse struct {
	Messages []models.LogMessage `json:"messages"`
	HasMore  bool                `json:"has_more"`
}

// HandleHistory returns historical log messages.
func (h *HistoryHandler) HandleHistory(w http.ResponseWriter, r *http.Request) {
	before := parseBefore(r)
	if before.IsZero() {
		http.Error(w, `{"error":"invalid 'before' timestamp, use RFC3339 format"}`, http.StatusBadRequest)
		return
	}

	count := parseCount(r)
	filter := query.ParseLabels(r.URL.Query().Get("labels"))
	preFilter := query.ParseLabels(r.URL.Query().Get("filter"))
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

	// Dispatcher mode: Redis → S3 fallback.
	if patternName != "" && h.manager != nil && h.manager.redisReader != nil {
		msgs := h.unifiedHistory(r.Context(), patternName, before, count, map[string]string(filter))
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(historyResponse{
			Messages: msgs,
			HasMore:  len(msgs) == count,
		}); err != nil {
			log.WithError(err).Warn("history: failed to encode response")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(historyResponse{}); err != nil {
		log.WithError(err).Warn("history: failed to encode response")
	}
}

// unifiedHistory reads from Redis first, then S3 for older data.
func (h *HistoryHandler) unifiedHistory(ctx context.Context, patternName string, before time.Time, count int, filter map[string]string) []models.LogMessage {
	var result []models.LogMessage
	matcher := query.LabelMatcher(filter)

	msgs, err := h.manager.RedisReader().GetMessagesBefore(ctx, patternName, before.UnixNano(), count)
	if err == nil {
		for _, msg := range msgs {
			if matcher.Matches(msg) {
				result = append(result, msg)
			}
		}
	}

	if len(result) >= count {
		return result[:count]
	}

	// Fall back to S3 cross-node reads.
	if h.xnStore != nil {
		s3Before := before
		if len(result) > 0 {
			s3Before = result[len(result)-1].Timestamp
		}

		remaining := count - len(result)
		nodes, err := h.xnStore.DiscoverNodes(ctx)
		if err == nil && len(nodes) > 0 {
			s3Msgs, err := h.xnStore.CrossNodeReadBefore(ctx, h.s3Prefix, patternName, nodes, s3Before, remaining, filter)
			if err == nil {
				result = append(result, s3Msgs...)
			}
		}
	}

	return result
}

// HandleLabels returns distinct label keys and their values from Redis.
func (m *ClientManager) HandleLabels(w http.ResponseWriter, r *http.Request) {
	patternName := r.URL.Query().Get("pattern")
	if patternName == "" {
		patterns, _ := m.redisReader.GetPatterns(r.Context())
		if len(patterns) > 0 {
			patternName = patterns[0]
		}
	}

	result := make(map[string][]string)
	if patternName != "" {
		labels, err := m.redisReader.GetLabels(r.Context(), patternName)
		if err == nil {
			for k, vals := range labels {
				sort.Strings(vals)
				result[k] = vals
			}
		}
	}

	// Remove pre-filter keys from the response.
	preFilter := query.ParseLabels(r.URL.Query().Get("filter"))
	for k := range preFilter {
		delete(result, k)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.WithError(err).Warn("labels: failed to encode response")
	}
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
