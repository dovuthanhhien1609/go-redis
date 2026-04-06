package persistence

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
	"github.com/hiendvt/go-redis/internal/storage"
)

// Replay reads the AOF file at path and re-executes every command against
// store, reconstructing the in-memory state as it was at the last shutdown.
//
// Commands are applied directly to the store — the Router is intentionally
// bypassed so that replay does not trigger further AOF writes (which would
// loop) and does not require the commands package (avoiding an import cycle).
//
// Returns the number of commands successfully replayed, or an error if the
// file is unreadable or contains an unrecognised command.
// A missing file (os.ErrNotExist) is not an error — it means a fresh start.
func Replay(path string, store storage.Store) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil // no AOF file yet — fresh start
		}
		return 0, fmt.Errorf("aof replay: open %q: %w", path, err)
	}
	defer f.Close()

	// Flush the store before replay so we start from a clean slate.
	store.Flush()

	parser := protocol.NewParser(f)
	replayed := 0

	for {
		args, err := parser.ReadCommand()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return replayed, fmt.Errorf("aof replay: parse error at command %d: %w", replayed+1, err)
		}

		if err := applyCommand(args, store); err != nil {
			return replayed, fmt.Errorf("aof replay: command %d (%q): %w", replayed+1, args[0], err)
		}
		replayed++
	}

	return replayed, nil
}

// applyCommand executes a single replayed command against the store.
// Only mutating commands that were written to the AOF are handled here.
func applyCommand(args []string, store storage.Store) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	switch strings.ToUpper(args[0]) {

	// ── String commands ──────────────────────────────────────────────────
	case "SET":
		if len(args) != 3 {
			return fmt.Errorf("SET requires 2 arguments, got %d", len(args)-1)
		}
		store.Set(args[1], args[2])

	case "DEL":
		if len(args) < 2 {
			return fmt.Errorf("DEL requires at least 1 argument")
		}
		store.Del(args[1:]...)

	case "MSET":
		if len(args) < 3 || len(args)%2 == 0 {
			return fmt.Errorf("MSET requires an even number of key/value pairs")
		}
		for i := 1; i+1 < len(args); i += 2 {
			store.Set(args[i], args[i+1])
		}

	case "APPEND":
		if len(args) != 3 {
			return fmt.Errorf("APPEND requires 2 arguments")
		}
		existing, _ := store.Get(args[1])
		store.Set(args[1], existing+args[2])

	// ── Expiry commands ──────────────────────────────────────────────────
	// PEXPIREAT is the canonical form used by the router when persisting
	// any time-relative expiry command (EXPIRE, SETEX, etc.).
	// It carries an absolute Unix-millisecond timestamp so that the correct
	// remaining TTL is restored even after a restart.
	case "PEXPIREAT":
		if len(args) != 3 {
			return fmt.Errorf("PEXPIREAT requires 2 arguments")
		}
		absMs, err := strconv.ParseInt(args[2], 10, 64)
		if err != nil {
			return fmt.Errorf("PEXPIREAT: invalid timestamp %q: %w", args[2], err)
		}
		remaining := time.Until(time.UnixMilli(absMs))
		if remaining <= 0 {
			// Key expired before (or during) this restart; delete it.
			store.Del(args[1])
		} else {
			store.Expire(args[1], remaining)
		}

	case "PERSIST":
		if len(args) != 2 {
			return fmt.Errorf("PERSIST requires 1 argument")
		}
		store.Persist(args[1])

	// ── Hash commands ────────────────────────────────────────────────────
	case "HSET", "HMSET":
		// HSET key field value [field value ...]
		if len(args) < 4 || (len(args)-2)%2 != 0 {
			return fmt.Errorf("%s requires key + field/value pairs", args[0])
		}
		fields := make(map[string]string, (len(args)-2)/2)
		for i := 2; i+1 < len(args); i += 2 {
			fields[args[i]] = args[i+1]
		}
		store.HSet(args[1], fields)

	case "HDEL":
		if len(args) < 3 {
			return fmt.Errorf("HDEL requires key and at least one field")
		}
		store.HDel(args[1], args[2:]...)

	// ── List commands ────────────────────────────────────────────────────
	case "LPUSH":
		if len(args) < 3 {
			return fmt.Errorf("LPUSH requires key and at least one value")
		}
		store.LPush(args[1], args[2:]...) //nolint:errcheck

	case "RPUSH":
		if len(args) < 3 {
			return fmt.Errorf("RPUSH requires key and at least one value")
		}
		store.RPush(args[1], args[2:]...) //nolint:errcheck

	case "LPUSHX":
		if len(args) < 3 {
			return fmt.Errorf("LPUSHX requires key and at least one value")
		}
		if store.Type(args[1]) == "list" {
			store.LPush(args[1], args[2:]...) //nolint:errcheck
		}

	case "RPUSHX":
		if len(args) < 3 {
			return fmt.Errorf("RPUSHX requires key and at least one value")
		}
		if store.Type(args[1]) == "list" {
			store.RPush(args[1], args[2:]...) //nolint:errcheck
		}

	case "LPOP":
		if len(args) < 2 {
			return fmt.Errorf("LPOP requires at least key")
		}
		count := 1
		if len(args) == 3 {
			if n, err := strconv.Atoi(args[2]); err == nil {
				count = n
			}
		}
		store.LPop(args[1], count) //nolint:errcheck

	case "RPOP":
		if len(args) < 2 {
			return fmt.Errorf("RPOP requires at least key")
		}
		count := 1
		if len(args) == 3 {
			if n, err := strconv.Atoi(args[2]); err == nil {
				count = n
			}
		}
		store.RPop(args[1], count) //nolint:errcheck

	case "LSET":
		if len(args) != 4 {
			return fmt.Errorf("LSET requires key index value")
		}
		idx, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("LSET: invalid index: %w", err)
		}
		store.LSet(args[1], idx, args[3]) //nolint:errcheck

	case "LINSERT":
		if len(args) != 5 {
			return fmt.Errorf("LINSERT requires key BEFORE|AFTER pivot value")
		}
		before := strings.ToUpper(args[2]) == "BEFORE"
		store.LInsert(args[1], before, args[3], args[4]) //nolint:errcheck

	case "LTRIM":
		if len(args) != 4 {
			return fmt.Errorf("LTRIM requires key start stop")
		}
		start, e1 := strconv.Atoi(args[2])
		stop, e2 := strconv.Atoi(args[3])
		if e1 != nil || e2 != nil {
			return fmt.Errorf("LTRIM: invalid range")
		}
		store.LTrim(args[1], start, stop) //nolint:errcheck

	case "LREM":
		if len(args) != 4 {
			return fmt.Errorf("LREM requires key count value")
		}
		count, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("LREM: invalid count: %w", err)
		}
		store.LRem(args[1], count, args[3]) //nolint:errcheck

	case "LMOVE":
		if len(args) != 5 {
			return fmt.Errorf("LMOVE requires src dst srcDir dstDir")
		}
		srcLeft := strings.ToUpper(args[3]) == "LEFT"
		dstLeft := strings.ToUpper(args[4]) == "LEFT"
		store.LMove(args[1], args[2], srcLeft, dstLeft) //nolint:errcheck

	case "RPOPLPUSH":
		if len(args) != 3 {
			return fmt.Errorf("RPOPLPUSH requires src dst")
		}
		store.LMove(args[1], args[2], false, true) //nolint:errcheck

	// ── Set commands ─────────────────────────────────────────────────────
	case "SADD":
		if len(args) < 3 {
			return fmt.Errorf("SADD requires key and at least one member")
		}
		store.SAdd(args[1], args[2:]...) //nolint:errcheck

	case "SREM":
		if len(args) < 3 {
			return fmt.Errorf("SREM requires key and at least one member")
		}
		store.SRem(args[1], args[2:]...) //nolint:errcheck

	case "SINTERSTORE":
		if len(args) < 3 {
			return fmt.Errorf("SINTERSTORE requires dst and at least one key")
		}
		store.SInterStore(args[1], args[2:]...) //nolint:errcheck

	case "SUNIONSTORE":
		if len(args) < 3 {
			return fmt.Errorf("SUNIONSTORE requires dst and at least one key")
		}
		store.SUnionStore(args[1], args[2:]...) //nolint:errcheck

	case "SDIFFSTORE":
		if len(args) < 3 {
			return fmt.Errorf("SDIFFSTORE requires dst and at least one key")
		}
		store.SDiffStore(args[1], args[2:]...) //nolint:errcheck

	case "SMOVE":
		if len(args) != 4 {
			return fmt.Errorf("SMOVE requires src dst member")
		}
		store.SMove(args[1], args[2], args[3]) //nolint:errcheck

	case "SPOP":
		if len(args) < 2 {
			return fmt.Errorf("SPOP requires key")
		}
		count := 1
		if len(args) == 3 {
			if n, err := strconv.Atoi(args[2]); err == nil {
				count = n
			}
		}
		store.SPop(args[1], count) //nolint:errcheck

	// ── Sorted set commands ──────────────────────────────────────────────
	case "ZADD":
		// ZADD key [NX|XX] [GT|LT] [CH] score member [score member ...]
		if len(args) < 4 {
			return fmt.Errorf("ZADD requires key and at least one score/member pair")
		}
		key := args[1]
		var nx, xx, gt, lt, ch bool
		i := 2
		for ; i < len(args); i++ {
			switch strings.ToUpper(args[i]) {
			case "NX":
				nx = true
			case "XX":
				xx = true
			case "GT":
				gt = true
			case "LT":
				lt = true
			case "CH":
				ch = true
			default:
				goto zaddMembers
			}
		}
	zaddMembers:
		if (len(args)-i)%2 != 0 {
			return fmt.Errorf("ZADD: odd number of score/member arguments")
		}
		members := make([]storage.ZMember, 0, (len(args)-i)/2)
		for j := i; j < len(args); j += 2 {
			score, err := strconv.ParseFloat(args[j], 64)
			if err != nil {
				return fmt.Errorf("ZADD: invalid score %q: %w", args[j], err)
			}
			members = append(members, storage.ZMember{Score: score, Member: args[j+1]})
		}
		store.ZAdd(key, members, nx, xx, gt, lt, ch) //nolint:errcheck

	case "ZREM":
		if len(args) < 3 {
			return fmt.Errorf("ZREM requires key and at least one member")
		}
		store.ZRem(args[1], args[2:]...) //nolint:errcheck

	case "ZINCRBY":
		if len(args) != 4 {
			return fmt.Errorf("ZINCRBY requires key increment member")
		}
		incr, err := strconv.ParseFloat(args[2], 64)
		if err != nil {
			return fmt.Errorf("ZINCRBY: invalid increment: %w", err)
		}
		store.ZIncrBy(args[1], incr, args[3]) //nolint:errcheck

	case "ZUNIONSTORE":
		if len(args) < 4 {
			return fmt.Errorf("ZUNIONSTORE requires dst numkeys key [key ...]")
		}
		numkeys, err := strconv.Atoi(args[2])
		if err != nil || numkeys < 1 || len(args) < 3+numkeys {
			return fmt.Errorf("ZUNIONSTORE: invalid arguments")
		}
		store.ZUnionStore(args[1], args[3:3+numkeys], nil, "SUM") //nolint:errcheck

	case "ZINTERSTORE":
		if len(args) < 4 {
			return fmt.Errorf("ZINTERSTORE requires dst numkeys key [key ...]")
		}
		numkeys, err := strconv.Atoi(args[2])
		if err != nil || numkeys < 1 || len(args) < 3+numkeys {
			return fmt.Errorf("ZINTERSTORE: invalid arguments")
		}
		store.ZInterStore(args[1], args[3:3+numkeys], nil, "SUM") //nolint:errcheck

	case "ZPOPMIN":
		if len(args) < 2 {
			return fmt.Errorf("ZPOPMIN requires key")
		}
		count := int64(1)
		if len(args) == 3 {
			if n, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				count = n
			}
		}
		store.ZPopMin(args[1], count) //nolint:errcheck

	case "ZPOPMAX":
		if len(args) < 2 {
			return fmt.Errorf("ZPOPMAX requires key")
		}
		count := int64(1)
		if len(args) == 3 {
			if n, err := strconv.ParseInt(args[2], 10, 64); err == nil {
				count = n
			}
		}
		store.ZPopMax(args[1], count) //nolint:errcheck

	// ── Admin commands ───────────────────────────────────────────────────
	case "FLUSHDB", "FLUSHALL":
		store.Flush()

	default:
		// Unknown commands in the AOF indicate file corruption or a version
		// mismatch. Treat as a hard error so operators notice.
		return fmt.Errorf("unknown command %q in AOF", args[0])
	}

	return nil
}
