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
//	Before any SUBSCRIBE:  handler goroutine writes directly to conn.
//	After first SUBSCRIBE: a write loop goroutine is the sole writer to conn.
//	                       The handler goroutine queues via sub.Send().
//
// This invariant means conn.Write is never called from two goroutines at once.
type handler struct {
	conn     net.Conn
	parser   *protocol.Parser
	router   *commands.Router
	log      *slog.Logger
	broker   *pubsub.Broker

	// sub is nil until the client sends its first SUBSCRIBE command.
	sub      *pubsub.Subscriber
	// subChans tracks which channels this client is subscribed to.
	// Owned exclusively by the handler goroutine — no mutex needed.
	subChans map[string]struct{}
}

// newHandler constructs a handler for the given connection.
func newHandler(conn net.Conn, router *commands.Router, broker *pubsub.Broker, log *slog.Logger) *handler {
	return &handler{
		conn:     conn,
		parser:   protocol.NewParser(conn),
		router:   router,
		log:      log,
		broker:   broker,
		subChans: make(map[string]struct{}),
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
			// Connection is broken; skip the write attempt and let defers run.
			return
		}

		switch strings.ToUpper(args[0]) {
		case "SUBSCRIBE":
			h.handleSubscribe(args[1:])
		case "UNSUBSCRIBE":
			h.handleUnsubscribe(args[1:])
		default:
			// Once in subscription mode, Redis only allows pub/sub commands.
			if h.sub != nil {
				h.respond(protocol.Serialize(protocol.Error(
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

// handleSubscribe processes a SUBSCRIBE command.
// On the first call it creates the Subscriber and starts the write loop.
// For each channel it registers with the broker and sends a confirmation ACK.
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

	// First SUBSCRIBE on this connection: create the subscriber and start
	// the write loop goroutine that will be the sole writer to conn.
	if h.sub == nil {
		h.sub = pubsub.NewSubscriber()
		go h.writeLoop()
	}

	for _, ch := range channels {
		if _, already := h.subChans[ch]; already {
			continue // idempotent: already subscribed to this channel
		}
		h.broker.Subscribe(ch, h.sub)
		h.subChans[ch] = struct{}{}
		// ACK goes through the inbox so the write loop delivers it in order.
		h.sub.Send(pubsub.EncodeSubscribeAck(ch, len(h.subChans)))
	}
}

// handleUnsubscribe processes an UNSUBSCRIBE command.
// With no channel arguments it unsubscribes from every active channel.
func (h *handler) handleUnsubscribe(channels []string) {
	if h.sub == nil {
		// Client is not subscribed to anything; nothing to do.
		return
	}

	// UNSUBSCRIBE with no args means "unsubscribe from all".
	if len(channels) == 0 {
		for ch := range h.subChans {
			h.broker.Unsubscribe(ch, h.sub)
			delete(h.subChans, ch)
			h.sub.Send(pubsub.EncodeUnsubscribeAck(ch, len(h.subChans)))
		}
		return
	}

	for _, ch := range channels {
		if _, ok := h.subChans[ch]; !ok {
			continue // not subscribed to this channel; skip
		}
		h.broker.Unsubscribe(ch, h.sub)
		delete(h.subChans, ch)
		h.sub.Send(pubsub.EncodeUnsubscribeAck(ch, len(h.subChans)))
	}
}

// writeLoop reads pre-serialized RESP strings from the subscriber inbox and
// writes them to the TCP connection. It is the sole writer to conn once a
// client has subscribed, ensuring no concurrent writes.
//
// The loop exits when sub.Done() is closed (client disconnect) or when a
// write to the connection fails. On shutdown it drains any messages already
// queued in the inbox so subscription ACKs are not silently lost.
//
// Note: the inbox channel is never closed (see subscriber.go for the reason).
// We rely on Done() to signal exit instead of ranging over Inbox().
func (h *handler) writeLoop() {
	for {
		select {
		case msg := <-h.sub.Inbox():
			if _, err := io.WriteString(h.conn, msg); err != nil {
				h.log.Debug("subscriber write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		case <-h.sub.Done():
			// Drain any messages that were queued before Close() was called.
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

// cleanupSubscriptions removes the client from all broker channels and closes
// the subscriber inbox, which signals the write loop goroutine to exit.
// Called via defer in serve() so it always runs on disconnect.
func (h *handler) cleanupSubscriptions() {
	if h.sub == nil {
		return
	}
	if h.broker != nil {
		h.broker.UnsubscribeAll(h.sub)
	}
	h.sub.Close() // signals writeLoop to drain and exit
}

// respond writes a pre-serialized RESP string to the client.
//
// Before first SUBSCRIBE: writes directly to conn (handler is sole writer).
// After first SUBSCRIBE:  queues in inbox for the write loop (which is the
//
//	sole writer). Returns nil in this case — the send is fire-and-forget.
func (h *handler) respond(s string) error {
	if h.sub != nil {
		h.sub.Send(s)
		return nil
	}
	_, err := io.WriteString(h.conn, s)
	return err
}
