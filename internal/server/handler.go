package server

import (
	"errors"
	"io"
	"log/slog"
	"net"

	"github.com/hiendvt/go-redis/internal/commands"
	"github.com/hiendvt/go-redis/internal/protocol"
)

// handler owns the read/parse/dispatch/write loop for a single TCP connection.
// It is created by the server for every accepted connection and runs in its
// own goroutine.
type handler struct {
	conn   net.Conn
	parser *protocol.Parser
	router *commands.Router
	log    *slog.Logger
}

// newHandler constructs a handler for the given connection.
func newHandler(conn net.Conn, router *commands.Router, log *slog.Logger) *handler {
	return &handler{
		conn:   conn,
		parser: protocol.NewParser(conn),
		router: router,
		log:    log,
	}
}

// serve runs the command loop until the client disconnects or a fatal error
// occurs. It always closes the connection before returning.
//
// Loop:
//
//  1. Read one RESP command from the socket
//  2. Dispatch to the command router
//  3. Serialize the response
//  4. Write back to the socket
//  5. Repeat
func (h *handler) serve() {
	defer h.conn.Close()

	for {
		// --- 1. Read and parse one RESP command ----------------------------
		args, err := h.parser.ReadCommand()
		if err != nil {
			if errors.Is(err, io.EOF) {
				// Clean disconnect — not an error.
				return
			}
			// Protocol error or broken connection — send an error response
			// if the connection is still writable, then close.
			h.log.Debug("read error", "remote", h.conn.RemoteAddr(), "err", err)
			h.writeResponse(protocol.Error("ERR " + err.Error()))
			return
		}

		// --- 2. Dispatch to the command router ----------------------------
		resp := h.router.Dispatch(args)

		// --- 3 & 4. Serialize and write the response ----------------------
		if err := h.writeResponse(resp); err != nil {
			h.log.Debug("write error", "remote", h.conn.RemoteAddr(), "err", err)
			return
		}
	}
}

// writeResponse serializes resp and writes it to the connection.
func (h *handler) writeResponse(resp protocol.Response) error {
	_, err := io.WriteString(h.conn, protocol.Serialize(resp))
	return err
}
