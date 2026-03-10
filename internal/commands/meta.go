package commands

import (
	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleCommand implements COMMAND and COMMAND COUNT.
//
// redis-cli sends "COMMAND" on every new connection to discover command
// metadata. We return an empty array — sufficient to satisfy redis-cli
// without implementing the full introspection protocol.
func handleCommand(args []string, _ storage.Store) protocol.Response {
	return protocol.Array([]protocol.Response{})
}
