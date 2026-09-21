package systemdtest

import (
	"context"
	"time"

	"codeberg.org/xchangeee/syslet/internal/systemd"
)

// MockAnalyzeRunner is a stub systemd.AnalyzeRunner that returns a fixed
// verification result, letting tests drive the post-render validation path
// without invoking `systemd-analyze verify`.
type MockAnalyzeRunner struct {
	Result systemd.AnalyzeResult
	Err    error
}

// Verify returns the configured result.
func (m *MockAnalyzeRunner) Verify(_ context.Context, _ []string) (systemd.AnalyzeResult, error) {
	return m.Result, m.Err
}

// MockJournalReader is a stub systemd.JournalReader that returns fixed quadlet
// error messages, standing in for the journal that a real daemon-reload would
// have written.
type MockJournalReader struct {
	Messages []string
	Err      error
}

// QuadletErrorsSince returns the configured messages.
func (m *MockJournalReader) QuadletErrorsSince(_ context.Context, _ time.Time) ([]string, error) {
	return m.Messages, m.Err
}
