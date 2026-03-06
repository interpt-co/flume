package cri

import (
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const maxPartialBufBytes = 1 << 20 // 1 MiB

// Line represents a parsed CRI log line.
type Line struct {
	Timestamp time.Time
	Stream    string // "stdout" or "stderr"
	Content   string
}

// ParseLine parses a single CRI log line.
// Format: 2006-01-02T15:04:05.999999999Z stdout F <content>
// The partial flag is P (partial) or F (full/final).
func ParseLine(raw string) (ts time.Time, stream string, partial bool, content string, err error) {
	// Find first space (after timestamp).
	i := strings.IndexByte(raw, ' ')
	if i < 0 {
		return time.Time{}, "", false, "", fmt.Errorf("cri: no timestamp separator in %q", raw)
	}
	ts, err = time.Parse(time.RFC3339Nano, raw[:i])
	if err != nil {
		return time.Time{}, "", false, "", fmt.Errorf("cri: invalid timestamp: %w", err)
	}
	rest := raw[i+1:]

	// Find second space (after stream).
	j := strings.IndexByte(rest, ' ')
	if j < 0 {
		return time.Time{}, "", false, "", fmt.Errorf("cri: no stream separator in %q", raw)
	}
	stream = rest[:j]
	rest = rest[j+1:]

	// Find third space (after partial flag).
	k := strings.IndexByte(rest, ' ')
	if k < 0 {
		// Flag only, no content.
		partial = rest == "P"
		return ts, stream, partial, "", nil
	}
	partial = rest[:k] == "P"
	content = rest[k+1:]

	return ts, stream, partial, content, nil
}

// Assembler reassembles partial CRI log lines into complete lines.
// It buffers partial lines per container until a full (F) line arrives.
type Assembler struct {
	mu      sync.Mutex
	buffers map[string]*strings.Builder // containerID -> partial content
}

// NewAssembler creates a new partial line assembler.
func NewAssembler() *Assembler {
	return &Assembler{
		buffers: make(map[string]*strings.Builder),
	}
}

// Process handles a parsed CRI line for a given container. If the line is
// partial (P flag), it buffers the content. If the line is full (F flag),
// it returns the assembled line. Returns nil if still buffering partials.
func (a *Assembler) Process(containerID string, ts time.Time, stream string, partial bool, content string) *Line {
	a.mu.Lock()
	defer a.mu.Unlock()

	if partial {
		buf, ok := a.buffers[containerID]
		if !ok {
			buf = &strings.Builder{}
			a.buffers[containerID] = buf
		}
		if buf.Len()+len(content) > maxPartialBufBytes {
			log.WithField("container", containerID).Warn("cri: partial buffer overflow, discarding")
			buf.Reset()
			return nil
		}
		buf.WriteString(content)
		return nil
	}

	// Full line — check if there's buffered partial content.
	buf, ok := a.buffers[containerID]
	if ok && buf.Len() > 0 {
		buf.WriteString(content)
		line := &Line{
			Timestamp: ts,
			Stream:    stream,
			Content:   buf.String(),
		}
		buf.Reset()
		return line
	}

	return &Line{
		Timestamp: ts,
		Stream:    stream,
		Content:   content,
	}
}

// Remove cleans up buffers for a container that is no longer being tailed.
func (a *Assembler) Remove(containerID string) {
	a.mu.Lock()
	delete(a.buffers, containerID)
	a.mu.Unlock()
}
