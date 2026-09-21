// Package testlog provides the logger used by tests that exercise code paths
// requiring a *slog.Logger. It emits only ERROR-level output, keeping test
// runs quiet while still surfacing genuine failures.
//
// It is deliberately a package of its own rather than part of the integration
// harness: untagged unit tests need it too, and they cannot import a package
// tagged `integration`.
package testlog

import (
	"log/slog"
	"os"
)

// New returns a logger that only emits ERROR-level messages to stderr.
func New() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}
