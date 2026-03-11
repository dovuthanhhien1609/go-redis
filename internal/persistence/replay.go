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
