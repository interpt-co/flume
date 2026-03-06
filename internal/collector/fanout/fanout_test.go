package fanout

import (
	"sync/atomic"
	"testing"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/pattern"
)

func TestDispatchMultiMatch(t *testing.T) {
	var ring1, ring2 int64
	s3ch1 := make(chan models.LogMessage, 10)
	s3ch2 := make(chan models.LogMessage, 10)

	dests := []Destination{
		{
			Pattern: pattern.PatternDef{
				Name:     "api",
				Selector: pattern.Selector{MatchLabels: map[string]string{"app": "api"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ring1, 1) },
			S3:   s3ch1,
		},
		{
			Pattern: pattern.PatternDef{
				Name:     "production",
				Selector: pattern.Selector{MatchLabels: map[string]string{"env": "production"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ring2, 1) },
			S3:   s3ch2,
		},
	}

	d := NewDispatcher(dests)

	// Message matching both patterns.
	msg := models.LogMessage{
		Content: "hello",
		Labels:  map[string]string{"app": "api", "env": "production"},
	}
	d.Dispatch(msg)

	if atomic.LoadInt64(&ring1) != 1 {
		t.Errorf("ring1 = %d, want 1", ring1)
	}
	if atomic.LoadInt64(&ring2) != 1 {
		t.Errorf("ring2 = %d, want 1", ring2)
	}
	if len(s3ch1) != 1 {
		t.Errorf("s3ch1 len = %d, want 1", len(s3ch1))
	}
	if len(s3ch2) != 1 {
		t.Errorf("s3ch2 len = %d, want 1", len(s3ch2))
	}
}

func TestDispatchNoMatch(t *testing.T) {
	var ringCalls int64
	s3ch := make(chan models.LogMessage, 10)

	dests := []Destination{
		{
			Pattern: pattern.PatternDef{
				Name:     "api",
				Selector: pattern.Selector{MatchLabels: map[string]string{"app": "api"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ringCalls, 1) },
			S3:   s3ch,
		},
	}

	d := NewDispatcher(dests)

	msg := models.LogMessage{
		Content: "hello",
		Labels:  map[string]string{"app": "worker"},
	}
	d.Dispatch(msg)

	if atomic.LoadInt64(&ringCalls) != 0 {
		t.Errorf("ringCalls = %d, want 0", ringCalls)
	}
	if len(s3ch) != 0 {
		t.Errorf("s3ch len = %d, want 0", len(s3ch))
	}
}

func TestDispatchSingleMatch(t *testing.T) {
	var ring1, ring2 int64

	dests := []Destination{
		{
			Pattern: pattern.PatternDef{
				Name:     "api",
				Selector: pattern.Selector{MatchLabels: map[string]string{"app": "api"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ring1, 1) },
		},
		{
			Pattern: pattern.PatternDef{
				Name:     "web",
				Selector: pattern.Selector{MatchLabels: map[string]string{"app": "web"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ring2, 1) },
		},
	}

	d := NewDispatcher(dests)

	msg := models.LogMessage{
		Content: "hello",
		Labels:  map[string]string{"app": "api"},
	}
	d.Dispatch(msg)

	if atomic.LoadInt64(&ring1) != 1 {
		t.Errorf("ring1 = %d, want 1", ring1)
	}
	if atomic.LoadInt64(&ring2) != 0 {
		t.Errorf("ring2 = %d, want 0", ring2)
	}
}

func TestDispatcherReplace(t *testing.T) {
	var ring1, ring2 int64

	dests1 := []Destination{
		{
			Pattern: pattern.PatternDef{
				Name:     "old",
				Selector: pattern.Selector{MatchLabels: map[string]string{"app": "old"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ring1, 1) },
		},
	}

	d := NewDispatcher(dests1)

	dests2 := []Destination{
		{
			Pattern: pattern.PatternDef{
				Name:     "new",
				Selector: pattern.Selector{MatchLabels: map[string]string{"app": "new"}},
			},
			Ring: func(m models.LogMessage) { atomic.AddInt64(&ring2, 1) },
		},
	}

	d.Replace(dests2)

	// Old pattern should no longer match.
	d.Dispatch(models.LogMessage{Labels: map[string]string{"app": "old"}})
	if atomic.LoadInt64(&ring1) != 0 {
		t.Errorf("ring1 = %d after replace, want 0", ring1)
	}

	// New pattern should match.
	d.Dispatch(models.LogMessage{Labels: map[string]string{"app": "new"}})
	if atomic.LoadInt64(&ring2) != 1 {
		t.Errorf("ring2 = %d, want 1", ring2)
	}
}
