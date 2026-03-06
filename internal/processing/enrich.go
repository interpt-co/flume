package processing

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/valyala/fastjson"

	"github.com/interpt-co/flume/internal/models"
)

// enrichWorker assigns an ID, fills in a missing timestamp, and extracts the
// log level from the message content.
func EnrichWorker(_ context.Context, msg models.LogMessage) (models.LogMessage, error) {
	msg.ID = uuid.NewString()
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	msg.Level = extractLevel(msg)
	return msg, nil
}

// knownLevels is the set of recognised log level strings (all lowercase).
var knownLevels = map[string]bool{
	"trace": true,
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
	"fatal": true,
}

// normalizeLevel maps common aliases to their canonical short form.
func normalizeLevel(raw string) string {
	l := strings.ToLower(strings.TrimSpace(raw))
	if l == "warning" {
		return "warn"
	}
	if knownLevels[l] {
		return l
	}
	return ""
}

// bracketRe matches patterns like [ERROR], [WARN], [INFO], etc.
var bracketRe = regexp.MustCompile(`(?i)\[(trace|debug|info|warn|warning|error|fatal)\]`)

// bareRe matches a bare level keyword that appears either at the start of a
// line or right after a timestamp-like prefix (digits, dashes, colons, dots,
// spaces, T, Z).
var bareRe = regexp.MustCompile(`(?im)(?:^|[\d\-:.TZ ]+\s)(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)\b`)

// extractLevel tries to detect a log level from the message content.
// It checks JSON fields first, then bracket patterns, then bare keywords.
func extractLevel(msg models.LogMessage) string {
	// 1. JSON: look for "level" or "severity" field.
	if msg.IsJson && len(msg.JsonContent) > 0 {
		v, err := fastjson.ParseBytes(msg.JsonContent)
		if err == nil {
			for _, key := range []string{"level", "severity"} {
				if s := v.GetStringBytes(key); len(s) > 0 {
					if lvl := normalizeLevel(string(s)); lvl != "" {
						return lvl
					}
				}
			}
		}
	}

	content := msg.Content

	// 2. Bracket patterns: [ERROR], [WARN], etc.
	if m := bracketRe.FindStringSubmatch(content); len(m) > 1 {
		if lvl := normalizeLevel(m[1]); lvl != "" {
			return lvl
		}
	}

	// 3. Bare level keywords.
	if m := bareRe.FindStringSubmatch(content); len(m) > 1 {
		if lvl := normalizeLevel(m[1]); lvl != "" {
			return lvl
		}
	}

	return ""
}
