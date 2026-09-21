// Package testlog provides the logger used by tests that exercise code paths
// requiring a *slog.Logger. It emits only ERROR-level output, keeping test
// runs quiet while still surfacing genuine failures.
//
// It sits under test/ because it is test scaffolding no production code may
// import, but it is deliberately a package of its own rather than part of the
// integration harness, and carries no build tag: untagged unit tests need it
// too, and they cannot import a package tagged `integration`.
package testlog

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// New returns a logger that only emits ERROR-level messages to stderr.
func New() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// Capture is a logger whose output a test can read back.
//
// Some of syslet's behavior is *only* observable through the log: when a
// daemon-reload fails, the quadlet generator's errors reach the operator as log
// records and nowhere else, so a logger writing to stderr makes that path
// untestable. A Capture keeps the records in memory instead, and Contains
// answers whether a given message was surfaced.
//
// It records at DEBUG so that the informational "stopping"/"starting" lines are
// available too, not just failures.
type Capture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewCapture returns a logger writing into the returned Capture.
func NewCapture() (*slog.Logger, *Capture) {
	c := &Capture{}
	return slog.New(slog.NewTextHandler(c, &slog.HandlerOptions{Level: slog.LevelDebug})), c
}

// Write implements io.Writer for the slog handler.
func (c *Capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

// String returns everything logged so far.
func (c *Capture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Contains reports whether any log record holds the given substring.
func (c *Capture) Contains(substr string) bool {
	return strings.Contains(c.String(), substr)
}

// Reset discards everything logged so far, so that one test can log across
// several applies and assert on each separately.
func (c *Capture) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf.Reset()
}
