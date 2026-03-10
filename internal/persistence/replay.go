package persistence

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
	// This matters when Replay is called after a partial write or crash.
	store.Flush()

	parser := protocol.NewParser(f)
	replayed := 0

	for {
		args, err := parser.ReadCommand()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break // normal end of file
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
// Read-only commands (GET, PING, EXISTS, KEYS) are never written to the AOF
// and should not appear in it.
func applyCommand(args []string, store storage.Store) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	switch strings.ToUpper(args[0]) {
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

	default:
		// Unknown commands in the AOF are a sign of file corruption or a
		// future version writing commands this version cannot handle.
		return fmt.Errorf("unknown command %q", args[0])
	}

	return nil
}
