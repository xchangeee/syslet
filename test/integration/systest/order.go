//go:build integration

package systest

import (
	"fmt"
	"strings"

	"github.com/xchangeee/syslet/test/oplog"
)

// syslet.Apply is a strictly phased executor, and the phases are not an
// implementation detail — each boundary exists to protect something:
//
//   - services stop before their config and unit files are rewritten, so a
//     running container never has its bind-mounted content swapped underneath it;
//   - secret deletes precede secret upserts, so a renamed key never leaves the
//     old and new names present at once;
//   - unit files are written before the daemon-reload, and the daemon-reload
//     precedes every start and in-place reload, so systemd never acts on a unit
//     it has not regenerated.
//
// None of that is observable from any single fake, because each phase leaves
// its trace in a different one. phaseRank flattens the whole sequence onto one
// scale, and assertPhaseOrder checks the shared timeline is non-decreasing
// along it — one comparison that covers every boundary above at once.
//
// The check runs automatically at the end of every Apply, so a test gets it
// without asking and cannot forget it. The cost of that is invisibility: no
// individual test names the invariant it is relying on. TestApplyPhaseOrder in
// apply_order_test.go compensates by exercising a scenario that touches every
// phase and asserting the ordering explicitly, which also keeps this table
// honest if the phases in Apply are ever rearranged.

// phaseRank maps a recorded operation to its position in Apply's phase
// sequence. Operations sharing a rank are unordered with respect to each other.
//
// The content stores — container configFiles, configDirs and build contexts — share
// one rank deliberately. Their order among themselves protects nothing; what
// matters is that all of them land after the stops and before the unit files.
var phaseRank = map[string]int{
	"stop": 1,

	"write-config":  2,
	"delete-config": 2,
	"write-build":   2,
	"delete-build":  2,

	"delete-secret": 3,
	"upsert-secret": 4,

	"write-unit":  5,
	"delete-unit": 5,

	"delete-volume":  6,
	"delete-network": 6,
	"delete-image":   6,

	"daemon-reload": 7,
	"reload-unit":   8,
	"start":         9,
}

// assertPhaseOrder fails the test if the recorded effects are out of phase
// order. It is called by the Apply terminals; tests do not call it directly.
func (e *Env) assertPhaseOrder() {
	e.t.Helper()

	events := e.Log.Events()
	maxRank, maxIdx := 0, -1
	for i, ev := range events {
		rank, known := phaseRank[ev.Op]
		if !known {
			e.t.Errorf("recorded operation %q has no phase rank; add it to phaseRank", ev.Op)
			continue
		}
		if rank < maxRank {
			e.t.Errorf(
				"apply ran out of phase order: %s happened after %s\ntimeline:\n%s",
				describe(events[i]), describe(events[maxIdx]), formatTimeline(events))
			return
		}
		if rank > maxRank {
			maxRank, maxIdx = rank, i
		}
	}
}

// describe renders one event for a failure message.
func describe(ev oplog.Event) string {
	if ev.Target == "" {
		return ev.Op
	}
	return fmt.Sprintf("%s(%s)", ev.Op, ev.Target)
}

// formatTimeline renders the whole recorded sequence, numbered, so a failure
// shows what actually happened rather than only which pair was inverted.
func formatTimeline(events []oplog.Event) string {
	var b strings.Builder
	for i, ev := range events {
		fmt.Fprintf(&b, "  %2d. [%s] %s\n", i+1, ev.Channel, describe(ev))
	}
	return b.String()
}

// AssertHappensBefore checks that the first matching operation on the timeline
// precedes the first matching second operation, naming both by op and target.
//
// The automatic phase check already covers every ordering Apply's phases
// impose; this is for tests that want to state one of those orderings out loud,
// or to pin an ordering between two effects sharing a phase rank.
func (e *Env) AssertHappensBefore(firstOp, firstTarget, secondOp, secondTarget string) {
	e.t.Helper()
	events := e.Log.Events()
	firstIdx := indexOf(events, firstOp, firstTarget)
	secondIdx := indexOf(events, secondOp, secondTarget)
	switch {
	case firstIdx < 0:
		e.t.Errorf("%s never happened\ntimeline:\n%s",
			describe(oplog.Event{Op: firstOp, Target: firstTarget}), formatTimeline(events))
	case secondIdx < 0:
		e.t.Errorf("%s never happened\ntimeline:\n%s",
			describe(oplog.Event{Op: secondOp, Target: secondTarget}), formatTimeline(events))
	case firstIdx > secondIdx:
		e.t.Errorf("expected %s before %s, got the reverse\ntimeline:\n%s",
			describe(oplog.Event{Op: firstOp, Target: firstTarget}),
			describe(oplog.Event{Op: secondOp, Target: secondTarget}),
			formatTimeline(events))
	}
}

// indexOf returns the position of the first event matching op and target, or -1.
// An empty target matches any target, so a caller can name "the daemon-reload"
// without naming a subject it does not have.
func indexOf(events []oplog.Event, op, target string) int {
	for i, ev := range events {
		if ev.Op != op {
			continue
		}
		if target == "" || ev.Target == target || strings.HasSuffix(ev.Target, "/"+target) {
			return i
		}
	}
	return -1
}

// AssertNoEffects checks that the last apply changed nothing at all on the
// host — no service transitions, no writes, no podman mutations.
//
// This is what "converged" means for an apply: a plan that found nothing to do
// must touch nothing. It reads the timeline rather than the individual fakes so
// that an effect on any channel counts, including ones no dedicated assertion
// covers yet.
func (e *Env) AssertNoEffects() {
	e.t.Helper()
	if events := e.Log.Events(); len(events) != 0 {
		e.t.Errorf("expected apply to be a no-op, but it had effects:\n%s", formatTimeline(events))
	}
}
