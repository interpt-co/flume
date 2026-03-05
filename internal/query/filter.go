package query

import (
	"strings"

	"github.com/interpt-co/flume/internal/models"
)

// LabelMatcher holds key-value pairs that must all match for a message to pass.
// An empty matcher matches everything.
type LabelMatcher map[string]string

// ParseLabels parses a comma-separated "key:value,key:value" string into a LabelMatcher.
// Returns nil for empty input.
func ParseLabels(raw string) LabelMatcher {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	m := make(LabelMatcher)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			if key != "" {
				m[key] = val
			}
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// Matches returns true if msg satisfies all label constraints.
// "level" is treated as a virtual label — it matches against msg.Level.
// An empty/nil matcher matches everything.
func (lm LabelMatcher) Matches(msg models.LogMessage) bool {
	if len(lm) == 0 {
		return true
	}
	for k, v := range lm {
		if k == "level" {
			if !strings.EqualFold(msg.Level, v) {
				return false
			}
			continue
		}
		if msg.Labels == nil {
			return false
		}
		if msg.Labels[k] != v {
			return false
		}
	}
	return true
}
