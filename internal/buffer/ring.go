package buffer

import "sync"

// Ring is a thread-safe, generic, fixed-capacity ring buffer.
// When full, Push overwrites the oldest item.
type Ring[T any] struct {
	mu   sync.Mutex
	buf  []T
	head int  // index of the oldest item
	len  int  // current number of items
	cap  int  // maximum capacity
}

// NewRing creates a new ring buffer with the given capacity.
// If capacity is less than 1 it is set to 1.
func NewRing[T any](capacity int) *Ring[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &Ring[T]{
		buf: make([]T, capacity),
		cap: capacity,
	}
}

// Push adds an item to the ring buffer. If the buffer is full the oldest
// item is overwritten.
func (r *Ring[T]) Push(item T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.len < r.cap {
		// Buffer not yet full: write at head+len.
		r.buf[(r.head+r.len)%r.cap] = item
		r.len++
	} else {
		// Buffer full: overwrite the oldest item and advance head.
		r.buf[r.head] = item
		r.head = (r.head + 1) % r.cap
	}
}

// GetAll returns all items in the buffer ordered from oldest to newest.
func (r *Ring[T]) GetAll() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]T, r.len)
	for i := 0; i < r.len; i++ {
		out[i] = r.buf[(r.head+i)%r.cap]
	}
	return out
}

// GetRange returns up to count items starting from the given logical index
// (0 = oldest item currently in the buffer). Out-of-range indices are clamped.
func (r *Ring[T]) GetRange(start, count int) []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	if start < 0 {
		start = 0
	}
	if start >= r.len {
		return nil
	}
	end := start + count
	if end > r.len {
		end = r.len
	}

	out := make([]T, end-start)
	for i := start; i < end; i++ {
		out[i-start] = r.buf[(r.head+i)%r.cap]
	}
	return out
}

// Len returns the current number of items in the buffer.
func (r *Ring[T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.len
}

// Cap returns the maximum capacity of the buffer.
func (r *Ring[T]) Cap() int {
	return r.cap
}
