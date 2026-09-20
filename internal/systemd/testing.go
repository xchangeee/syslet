package systemd

import (
	"context"
	"time"
)

// MockDBusConn is a test helper that implements DBusConn for testing.
// It tracks all operations and allows tests to configure behavior and state.
type MockDBusConn struct {
	UnitStates      map[string]*UnitState
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
}

// NewMockDBusConn creates a new mock DBus connection for testing.
func NewMockDBusConn() *MockDBusConn {
	return &MockDBusConn{
		UnitStates:      make(map[string]*UnitState),
		StartJobResult:  "done",
		StopJobResult:   "done",
		ReloadJobResult: "done",
	}
}

func (m *MockDBusConn) Close() {}

func (m *MockDBusConn) ReloadContext(_ context.Context) error {
	m.Reloaded = true
	return m.ReloadErr
}

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

func (m *MockDBusConn) StartUnitContext(_ context.Context, name string, _ string, ch chan<- string) (int, error) {
	if m.StartErr != nil {
		return 0, m.StartErr
	}
	m.Started = append(m.Started, name)
	if m.UnitStates[name] == nil {
		m.UnitStates[name] = &UnitState{}
	}
	m.UnitStates[name].ActiveState = ActiveStateActive
	ch <- m.StartJobResult
	return 0, nil
}

func (m *MockDBusConn) StopUnitContext(_ context.Context, name string, _ string, ch chan<- string) (int, error) {
	if m.StopErr != nil {
		return 0, m.StopErr
	}
	m.Stopped = append(m.Stopped, name)
	if m.UnitStates[name] == nil {
		m.UnitStates[name] = &UnitState{}
	}
	m.UnitStates[name].ActiveState = ActiveStateInactive
	ch <- m.StopJobResult
	return 0, nil
}

func (m *MockDBusConn) ReloadUnitContext(_ context.Context, name string, _ string, ch chan<- string) (int, error) {
	m.ReloadedUnits = append(m.ReloadedUnits, name)
	ch <- m.ReloadJobResult
	return 0, nil
}

// SetUnitState is a test helper to set the state of a unit.
func (m *MockDBusConn) SetUnitState(serviceName string, activeState ActiveState) {
	m.UnitStates[serviceName] = &UnitState{
		ActiveState: activeState,
		Enabled:     true,
	}
}

// MockQuadletGeneratorRunner is a test stub for QuadletGeneratorRunner.
type MockQuadletGeneratorRunner struct {
	GenerateCode int
	GenerateErr  error
}

func (m *MockQuadletGeneratorRunner) Run(_ context.Context, _, _, _, _ string) (int, error) {
	return m.GenerateCode, m.GenerateErr
}

// MockSystemdAnalyzeRunner is a test stub for SystemdAnalyzeRunner.
type MockSystemdAnalyzeRunner struct {
	Result AnalyzeResult
	Err    error
}

func (m *MockSystemdAnalyzeRunner) Verify(_ context.Context, _ []string) (AnalyzeResult, error) {
	return m.Result, m.Err
}

// MockJournalReader is a test stub for JournalReader.
type MockJournalReader struct {
	Messages []string
	Err      error
}

func (m *MockJournalReader) QuadletErrorsSince(_ context.Context, _ time.Time) ([]string, error) {
	return m.Messages, m.Err
}
