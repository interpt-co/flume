package flumegrpc

import (
	"sync"
	"time"
)

// NodeInfo holds metadata about a connected collector node.
type NodeInfo struct {
	Name      string    `json:"name"`
	Connected bool      `json:"connected"`
	LastSeen  time.Time `json:"last_seen"`
}

// Tracker tracks which collector nodes are connected per pattern.
type Tracker struct {
	mu    sync.RWMutex
	nodes map[string]map[string]*NodeInfo // pattern -> nodeName -> info
}

// NewTracker creates a new collector connection tracker.
func NewTracker() *Tracker {
	return &Tracker{
		nodes: make(map[string]map[string]*NodeInfo),
	}
}

// Add registers a node as connected for a pattern.
func (t *Tracker) Add(pattern, node string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.nodes[pattern] == nil {
		t.nodes[pattern] = make(map[string]*NodeInfo)
	}
	t.nodes[pattern][node] = &NodeInfo{
		Name:      node,
		Connected: true,
		LastSeen:  time.Now(),
	}
}

// Remove marks a node as disconnected for a pattern.
func (t *Tracker) Remove(pattern, node string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if nodes, ok := t.nodes[pattern]; ok {
		if info, ok := nodes[node]; ok {
			info.Connected = false
			info.LastSeen = time.Now()
		}
	}
}

// ConnectedNodes returns the names of currently connected nodes for a pattern.
func (t *Tracker) ConnectedNodes(pattern string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var names []string
	for _, info := range t.nodes[pattern] {
		if info.Connected {
			names = append(names, info.Name)
		}
	}
	return names
}

// AllNodes returns all known nodes (connected and recently disconnected) for a pattern.
func (t *Tracker) AllNodes(pattern string) []NodeInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var infos []NodeInfo
	for _, info := range t.nodes[pattern] {
		infos = append(infos, *info)
	}
	return infos
}

// AllPatterns returns all pattern names that have or had connected nodes.
func (t *Tracker) AllPatterns() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var patterns []string
	for p := range t.nodes {
		patterns = append(patterns, p)
	}
	return patterns
}
