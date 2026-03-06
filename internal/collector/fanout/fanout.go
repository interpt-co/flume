package fanout

import (
	"sync"

	"github.com/interpt-co/flume/internal/models"
	"github.com/interpt-co/flume/internal/pattern"
)

// Destination represents a matched pattern's output channels.
type Destination struct {
	Pattern pattern.PatternDef
	Ring    func(models.LogMessage) // push to ring buffer
	S3     chan<- models.LogMessage // per-pattern S3 storage writer
	GRPC   chan<- models.LogMessage // per-pattern gRPC stream
}

// Dispatcher evaluates each message against all pattern selectors and sends
// matching messages to the corresponding destinations.
type Dispatcher struct {
	mu           sync.RWMutex
	destinations []Destination
}

// NewDispatcher creates a new fan-out dispatcher.
func NewDispatcher(dests []Destination) *Dispatcher {
	return &Dispatcher{destinations: dests}
}

// Dispatch sends a message to all destinations whose pattern selector matches
// the message's pod labels. Labels are read from msg.Labels.
func (d *Dispatcher) Dispatch(msg models.LogMessage) {
	d.mu.RLock()
	dests := d.destinations
	d.mu.RUnlock()

	for _, dest := range dests {
		if !dest.Pattern.Selector.Matches(msg.Labels) {
			continue
		}

		if dest.Ring != nil {
			dest.Ring(msg)
		}

		if dest.S3 != nil {
			select {
			case dest.S3 <- msg:
			default: // non-blocking
			}
		}

		if dest.GRPC != nil {
			select {
			case dest.GRPC <- msg:
			default: // non-blocking
			}
		}
	}
}

// Replace atomically swaps the destinations list. Used for config reload.
func (d *Dispatcher) Replace(dests []Destination) {
	d.mu.Lock()
	d.destinations = dests
	d.mu.Unlock()
}
