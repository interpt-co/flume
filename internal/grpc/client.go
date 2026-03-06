package flumegrpc

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client streams log batches from a collector to the aggregator for a
// single (node, pattern) pair.
type Client struct {
	addr        string
	nodeName    string
	patternName string
	batchSize   int
	batchDelay  time.Duration
	sequence    uint64
}

// NewClient creates a new gRPC streaming client.
func NewClient(addr, nodeName, patternName string) *Client {
	return &Client{
		addr:        addr,
		nodeName:    nodeName,
		patternName: patternName,
		batchSize:   100,
		batchDelay:  200 * time.Millisecond,
	}
}

// Run reads messages from ch and streams them to the aggregator.
// It reconnects with exponential backoff on failure. Blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context, ch <-chan LogEntry) {
	backoff := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := c.stream(ctx, ch)
		if ctx.Err() != nil {
			return
		}

		log.WithError(err).WithFields(log.Fields{
			"addr":    c.addr,
			"pattern": c.patternName,
			"backoff": backoff,
		}).Warn("gRPC client: stream ended, reconnecting")

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, 30*time.Second)
	}
}

func (c *Client) stream(ctx context.Context, ch <-chan LogEntry) error {
	conn, err := grpc.NewClient(c.addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{
		StreamName:    "StreamLogs",
		ServerStreams:  true,
		ClientStreams:  true,
	}, "/flume.v1.CollectorService/StreamLogs")
	if err != nil {
		return err
	}

	// Send handshake.
	hs := envelope{
		Type: "handshake",
		Data: mustJSON(Handshake{
			NodeName:    c.nodeName,
			PatternName: c.patternName,
		}),
	}
	hsBytes, _ := json.Marshal(hs)
	hsMsg := json.RawMessage(hsBytes)
	if err := stream.SendMsg(&hsMsg); err != nil {
		return err
	}

	// Start ack reader goroutine.
	go func() {
		for {
			var ack json.RawMessage
			if err := stream.RecvMsg(&ack); err != nil {
				return
			}
		}
	}()

	// Batch and send log entries.
	ticker := time.NewTicker(c.batchDelay)
	defer ticker.Stop()

	var batch []LogEntry

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				c.sendBatch(stream, batch)
			}
			stream.CloseSend()
			return ctx.Err()

		case entry, ok := <-ch:
			if !ok {
				if len(batch) > 0 {
					c.sendBatch(stream, batch)
				}
				stream.CloseSend()
				return nil
			}
			batch = append(batch, entry)
			if len(batch) >= c.batchSize {
				if err := c.sendBatch(stream, batch); err != nil {
					return err
				}
				batch = nil
			}

		case <-ticker.C:
			if len(batch) > 0 {
				if err := c.sendBatch(stream, batch); err != nil {
					return err
				}
				batch = nil
			}
		}
	}
}

func (c *Client) sendBatch(stream grpc.ClientStream, entries []LogEntry) error {
	seq := atomic.AddUint64(&c.sequence, 1)
	env := envelope{
		Type: "log_batch",
		Data: mustJSON(LogBatch{
			Entries:  entries,
			Sequence: seq,
		}),
	}
	data, _ := json.Marshal(env)
	msg := json.RawMessage(data)
	return stream.SendMsg(&msg)
}
