// Package server implements the TCP listener and per-connection handler.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/hiendvt/go-redis/internal/commands"
	"github.com/hiendvt/go-redis/internal/config"
)

// Server wraps a TCP listener and manages the lifecycle of client connections.
type Server struct {
	cfg    *config.Config
	log    *slog.Logger
	router *commands.Router

	// wg tracks active connection handler goroutines so Serve can wait
	// for them to finish on shutdown.
	wg sync.WaitGroup

	// connCount is the number of currently active connections.
	// Accessed atomically — no lock needed for a simple counter.
	connCount atomic.Int64

	// conns tracks every open net.Conn so Serve can close them all when
	// the context is cancelled, unblocking handler goroutines that are
	// blocked in conn.Read().
	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
}

// New creates a Server. It does not start listening yet — call Run or Serve.
func New(cfg *config.Config, log *slog.Logger, router *commands.Router) *Server {
	return &Server{
		cfg:    cfg,
		log:    log,
		router: router,
		conns:  make(map[net.Conn]struct{}),
	}
}

// Run binds the TCP listener and starts the accept loop.
// It is a convenience wrapper around Serve for production use.
// Blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.cfg.Addr())
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve starts the accept loop on an already-bound listener.
// Separating Listen from Serve makes the server testable: tests can call
// net.Listen(":0"), read the OS-assigned address, then call Serve.
//
// Shutdown sequence:
//  1. ctx is cancelled
//  2. The listener is closed — Accept() unblocks with net.ErrClosed
//  3. All active client connections are closed — Read() unblocks with io.EOF
//  4. Handler goroutines return; wg.Wait() completes
//  5. Serve returns
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.log.Info("server listening", "addr", ln.Addr())

	go func() {
		<-ctx.Done()
		s.log.Info("shutdown signal received, closing listener")
		ln.Close()
		// Close all active client connections so handler goroutines unblock
		// from conn.Read() and can return, allowing wg.Wait() to complete.
		s.closeAllConns()
	}()

	s.acceptLoop(ln)
	s.wg.Wait()
	s.log.Info("all connections closed, server stopped")
	return nil
}

// acceptLoop calls Accept in a tight loop, spawning a handler goroutine per
// connection. Returns when the listener is closed.
func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return // normal shutdown
			}
			s.log.Error("accept error", "err", err)
			continue // transient error (e.g. EMFILE) — keep accepting
		}

		// Enforce the max-clients limit if configured.
		if s.cfg.MaxClients > 0 && int(s.connCount.Load()) >= s.cfg.MaxClients {
			s.log.Warn("max clients reached, rejecting connection",
				"remote", conn.RemoteAddr(),
				"max", s.cfg.MaxClients,
			)
			conn.Close()
			continue
		}

		s.trackConn(conn)
		s.connCount.Add(1)
		s.wg.Add(1)

		go func(c net.Conn) {
			defer s.wg.Done()
			defer s.connCount.Add(-1)
			defer s.untrackConn(c)

			s.log.Debug("client connected", "remote", c.RemoteAddr())
			newHandler(c, s.router, s.log).serve()
			s.log.Debug("client disconnected", "remote", c.RemoteAddr())
		}(conn)
	}
}

func (s *Server) trackConn(c net.Conn) {
	s.connsMu.Lock()
	s.conns[c] = struct{}{}
	s.connsMu.Unlock()
}

func (s *Server) untrackConn(c net.Conn) {
	s.connsMu.Lock()
	delete(s.conns, c)
	s.connsMu.Unlock()
}

// closeAllConns closes every tracked connection. Called on shutdown so that
// handler goroutines blocked in conn.Read() receive io.EOF and can return.
func (s *Server) closeAllConns() {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()
	for c := range s.conns {
		c.Close()
	}
}
