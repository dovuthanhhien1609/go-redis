package commands

import (
	"path/filepath"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleSet implements SET key value.
// Phase 1: no options (EX, PX, NX, XX). Returns +OK on success.
func handleSet(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'SET'")
	}
	store.Set(args[1], args[2])
	return protocol.SimpleString("OK")
}

// handleGet implements GET key.
// Returns the bulk string value, or $-1 (null bulk string) if the key does
// not exist.
func handleGet(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'GET'")
	}
	val, ok := store.Get(args[1])
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(val)
}

// handleDel implements DEL key [key ...].
// Returns the integer count of keys that were actually deleted.
func handleDel(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'DEL'")
	}
	n := store.Del(args[1:]...)
	return protocol.Integer(int64(n))
}

// handleExists implements EXISTS key [key ...].
// Returns the integer count of keys that exist. A repeated key counts each time.
func handleExists(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'EXISTS'")
	}
	n := store.Exists(args[1:]...)
	return protocol.Integer(int64(n))
}

// handleKeys implements KEYS pattern.
// Phase 1 supports only "*" (all keys). Full glob matching via filepath.Match
// is included for completeness.
func handleKeys(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'KEYS'")
	}

	// Validate the pattern is a legal glob before querying the store.
	// filepath.Match returns an error only on malformed patterns.
	if _, err := filepath.Match(args[1], ""); err != nil {
		return protocol.Error("ERR invalid pattern")
	}

	keys := store.Keys(args[1])
	elems := make([]protocol.Response, len(keys))
	for i, k := range keys {
		elems[i] = protocol.BulkString(k)
	}
	return protocol.Array(elems)
}
