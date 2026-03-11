// main.go is the binary entry point for go-redis.
// It wires together all internal components and starts the TCP server.
// No business logic lives here — only dependency injection and startup sequencing.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/hiendvt/go-redis/internal/commands"
	"github.com/hiendvt/go-redis/internal/config"
	"github.com/hiendvt/go-redis/internal/logger"
	"github.com/hiendvt/go-redis/internal/persistence"
	"github.com/hiendvt/go-redis/internal/pubsub"
	"github.com/hiendvt/go-redis/internal/server"
	"github.com/hiendvt/go-redis/internal/storage"
)

func main() {
	// 1. Load configuration from flags / defaults.
	cfg := config.Load()

	// 2. Initialise the structured logger.
	log := logger.New(cfg.LogLevel)

	// 3. Initialise the in-memory store.
	store := storage.NewMemoryStore()

	// 4. AOF persistence: replay existing log, then open for writing.
	var aof commands.Appender
	if cfg.AOFEnabled {
		aof = setupAOF(cfg, log, store)
	}

	// 5. Create the pub/sub broker (shared across all connections).
	broker := pubsub.NewBroker()

	// 6. Build the command router.
	router := commands.NewRouter(store, aof, broker)

	// 7. Create the TCP server.
	srv := server.New(cfg, log, router, broker)

	// Set up a context that is cancelled on SIGINT / SIGTERM so the server
	// can shut down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

// setupAOF replays the existing AOF file (if any) into store, then opens the
// file for appending. Returns nil on any error so the server still starts.
func setupAOF(cfg *config.Config, log *slog.Logger, store *storage.MemoryStore) commands.Appender {
	// Replay existing log to restore state.
	n, err := persistence.Replay(cfg.AOFPath, store)
	if err != nil {
		log.Error("aof replay failed", "err", err)
		// Non-fatal: start with whatever state was replayed.
	} else if n > 0 {
		log.Info("aof replay complete", "commands", n, "keys", store.Len())
	}

	// Open AOF for writing.
	aof, err := persistence.Open(cfg.AOFPath, persistence.SyncPolicy(cfg.AOFSync))
	if err != nil {
		log.Error("aof open failed, persistence disabled", "err", err)
		return nil
	}
	log.Info("aof enabled", "path", cfg.AOFPath, "sync", cfg.AOFSync)
	return aof
}
