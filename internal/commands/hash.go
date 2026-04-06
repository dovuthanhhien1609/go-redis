package commands

import (
	"strconv"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleHSet implements HSET key field value [field value ...].
// Also handles HMSET (same syntax, deprecated alias).
// Returns the number of fields that were added (not updated).
func handleHSet(args []string, store storage.Store) protocol.Response {
	// args: HSET key field value [field value ...]
	if len(args) < 4 || (len(args)-2)%2 != 0 {
		return protocol.Error("ERR wrong number of arguments for 'HSET' command")
	}
	fields := make(map[string]string, (len(args)-2)/2)
	for i := 2; i+1 < len(args); i += 2 {
		fields[args[i]] = args[i+1]
	}
	n := store.HSet(args[1], fields)
	return protocol.Integer(int64(n))
}

// handleHGet implements HGET key field.
// Returns the bulk-string value or null bulk string if field/key is missing.
func handleHGet(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'HGET' command")
	}
	val, ok := store.HGet(args[1], args[2])
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(val)
}

// handleHDel implements HDEL key field [field ...].
// Returns the count of fields that were removed.
func handleHDel(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'HDEL' command")
	}
	n := store.HDel(args[1], args[2:]...)
	return protocol.Integer(int64(n))
}

// handleHGetAll implements HGETALL key.
// Returns an array of alternating field / value pairs, or an empty array.
func handleHGetAll(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'HGETALL' command")
	}
	all := store.HGetAll(args[1])
	elems := make([]protocol.Response, 0, len(all)*2)
	for f, v := range all {
		elems = append(elems, protocol.BulkString(f))
		elems = append(elems, protocol.BulkString(v))
	}
	return protocol.Array(elems)
}

// handleHMGet implements HMGET key field [field ...].
// Returns an array where each entry is the value for the field, or null bulk
// string if the field does not exist.
func handleHMGet(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'HMGET' command")
	}
	key := args[1]
	elems := make([]protocol.Response, len(args)-2)
	for i, field := range args[2:] {
		val, ok := store.HGet(key, field)
		if ok {
			elems[i] = protocol.BulkString(val)
		} else {
			elems[i] = protocol.NullBulkString()
		}
	}
	return protocol.Array(elems)
}

// handleHLen implements HLEN key.
// Returns the number of fields in the hash.
func handleHLen(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'HLEN' command")
	}
	return protocol.Integer(int64(store.HLen(args[1])))
}

// handleHExists implements HEXISTS key field.
// Returns :1 if the field exists, :0 otherwise.
func handleHExists(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'HEXISTS' command")
	}
	if store.HExists(args[1], args[2]) {
		return protocol.Integer(1)
	}
	return protocol.Integer(0)
}

// handleHKeys implements HKEYS key.
// Returns an array of all field names in the hash.
func handleHKeys(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'HKEYS' command")
	}
	keys := store.HKeys(args[1])
	elems := make([]protocol.Response, len(keys))
	for i, k := range keys {
		elems[i] = protocol.BulkString(k)
	}
	return protocol.Array(elems)
}

// handleHVals implements HVALS key.
// Returns an array of all values in the hash.
func handleHVals(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'HVALS' command")
	}
	vals := store.HVals(args[1])
	elems := make([]protocol.Response, len(vals))
	for i, v := range vals {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleHIncrByFloat implements HINCRBYFLOAT key field increment.
func handleHIncrByFloat(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'HINCRBYFLOAT' command")
	}
	key, field := args[1], args[2]
	delta, err := strconv.ParseFloat(args[3], 64)
	if err != nil {
		return protocol.Error("ERR value is not a valid float")
	}
	var current float64
	if raw, ok := store.HGet(key, field); ok {
		current, err = strconv.ParseFloat(raw, 64)
		if err != nil {
			return protocol.Error("ERR hash value is not a valid float")
		}
	}
	result := current + delta
	resultStr := strconv.FormatFloat(result, 'f', -1, 64)
	store.HSet(key, map[string]string{field: resultStr})
	return protocol.Response{Type: protocol.TypeBulkString, Str: resultStr}
}

// handleHIncrBy implements HINCRBY key field increment.
// Increments the integer value of a hash field by increment.
func handleHIncrBy(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'HINCRBY' command")
	}
	key, field := args[1], args[2]

	var current int64
	if raw, ok := store.HGet(key, field); ok {
		var err error
		current, err = parseInt64(raw)
		if err != nil {
			return protocol.Error("ERR hash value is not an integer")
		}
	}

	delta, err := parseInt64(args[3])
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}

	next := current + delta
	store.HSet(key, map[string]string{field: formatInt64(next)})
	return protocol.Integer(next)
}
