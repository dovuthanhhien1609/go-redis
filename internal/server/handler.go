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

	// ── Transaction state ────────────────────────────────────────────────
	// inMulti is true between MULTI and EXEC/DISCARD.
	inMulti bool
	// txQueue holds commands queued during MULTI.
	txQueue [][]string
	// txErrors counts commands queued with syntax errors during MULTI.
	txErrors int

	// ── WATCH state ──────────────────────────────────────────────────────
	// watchedKeys maps key → version at the time of WATCH.
	// If any key's version has changed by EXEC time, EXEC returns nil.
	watchedKeys map[string]uint64
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
		watchedKeys: make(map[string]uint64),
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

		name := strings.ToUpper(args[0])

		switch name {
		case "SUBSCRIBE":
			h.handleSubscribe(args[1:])
		case "UNSUBSCRIBE":
			h.handleUnsubscribe(args[1:])
		case "PSUBSCRIBE":
			h.handlePSubscribe(args[1:])
		case "PUNSUBSCRIBE":
			h.handlePUnsubscribe(args[1:])

		// ── Transaction commands ─────────────────────────────────────────
		case "MULTI":
			if err := h.respond(protocol.Serialize(h.handleMulti())); err != nil {
				h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		case "EXEC":
			if err := h.respondMany(h.handleExec()); err != nil {
				h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		case "DISCARD":
			if err := h.respond(protocol.Serialize(h.handleDiscard())); err != nil {
				h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		case "WATCH":
			if err := h.respond(protocol.Serialize(h.handleWatch(args[1:]))); err != nil {
				h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}
		case "UNWATCH":
			h.watchedKeys = make(map[string]uint64)
			if err := h.respond(protocol.Serialize(protocol.SimpleString("OK"))); err != nil {
				h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
				return
			}

		default:
			// Once in subscription mode, only pub/sub management commands are
			// allowed. This matches Redis 6+ behaviour.
			if h.sub != nil {
				h.respond(protocol.Serialize(protocol.Error( //nolint:errcheck
					"ERR Command not allowed in subscription mode")))
				continue
			}

			// In MULTI mode: queue the command instead of executing it.
			if h.inMulti {
				h.txQueue = append(h.txQueue, args)
				if err := h.respond(protocol.Serialize(protocol.SimpleString("QUEUED"))); err != nil {
					h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
					return
				}
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

// handleMulti processes the MULTI command.
func (h *handler) handleMulti() protocol.Response {
	if h.inMulti {
		return protocol.Error("ERR MULTI calls can not be nested")
	}
	h.inMulti = true
	h.txQueue = h.txQueue[:0]
	h.txErrors = 0
	return protocol.SimpleString("OK")
}

// handleExec processes the EXEC command, executing all queued commands.
// Returns a slice of serialized responses (one per queued command).
func (h *handler) handleExec() []string {
	if !h.inMulti {
		return []string{protocol.Serialize(protocol.Error("ERR EXEC without MULTI"))}
	}

	// Clean up transaction state on exit.
	defer func() {
		h.inMulti = false
		h.txQueue = nil
		h.txErrors = 0
		h.watchedKeys = make(map[string]uint64)
	}()

	// If there were command errors during MULTI, abort.
	if h.txErrors > 0 {
		return []string{protocol.Serialize(protocol.Error("EXECABORT Transaction discarded because of previous errors."))}
	}

	// Check WATCH: if any watched key was modified, return null array.
	if h.watchDirty() {
		return []string{protocol.Serialize(protocol.Response{Type: protocol.TypeNullArray})}
	}

	// Execute all queued commands, collecting responses.
	results := make([]string, len(h.txQueue))
	for i, cmd := range h.txQueue {
		resp := h.router.Dispatch(cmd)
		results[i] = protocol.Serialize(resp)
	}

	// Wrap in array response.
	var sb strings.Builder
	sb.WriteString("*")
	sb.WriteString(itoa(len(results)))
	sb.WriteString("\r\n")
	for _, r := range results {
		sb.WriteString(r)
	}
	return []string{sb.String()}
}

// handleDiscard processes the DISCARD command.
func (h *handler) handleDiscard() protocol.Response {
	if !h.inMulti {
		return protocol.Error("ERR DISCARD without MULTI")
	}
	h.inMulti = false
	h.txQueue = nil
	h.txErrors = 0
	h.watchedKeys = make(map[string]uint64)
	return protocol.SimpleString("OK")
}

// handleWatch processes the WATCH key [key ...] command.
// Saves the current version of each key so EXEC can detect concurrent modifications.
func (h *handler) handleWatch(keys []string) protocol.Response {
	if h.inMulti {
		return protocol.Error("ERR WATCH inside MULTI is not allowed")
	}
	if len(keys) == 0 {
		return protocol.Error("ERR wrong number of arguments for 'watch' command")
	}
	for _, k := range keys {
		h.watchedKeys[k] = h.router.KeyVersion(k)
	}
	return protocol.SimpleString("OK")
}

// watchDirty returns true if any watched key has been modified since WATCH.
func (h *handler) watchDirty() bool {
	for k, ver := range h.watchedKeys {
		if h.router.KeyVersion(k) != ver {
			return true
		}
	}
	return false
}

// itoa converts an int to a string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 20)
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
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
func (h *handler) respond(s string) error {
	if h.sub != nil {
		h.sub.Send(s)
		return nil
	}
	_, err := io.WriteString(h.conn, s)
	return err
}

// respondMany writes multiple pre-serialized RESP strings to the client.
func (h *handler) respondMany(parts []string) error {
	for _, p := range parts {
		if err := h.respond(p); err != nil {
			return err
		}
	}
	return nil
}
