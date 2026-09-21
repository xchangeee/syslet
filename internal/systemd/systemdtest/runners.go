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
//
// It records the timestamps it was called with, because returning canned
// messages is only half of what the journal is for: the reason syslet reads it
// at all is to surface the quadlet generator's complaints to the operator when
// a daemon-reload fails. A test that only asserts "apply returned an error"
// passes just as well against an apply that never consults the journal, so the
// calls are recorded and asserted on explicitly. The recorded time is the
// reload timestamp syslet passes, which must not predate the reload or the
// query would sweep in unrelated older journal entries.
type MockJournalReader struct {
	Messages []string
	Err      error

	calls []time.Time
}

// QuadletErrorsSince records the call and returns the configured messages.
func (m *MockJournalReader) QuadletErrorsSince(_ context.Context, since time.Time) ([]string, error) {
	m.calls = append(m.calls, since)
	return m.Messages, m.Err
}

// Calls returns the "since" timestamps QuadletErrorsSince was called with, in
// order. Its length is how many times syslet consulted the journal.
func (m *MockJournalReader) Calls() []time.Time {
	return m.calls
}
