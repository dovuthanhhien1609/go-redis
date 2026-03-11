package commands

import (
	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// Publisher is satisfied by *pubsub.Broker (and any mock in tests).
// Defined as a local interface — same pattern used for Appender — so the
// commands package does not import pubsub and avoids a circular dependency.
type Publisher interface {
	Publish(channel, message string) int
}

// makePublishHandler returns a HandlerFunc that routes a message through the
// pub/sub broker and returns the integer count of subscribers that received it.
// When pub is nil (pub/sub not wired up), it always returns 0.
func makePublishHandler(pub Publisher) HandlerFunc {
	return func(args []string, _ storage.Store) protocol.Response {
		if len(args) != 3 {
			return protocol.Error("ERR wrong number of arguments for 'publish' command")
		}
		if pub == nil {
			return protocol.Integer(0)
		}
		count := pub.Publish(args[1], args[2])
		return protocol.Integer(int64(count))
	}
}
