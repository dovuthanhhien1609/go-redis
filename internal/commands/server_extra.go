package commands

import (
	"strings"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// handleObject implements OBJECT subcommand [arguments ...].
// Supports ENCODING, REFCOUNT, IDLETIME, FREQ, HELP.
func handleObject(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'OBJECT' command")
	}
	sub := strings.ToUpper(args[1])
	switch sub {
	case "ENCODING":
		if len(args) != 3 {
			return protocol.Error("ERR wrong number of arguments for 'OBJECT|ENCODING' command")
		}
		t := store.Type(args[2])
		switch t {
		case "none":
			return protocol.Error("ERR no such key")
		case "string":
			val, _ := store.Get(args[2])
			// Check if it looks like an integer.
			if _, err := parseInt64(val); err == nil && len(val) < 20 {
				return protocol.BulkString("int")
			}
			if len(val) <= 44 {
				return protocol.BulkString("embstr")
			}
			return protocol.BulkString("raw")
		case "hash":
			if store.HLen(args[2]) <= 128 {
				return protocol.BulkString("ziplist")
			}
			return protocol.BulkString("hashtable")
		case "list":
			n, _ := store.LLen(args[2])
			if n <= 128 {
				return protocol.BulkString("listpack")
			}
			return protocol.BulkString("quicklist")
		case "set":
			n, _ := store.SCard(args[2])
			if n <= 128 {
				return protocol.BulkString("listpack")
			}
			return protocol.BulkString("hashtable")
		case "zset":
			n, _ := store.ZCard(args[2])
			if n <= 128 {
				return protocol.BulkString("listpack")
			}
			return protocol.BulkString("skiplist")
		}
		return protocol.BulkString("embstr")

	case "REFCOUNT":
		if len(args) != 3 {
			return protocol.Error("ERR wrong number of arguments for 'OBJECT|REFCOUNT' command")
		}
		if store.Type(args[2]) == "none" {
			return protocol.Error("ERR no such key")
		}
		return protocol.Integer(1)

	case "IDLETIME":
		if len(args) != 3 {
			return protocol.Error("ERR wrong number of arguments for 'OBJECT|IDLETIME' command")
		}
		if store.Type(args[2]) == "none" {
			return protocol.Error("ERR no such key")
		}
		return protocol.Integer(0)

	case "FREQ":
		if len(args) != 3 {
			return protocol.Error("ERR wrong number of arguments for 'OBJECT|FREQ' command")
		}
		if store.Type(args[2]) == "none" {
			return protocol.Error("ERR no such key")
		}
		return protocol.Integer(0)

	case "HELP":
		help := []protocol.Response{
			protocol.BulkString("OBJECT <subcommand> [<arg> [value] [opt] ...]. subcommands are:"),
			protocol.BulkString("ENCODING <key>"),
			protocol.BulkString("    Return the kind of internal representation the Redis object stored at <key> is using."),
			protocol.BulkString("FREQ <key>"),
			protocol.BulkString("    Return the access frequency index of the key. The returned integer is proportional to the logarithm of the recent access frequency."),
			protocol.BulkString("HELP"),
			protocol.BulkString("    Return subcommand help summary."),
			protocol.BulkString("IDLETIME <key>"),
			protocol.BulkString("    Return the idle time of the key, that is the approximated number of seconds elapsed since the last access to the key."),
			protocol.BulkString("REFCOUNT <key>"),
			protocol.BulkString("    Return the reference count of the object stored at <key>."),
		}
		return protocol.Array(help)
	}
	return protocol.Error("ERR unknown subcommand '" + args[1] + "'. Try OBJECT HELP.")
}

// handleClient implements CLIENT subcommand [arguments ...].
// Supports ID, GETNAME, SETNAME, LIST, INFO, NO-EVICT, NO-TOUCH, CACHING.
func handleClient(args []string, store storage.Store) protocol.Response {
	if len(args) < 2 {
		return protocol.Error("ERR wrong number of arguments for 'CLIENT' command")
	}
	sub := strings.ToUpper(args[1])
	switch sub {
	case "ID":
		// Return a fixed connection ID (real tracking requires per-conn state).
		return protocol.Integer(1)
	case "GETNAME":
		return protocol.NullBulkString()
	case "SETNAME":
		if len(args) != 3 {
			return protocol.Error("ERR wrong number of arguments for 'CLIENT|SETNAME' command")
		}
		return protocol.SimpleString("OK")
	case "LIST":
		return protocol.BulkString("id=1 addr=127.0.0.1:0 laddr=127.0.0.1:6379 fd=0 name= age=0 idle=0 flags=N db=0 sub=0 psub=0 multi=-1 watch=0 qbuf=0 qbuf-free=0 argv-mem=0 multi-mem=0 tot-mem=0 rbs=0 rbp=0 obl=0 oll=0 omem=0 events=r cmd=client|list user=default library-name= library-ver= resp=2\n")
	case "INFO":
		return protocol.BulkString("id=1 addr=127.0.0.1:0 laddr=127.0.0.1:6379 fd=0 name= age=0 idle=0 flags=N db=0 sub=0 psub=0 multi=-1 watch=0 qbuf=0 qbuf-free=0 argv-mem=0 multi-mem=0 tot-mem=0 rbs=0 rbp=0 obl=0 oll=0 omem=0 events=r cmd=client|info user=default library-name= library-ver= resp=2\n")
	case "NO-EVICT", "NO-TOUCH", "CACHING":
		return protocol.SimpleString("OK")
	case "REPLY":
		return protocol.SimpleString("OK")
	case "UNPAUSE":
		return protocol.SimpleString("OK")
	case "PAUSE":
		return protocol.SimpleString("OK")
	case "KILL":
		return protocol.Integer(0)
	}
	return protocol.Error("ERR unknown subcommand '" + args[1] + "'. Try CLIENT HELP.")
}
