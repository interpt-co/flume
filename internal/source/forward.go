package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"github.com/vmihailenco/msgpack/v5/msgpcode"

	"github.com/interpt-co/flume/internal/models"
)

// ForwardSource accepts TCP connections speaking the Fluent Bit Forward
// protocol (msgpack over TCP). It supports Message, Forward, and
// PackedForward modes.
type ForwardSource struct {
	addr     string
	listener net.Listener
}

// NewForwardSource creates a ForwardSource that will listen on the given address.
func NewForwardSource(addr string) *ForwardSource {
	return &ForwardSource{addr: addr}
}

// NewForwardSourceFromListener creates a ForwardSource using a pre-existing
// net.Listener. This is useful for testing with a random port (:0).
func NewForwardSourceFromListener(l net.Listener) *ForwardSource {
	return &ForwardSource{listener: l}
}

// Name returns "forward".
func (f *ForwardSource) Name() string {
	return "forward"
}

// Addr returns the listener's address. It is nil before Start() is called.
func (f *ForwardSource) Addr() net.Addr {
	if f.listener == nil {
		return nil
	}
	return f.listener.Addr()
}

// Start begins accepting TCP connections from Fluent Bit sidecars.
// Each connection is handled in its own goroutine. The returned channel
// receives decoded log messages and is closed when the context is cancelled.
func (f *ForwardSource) Start(ctx context.Context) (<-chan models.LogMessage, error) {
	if f.listener == nil {
		l, err := net.Listen("tcp", f.addr)
		if err != nil {
			return nil, err
		}
		f.listener = l
	}

	ch := make(chan models.LogMessage)
	var connWg sync.WaitGroup

	// Close the listener when the context is cancelled to break Accept().
	go func() {
		<-ctx.Done()
		f.listener.Close()
	}()

	go func() {
		defer func() {
			connWg.Wait()
			close(ch)
		}()

		for {
			conn, err := f.listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}

			connWg.Add(1)
			go func(c net.Conn) {
				defer connWg.Done()
				defer c.Close()
				f.handleConnection(ctx, c, ch)
			}(conn)
		}
	}()

	return ch, nil
}

// handleConnection decodes Forward protocol messages from a single connection.
func (f *ForwardSource) handleConnection(ctx context.Context, conn net.Conn, ch chan<- models.LogMessage) {
	dec := msgpack.NewDecoder(conn)

	for {
		// Each top-level message is an array.
		arrLen, err := dec.DecodeArrayLen()
		if err != nil {
			if err == io.EOF {
				return
			}
			// Connection broken or context cancelled.
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}

		if arrLen < 2 {
			// Invalid message, skip.
			for i := 0; i < arrLen; i++ {
				dec.DecodeInterface()
			}
			continue
		}

		// First element is always the tag.
		tag, err := dec.DecodeString()
		if err != nil {
			return
		}

		// Peek at the second element type to determine the mode.
		code, err := dec.PeekCode()
		if err != nil {
			return
		}

		var options map[string]interface{}

		switch {
		case isNumericCode(code):
			// Message mode: [tag, time, record]
			// or [tag, time, record, option]
			if arrLen < 3 {
				for i := 0; i < arrLen-1; i++ {
					dec.DecodeInterface()
				}
				continue
			}
			ts, err := decodeTime(dec)
			if err != nil {
				return
			}
			record, err := decodeRecord(dec)
			if err != nil {
				return
			}
			if arrLen >= 4 {
				options = decodeOptions(dec)
			}

			msg := recordToLogMessage(tag, ts, record)
			if !emit(ctx, ch, msg) {
				return
			}

		case msgpcode.IsFixedArray(code) || code == msgpcode.Array16 || code == msgpcode.Array32:
			// Forward mode: [tag, [[time, record], ...]]
			// or [tag, [[time, record], ...], option]
			entries, err := decodeEntries(dec)
			if err != nil {
				return
			}
			if arrLen >= 3 {
				options = decodeOptions(dec)
			}
			for _, entry := range entries {
				msg := recordToLogMessage(tag, entry.ts, entry.record)
				if !emit(ctx, ch, msg) {
					return
				}
			}

		case msgpcode.IsBin(code):
			// PackedForward mode: [tag, msgpack_bin]
			// or [tag, msgpack_bin, option]
			binData, err := dec.DecodeBytes()
			if err != nil {
				return
			}
			if arrLen >= 3 {
				options = decodeOptions(dec)
			}
			entries, err := unpackBin(binData)
			if err != nil {
				return
			}
			for _, entry := range entries {
				msg := recordToLogMessage(tag, entry.ts, entry.record)
				if !emit(ctx, ch, msg) {
					return
				}
			}

		default:
			// Unknown mode, skip remaining elements.
			for i := 1; i < arrLen; i++ {
				dec.DecodeInterface()
			}
		}

		// Handle ack if chunk option is present.
		if chunk, ok := options["chunk"]; ok {
			ack := map[string]interface{}{"ack": chunk}
			ackBytes, err := msgpack.Marshal(ack)
			if err == nil {
				conn.Write(ackBytes)
			}
		}
	}
}

// isNumericCode returns true if the msgpack code represents a numeric type
// (positive fixnum, negative fixnum, or explicit int/uint/float).
func isNumericCode(code byte) bool {
	if msgpcode.IsFixedNum(code) {
		return true
	}
	switch code {
	case msgpcode.Uint8, msgpcode.Uint16, msgpcode.Uint32, msgpcode.Uint64,
		msgpcode.Int8, msgpcode.Int16, msgpcode.Int32, msgpcode.Int64,
		msgpcode.Float, msgpcode.Double:
		return true
	}
	// Also handle ext types (Fluent Bit EventTime uses ext type 0).
	if msgpcode.IsExt(code) || msgpcode.IsFixedExt(code) {
		return true
	}
	return false
}

// entry holds a single time+record pair.
type entry struct {
	ts     time.Time
	record map[string]interface{}
}

// decodeTime decodes a time value from the Forward protocol.
// It handles both integer epoch seconds and Fluent Bit's EventTime extension.
func decodeTime(dec *msgpack.Decoder) (time.Time, error) {
	raw, err := dec.DecodeInterface()
	if err != nil {
		return time.Time{}, err
	}
	return interpretTime(raw)
}

// interpretTime converts a raw decoded value to time.Time.
func interpretTime(raw interface{}) (time.Time, error) {
	switch v := raw.(type) {
	case int64:
		return time.Unix(v, 0), nil
	case uint64:
		return time.Unix(int64(v), 0), nil
	case int32:
		return time.Unix(int64(v), 0), nil
	case uint32:
		return time.Unix(int64(v), 0), nil
	case int8:
		return time.Unix(int64(v), 0), nil
	case uint8:
		return time.Unix(int64(v), 0), nil
	case int16:
		return time.Unix(int64(v), 0), nil
	case uint16:
		return time.Unix(int64(v), 0), nil
	case int:
		return time.Unix(int64(v), 0), nil
	case float64:
		sec := int64(v)
		nsec := int64((v - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	default:
		return time.Now(), nil
	}
}

// decodeRecord decodes a msgpack map into a Go map.
func decodeRecord(dec *msgpack.Decoder) (map[string]interface{}, error) {
	raw, err := dec.DecodeInterface()
	if err != nil {
		return nil, err
	}
	return toStringKeyMap(raw), nil
}

// toStringKeyMap converts a decoded interface to map[string]interface{}.
func toStringKeyMap(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	case map[interface{}]interface{}:
		result := make(map[string]interface{}, len(v))
		for k, val := range v {
			result[fmt.Sprintf("%v", k)] = val
		}
		return result
	default:
		return nil
	}
}

// decodeOptions decodes the optional options map from the stream.
func decodeOptions(dec *msgpack.Decoder) map[string]interface{} {
	raw, err := dec.DecodeInterface()
	if err != nil {
		return nil
	}
	return toStringKeyMap(raw)
}

// decodeEntries decodes an array of [time, record] pairs (Forward mode).
func decodeEntries(dec *msgpack.Decoder) ([]entry, error) {
	arrLen, err := dec.DecodeArrayLen()
	if err != nil {
		return nil, err
	}

	entries := make([]entry, 0, arrLen)
	for i := 0; i < arrLen; i++ {
		pairLen, err := dec.DecodeArrayLen()
		if err != nil {
			return nil, err
		}
		if pairLen < 2 {
			// Skip malformed entry.
			for j := 0; j < pairLen; j++ {
				dec.DecodeInterface()
			}
			continue
		}
		ts, err := decodeTime(dec)
		if err != nil {
			return nil, err
		}
		record, err := decodeRecord(dec)
		if err != nil {
			return nil, err
		}
		// Skip extra fields in the pair.
		for j := 2; j < pairLen; j++ {
			dec.DecodeInterface()
		}
		entries = append(entries, entry{ts: ts, record: record})
	}
	return entries, nil
}

// unpackBin unpacks a PackedForward binary blob into entries.
// The blob contains concatenated msgpack arrays of [time, record].
func unpackBin(data []byte) ([]entry, error) {
	dec := msgpack.NewDecoder(bytes.NewReader(data))

	var entries []entry
	for {
		pairLen, err := dec.DecodeArrayLen()
		if err != nil {
			if err == io.EOF {
				break
			}
			return entries, nil
		}
		if pairLen < 2 {
			for j := 0; j < pairLen; j++ {
				dec.DecodeInterface()
			}
			continue
		}
		ts, err := decodeTime(dec)
		if err != nil {
			break
		}
		record, err := decodeRecord(dec)
		if err != nil {
			break
		}
		for j := 2; j < pairLen; j++ {
			dec.DecodeInterface()
		}
		entries = append(entries, entry{ts: ts, record: record})
	}
	return entries, nil
}

// recordToLogMessage converts a Forward protocol event into a LogMessage.
func recordToLogMessage(tag string, ts time.Time, record map[string]interface{}) models.LogMessage {
	content, _ := json.Marshal(record)

	msg := models.LogMessage{
		Content:     string(content),
		JsonContent: json.RawMessage(content),
		IsJson:      true,
		Timestamp:   ts,
		Source:      models.SourceForward,
		Origin:      models.Origin{Name: tag},
		Labels:      extractKubernetesLabels(record),
	}

	// Extract log level if present.
	if level, ok := record["level"]; ok {
		msg.Level = fmt.Sprintf("%v", level)
	}

	return msg
}

// extractKubernetesLabels extracts Kubernetes metadata from the record.
// Fluent Bit typically nests these under a "kubernetes" key as a sub-map.
func extractKubernetesLabels(record map[string]interface{}) map[string]string {
	labels := make(map[string]string)

	k8sRaw, ok := record["kubernetes"]
	if !ok {
		return nil
	}

	k8s := toStringKeyMap(k8sRaw)
	if k8s == nil {
		return nil
	}

	// Map of Fluent Bit kubernetes fields to label keys.
	fieldMapping := map[string]string{
		"pod_name":       "kubernetes.pod_name",
		"namespace_name": "kubernetes.namespace_name",
		"container_name": "kubernetes.container_name",
		"host":           "kubernetes.host",
		"pod_id":         "kubernetes.pod_id",
	}

	for field, labelKey := range fieldMapping {
		if v, exists := k8s[field]; exists {
			labels[labelKey] = fmt.Sprintf("%v", v)
		}
	}

	// Extract labels from the kubernetes.labels sub-map.
	if labelsRaw, exists := k8s["labels"]; exists {
		if k8sLabels := toStringKeyMap(labelsRaw); k8sLabels != nil {
			for k, v := range k8sLabels {
				labels["kubernetes.labels."+k] = fmt.Sprintf("%v", v)
			}
		}
	}

	if len(labels) == 0 {
		return nil
	}

	return labels
}

// emit sends a message to the channel or returns false if the context is done.
func emit(ctx context.Context, ch chan<- models.LogMessage, msg models.LogMessage) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- msg:
		return true
	}
}
