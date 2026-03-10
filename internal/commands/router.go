// Package commands implements the command registry and dispatch logic.
// Each command is a pure function: (args []string, store storage.Store) → protocol.Response.
package commands

import (
	"strings"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// HandlerFunc is the signature every command handler must implement.
// args[0] is always the command name (upper-cased before dispatch).
type HandlerFunc func(args []string, store storage.Store) protocol.Response

// Appender is satisfied by *persistence.AOF (and any mock in tests).
// Defined here to avoid an import cycle: commands must not import persistence.
type Appender interface {
	Append(args []string) error
}

// mutatingCommands is the set of commands that change state and must be
// written to the AOF. Read-only commands are intentionally excluded.
var mutatingCommands = map[string]bool{
	"SET": true,
	"DEL": true,
}

// Router holds the command registry, the shared storage backend, and an
// optional AOF appender for persistence.
// It is safe for concurrent use — Dispatch is read-only on the registry map,
// and the store handles its own synchronization.
type Router struct {
	store    storage.Store
	aof      Appender // nil when AOF is disabled
	handlers map[string]HandlerFunc
}

// NewRouter creates a Router with the given store and registers all built-in
// command handlers. Pass a non-nil Appender to enable AOF persistence.
func NewRouter(store storage.Store, aof Appender) *Router {
	r := &Router{
		store:    store,
		aof:      aof,
		handlers: make(map[string]HandlerFunc),
	}
	r.register()
	return r
}

// register adds all built-in commands to the registry.
// Adding a new command is a single line here plus a handler function.
func (r *Router) register() {
	r.handlers["PING"] = handlePing
	r.handlers["SET"] = handleSet
	r.handlers["GET"] = handleGet
	r.handlers["DEL"] = handleDel
	r.handlers["EXISTS"] = handleExists
	r.handlers["KEYS"] = handleKeys
	r.handlers["COMMAND"] = handleCommand
}

// Dispatch normalizes the command name, looks it up in the registry, calls
// the handler, and — for mutating commands — appends the command to the AOF.
//
// AOF append happens after the command succeeds. If the append fails we log
// it but still return the successful response to the client — a failed append
// is a persistence concern, not a correctness concern for the current request.
func (r *Router) Dispatch(args []string) protocol.Response {
	if len(args) == 0 {
		return protocol.Error("ERR empty command")
	}

	name := strings.ToUpper(args[0])
	fn, ok := r.handlers[name]
	if !ok {
		return protocol.Error("ERR unknown command '" + args[0] + "'")
	}

	resp := fn(args, r.store)

	// Persist mutating commands to the AOF after successful execution.
	// We only append when:
	//   1. AOF is configured (r.aof != nil)
	//   2. The command is in the mutating set
	//   3. The response is not an error (failed commands must not be replayed)
	if r.aof != nil && mutatingCommands[name] && resp.Type != protocol.TypeError {
		// AOF errors are non-fatal: the in-memory state is already updated.
		// A real production system would log this and potentially alert.
		_ = r.aof.Append(args)
	}

	return resp
}
