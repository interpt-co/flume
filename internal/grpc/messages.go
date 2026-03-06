package flumegrpc

import (
	"time"

	"github.com/interpt-co/flume/internal/models"
)

// LogEntry is the wire format for a single log message over gRPC.
type LogEntry struct {
	ID          string            `json:"id"`
	Content     string            `json:"content"`
	JsonContent []byte            `json:"json_content,omitempty"`
	IsJson      bool              `json:"is_json"`
	Timestamp   time.Time         `json:"timestamp"`
	Level       string            `json:"level,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Namespace   string            `json:"namespace"`
	Pod         string            `json:"pod"`
	Container   string            `json:"container"`
	NodeName    string            `json:"node_name"`
}

// ToLogMessage converts a wire LogEntry to the internal LogMessage model.
func (e *LogEntry) ToLogMessage() models.LogMessage {
	msg := models.LogMessage{
		ID:        e.ID,
		Content:   e.Content,
		IsJson:    e.IsJson,
		Timestamp: e.Timestamp,
		Source:    models.SourceContainer,
		Level:     e.Level,
		Labels:    e.Labels,
		Kube: &models.KubeMeta{
			Namespace: e.Namespace,
			Pod:       e.Pod,
			Container: e.Container,
			NodeName:  e.NodeName,
		},
	}
	if e.IsJson && len(e.JsonContent) > 0 {
		msg.JsonContent = e.JsonContent
	}
	return msg
}

// LogEntryFromMessage converts an internal LogMessage to the wire format.
func LogEntryFromMessage(msg models.LogMessage) LogEntry {
	e := LogEntry{
		ID:        msg.ID,
		Content:   msg.Content,
		IsJson:    msg.IsJson,
		Timestamp: msg.Timestamp,
		Level:     msg.Level,
		Labels:    msg.Labels,
	}
	if msg.IsJson {
		e.JsonContent = msg.JsonContent
	}
	if msg.Kube != nil {
		e.Namespace = msg.Kube.Namespace
		e.Pod = msg.Kube.Pod
		e.Container = msg.Kube.Container
		e.NodeName = msg.Kube.NodeName
	}
	return e
}

// Handshake is sent by the collector on stream establishment.
type Handshake struct {
	NodeName    string `json:"node_name"`
	PatternName string `json:"pattern_name"`
}

// LogBatch is a batch of log entries sent by the collector.
type LogBatch struct {
	Entries  []LogEntry `json:"entries"`
	Sequence uint64     `json:"sequence"`
}

// Ack is sent by the aggregator to confirm receipt.
type Ack struct {
	Sequence uint64 `json:"sequence"`
}
