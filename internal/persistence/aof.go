// Package persistence implements the Append-Only File (AOF) persistence strategy.
//
// Every mutating command (SET, DEL) is serialized in RESP format and appended
// to a file on disk after it executes successfully in memory. On startup the
// file is replayed top-to-bottom to reconstruct the in-memory state.
//
// File format: back-to-back RESP arrays, one per command.
//
//	*3\r\n$3\r\nSET\r\n$5\r\nhello\r\n$5\r\nworld\r\n
//	*2\r\n$3\r\nDEL\r\n$5\r\nhello\r\n
package persistence

import (
	"bufio"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/hiendvt/go-redis/internal/protocol"
)

// SyncPolicy controls when fsync is called after a write.
type SyncPolicy string

const (
	SyncAlways   SyncPolicy = "always"   // fsync after every Append call
	SyncEverySec SyncPolicy = "everysec" // fsync once per second in background
	SyncNo       SyncPolicy = "no"       // never fsync; let the OS decide
)

// AOF is a concurrent-safe append-only file writer.
// Call Append after every successful mutating command.
// Call Close on shutdown to flush buffers and stop background goroutines.
type AOF struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	policy SyncPolicy
	done   chan struct{} // closed by Close to stop the everysec goroutine
}

// Open opens (or creates) the AOF file at path and returns an AOF ready for
// writing. Existing content is preserved — new entries are appended.
func Open(path string, policy SyncPolicy) (*AOF, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("aof: open %q: %w", path, err)
	}

	a := &AOF{
		file:   f,
		writer: bufio.NewWriterSize(f, 64*1024), // 64 KB write buffer
		policy: policy,
		done:   make(chan struct{}),
	}

	if policy == SyncEverySec {
		go a.syncLoop()
	}

	return a, nil
}

// Append serializes args as a RESP array and appends it to the AOF file.
// It is safe to call from multiple goroutines.
//
// The write is always flushed from the bufio buffer to the OS page cache.
// Whether it is further flushed to disk depends on the SyncPolicy.
func (a *AOF) Append(args []string) error {
	// Build the RESP array from args.
	elems := make([]protocol.Response, len(args))
	for i, arg := range args {
		elems[i] = protocol.BulkString(arg)
	}
	encoded := protocol.Serialize(protocol.Array(elems))

	a.mu.Lock()
	defer a.mu.Unlock()

	// Write to the buffered writer (stays in userspace memory).
	if _, err := a.writer.WriteString(encoded); err != nil {
		return fmt.Errorf("aof: write: %w", err)
	}

	// Flush the buffer to the OS page cache.
	if err := a.writer.Flush(); err != nil {
		return fmt.Errorf("aof: flush: %w", err)
	}

	// For "always" policy: immediately fsync to disk.
	if a.policy == SyncAlways {
		if err := a.file.Sync(); err != nil {
			return fmt.Errorf("aof: sync: %w", err)
		}
	}

	return nil
}

// Close flushes any buffered data, stops background goroutines, and closes
// the underlying file. Must be called on server shutdown.
func (a *AOF) Close() error {
	// Signal the everysec goroutine to stop.
	close(a.done)

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.writer.Flush(); err != nil {
		return fmt.Errorf("aof: close flush: %w", err)
	}
	// Always fsync on close to minimise data loss on orderly shutdown.
	if err := a.file.Sync(); err != nil {
		return fmt.Errorf("aof: close sync: %w", err)
	}
	return a.file.Close()
}

// syncLoop runs as a background goroutine when policy == SyncEverySec.
// It calls fsync once per second to bound the maximum data loss window.
func (a *AOF) syncLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.mu.Lock()
			// Flush buffer first so fsync sees all pending writes.
			_ = a.writer.Flush()
			_ = a.file.Sync()
			a.mu.Unlock()
		case <-a.done:
			return
		}
	}
}
