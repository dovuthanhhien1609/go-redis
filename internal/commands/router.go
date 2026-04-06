// Package commands implements the command registry and dispatch logic.
// Each command is a pure function: (args []string, store storage.Store) → protocol.Response.
package commands

import (
	"strconv"
	"strings"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// HandlerFunc is the signature every command handler must implement.
// args[0] is always the command name (upper-cased before dispatch).
type HandlerFunc func(args []string, store storage.Store) protocol.Response

// Appender is satisfied by *persistence.AOF (and any mock in tests).
// Defined here to avoid an import cycle: commands must not import persistence.
type Appender interface {
	Append(args []string) error
}

// Router holds the command registry, the shared storage backend, an optional
// AOF appender for persistence, and an optional pub/sub publisher.
// It is safe for concurrent use — Dispatch is read-only on the registry map,
// and the store handles its own synchronization.
type Router struct {
	store    storage.Store
	aof      Appender  // nil when AOF is disabled
	pub      Publisher // nil when pub/sub is not wired up
	handlers map[string]HandlerFunc
}

// NewRouter creates a Router with the given store and registers all built-in
// command handlers. Pass a non-nil Appender to enable AOF persistence.
// Pass a non-nil Publisher to enable the PUBLISH command.
func NewRouter(store storage.Store, aof Appender, pub Publisher) *Router {
	r := &Router{
		store:    store,
		aof:      aof,
		pub:      pub,
		handlers: make(map[string]HandlerFunc),
	}
	r.register()
	return r
}

// register adds all built-in commands to the registry.
func (r *Router) register() {
	// Meta
	r.handlers["PING"] = handlePing
	r.handlers["COMMAND"] = handleCommand

	// String
	r.handlers["SET"] = handleSet
	r.handlers["GET"] = handleGet
	r.handlers["GETEX"] = handleGetEX
	r.handlers["DEL"] = handleDel
	r.handlers["EXISTS"] = handleExists
	r.handlers["KEYS"] = handleKeys
	r.handlers["SETNX"] = handleSetNX
	r.handlers["SETEX"] = handleSetEX
	r.handlers["PSETEX"] = handlePSetEX
	r.handlers["MSET"] = handleMSet
	r.handlers["MGET"] = handleMGet
	r.handlers["GETSET"] = handleGetSet
	r.handlers["GETDEL"] = handleGetDel
	r.handlers["APPEND"] = handleAppend
	r.handlers["STRLEN"] = handleStrLen

	// Expiry
	r.handlers["EXPIRE"] = handleExpire
	r.handlers["PEXPIRE"] = handlePExpire
	r.handlers["EXPIREAT"] = handleExpireAt
	r.handlers["PEXPIREAT"] = handlePExpireAt
	r.handlers["TTL"] = handleTTL
	r.handlers["PTTL"] = handlePTTL
	r.handlers["PERSIST"] = handlePersist

	// Counters
	r.handlers["INCR"] = handleIncr
	r.handlers["INCRBY"] = handleIncrBy
	r.handlers["INCRBYFLOAT"] = handleIncrByFloat
	r.handlers["DECR"] = handleDecr
	r.handlers["DECRBY"] = handleDecrBy

	// Hash
	r.handlers["HSET"] = handleHSet
	r.handlers["HMSET"] = handleHSet // deprecated alias; same syntax
	r.handlers["HGET"] = handleHGet
	r.handlers["HDEL"] = handleHDel
	r.handlers["HGETALL"] = handleHGetAll
	r.handlers["HMGET"] = handleHMGet
	r.handlers["HLEN"] = handleHLen
	r.handlers["HEXISTS"] = handleHExists
	r.handlers["HKEYS"] = handleHKeys
	r.handlers["HVALS"] = handleHVals
	r.handlers["HINCRBY"] = handleHIncrBy
	r.handlers["HINCRBYFLOAT"] = handleHIncrByFloat

	// List
	r.handlers["LPUSH"] = handleLPush
	r.handlers["RPUSH"] = handleRPush
	r.handlers["LPUSHX"] = handleLPushX
	r.handlers["RPUSHX"] = handleRPushX
	r.handlers["LPOP"] = handleLPop
	r.handlers["RPOP"] = handleRPop
	r.handlers["LLEN"] = handleLLen
	r.handlers["LRANGE"] = handleLRange
	r.handlers["LINDEX"] = handleLIndex
	r.handlers["LSET"] = handleLSet
	r.handlers["LINSERT"] = handleLInsert
	r.handlers["LREM"] = handleLRem
	r.handlers["LTRIM"] = handleLTrim
	r.handlers["LMOVE"] = handleLMove
	r.handlers["RPOPLPUSH"] = handleRPopLPush
	r.handlers["LPOS"] = handleLPos

	// Set
	r.handlers["SADD"] = handleSAdd
	r.handlers["SREM"] = handleSRem
	r.handlers["SMEMBERS"] = handleSMembers
	r.handlers["SCARD"] = handleSCard
	r.handlers["SISMEMBER"] = handleSIsMember
	r.handlers["SMISMEMBER"] = handleSMIsMember
	r.handlers["SINTER"] = handleSInter
	r.handlers["SUNION"] = handleSUnion
	r.handlers["SDIFF"] = handleSDiff
	r.handlers["SINTERSTORE"] = handleSInterStore
	r.handlers["SUNIONSTORE"] = handleSUnionStore
	r.handlers["SDIFFSTORE"] = handleSDiffStore
	r.handlers["SRANDMEMBER"] = handleSRandMember
	r.handlers["SPOP"] = handleSPop
	r.handlers["SMOVE"] = handleSMove

	// Sorted set
	r.handlers["ZADD"] = handleZAdd
	r.handlers["ZREM"] = handleZRem
	r.handlers["ZSCORE"] = handleZScore
	r.handlers["ZCARD"] = handleZCard
	r.handlers["ZRANK"] = handleZRank
	r.handlers["ZREVRANK"] = handleZRevRank
	r.handlers["ZINCRBY"] = handleZIncrBy
	r.handlers["ZRANGE"] = handleZRange
	r.handlers["ZREVRANGE"] = handleZRevRange
	r.handlers["ZRANGEBYSCORE"] = handleZRangeByScore
	r.handlers["ZREVRANGEBYSCORE"] = handleZRevRangeByScore
	r.handlers["ZRANGEBYLEX"] = handleZRangeByLex
	r.handlers["ZREVRANGEBYLEX"] = handleZRevRangeByLex
	r.handlers["ZCOUNT"] = handleZCount
	r.handlers["ZLEXCOUNT"] = handleZLexCount
	r.handlers["ZPOPMIN"] = handleZPopMin
	r.handlers["ZPOPMAX"] = handleZPopMax
	r.handlers["ZUNIONSTORE"] = handleZUnionStore
	r.handlers["ZINTERSTORE"] = handleZInterStore

	// Scan
	r.handlers["SCAN"] = handleScan
	r.handlers["HSCAN"] = handleHScan
	r.handlers["SSCAN"] = handleSScan
	r.handlers["ZSCAN"] = handleZScan

	// Server / admin
	r.handlers["INFO"] = handleInfo
	r.handlers["DBSIZE"] = handleDBSize
	r.handlers["TYPE"] = handleType
	r.handlers["RENAME"] = handleRename
	r.handlers["FLUSHDB"] = handleFlushDB
	r.handlers["FLUSHALL"] = handleFlushAll
	r.handlers["SELECT"] = handleSelect
	r.handlers["OBJECT"] = handleObject
	r.handlers["CLIENT"] = handleClient

	// Pub/sub (PUBLISH only — SUBSCRIBE/UNSUBSCRIBE are handled by the server
	// layer because they are connection-state-aware)
	r.handlers["PUBLISH"] = makePublishHandler(r.pub)
}

// KeyVersion returns the current modification version of key from the store.
// Used by the server handler to implement WATCH.
func (r *Router) KeyVersion(key string) uint64 {
	return r.store.Version(key)
}

// Dispatch normalises the command name, looks it up in the registry, calls
// the handler, and — for mutating commands — appends the appropriate record
// to the AOF for later replay.
//
// AOF append happens after the command succeeds. A failed AOF write is
// non-fatal: the in-memory state is already updated and the error is silently
// dropped (a production system would alert on this).
func (r *Router) Dispatch(args []string) protocol.Response {
	if len(args) == 0 {
		return protocol.Error("ERR empty command")
	}

	name := strings.ToUpper(args[0])
	fn, ok := r.handlers[name]
	if !ok {
		return protocol.Error("ERR unknown command '" + args[0] + "'")
	}

	resp := fn(args, r.store)

	if r.aof != nil && resp.Type != protocol.TypeError {
		r.appendToAOF(name, args, resp)
	}

	return resp
}

// appendToAOF writes the correct AOF record(s) for a successfully-executed
// mutating command. Read-only commands produce no AOF entries.
//
// Design goals:
//   - Commands with time-relative arguments (EXPIRE, SETEX, …) are converted
//     to PEXPIREAT with an absolute millisecond timestamp so that TTLs survive
//     server restarts with the correct remaining duration.
//   - Counter commands (INCR, DECR, …) are logged as SET key <final_value>
//     so the value is idempotently restored on replay without needing to
//     re-execute arithmetic.
//   - All other mutating commands are logged as-is.
func (r *Router) appendToAOF(name string, args []string, resp protocol.Response) {
	switch name {
	// ── Commands logged verbatim ─────────────────────────────────────────
	case "SET", "DEL", "MSET", "HSET", "HMSET", "HDEL",
		"FLUSHDB", "FLUSHALL", "PERSIST", "APPEND",
		"LPUSH", "RPUSH", "LPUSHX", "RPUSHX",
		"LSET", "LINSERT", "LTRIM",
		"SADD", "SREM", "SINTERSTORE", "SUNIONSTORE", "SDIFFSTORE",
		"SMOVE",
		"ZADD", "ZREM", "ZINCRBY", "ZUNIONSTORE", "ZINTERSTORE":
		_ = r.aof.Append(args)

	// ── SET if not exists ────────────────────────────────────────────────
	case "SETNX":
		if resp.Integer == 1 {
			_ = r.aof.Append([]string{"SET", args[1], args[2]})
		}

	// ── GET-then-SET variants ────────────────────────────────────────────
	case "GETSET":
		_ = r.aof.Append([]string{"SET", args[1], args[2]})

	case "GETDEL":
		if resp.Type != protocol.TypeNullBulkString {
			_ = r.aof.Append([]string{"DEL", args[1]})
		}

	// ── List pop/move ────────────────────────────────────────────────────
	case "LPOP", "RPOP":
		if resp.Type != protocol.TypeNullBulkString && resp.Type != protocol.TypeNullArray {
			_ = r.aof.Append(args)
		}

	case "LMOVE", "RPOPLPUSH":
		if resp.Type != protocol.TypeNullBulkString {
			_ = r.aof.Append(args)
		}

	case "LREM":
		if resp.Integer > 0 {
			_ = r.aof.Append(args)
		}

	// ── Set pop ──────────────────────────────────────────────────────────
	case "SPOP":
		if resp.Type != protocol.TypeNullBulkString {
			_ = r.aof.Append(args)
		}

	// ── ZSet pop ─────────────────────────────────────────────────────────
	case "ZPOPMIN", "ZPOPMAX":
		if len(resp.Array) > 0 {
			_ = r.aof.Append(args)
		}

	// ── TTL-bearing set commands ─────────────────────────────────────────
	case "SETEX":
		if len(args) == 4 {
			_ = r.aof.Append([]string{"SET", args[1], args[3]})
			if secs, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				absMs := time.Now().UnixMilli() + secs*1000
				_ = r.aof.Append([]string{"PEXPIREAT", args[1], strconv.FormatInt(absMs, 10)})
			}
		}

	case "PSETEX":
		if len(args) == 4 {
			_ = r.aof.Append([]string{"SET", args[1], args[3]})
			if ms, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				absMs := time.Now().UnixMilli() + ms
				_ = r.aof.Append([]string{"PEXPIREAT", args[1], strconv.FormatInt(absMs, 10)})
			}
		}

	// ── Expiry commands ──────────────────────────────────────────────────
	case "EXPIRE":
		if resp.Integer == 1 && len(args) == 3 {
			if secs, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				absMs := time.Now().UnixMilli() + secs*1000
				_ = r.aof.Append([]string{"PEXPIREAT", args[1], strconv.FormatInt(absMs, 10)})
			}
		}

	case "PEXPIRE":
		if resp.Integer == 1 && len(args) == 3 {
			if ms, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				absMs := time.Now().UnixMilli() + ms
				_ = r.aof.Append([]string{"PEXPIREAT", args[1], strconv.FormatInt(absMs, 10)})
			}
		}

	case "EXPIREAT":
		if resp.Integer == 1 && len(args) == 3 {
			if secs, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				absMs := secs * 1000
				_ = r.aof.Append([]string{"PEXPIREAT", args[1], strconv.FormatInt(absMs, 10)})
			}
		}

	case "PEXPIREAT":
		if resp.Integer == 1 && len(args) == 3 {
			_ = r.aof.Append(args)
		}

	// ── Counter commands ─────────────────────────────────────────────────
	case "INCR", "INCRBY", "DECR", "DECRBY":
		_ = r.aof.Append([]string{"SET", args[1], strconv.FormatInt(resp.Integer, 10)})

	case "INCRBYFLOAT":
		_ = r.aof.Append([]string{"SET", args[1], resp.Str})

	case "HINCRBY":
		if len(args) == 4 {
			_ = r.aof.Append([]string{"HSET", args[1], args[2], strconv.FormatInt(resp.Integer, 10)})
		}

	case "HINCRBYFLOAT":
		if len(args) == 4 {
			_ = r.aof.Append([]string{"HSET", args[1], args[2], resp.Str})
		}

	// ── Rename ───────────────────────────────────────────────────────────
	case "RENAME":
		if len(args) == 3 && args[1] != args[2] {
			dst := args[2]
			switch r.store.Type(dst) {
			case "string":
				if val, ok := r.store.Get(dst); ok {
					_ = r.aof.Append([]string{"SET", dst, val})
				}
			case "hash":
				if all := r.store.HGetAll(dst); len(all) > 0 {
					hsetArgs := make([]string, 0, 2+len(all)*2)
					hsetArgs = append(hsetArgs, "HSET", dst)
					for f, v := range all {
						hsetArgs = append(hsetArgs, f, v)
					}
					_ = r.aof.Append(hsetArgs)
				}
			}
			_ = r.aof.Append([]string{"DEL", args[1]})
		}

	// Read-only and pub/sub commands produce no AOF entries.
	default:
		// intentionally empty
	}
}
