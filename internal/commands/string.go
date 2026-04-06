package commands

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleSet implements SET key value [NX|XX] [GET]
// [EX seconds | PX milliseconds | EXAT unix-time-seconds | PXAT unix-time-milliseconds | KEEPTTL].
func handleSet(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SET' command")
	}
	key, value := args[1], args[2]
	var opts storage.SetAdvOpts
	var ttl time.Duration

	for i := 3; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "NX":
			opts.NX = true
		case "XX":
			opts.XX = true
		case "GET":
			opts.Get = true
		case "KEEPTTL":
			opts.KeepTTL = true
		case "EX":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			secs, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || secs <= 0 {
				return protocol.Error("ERR invalid expire time in 'SET' command")
			}
			ttl = time.Duration(secs) * time.Second
		case "PX":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			ms, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || ms <= 0 {
				return protocol.Error("ERR invalid expire time in 'SET' command")
			}
			ttl = time.Duration(ms) * time.Millisecond
		case "EXAT":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			unixSec, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || unixSec <= 0 {
				return protocol.Error("ERR invalid expire time in 'SET' command")
			}
			ttl = time.Until(time.Unix(unixSec, 0))
			if ttl <= 0 {
				ttl = 1 // immediate but non-zero to trigger expiry path
			}
		case "PXAT":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			unixMs, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || unixMs <= 0 {
				return protocol.Error("ERR invalid expire time in 'SET' command")
			}
			ttl = time.Until(time.UnixMilli(unixMs))
			if ttl <= 0 {
				ttl = 1
			}
		default:
			return protocol.Error("ERR syntax error")
		}
	}

	if opts.NX && opts.XX {
		return protocol.Error("ERR syntax error")
	}

	oldVal, oldExists, wasSet := store.SetAdv(key, value, ttl, opts)

	if opts.Get {
		if !wasSet && opts.NX {
			// NX+GET: return current value even though we didn't set
		}
		if !oldExists {
			return protocol.NullBulkString()
		}
		return protocol.BulkString(oldVal)
	}

	if !wasSet {
		return protocol.NullBulkString()
	}
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
// Full glob matching via filepath.Match.
func handleKeys(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'KEYS'")
	}

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

// handleSetNX implements SETNX key value.
// Sets key only if it does not already exist. Returns :1 if set, :0 if not.
func handleSetNX(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'SETNX' command")
	}
	if _, exists := store.Get(args[1]); exists {
		return protocol.Integer(0)
	}
	store.Set(args[1], args[2])
	return protocol.Integer(1)
}

// handleSetEX implements SETEX key seconds value.
// Sets key to value with a TTL in seconds.
func handleSetEX(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'SETEX' command")
	}
	secs, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || secs <= 0 {
		return protocol.Error("ERR invalid expire time in 'SETEX' command")
	}
	store.SetWithTTL(args[1], args[3], time.Duration(secs)*time.Second)
	return protocol.SimpleString("OK")
}

// handlePSetEX implements PSETEX key milliseconds value.
// Sets key to value with a TTL in milliseconds.
func handlePSetEX(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'PSETEX' command")
	}
	ms, err := strconv.ParseInt(args[2], 10, 64)
	if err != nil || ms <= 0 {
		return protocol.Error("ERR invalid expire time in 'PSETEX' command")
	}
	store.SetWithTTL(args[1], args[3], time.Duration(ms)*time.Millisecond)
	return protocol.SimpleString("OK")
}

// handleMSet implements MSET key value [key value ...].
// Atomically sets multiple keys. Always returns +OK.
func handleMSet(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 || len(args)%2 == 0 {
		return protocol.Error("ERR wrong number of arguments for 'MSET' command")
	}
	for i := 1; i+1 < len(args); i += 2 {
		store.Set(args[i], args[i+1])
	}
	return protocol.SimpleString("OK")
}

// handleMGet implements MGET key [key ...].
// Returns an array of values; missing keys return null bulk string.
func handleMGet(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'MGET' command")
	}
	elems := make([]protocol.Response, len(args)-1)
	for i, key := range args[1:] {
		val, ok := store.Get(key)
		if ok {
			elems[i] = protocol.BulkString(val)
		} else {
			elems[i] = protocol.NullBulkString()
		}
	}
	return protocol.Array(elems)
}

// handleGetSet implements GETSET key value.
// Sets key to value and returns the old value, or null bulk string if it
// did not exist.
func handleGetSet(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'GETSET' command")
	}
	old, existed := store.Get(args[1])
	store.Set(args[1], args[2])
	if !existed {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(old)
}

// handleGetDel implements GETDEL key.
// Returns the value and deletes the key in one atomic step.
func handleGetDel(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'GETDEL' command")
	}
	val, ok := store.Get(args[1])
	if !ok {
		return protocol.NullBulkString()
	}
	store.Del(args[1])
	return protocol.BulkString(val)
}

// handleGetEX implements GETEX key [EX seconds | PX milliseconds | EXAT ts | PXAT ts | PERSIST].
func handleGetEX(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'GETEX' command")
	}
	key := args[1]
	var ttl time.Duration
	var persist, hasTTL bool

	for i := 2; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "PERSIST":
			persist = true
		case "EX":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			secs, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || secs <= 0 {
				return protocol.Error("ERR invalid expire time in 'GETEX' command")
			}
			ttl = time.Duration(secs) * time.Second
			hasTTL = true
		case "PX":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			ms, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || ms <= 0 {
				return protocol.Error("ERR invalid expire time in 'GETEX' command")
			}
			ttl = time.Duration(ms) * time.Millisecond
			hasTTL = true
		case "EXAT":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			unixSec, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || unixSec <= 0 {
				return protocol.Error("ERR invalid expire time in 'GETEX' command")
			}
			ttl = time.Until(time.Unix(unixSec, 0))
			hasTTL = true
		case "PXAT":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			unixMs, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || unixMs <= 0 {
				return protocol.Error("ERR invalid expire time in 'GETEX' command")
			}
			ttl = time.Until(time.UnixMilli(unixMs))
			hasTTL = true
		default:
			return protocol.Error("ERR syntax error")
		}
	}

	val, ok := store.GetEx(key, ttl, persist, hasTTL)
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(val)
}

// handleAppend implements APPEND key value.
// Appends value to the existing string at key (or sets it if key is missing).
// Returns the new length of the string.
func handleAppend(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'APPEND' command")
	}
	existing, _ := store.Get(args[1])
	newVal := existing + args[2]
	store.Set(args[1], newVal)
	return protocol.Integer(int64(len(newVal)))
}

// handleStrLen implements STRLEN key.
// Returns the length of the string value at key, or 0 if the key does not exist.
func handleStrLen(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'STRLEN' command")
	}
	val, ok := store.Get(args[1])
	if !ok {
		return protocol.Integer(0)
	}
	return protocol.Integer(int64(len(val)))
}
