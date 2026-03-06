package pattern

import (
	"testing"
)

func TestSelectorMatches(t *testing.T) {
	tests := []struct {
		name      string
		selector  Selector
		podLabels map[string]string
		want      bool
	}{
		{
			name:      "empty selector matches everything",
			selector:  Selector{},
			podLabels: map[string]string{"app": "web"},
			want:      true,
		},
		{
			name:      "nil matchLabels matches everything",
			selector:  Selector{MatchLabels: nil},
			podLabels: map[string]string{"app": "web"},
			want:      true,
		},
		{
			name:      "single label match",
			selector:  Selector{MatchLabels: map[string]string{"app": "api-gateway"}},
			podLabels: map[string]string{"app": "api-gateway", "env": "production"},
			want:      true,
		},
		{
			name:      "multi label match",
			selector:  Selector{MatchLabels: map[string]string{"app": "web", "env": "production"}},
			podLabels: map[string]string{"app": "web", "env": "production", "version": "v2"},
			want:      true,
		},
		{
			name:      "single label no match",
			selector:  Selector{MatchLabels: map[string]string{"app": "api-gateway"}},
			podLabels: map[string]string{"app": "web"},
			want:      false,
		},
		{
			name:      "partial match fails",
			selector:  Selector{MatchLabels: map[string]string{"app": "web", "env": "staging"}},
			podLabels: map[string]string{"app": "web", "env": "production"},
			want:      false,
		},
		{
			name:      "missing label",
			selector:  Selector{MatchLabels: map[string]string{"app": "web"}},
			podLabels: map[string]string{"env": "production"},
			want:      false,
		},
		{
			name:      "nil pod labels",
			selector:  Selector{MatchLabels: map[string]string{"app": "web"}},
			podLabels: nil,
			want:      false,
		},
		{
			name:      "empty selector + nil pod labels",
			selector:  Selector{},
			podLabels: nil,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.selector.Matches(tt.podLabels)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchingPatterns(t *testing.T) {
	patterns := []PatternDef{
		{Name: "api-logs", Selector: Selector{MatchLabels: map[string]string{"app": "api-gateway"}}},
		{Name: "all-production", Selector: Selector{MatchLabels: map[string]string{"env": "production"}}},
		{Name: "web-prod", Selector: Selector{MatchLabels: map[string]string{"app": "web", "env": "production"}}},
	}

	tests := []struct {
		name      string
		podLabels map[string]string
		wantNames []string
	}{
		{
			name:      "matches api-gateway",
			podLabels: map[string]string{"app": "api-gateway", "env": "staging"},
			wantNames: []string{"api-logs"},
		},
		{
			name:      "matches production patterns",
			podLabels: map[string]string{"app": "web", "env": "production"},
			wantNames: []string{"all-production", "web-prod"},
		},
		{
			name:      "matches all three",
			podLabels: map[string]string{"app": "api-gateway", "env": "production"},
			wantNames: []string{"api-logs", "all-production"},
		},
		{
			name:      "no match",
			podLabels: map[string]string{"app": "worker", "env": "staging"},
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchingPatterns(patterns, tt.podLabels)
			var gotNames []string
			for _, p := range got {
				gotNames = append(gotNames, p.Name)
			}
			if len(gotNames) != len(tt.wantNames) {
				t.Fatalf("got %v, want %v", gotNames, tt.wantNames)
			}
			for i := range gotNames {
				if gotNames[i] != tt.wantNames[i] {
					t.Errorf("got[%d] = %q, want %q", i, gotNames[i], tt.wantNames[i])
				}
			}
		})
	}
}
