package pattern

import (
	"sync"
	"sync/atomic"

	"github.com/interpt-co/flume/internal/buffer"
	"github.com/interpt-co/flume/internal/models"
)

// Subscriber receives messages from a pattern.
type Subscriber struct {
	ID     string
	Ch     chan models.LogMessage
	Done   chan struct{}
	// Filter is called before sending; if it returns false the message is dropped.
	// A nil Filter accepts all messages.
	Filter func(models.LogMessage) bool
}

// Pattern holds per-pattern state: a ring buffer and subscriber fan-out.
type Pattern struct {
	Name        string
	ring        *buffer.Ring[models.LogMessage]
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	msgCount    uint64
}

// NewPattern creates a new pattern with the given ring buffer capacity.
func NewPattern(name string, bufCap int) *Pattern {
	return &Pattern{
		Name:        name,
		ring:        buffer.NewRing[models.LogMessage](bufCap),
		subscribers: make(map[string]*Subscriber),
	}
}

// Ingest pushes messages into the ring buffer and fans out to all subscribers.
func (p *Pattern) Ingest(msgs []models.LogMessage) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, msg := range msgs {
		p.ring.Push(msg)
		atomic.AddUint64(&p.msgCount, 1)

		for _, sub := range p.subscribers {
			if sub.Filter != nil && !sub.Filter(msg) {
				continue
			}
			select {
			case sub.Ch <- msg:
			default: // drop if subscriber is slow
			}
		}
	}
}

// Subscribe registers a subscriber to receive messages from this pattern.
func (p *Pattern) Subscribe(sub *Subscriber) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subscribers[sub.ID] = sub
}

// Unsubscribe removes a subscriber.
func (p *Pattern) Unsubscribe(clientID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.subscribers, clientID)
}

// Ring returns the pattern's ring buffer.
func (p *Pattern) Ring() *buffer.Ring[models.LogMessage] {
	return p.ring
}

// MessageCount returns the total number of messages ingested.
func (p *Pattern) MessageCount() uint64 {
	return atomic.LoadUint64(&p.msgCount)
}

// SubscriberCount returns the current number of subscribers.
func (p *Pattern) SubscriberCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.subscribers)
}
