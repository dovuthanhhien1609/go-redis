package commands

import (
	"strconv"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleExpire implements EXPIRE key seconds.
// Sets a TTL on key. Returns :1 if the TTL was set, :0 if key does not exist.
func handleExpire(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'EXPIRE' command")
	}
	secs, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || secs < 0 {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	if store.Expire(args[1], time.Duration(secs)*time.Second) {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}

// handlePExpire implements PEXPIRE key milliseconds.
// Same as EXPIRE but accepts milliseconds.
func handlePExpire(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'PEXPIRE' command")
	}
	ms, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || ms < 0 {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	if store.Expire(args[1], time.Duration(ms)*time.Millisecond) {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}

// handleTTL implements TTL key.
// Returns the TTL in seconds: -1 (no TTL), -2 (not found), or N (seconds).
func handleTTL(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'TTL' command")
	}
	d := store.TTL(args[1])
	switch d {
	case -1 * time.Second:
		return protocol.Integer(-1)
	case -2 * time.Second:
		return protocol.Integer(-2)
	default:
		secs := int64(d.Seconds())
		if secs < 1 {
			secs = 1 // round up partial seconds so client sees > 0
		}
		return protocol.Integer(secs)
	}
}

// handlePTTL implements PTTL key.
// Returns the TTL in milliseconds: -1 (no TTL), -2 (not found), or N (ms).
func handlePTTL(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'PTTL' command")
	}
	d := store.TTL(args[1])
	switch d {
	case -1 * time.Second:
		return protocol.Integer(-1)
	case -2 * time.Second:
		return protocol.Integer(-2)
	default:
		ms := d.Milliseconds()
		if ms < 1 {
			ms = 1
		}
		return protocol.Integer(ms)
	}
}

// handleExpireAt implements EXPIREAT key unix-time-seconds.
func handleExpireAt(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'EXPIREAT' command")
	}
	unixSec, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	remaining := time.Until(time.Unix(unixSec, 0))
	if remaining <= 0 {
		// Expire immediately.
		store.Del(args[1])
		return protocol.Integer(1)
	}
	if store.Expire(args[1], remaining) {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}

// handlePExpireAt implements PEXPIREAT key unix-time-milliseconds.
func handlePExpireAt(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'PEXPIREAT' command")
	}
	unixMs, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	remaining := time.Until(time.UnixMilli(unixMs))
	if remaining <= 0 {
		store.Del(args[1])
		return protocol.Integer(1)
	}
	if store.Expire(args[1], remaining) {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}

// handlePersist implements PERSIST key.
// Removes the TTL from key. Returns :1 if removed, :0 if key had no TTL.
func handlePersist(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'PERSIST' command")
	}
	if store.Persist(args[1]) {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}
