package testutil

import (
	"log/slog"
	"os"
)

// NewTestLogger returns a logger for tests that only emits ERROR-level messages to stderr.
func NewTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}
