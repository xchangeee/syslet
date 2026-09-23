// Package systemdtest provides the fake systemd implementations used by tests.
//
// It exists so that test scaffolding does not ship inside the production
// internal/systemd package, while still being importable by everything that
// needs it: internal/ unit tests, systemd's own external test package, and the
// tagged integration harness in test/integration/systest. The package carries
// no build tag for exactly that reason — an untagged unit test cannot import a
// package tagged `integration`.
//
// The dependency runs one way only: systemdtest imports systemd for the data
// types the fakes hand back (UnitState, ActiveState, AnalyzeResult); systemd
// must never import systemdtest. The interfaces themselves are satisfied
// structurally, so no import is needed for those.
package systemdtest

import (
	"context"

	"github.com/xchangeee/syslet/internal/systemd"
	"github.com/xchangeee/syslet/test/oplog"
)

// systemdChannel labels this fake's entries on a shared oplog timeline.
const systemdChannel = "systemd"

// MockDBusConn implements systemd.DBusConn in memory. It records every
// operation the client performs — reloads, starts, stops, in-place unit
// reloads — so tests can assert on what the client asked systemd to do, and
// serves unit properties out of UnitStates so tests can stage prior host state.
//
// The per-operation fields below answer "did this happen"; they cannot answer
// "did this happen before that", because each lands in its own bucket. Tests
// that need the relative order of systemd effects — against each other or
// against podman and filesystem effects — set Log to a shared oplog.Log, which
// every operation also appends to in arrival order. Log is optional: untagged
// unit tests leave it nil and a nil Log discards what it is given.
type MockDBusConn struct {
	UnitStates      map[string]*systemd.UnitState
	Reloaded        bool
	Started         []string
	Stopped         []string
	ReloadedUnits   []string
	ReloadErr       error
	GetPropsErr     error
	StartErr        error
	StopErr         error
	StartJobResult  string
	StopJobResult   string
	ReloadJobResult string

	// Log, when set, receives every operation in arrival order alongside the
	// effects recorded by the other fakes sharing it.
	Log *oplog.Log
}

// NewMockDBusConn creates a new mock DBus connection for testing.
func NewMockDBusConn() *MockDBusConn {
	return &MockDBusConn{
		UnitStates:      make(map[string]*systemd.UnitState),
		StartJobResult:  "done",
		StopJobResult:   "done",
		ReloadJobResult: "done",
	}
}

// Close satisfies systemd.DBusConn; the mock holds no resources.
func (m *MockDBusConn) Close() {}

// ReloadContext records that a daemon-reload was requested.
func (m *MockDBusConn) ReloadContext(_ context.Context) error {
	m.Log.Record(systemdChannel, "daemon-reload", "")
	m.Reloaded = true
	return m.ReloadErr
}

// GetUnitPropertiesContext reports the staged state of a unit, defaulting to
// inactive and disabled for units the test never seeded.
func (m *MockDBusConn) GetUnitPropertiesContext(_ context.Context, unit string) (map[string]any, error) {
	if m.GetPropsErr != nil {
		return nil, m.GetPropsErr
	}
	state, ok := m.UnitStates[unit]
	if !ok {
		return map[string]any{
			"ActiveState":   "inactive",
			"UnitFileState": "disabled",
		}, nil
	}
	unitFileState := "disabled"
	if state.Enabled {
		unitFileState = "enabled"
	}
	return map[string]any{
		"ActiveState":   string(state.ActiveState),
		"UnitFileState": unitFileState,
	}, nil
}

// StartUnitContext records the start and marks the unit active.
func (m *MockDBusConn) StartUnitContext(_ context.Context, name string, _ string, ch chan<- string) (int, error) {
	if m.StartErr != nil {
		return 0, m.StartErr
	}
	m.Log.Record(systemdChannel, "start", name)
	m.Started = append(m.Started, name)
	if m.UnitStates[name] == nil {
		m.UnitStates[name] = &systemd.UnitState{}
	}
	m.UnitStates[name].ActiveState = systemd.ActiveStateActive
	ch <- m.StartJobResult
	return 0, nil
}

// StopUnitContext records the stop and marks the unit inactive.
func (m *MockDBusConn) StopUnitContext(_ context.Context, name string, _ string, ch chan<- string) (int, error) {
	if m.StopErr != nil {
		return 0, m.StopErr
	}
	m.Log.Record(systemdChannel, "stop", name)
	m.Stopped = append(m.Stopped, name)
	if m.UnitStates[name] == nil {
		m.UnitStates[name] = &systemd.UnitState{}
	}
	m.UnitStates[name].ActiveState = systemd.ActiveStateInactive
	ch <- m.StopJobResult
	return 0, nil
}

// ReloadUnitContext records an in-place reload of a single unit, as distinct
// from the daemon-reload recorded by ReloadContext.
func (m *MockDBusConn) ReloadUnitContext(_ context.Context, name string, _ string, ch chan<- string) (int, error) {
	m.Log.Record(systemdChannel, "reload-unit", name)
	m.ReloadedUnits = append(m.ReloadedUnits, name)
	ch <- m.ReloadJobResult
	return 0, nil
}

// SetUnitState stages a unit as present and enabled in the given active state,
// simulating a unit already running on the host before the test acts.
func (m *MockDBusConn) SetUnitState(serviceName string, activeState systemd.ActiveState) {
	m.UnitStates[serviceName] = &systemd.UnitState{
		ActiveState: activeState,
		Enabled:     true,
	}
}
