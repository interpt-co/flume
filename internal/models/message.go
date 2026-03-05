package models

import (
	"encoding/json"
	"time"
)

// SourceType identifies the origin type of a log message.
type SourceType string

const (
	SourceLoki   SourceType = "loki"
	SourceStdin  SourceType = "stdin"
	SourceFile   SourceType = "file"
	SourceSocket  SourceType = "socket"
	SourceForward SourceType = "forward"
	SourceDemo    SourceType = "demo"
)

// Origin describes where a log message came from.
type Origin struct {
	Name string            `json:"name"`
	Meta map[string]string `json:"meta,omitempty"`
}

// LogMessage is the item type that flows through the processing pipeline.
type LogMessage struct {
	ID          string            `json:"id"`
	Content     string            `json:"content"`
	JsonContent json.RawMessage   `json:"json_content,omitempty"`
	IsJson      bool              `json:"is_json"`
	Timestamp   time.Time         `json:"ts"`
	Source      SourceType        `json:"source"`
	Origin      Origin            `json:"origin"`
	Labels      map[string]string `json:"labels,omitempty"`
	Level       string            `json:"level,omitempty"`
}
