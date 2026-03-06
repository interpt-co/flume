package pattern

import (
	"sync"
)

// Registry manages named patterns for the aggregator.
type Registry struct {
	mu       sync.RWMutex
	patterns map[string]*Pattern
	bufCap   int // default ring buffer capacity for new patterns
}

// NewRegistry creates a new pattern registry.
func NewRegistry(bufCap int) *Registry {
	if bufCap <= 0 {
		bufCap = 10000
	}
	return &Registry{
		patterns: make(map[string]*Pattern),
		bufCap:   bufCap,
	}
}

// GetOrCreate returns an existing pattern or creates a new one.
func (r *Registry) GetOrCreate(name string) *Pattern {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.patterns[name]; ok {
		return p
	}
	p := NewPattern(name, r.bufCap)
	r.patterns[name] = p
	return p
}

// Get returns a pattern by name, or nil if not found.
func (r *Registry) Get(name string) *Pattern {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.patterns[name]
}

// Names returns all registered pattern names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.patterns))
	for name := range r.patterns {
		names = append(names, name)
	}
	return names
}

// All returns all registered patterns.
func (r *Registry) All() map[string]*Pattern {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*Pattern, len(r.patterns))
	for k, v := range r.patterns {
		result[k] = v
	}
	return result
}
