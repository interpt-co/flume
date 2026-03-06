package flumegrpc

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"time"
)

// Ingester is called by the server when log messages arrive for a pattern.
type Ingester interface {
	Ingest(patternName string, msgs []LogEntry)
}

// Server accepts gRPC streams from collectors.
type Server struct {
	grpcServer *grpc.Server
	tracker    *Tracker
	ingester   Ingester
}

// NewServer creates a new gRPC server for receiving collector streams.
func NewServer(ingester Ingester, tracker *Tracker) *Server {
	s := &Server{
		tracker:  tracker,
		ingester: ingester,
	}

	gs := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	// Register our custom stream handler.
	gs.RegisterService(&collectorServiceDesc, s)
	s.grpcServer = gs
	return s
}

// Serve starts the gRPC server on the given address.
func (s *Server) Serve(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", addr, err)
	}
	log.WithField("addr", addr).Info("gRPC server starting")
	return s.grpcServer.Serve(lis)
}

// GracefulStop gracefully shuts down the server.
func (s *Server) GracefulStop() {
	s.grpcServer.GracefulStop()
}

// StreamLogs handles a bidirectional stream from a collector.
func (s *Server) StreamLogs(stream grpc.BidiStreamingServer[json.RawMessage, json.RawMessage]) error {
	// First message must be a handshake.
	raw, err := stream.Recv()
	if err != nil {
		return err
	}

	var env envelope
	if err := json.Unmarshal(*raw, &env); err != nil {
		return fmt.Errorf("invalid handshake: %w", err)
	}
	if env.Type != "handshake" {
		return fmt.Errorf("expected handshake, got %q", env.Type)
	}

	var hs Handshake
	if err := json.Unmarshal(env.Data, &hs); err != nil {
		return fmt.Errorf("invalid handshake data: %w", err)
	}

	log.WithFields(log.Fields{
		"node":    hs.NodeName,
		"pattern": hs.PatternName,
	}).Info("gRPC: collector connected")

	s.tracker.Add(hs.PatternName, hs.NodeName)
	defer s.tracker.Remove(hs.PatternName, hs.NodeName)

	for {
		raw, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		var env envelope
		if err := json.Unmarshal(*raw, &env); err != nil {
			log.WithError(err).Warn("gRPC: invalid message")
			continue
		}

		switch env.Type {
		case "log_batch":
			var batch LogBatch
			if err := json.Unmarshal(env.Data, &batch); err != nil {
				log.WithError(err).Warn("gRPC: invalid log_batch")
				continue
			}
			s.ingester.Ingest(hs.PatternName, batch.Entries)

			// Send ack.
			ackData, _ := json.Marshal(envelope{
				Type: "ack",
				Data: mustJSON(Ack{Sequence: batch.Sequence}),
			})
			msg := json.RawMessage(ackData)
			if err := stream.Send(&msg); err != nil {
				return err
			}

		case "heartbeat":
			// Keep connection alive, no action needed.
		}
	}
}

// envelope wraps typed messages over the gRPC JSON stream.
type envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// collectorServiceDesc is a minimal gRPC service descriptor for our
// JSON-encoded bidirectional streaming RPC.
var collectorServiceDesc = grpc.ServiceDesc{
	ServiceName: "flume.v1.CollectorService",
	HandlerType: (*interface{})(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "StreamLogs",
			Handler:       streamLogsHandler,
			ServerStreams:  true,
			ClientStreams:  true,
		},
	},
	Metadata: "api/proto/flume/v1/collector.proto",
}

func streamLogsHandler(srv interface{}, stream grpc.ServerStream) error {
	s := srv.(*Server)
	return s.StreamLogs(&bidiStream{stream})
}

// bidiStream adapts grpc.ServerStream to grpc.BidiStreamingServer[json.RawMessage, json.RawMessage].
type bidiStream struct {
	grpc.ServerStream
}

func (b *bidiStream) Send(msg *json.RawMessage) error {
	return b.ServerStream.SendMsg(msg)
}

func (b *bidiStream) Recv() (*json.RawMessage, error) {
	msg := new(json.RawMessage)
	if err := b.ServerStream.RecvMsg(msg); err != nil {
		return nil, err
	}
	return msg, nil
}
