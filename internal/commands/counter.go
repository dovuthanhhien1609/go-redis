package commands

import (
	"fmt"
	"strconv"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// incrBy fetches the current integer value for key, adds delta, stores it,
// and returns the new value. If the key does not exist it starts from 0.
// Returns an error response if the stored value is not a valid integer.
func incrBy(key string, delta int64, store storage.Store) protocol.Response {
	var current int64
	if raw, ok := store.Get(key); ok {
		var err error
		current, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return protocol.Error("ERR value is not an integer or out of range")
		}
	}

	// Check for overflow.
	next := current + delta
	if delta > 0 && next < current {
		return protocol.Error("ERR increment would overflow")
	}
	if delta < 0 && next > current {
		return protocol.Error("ERR decrement would overflow")
	}

	store.Set(key, fmt.Sprintf("%d", next))
	return protocol.Integer(next)
}

// handleIncr implements INCR key.
func handleIncr(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'INCR' command")
	}
	return incrBy(args[1], 1, store)
}

// handleIncrBy implements INCRBY key increment.
func handleIncrBy(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'INCRBY' command")
	}
	delta, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	return incrBy(args[1], delta, store)
}

// handleDecr implements DECR key.
func handleDecr(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'DECR' command")
	}
	return incrBy(args[1], -1, store)
}

// handleDecrBy implements DECRBY key decrement.
func handleDecrBy(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'DECRBY' command")
	}
	delta, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	return incrBy(args[1], -delta, store)
}
