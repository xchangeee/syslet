// Package testutil provides testing utilities for syslet.
package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockUnit represents the state of a mocked systemd unit.
type MockUnit struct {
	ActiveState   string // "active", "inactive", "failed", "activating", "deactivating"
	UnitFileState string // "enabled", "disabled", "static"
	StartCount    int
	StopCount     int
}

// Operation records a D-Bus operation for verification.
type Operation struct {
	Type      string // "reload", "start", "stop", "get_props"
	Unit      string
	Timestamp time.Time
}

// MockDBusConn is a mock implementation of systemd.DBusConn for testing.
type MockDBusConn struct {
	mu         sync.Mutex
	units      map[string]*MockUnit
	operations []Operation
	closed     bool

	// Error injection
	ReloadErr   error
	StartErr    map[string]error
	StopErr     map[string]error
	GetPropsErr map[string]error

	// Callback hooks for additional verification
	OnStart  func(unit string)
	OnStop   func(unit string)
	OnReload func()
}

// NewMockDBusConn creates a new mock D-Bus connection.
func NewMockDBusConn() *MockDBusConn {
	return &MockDBusConn{
		units:       make(map[string]*MockUnit),
		StartErr:    make(map[string]error),
		StopErr:     make(map[string]error),
		GetPropsErr: make(map[string]error),
	}
}

// Close marks the connection as closed.
func (m *MockDBusConn) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// ReloadContext simulates daemon-reload.
func (m *MockDBusConn) ReloadContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.operations = append(m.operations, Operation{
		Type:      "reload",
		Timestamp: time.Now(),
	})

	if m.OnReload != nil {
		m.OnReload()
	}

	return m.ReloadErr
}

// GetUnitPropertiesContext returns mock properties for a unit.
func (m *MockDBusConn) GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.operations = append(m.operations, Operation{
		Type:      "get_props",
		Unit:      unit,
		Timestamp: time.Now(),
	})

	if err := m.GetPropsErr[unit]; err != nil {
		return nil, err
	}

	u, ok := m.units[unit]
	if !ok {
		// Unit doesn't exist - return inactive state
		return map[string]interface{}{
			"ActiveState":   "inactive",
			"UnitFileState": "disabled",
		}, nil
	}

	return map[string]interface{}{
		"ActiveState":   u.ActiveState,
		"UnitFileState": u.UnitFileState,
	}, nil
}

// StartUnitContext simulates starting a unit.
func (m *MockDBusConn) StartUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	m.mu.Lock()

	m.operations = append(m.operations, Operation{
		Type:      "start",
		Unit:      name,
		Timestamp: time.Now(),
	})

	if err := m.StartErr[name]; err != nil {
		m.mu.Unlock()
		ch <- "failed"
		return 0, err
	}

	// Create or update unit state
	u, ok := m.units[name]
	if !ok {
		u = &MockUnit{
			ActiveState:   "inactive",
			UnitFileState: "enabled",
		}
		m.units[name] = u
	}
	u.ActiveState = "active"
	u.StartCount++

	if m.OnStart != nil {
		m.OnStart(name)
	}

	m.mu.Unlock()
	ch <- "done"
	return 1, nil
}

// StopUnitContext simulates stopping a unit.
func (m *MockDBusConn) StopUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	m.mu.Lock()

	m.operations = append(m.operations, Operation{
		Type:      "stop",
		Unit:      name,
		Timestamp: time.Now(),
	})

	if err := m.StopErr[name]; err != nil {
		m.mu.Unlock()
		ch <- "failed"
		return 0, err
	}

	// Update unit state
	u, ok := m.units[name]
	if ok {
		u.ActiveState = "inactive"
		u.StopCount++
	}

	if m.OnStop != nil {
		m.OnStop(name)
	}

	m.mu.Unlock()
	ch <- "done"
	return 1, nil
}

// SetUnitState sets the state of a mock unit, preserving start/stop counts.
func (m *MockDBusConn) SetUnitState(serviceName, activeState, unitFileState string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.units[serviceName]
	if ok {
		// Preserve counts when updating state
		existing.ActiveState = activeState
		existing.UnitFileState = unitFileState
	} else {
		m.units[serviceName] = &MockUnit{
			ActiveState:   activeState,
			UnitFileState: unitFileState,
		}
	}
}

// GetUnit returns the mock unit state for inspection.
func (m *MockDBusConn) GetUnit(serviceName string) *MockUnit {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.units[serviceName]
}

// Operations returns a copy of all recorded operations.
func (m *MockDBusConn) Operations() []Operation {
	m.mu.Lock()
	defer m.mu.Unlock()
	ops := make([]Operation, len(m.operations))
	copy(ops, m.operations)
	return ops
}

// ReloadCount returns the number of daemon-reload calls.
func (m *MockDBusConn) ReloadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for _, op := range m.operations {
		if op.Type == "reload" {
			count++
		}
	}
	return count
}

// StartCount returns the number of start calls for a specific unit.
func (m *MockDBusConn) StartCount(serviceName string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if u := m.units[serviceName]; u != nil {
		return u.StartCount
	}
	return 0
}

// StopCount returns the number of stop calls for a specific unit.
func (m *MockDBusConn) StopCount(serviceName string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if u := m.units[serviceName]; u != nil {
		return u.StopCount
	}
	return 0
}

// Reset clears all state and recorded operations.
func (m *MockDBusConn) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.units = make(map[string]*MockUnit)
	m.operations = nil
	m.ReloadErr = nil
	m.StartErr = make(map[string]error)
	m.StopErr = make(map[string]error)
	m.GetPropsErr = make(map[string]error)
}

// InjectStartError sets an error to be returned when starting a unit.
func (m *MockDBusConn) InjectStartError(serviceName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StartErr[serviceName] = err
}

// InjectStopError sets an error to be returned when stopping a unit.
func (m *MockDBusConn) InjectStopError(serviceName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StopErr[serviceName] = err
}

// InjectReloadError sets an error to be returned on daemon-reload.
func (m *MockDBusConn) InjectReloadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ReloadErr = err
}

// ErrUnitNotFound is returned when a unit doesn't exist.
var ErrUnitNotFound = fmt.Errorf("unit not found")
