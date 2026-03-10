// Package logger provides a shared structured logger backed by log/slog.
// All packages receive a *slog.Logger — they never call slog globals directly.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New creates a slog.Logger writing JSON to stdout at the given level.
// level must be one of: "debug", "info", "warn", "error".
// An unrecognised level defaults to INFO.
func New(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l})
	return slog.New(handler)
}
