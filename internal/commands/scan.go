package commands

import (
	"errors"
	"strconv"
	"strings"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// parseScanArgs extracts the common [MATCH pattern] [COUNT count] arguments
// starting at args[offset]. Returns (match, count, error).
func parseScanArgs(args []string, offset int) (string, int, error) {
	match := "*"
	count := 10
	for i := offset; i < len(args); i++ {
		switch strings.ToUpper(args[i]) {
		case "MATCH":
			if i+1 >= len(args) {
				return "", 0, errors.New("ERR syntax error")
			}
			match = args[i+1]
			i++
		case "COUNT":
			if i+1 >= len(args) {
				return "", 0, errors.New("ERR syntax error")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				return "", 0, errors.New("ERR value is not an integer or out of range")
			}
			count = n
			i++
		default:
			return "", 0, errors.New("ERR syntax error")
		}
	}
	return match, count, nil
}

// handleScan implements SCAN cursor [MATCH pattern] [COUNT count] [TYPE type].
func handleScan(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'SCAN' command")
	}
	cursor, err := strconv.ParseUint(args[1], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	match, count, err := parseScanArgs(args, 2)
	if err != nil {
		return protocol.Error(err.Error())
	}
	if match == "*" {
		match = ""
	}
	nextCursor, keys := store.Scan(cursor, match, count)
	elems := make([]protocol.Response, len(keys))
	for i, k := range keys {
		elems[i] = protocol.BulkString(k)
	}
	return protocol.Array([]protocol.Response{
		protocol.BulkString(strconv.FormatUint(nextCursor, 10)),
		protocol.Array(elems),
	})
}

// handleHScan implements HSCAN key cursor [MATCH pattern] [COUNT count].
func handleHScan(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'HSCAN' command")
	}
	cursor, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	match, count, err := parseScanArgs(args, 3)
	if err != nil {
		return protocol.Error(err.Error())
	}
	if match == "*" {
		match = ""
	}
	nextCursor, pairs, serr := store.HScan(args[1], cursor, match, count)
	if errors.Is(serr, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(pairs))
	for i, p := range pairs {
		elems[i] = protocol.BulkString(p)
	}
	return protocol.Array([]protocol.Response{
		protocol.BulkString(strconv.FormatUint(nextCursor, 10)),
		protocol.Array(elems),
	})
}

// handleSScan implements SSCAN key cursor [MATCH pattern] [COUNT count].
func handleSScan(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'SSCAN' command")
	}
	cursor, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	match, count, err := parseScanArgs(args, 3)
	if err != nil {
		return protocol.Error(err.Error())
	}
	if match == "*" {
		match = ""
	}
	nextCursor, members, serr := store.SScan(args[1], cursor, match, count)
	if errors.Is(serr, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(members))
	for i, m := range members {
		elems[i] = protocol.BulkString(m)
	}
	return protocol.Array([]protocol.Response{
		protocol.BulkString(strconv.FormatUint(nextCursor, 10)),
		protocol.Array(elems),
	})
}

// handleZScan implements ZSCAN key cursor [MATCH pattern] [COUNT count].
func handleZScan(args []string, store storage.Store) protocol.Response {
	if len(args) < 3 {
		return protocol.Error("ERR wrong number of arguments for 'ZSCAN' command")
	}
	cursor, err := strconv.ParseUint(args[2], 10, 64)
	if err != nil {
		return protocol.Error("ERR value is not an integer or out of range")
	}
	match, count, err := parseScanArgs(args, 3)
	if err != nil {
		return protocol.Error(err.Error())
	}
	if match == "*" {
		match = ""
	}
	nextCursor, pairs, serr := store.ZScan(args[1], cursor, match, count)
	if errors.Is(serr, storage.ErrWrongType) {
		return protocol.Error(storage.ErrWrongType.Error())
	}
	elems := make([]protocol.Response, len(pairs))
	for i, p := range pairs {
		elems[i] = protocol.BulkString(p)
	}
	return protocol.Array([]protocol.Response{
		protocol.BulkString(strconv.FormatUint(nextCursor, 10)),
		protocol.Array(elems),
	})
}
