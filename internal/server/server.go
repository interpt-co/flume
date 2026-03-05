package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server is the HTTP/WebSocket server for flume.
type Server struct {
	httpServer *http.Server
	manager    *ClientManager
}

// ServerOption is a functional option for NewServer.
type ServerOption func(*serverOptions)

type serverOptions struct {
	storage Storage
}

// WithStorage attaches a Storage backend for the /api/history endpoint.
func WithStorage(s Storage) ServerOption {
	return func(o *serverOptions) {
		o.storage = s
	}
}

// NewServer creates a new Server bound to the given host and port.
func NewServer(host string, port int, manager *ClientManager, opts ...ServerOption) *Server {
	var so serverOptions
	for _, o := range opts {
		o(&so)
	}

	mux := http.NewServeMux()

	mux.Handle("GET /", FrontendHandler())

	mux.HandleFunc("GET /ws", manager.HandleWS)
	mux.HandleFunc("GET /api/status", manager.HandleStatus)
	mux.HandleFunc("GET /api/client/load", manager.HandleLoadRange)
	mux.HandleFunc("GET /api/labels", manager.HandleLabels)

	if so.storage != nil {
		h := &HistoryHandler{storage: so.storage}
		mux.HandleFunc("GET /api/history", h.HandleHistory)
	}

	return &Server{
		httpServer: &http.Server{
			Addr:    net.JoinHostPort(host, fmt.Sprintf("%d", port)),
			Handler: mux,
		},
		manager: manager,
	}
}

// Handler returns the HTTP handler (mux) for use in tests.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// Start runs the HTTP server and blocks until ctx is cancelled, at which
// point it initiates a graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
