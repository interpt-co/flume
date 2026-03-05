package query

import (
	"testing"

	"github.com/interpt-co/flume/internal/models"
)

func TestParseLabels(t *testing.T) {
	tests := []struct {
		input string
		want  int // expected number of keys, -1 for nil
	}{
		{"", -1},
		{"   ", -1},
		{"level:error", 1},
		{"level:error,ns:prod", 2},
		{" level : error , ns : prod ", 2},
		{"bad", -1},
		{"a:b,,c:d", 2},
	}
	for _, tc := range tests {
		got := ParseLabels(tc.input)
		if tc.want == -1 {
			if got != nil {
				t.Errorf("ParseLabels(%q) = %v, want nil", tc.input, got)
			}
		} else if len(got) != tc.want {
			t.Errorf("ParseLabels(%q) len = %d, want %d", tc.input, len(got), tc.want)
		}
	}
}

func TestLabelMatcher_Matches(t *testing.T) {
	msg := models.LogMessage{
		Level:  "error",
		Labels: map[string]string{"ns": "prod", "app": "web"},
	}

	tests := []struct {
		name    string
		matcher LabelMatcher
		want    bool
	}{
		{"empty matches all", nil, true},
		{"level match", LabelMatcher{"level": "error"}, true},
		{"level mismatch", LabelMatcher{"level": "info"}, false},
		{"level case insensitive", LabelMatcher{"level": "ERROR"}, true},
		{"label match", LabelMatcher{"ns": "prod"}, true},
		{"label mismatch", LabelMatcher{"ns": "staging"}, false},
		{"missing label", LabelMatcher{"env": "prod"}, false},
		{"multi match", LabelMatcher{"ns": "prod", "level": "error"}, true},
		{"multi partial fail", LabelMatcher{"ns": "prod", "level": "warn"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.matcher.Matches(msg)
			if got != tc.want {
				t.Errorf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLabelMatcher_NilLabels(t *testing.T) {
	msg := models.LogMessage{Level: "info"}

	m := LabelMatcher{"ns": "prod"}
	if m.Matches(msg) {
		t.Error("expected no match when msg has no labels")
	}

	// level-only should still work
	m2 := LabelMatcher{"level": "info"}
	if !m2.Matches(msg) {
		t.Error("expected level match even without labels")
	}
}
