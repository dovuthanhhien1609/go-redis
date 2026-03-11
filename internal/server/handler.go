package server

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"

	"github.com/hiendvt/go-redis/internal/commands"
	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/pubsub"
)

// handler owns the read/parse/dispatch/write loop for a single TCP connection.
// It is created by the server for every accepted connection and runs in its
// own goroutine.
//
// Concurrency model — two write paths, never concurrent:
//
//	Before any SUBSCRIBE / PSUBSCRIBE: handler goroutine writes directly.
//	After first SUBSCRIBE / PSUBSCRIBE: a write loop goroutine is the sole
//	  writer. The handler goroutine queues outbound data via sub.Send().
//
// This invariant means conn.Write is never called from two goroutines at once.
type handler struct {
	conn   net.Conn
	parser *protocol.Parser
	router *commands.Router
	log    *slog.Logger
	broker *pubsub.Broker

	// sub is nil until the client sends its first SUBSCRIBE or PSUBSCRIBE.
	sub *pubsub.Subscriber

	// subChans tracks exact-channel subscriptions for this client.
	// Owned exclusively by the handler goroutine — no mutex needed.
	subChans map[string]struct{}

	// subPatterns tracks pattern subscriptions for this client.
	subPatterns map[string]struct{}
}

// newHandler constructs a handler for the given connection.
func newHandler(conn net.Conn, router *commands.Router, broker *pubsub.Broker, log *slog.Logger) *handler {
	return &handler{
		conn:        conn,
		parser:      protocol.NewParser(conn),
		router:      router,
		log:         log,
		broker:      broker,
		subChans:    make(map[string]struct{}),
		subPatterns: make(map[string]struct{}),
	}
}

// serve runs the command loop until the client disconnects or a fatal error
// occurs. It always closes the connection and cleans up subscriptions before
// returning.
func (h *handler) serve() {
	defer h.conn.Close()
	defer h.cleanupSubscriptions()

	for {
		args, err := h.parser.ReadCommand()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return // clean disconnect
			}
			h.log.Debug("read error", "remote", h.conn.RemoteAddr(), "err", err)
			return
		}

		switch strings.ToUpper(args[0]) {
		case "SUBSCRIBE":
			h.handleSubscribe(args[1:])
		case "UNSUBSCRIBE":
			h.handleUnsubscribe(args[1:])
		case "PSUBSCRIBE":
			h.handlePSubscribe(args[1:])
		case "PUNSUBSCRIBE":
			h.handlePUnsubscribe(args[1:])
		default:
			// Once in subscription mode, only pub/sub management commands are
			// allowed. This matches Redis 6+ behaviour.
			if h.sub != nil {
				h.respond(protocol.Serialize(protocol.Error( //nolint:errcheck
					"ERR Command not allowed in subscription mode")))
				continue
			}
			resp := h.router.Dispatch(args)
			if err := h.respond(protocol.Serialize(resp)); err != nil {
				h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		}
	}
}

// totalSubscriptions returns the total number of active exact-channel and
// pattern subscriptions for this connection. Used in ACK counts.
func (h *handler) totalSubscriptions() int {
	return len(h.subChans) + len(h.subPatterns)
}

// ensureSubscriber creates the Subscriber and starts the write loop goroutine
// on the first subscription of any kind. Subsequent calls are no-ops.
func (h *handler) ensureSubscriber() {
	if h.sub == nil {
		h.sub = pubsub.NewSubscriber()
		go h.writeLoop()
	}
}

// handleSubscribe processes a SUBSCRIBE command.
func (h *handler) handleSubscribe(channels []string) {
	if h.broker == nil {
		h.respond(protocol.Serialize(protocol.Error("ERR pub/sub not available"))) //nolint:errcheck
		return
	}
	if len(channels) == 0 {
		h.respond(protocol.Serialize(protocol.Error( //nolint:errcheck
			"ERR wrong number of arguments for 'subscribe' command")))
		return
	}

	h.ensureSubscriber()

	for _, ch := range channels {
		if _, already := h.subChans[ch]; already {
			continue
		}
		h.broker.Subscribe(ch, h.sub)
		h.subChans[ch] = struct{}{}
		h.sub.Send(pubsub.EncodeSubscribeAck(ch, h.totalSubscriptions()))
	}
}

// handleUnsubscribe processes an UNSUBSCRIBE command.
// With no channel arguments it unsubscribes from every exact channel.
func (h *handler) handleUnsubscribe(channels []string) {
	if h.sub == nil {
		return
	}

	if len(channels) == 0 {
		for ch := range h.subChans {
			h.broker.Unsubscribe(ch, h.sub)
			delete(h.subChans, ch)
			h.sub.Send(pubsub.EncodeUnsubscribeAck(ch, h.totalSubscriptions()))
		}
		return
	}

	for _, ch := range channels {
		if _, ok := h.subChans[ch]; !ok {
			continue
		}
		h.broker.Unsubscribe(ch, h.sub)
		delete(h.subChans, ch)
		h.sub.Send(pubsub.EncodeUnsubscribeAck(ch, h.totalSubscriptions()))
	}
}

// handlePSubscribe processes a PSUBSCRIBE command.
func (h *handler) handlePSubscribe(patterns []string) {
	if h.broker == nil {
		h.respond(protocol.Serialize(protocol.Error("ERR pub/sub not available"))) //nolint:errcheck
		return
	}
	if len(patterns) == 0 {
		h.respond(protocol.Serialize(protocol.Error( //nolint:errcheck
			"ERR wrong number of arguments for 'psubscribe' command")))
		return
	}

	h.ensureSubscriber()

	for _, pat := range patterns {
		if _, already := h.subPatterns[pat]; already {
			continue
		}
		h.broker.PSubscribe(pat, h.sub)
		h.subPatterns[pat] = struct{}{}
		h.sub.Send(pubsub.EncodePSubscribeAck(pat, h.totalSubscriptions()))
	}
}

// handlePUnsubscribe processes a PUNSUBSCRIBE command.
// With no pattern arguments it unsubscribes from every pattern.
func (h *handler) handlePUnsubscribe(patterns []string) {
	if h.sub == nil {
		return
	}

	if len(patterns) == 0 {
		for pat := range h.subPatterns {
			h.broker.PUnsubscribe(pat, h.sub)
			delete(h.subPatterns, pat)
			h.sub.Send(pubsub.EncodePUnsubscribeAck(pat, h.totalSubscriptions()))
		}
		return
	}

	for _, pat := range patterns {
		if _, ok := h.subPatterns[pat]; !ok {
			continue
		}
		h.broker.PUnsubscribe(pat, h.sub)
		delete(h.subPatterns, pat)
		h.sub.Send(pubsub.EncodePUnsubscribeAck(pat, h.totalSubscriptions()))
	}
}

// writeLoop reads pre-serialized RESP strings from the subscriber inbox and
// writes them to the TCP connection. It is the sole writer to conn once a
// client has subscribed, ensuring no concurrent writes.
//
// The loop exits when sub.Done() is closed (client disconnect) or when a
// write to the connection fails. On shutdown it drains any messages already
// queued in the inbox so subscription ACKs are not silently lost.
func (h *handler) writeLoop() {
	for {
		select {
		case msg := <-h.sub.Inbox():
			if _, err := io.WriteString(h.conn, msg); err != nil {
				h.log.Debug("subscriber write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		case <-h.sub.Done():
			// Drain any messages queued before Close() was called.
			for {
				select {
				case msg := <-h.sub.Inbox():
					io.WriteString(h.conn, msg) //nolint:errcheck — connection is closing
				default:
					return
				}
			}
		}
	}
}

// cleanupSubscriptions removes the client from all broker channels and
// patterns, then closes the subscriber, which signals the write loop to exit.
// Called via defer in serve() so it always runs on disconnect.
func (h *handler) cleanupSubscriptions() {
	if h.sub == nil {
		return
	}
	if h.broker != nil {
		h.broker.UnsubscribeAll(h.sub)
		h.broker.PUnsubscribeAll(h.sub)
	}
	h.sub.Close()
}

// respond writes a pre-serialized RESP string to the client.
//
// Before first SUBSCRIBE/PSUBSCRIBE: writes directly to conn (handler is sole writer).
// After first SUBSCRIBE/PSUBSCRIBE:  queues in inbox for the write loop.
// Returns nil in this case — the send is fire-and-forget.
func (h *handler) respond(s string) error {
	if h.sub != nil {
		h.sub.Send(s)
		return nil
	}
	_, err := io.WriteString(h.conn, s)
	return err
}
