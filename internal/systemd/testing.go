package systemd

import (
	"context"
	"time"
)

// MockDBusConn is a test helper that implements DBusConn for testing.
// It tracks all operations and allows tests to configure behavior and state.
type MockDBusConn struct {
	UnitStates     map[string]*UnitState
	Reloaded       bool
	Started        []string
	Stopped        []string
	ReloadErr      error
	GetPropsErr    error
	StartErr       error
	StopErr        error
	StartJobResult string
	StopJobResult  string
}

// NewMockDBusConn creates a new mock DBus connection for testing.
func NewMockDBusConn() *MockDBusConn {
	return &MockDBusConn{
		UnitStates:     make(map[string]*UnitState),
		StartJobResult: "done",
		StopJobResult:  "done",
	}
}

func (m *MockDBusConn) Close() {}

func (m *MockDBusConn) ReloadContext(ctx context.Context) error {
	m.Reloaded = true
	return m.ReloadErr
}

func (m *MockDBusConn) GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]interface{}, error) {
	if m.GetPropsErr != nil {
		return nil, m.GetPropsErr
	}
	state, ok := m.UnitStates[unit]
	if !ok {
		return map[string]interface{}{
			"ActiveState":   "inactive",
			"UnitFileState": "disabled",
		}, nil
	}
	unitFileState := "disabled"
	if state.Enabled {
		unitFileState = "enabled"
	}
	return map[string]interface{}{
		"ActiveState":   state.ActiveState,
		"UnitFileState": unitFileState,
	}, nil
}

func (m *MockDBusConn) StartUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	if m.StartErr != nil {
		return 0, m.StartErr
	}
	m.Started = append(m.Started, name)
	if m.UnitStates[name] == nil {
		m.UnitStates[name] = &UnitState{}
	}
	m.UnitStates[name].ActiveState = "active"
	ch <- m.StartJobResult
	return 0, nil
}

func (m *MockDBusConn) StopUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	if m.StopErr != nil {
		return 0, m.StopErr
	}
	m.Stopped = append(m.Stopped, name)
	if m.UnitStates[name] == nil {
		m.UnitStates[name] = &UnitState{}
	}
	m.UnitStates[name].ActiveState = "inactive"
	ch <- m.StopJobResult
	return 0, nil
}

// SetUnitState is a test helper to set the state of a unit.
func (m *MockDBusConn) SetUnitState(serviceName string, activeState string) {
	m.UnitStates[serviceName] = &UnitState{
		ActiveState: activeState,
		Enabled:     true,
	}
}

// MockJournalReader is a test stub for JournalReader.
type MockJournalReader struct {
	Messages []string
	Err      error
}

func (m *MockJournalReader) QuadletErrorsSince(_ context.Context, _ time.Time) ([]string, error) {
	return m.Messages, m.Err
}
