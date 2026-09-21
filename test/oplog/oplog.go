// Package oplog is the shared, ordered record of the side effects a test
// observes syslet performing.
//
// syslet.Apply is a strictly phased executor: it stops services, then writes
// file content, then mutates podman secrets, then writes unit files, then
// reclaims podman resources, then daemon-reloads, then reloads and starts
// services. Each of those effects leaves its trace in a *different* fake — the
// mock D-Bus connection, the fake podman, the filesystem — and a fake that only
// records into its own bucket can prove that an effect happened but never that
// it happened at the right time. Restarting a container before its new config
// file is on disk, or starting a unit before systemd has regenerated it, is
// invisible to per-fake recording and catastrophic on a real host.
//
// A Log is the one timeline all of those fakes append to, so the relative order
// of effects across channels becomes assertable. The integration harness
// resets it before each apply and checks it afterwards; see
// test/integration/systest/order.go for the phase ranking it is checked
// against.
//
// The package sits under test/ because it is pure test scaffolding that no
// production code may import, but it deliberately carries no build tag: its
// producers straddle the tag boundary. systemdtest and podmantest are untagged
// so that untagged unit tests can use them, while the filesystem recorder lives
// in the integration-tagged harness, and a tagged package cannot be imported by
// an untagged one.
package oplog

import "sync"

// Event is one recorded side effect.
//
// Channel names the fake that observed it ("systemd", "podman", "fs"), Op names
// the operation in the vocabulary the phase ranking uses ("stop", "write-unit",
// "daemon-reload"), and Target names what it acted on — a unit name, a secret
// name, a path — or is empty for effects with no single subject, such as a
// daemon-reload.
type Event struct {
	Channel string
	Op      string
	Target  string
}

// Log is an ordered, concurrency-safe list of Events.
//
// The zero value is ready to use, and a nil *Log silently discards everything
// recorded on it: fakes hold a *Log that untagged unit tests leave unset, and
// they must not have to nil-check at every call site.
type Log struct {
	mu     sync.Mutex
	events []Event
}

// Record appends one event. It is a no-op on a nil Log.
func (l *Log) Record(channel, op, target string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, Event{Channel: channel, Op: op, Target: target})
}

// Events returns a copy of the timeline in arrival order.
func (l *Log) Events() []Event {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

// Reset empties the timeline.
//
// The harness calls this immediately before each apply, so that the effects of
// seeding the host — which go through the same stores and the same filesystem
// as apply does — are not mistaken for effects of the apply under test.
func (l *Log) Reset() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = nil
}
