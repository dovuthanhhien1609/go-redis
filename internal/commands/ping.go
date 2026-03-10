package commands

import (
	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handlePing implements PING [message].
// With no argument it returns +PONG.
// With one argument it echoes the argument as a bulk string.
func handlePing(args []string, _ storage.Store) protocol.Response {
	if len(args) == 1 {
		return protocol.SimpleString("PONG")
	}
	if len(args) == 2 {
		return protocol.BulkString(args[1])
	}
	return protocol.Error("ERR wrong number of arguments for 'PING'")
}
