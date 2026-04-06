package commands

import (
	"errors"
	"strconv"
	"strings"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleLPush implements LPUSH key value [value ...].
func handleLPush(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'LPUSH' command")
	}
	n, err := store.LPush(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleRPush implements RPUSH key value [value ...].
func handleRPush(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'RPUSH' command")
	}
	n, err := store.RPush(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleLPushX implements LPUSHX key value [value ...].
// Only pushes if the key already exists and is a list.
func handleLPushX(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'LPUSHX' command")
	}
	if store.Type(args[1]) != "list" {
		return protocol.Integer(0)
	}
	n, err := store.LPush(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleRPushX implements RPUSHX key value [value ...].
func handleRPushX(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'RPUSHX' command")
	}
	if store.Type(args[1]) != "list" {
		return protocol.Integer(0)
	}
	n, err := store.RPush(args[1], args[2:]...)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleLPop implements LPOP key [count].
func handleLPop(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 || len(args) > 3 {
		return protocol.Error("ERR wrong number of arguments for 'LPOP' command")
	}
	count := 1
	withCount := len(args) == 3
	if withCount {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 {
			return protocol.Error("ERR value is not an integer or out of range")
		}
		count = n
	}
	vals, err := store.LPop(args[1], count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if vals == nil {
		return protocol.NullBulkString()
	}
	if !withCount {
		if len(vals) == 0 {
			return protocol.NullBulkString()
		}
		return protocol.BulkString(vals[0])
	}
	elems := make([]protocol.Response, len(vals))
	for i, v := range vals {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleRPop implements RPOP key [count].
func handleRPop(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 || len(args) > 3 {
		return protocol.Error("ERR wrong number of arguments for 'RPOP' command")
	}
	count := 1
	withCount := len(args) == 3
	if withCount {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 {
			return protocol.Error("ERR value is not an integer or out of range")
		}
		count = n
	}
	vals, err := store.RPop(args[1], count)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if vals == nil {
		return protocol.NullBulkString()
	}
	if !withCount {
		if len(vals) == 0 {
			return protocol.NullBulkString()
		}
		return protocol.BulkString(vals[0])
	}
	elems := make([]protocol.Response, len(vals))
	for i, v := range vals {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleLLen implements LLEN key.
func handleLLen(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'LLEN' command")
	}
	n, err := store.LLen(args[1])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleLRange implements LRANGE key start stop.
func handleLRange(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'LRANGE' command")
	}
	start, err1 := strconv.Atoi(args[2])
	stop, err2 := strconv.Atoi(args[3])
	if err1 != nil || err2 != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	vals, err := store.LRange(args[1], start, stop)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(vals))
	for i, v := range vals {
		elems[i] = protocol.BulkString(v)
	}
	return protocol.Array(elems)
}

// handleLIndex implements LINDEX key index.
func handleLIndex(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'LINDEX' command")
	}
	idx, err := strconv.Atoi(args[2])
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	val, ok, err := store.LIndex(args[1], idx)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(val)
}

// handleLSet implements LSET key index value.
func handleLSet(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'LSET' command")
	}
	idx, err := strconv.Atoi(args[2])
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	if err := store.LSet(args[1], idx, args[3]); err != nil {
		if errors.Is(err, storage.ErrWrongType) {
			return protocol.Error(storage.ErrWrongType.Error())
		}
		return protocol.Error(err.Error())
	}
	return protocol.SimpleString("OK")
}

// handleLInsert implements LINSERT key BEFORE|AFTER pivot value.
func handleLInsert(args []string, store storage.Store) protocol.Response {
	if len(args) != 5 {
		return protocol.Error("ERR wrong number of arguments for 'LINSERT' command")
	}
	var before bool
	switch strings.ToUpper(args[2]) {
	case "BEFORE":
		before = true
	case "AFTER":
		before = false
	default:
		return protocol.Error("ERR syntax error")
	}
	n, err := store.LInsert(args[1], before, args[3], args[4])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleLRem implements LREM key count value.
func handleLRem(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'LREM' command")
	}
	count, err := strconv.Atoi(args[2])
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	n, err := store.LRem(args[1], count, args[3])
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	return protocol.Integer(int64(n))
}

// handleLTrim implements LTRIM key start stop.
func handleLTrim(args []string, store storage.Store) protocol.Response {
	if len(args) != 4 {
		return protocol.Error("ERR wrong number of arguments for 'LTRIM' command")
	}
	start, err1 := strconv.Atoi(args[2])
	stop, err2 := strconv.Atoi(args[3])
	if err1 != nil || err2 != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	if err := store.LTrim(args[1], start, stop); err != nil {
		if errors.Is(err, storage.ErrWrongType) {
			return protocol.Error(storage.ErrWrongType.Error())
		}
		return protocol.Error(err.Error())
	}
	return protocol.SimpleString("OK")
}

// handleLMove implements LMOVE source destination LEFT|RIGHT LEFT|RIGHT.
func handleLMove(args []string, store storage.Store) protocol.Response {
	if len(args) != 5 {
		return protocol.Error("ERR wrong number of arguments for 'LMOVE' command")
	}
	var srcLeft, dstLeft bool
	switch strings.ToUpper(args[3]) {
	case "LEFT":
		srcLeft = true
	case "RIGHT":
		srcLeft = false
	default:
		return protocol.Error("ERR syntax error")
	}
	switch strings.ToUpper(args[4]) {
	case "LEFT":
		dstLeft = true
	case "RIGHT":
		dstLeft = false
	default:
		return protocol.Error("ERR syntax error")
	}
	val, ok, err := store.LMove(args[1], args[2], srcLeft, dstLeft)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(val)
}

// handleRPopLPush implements RPOPLPUSH source destination.
// Deprecated alias for LMOVE source destination RIGHT LEFT.
func handleRPopLPush(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'RPOPLPUSH' command")
	}
	val, ok, err := store.LMove(args[1], args[2], false, true)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	if !ok {
		return protocol.NullBulkString()
	}
	return protocol.BulkString(val)
}

// handleLPos implements LPOS key element [RANK rank] [COUNT num] [MAXLEN maxlen].
func handleLPos(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'LPOS' command")
	}
	key := args[1]
	target := args[2]
	rank := 1
	count := 1 // -1 = all
	hasCount := false
	maxlen := 0

	for i := 3; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "RANK":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n == 0 {
				return protocol.Error("ERR RANK can't be zero: use 1 to start from the first match, 2 from the second … or use negative to start from the end of the list")
			}
			rank = n
		case "COUNT":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			count = n
			hasCount = true
		case "MAXLEN":
			i++
			if i >= len(args) {
				return protocol.Error("ERR syntax error")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return protocol.Error("ERR value is not an integer or out of range")
			}
			maxlen = n
		default:
			return protocol.Error("ERR syntax error")
		}
	}

	vals, err := store.LRange(key, 0, -1)
	if errors.Is(err, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}

	limit := len(vals)
	if maxlen > 0 && maxlen < limit {
		limit = maxlen
	}

	var positions []int
	matchCount := 0

	if rank >= 0 {
		for i := 0; i < limit; i++ {
			if vals[i] == target {
				matchCount++
				if matchCount >= rank {
					positions = append(positions, i)
					if hasCount && count > 0 && len(positions) >= count {
						break
					}
				}
			}
		}
	} else {
		// Negative rank: scan from tail.
		absRank := -rank
		for i := limit - 1; i >= 0; i-- {
			if vals[i] == target {
				matchCount++
				if matchCount >= absRank {
					positions = append(positions, i)
					if hasCount && count > 0 && len(positions) >= count {
						break
					}
				}
			}
		}
	}

	if hasCount {
		elems := make([]protocol.Response, len(positions))
		for i, pos := range positions {
			elems[i] = protocol.Integer(int64(pos))
		}
		return protocol.Array(elems)
	}
	if len(positions) == 0 {
		return protocol.NullBulkString()
	}
	return protocol.Integer(int64(positions[0]))
}
