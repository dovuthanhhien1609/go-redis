package commands

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// serverStartTime is captured once at startup for the UPTIME field in INFO.
var serverStartTime = time.Now()

// handleInfo implements INFO [section].
// Returns a bulk string with server statistics. Supports sections:
// "server", "keyspace", or no argument (returns all sections).
func handleInfo(args []string, store storage.Store) protocol.Response {
	if len(args) > 2 {
		return protocol.Error("ERR wrong number of arguments for 'INFO' command")
	}
	section := "all"
	if len(args) == 2 {
		section = strings.ToLower(args[1])
	}

	var sb strings.Builder

	if section == "server" || section == "all" || section == "default" {
		uptime := int64(time.Since(serverStartTime).Seconds())
		sb.WriteString("# Server\r\n")
		sb.WriteString("redis_version:7.0.0-go-clone\r\n")
		sb.WriteString(fmt.Sprintf("os:%s\r\n", runtime.GOOS))
		sb.WriteString(fmt.Sprintf("arch_bits:%d\r\n", 32<<(^uint(0)>>63)))
		sb.WriteString(fmt.Sprintf("go_version:%s\r\n", runtime.Version()))
		sb.WriteString(fmt.Sprintf("uptime_in_seconds:%d\r\n", uptime))
		sb.WriteString(fmt.Sprintf("uptime_in_days:%d\r\n", uptime/86400))
		sb.WriteString("connected_clients:0\r\n") // placeholder
		sb.WriteString("\r\n")
	}

	if section == "keyspace" || section == "all" || section == "default" {
		sb.WriteString("# Keyspace\r\n")
		sb.WriteString(fmt.Sprintf("db0:keys=%d,expires=0\r\n", store.Len()))
		sb.WriteString("\r\n")
	}

	if section == "memory" || section == "all" || section == "default" {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		sb.WriteString("# Memory\r\n")
		sb.WriteString(fmt.Sprintf("used_memory:%d\r\n", ms.Alloc))
		sb.WriteString(fmt.Sprintf("used_memory_human:%.2fK\r\n", float64(ms.Alloc)/1024))
		sb.WriteString("\r\n")
	}

	return protocol.BulkString(sb.String())
}

// handleDBSize implements DBSIZE.
// Returns the number of keys in the database.
func handleDBSize(args []string, store storage.Store) protocol.Response {
	if len(args) != 1 {
		return protocol.Error("ERR wrong number of arguments for 'DBSIZE' command")
	}
	return protocol.Integer(int64(store.Len()))
}

// handleType implements TYPE key.
// Returns a simple string: "string", "hash", or "none".
func handleType(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'TYPE' command")
	}
	return protocol.SimpleString(store.Type(args[1]))
}

// handleRename implements RENAME source destination.
// Renames source to destination atomically.
func handleRename(args []string, store storage.Store) protocol.Response {
	if len(args) != 3 {
		return protocol.Error("ERR wrong number of arguments for 'RENAME' command")
	}
	if args[1] == args[2] {
		// Redis returns OK for RENAME to same key if key exists.
		if store.Type(args[1]) == "none" {
			return protocol.Error("ERR no such key")
		}
		return protocol.SimpleString("OK")
	}
	if !store.Rename(args[1], args[2]) {
		return protocol.Error("ERR no such key")
	}
	return protocol.SimpleString("OK")
}

// handleFlushDB implements FLUSHDB [ASYNC].
// Removes all keys from the database.
func handleFlushDB(args []string, store storage.Store) protocol.Response {
	if len(args) > 2 {
		return protocol.Error("ERR wrong number of arguments for 'FLUSHDB' command")
	}
	store.Flush()
	return protocol.SimpleString("OK")
}

// handleFlushAll implements FLUSHALL [ASYNC].
// Same as FLUSHDB in this single-database implementation.
func handleFlushAll(args []string, store storage.Store) protocol.Response {
	if len(args) > 2 {
		return protocol.Error("ERR wrong number of arguments for 'FLUSHALL' command")
	}
	store.Flush()
	return protocol.SimpleString("OK")
}

// handleSelect implements SELECT index.
// This implementation supports only database 0. SELECT 0 returns OK;
// any other index returns an error.
func handleSelect(args []string, store storage.Store) protocol.Response {
	if len(args) != 2 {
		return protocol.Error("ERR wrong number of arguments for 'SELECT' command")
	}
	if args[1] != "0" {
		return protocol.Error("ERR DB index is out of range")
	}
	return protocol.SimpleString("OK")
}
