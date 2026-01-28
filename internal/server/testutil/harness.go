package testutil

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/api"
	"codeberg.org/xchangeee/syslet/internal/server/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/server/daemon"
	"codeberg.org/xchangeee/syslet/internal/server/store"
	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	"github.com/coder/quartz"
	"github.com/spf13/afero"
)

const (
	TestQuadletDir = "/etc/containers/systemd"
	TestConfigBase = "/var/syslet/config"
	TestDBPath     = ":memory:"
)

// Harness provides a complete test environment for syslet integration tests.
type Harness struct {
	T *testing.T

	// Components
	Daemon     *daemon.Daemon
	Reconciler *daemon.Reconciler
	Store      *store.Store
	Systemd    *systemd.Client
	Config     *containerconfig.Manager

	// Mocks
	FS    afero.Fs
	DBus  *MockDBusConn
	Clock *quartz.Mock

	// Internals
	logger *slog.Logger
	ctx    context.Context
	cancel context.CancelFunc
}

// NewHarness creates a new test harness with all components wired together.
func NewHarness(t *testing.T) *Harness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	// Create mocks
	fs := afero.NewMemMapFs()
	dbus := NewMockDBusConn()
	clock := quartz.NewMock(t)

	// Create components with mocked dependencies
	systemdClient := systemd.NewClientWithPaths(dbus, fs, TestQuadletDir)
	configManager := containerconfig.NewManagerWithPaths(fs, TestConfigBase)

	// Use a real SQLite in-memory database
	st, err := store.New(fs, TestDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Silent logger for tests (can be replaced with t.Log based logger)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create daemon
	d := daemon.New(systemdClient, configManager, st, logger, daemon.Config{
		ConfigBase: TestConfigBase,
		Interval:   10 * time.Second,
		Clock:      clock,
	})

	h := &Harness{
		T:          t,
		Daemon:     d,
		Store:      st,
		Systemd:    systemdClient,
		Config:     configManager,
		FS:         fs,
		DBus:       dbus,
		Clock:      clock,
		logger:     logger,
		ctx:        ctx,
		cancel:     cancel,
	}

	t.Cleanup(func() {
		cancel()
		st.Close()
	})

	return h
}

// Context returns the test context.
func (h *Harness) Context() context.Context {
	return h.ctx
}

// ApplySpec applies a JSON spec via the daemon.
func (h *Harness) ApplySpec(specJSON string) error {
	return h.Daemon.ApplySpecs(h.ctx, []string{specJSON})
}

// ApplySpecs applies multiple JSON specs.
func (h *Harness) ApplySpecs(specJSONs ...string) error {
	return h.Daemon.ApplySpecs(h.ctx, specJSONs)
}

// Reconcile runs a single reconciliation cycle and returns results.
func (h *Harness) Reconcile() []daemon.UnitResult {
	h.T.Helper()

	// Load units from store and run reconciliation
	units := h.loadUnitsFromStore()
	if len(units) == 0 {
		return nil
	}

	reconciler := daemon.NewReconciler(h.Systemd, h.Config, h.logger)

	plan, err := reconciler.Diff(h.ctx, units)
	if err != nil {
		h.T.Fatalf("Diff failed: %v", err)
	}

	return reconciler.Execute(h.ctx, plan)
}

func (h *Harness) loadUnitsFromStore() []daemon.UnitWithConfigs {
	h.T.Helper()

	specJSONs, err := h.Store.List()
	if err != nil {
		h.T.Fatalf("failed to list specs: %v", err)
	}

	var units []daemon.UnitWithConfigs
	for _, j := range specJSONs {
		pu, cfgFiles, err := ParseSpec(j, TestConfigBase)
		if err != nil {
			h.T.Fatalf("failed to parse spec: %v", err)
		}
		units = append(units, daemon.UnitWithConfigs{Unit: pu, Configs: cfgFiles})
	}
	return units
}

// AdvanceTime advances the mock clock by the given duration.
func (h *Harness) AdvanceTime(d time.Duration) {
	h.Clock.Advance(d)
}

// SetUnitRunning sets a unit as running in the mock D-Bus.
func (h *Harness) SetUnitRunning(fullUnitName string) {
	serviceName := unitNameToService(fullUnitName)
	h.DBus.SetUnitState(serviceName, "active", "enabled")
}

// SetUnitStopped sets a unit as stopped in the mock D-Bus.
func (h *Harness) SetUnitStopped(fullUnitName string) {
	serviceName := unitNameToService(fullUnitName)
	h.DBus.SetUnitState(serviceName, "inactive", "enabled")
}

// SetUnitFailed sets a unit as failed in the mock D-Bus.
func (h *Harness) SetUnitFailed(fullUnitName string) {
	serviceName := unitNameToService(fullUnitName)
	h.DBus.SetUnitState(serviceName, "failed", "enabled")
}

// unitNameToService converts a quadlet name to a systemd service name.
// e.g., "webapp.container" → "webapp.service"
func unitNameToService(fullUnitName string) string {
	ext := filepath.Ext(fullUnitName)
	base := strings.TrimSuffix(fullUnitName, ext)
	return base + ".service"
}

// --- Assertion Helpers ---

// AssertUnitFileExists asserts that a unit file exists in the mock filesystem.
func (h *Harness) AssertUnitFileExists(fullUnitName string) {
	h.T.Helper()
	path := filepath.Join(TestQuadletDir, fullUnitName)
	_, err := h.FS.Stat(path)
	if err != nil {
		h.T.Errorf("expected unit file %s to exist, but got error: %v", fullUnitName, err)
	}
}

// AssertUnitFileNotExists asserts that a unit file does not exist.
func (h *Harness) AssertUnitFileNotExists(fullUnitName string) {
	h.T.Helper()
	path := filepath.Join(TestQuadletDir, fullUnitName)
	_, err := h.FS.Stat(path)
	if err == nil {
		h.T.Errorf("expected unit file %s to not exist, but it does", fullUnitName)
	}
}

// AssertUnitFileContains asserts that a unit file contains the given substrings.
func (h *Harness) AssertUnitFileContains(fullUnitName string, substrings ...string) {
	h.T.Helper()
	path := filepath.Join(TestQuadletDir, fullUnitName)
	content, err := afero.ReadFile(h.FS, path)
	if err != nil {
		h.T.Fatalf("failed to read unit file %s: %v", fullUnitName, err)
	}

	for _, substr := range substrings {
		if !strings.Contains(string(content), substr) {
			h.T.Errorf("unit file %s does not contain %q\nContent:\n%s", fullUnitName, substr, string(content))
		}
	}
}

// AssertConfigFileExists asserts that a config file exists for a unit.
func (h *Harness) AssertConfigFileExists(unitName, filename string) {
	h.T.Helper()
	path := filepath.Join(TestConfigBase, unitName, filename)
	_, err := h.FS.Stat(path)
	if err != nil {
		h.T.Errorf("expected config file %s/%s to exist, but got error: %v", unitName, filename, err)
	}
}

// AssertConfigFileNotExists asserts that a config file does not exist.
func (h *Harness) AssertConfigFileNotExists(unitName, filename string) {
	h.T.Helper()
	path := filepath.Join(TestConfigBase, unitName, filename)
	_, err := h.FS.Stat(path)
	if err == nil {
		h.T.Errorf("expected config file %s/%s to not exist, but it does", unitName, filename)
	}
}

// AssertConfigFileContent asserts that a config file has the exact content.
func (h *Harness) AssertConfigFileContent(unitName, filename, expected string) {
	h.T.Helper()
	path := filepath.Join(TestConfigBase, unitName, filename)
	content, err := afero.ReadFile(h.FS, path)
	if err != nil {
		h.T.Fatalf("failed to read config file %s/%s: %v", unitName, filename, err)
	}

	if string(content) != expected {
		h.T.Errorf("config file %s/%s content mismatch\nexpected: %q\ngot: %q", unitName, filename, expected, string(content))
	}
}

// AssertUnitStarted asserts that a unit was started via D-Bus.
func (h *Harness) AssertUnitStarted(fullUnitName string) {
	h.T.Helper()
	serviceName := unitNameToService(fullUnitName)
	if h.DBus.StartCount(serviceName) == 0 {
		h.T.Errorf("expected unit %s to be started, but StartCount is 0", fullUnitName)
	}
}

// AssertUnitNotStarted asserts that a unit was not started.
func (h *Harness) AssertUnitNotStarted(fullUnitName string) {
	h.T.Helper()
	serviceName := unitNameToService(fullUnitName)
	if h.DBus.StartCount(serviceName) > 0 {
		h.T.Errorf("expected unit %s to not be started, but StartCount is %d", fullUnitName, h.DBus.StartCount(serviceName))
	}
}

// AssertUnitStopped asserts that a unit was stopped via D-Bus.
func (h *Harness) AssertUnitStopped(fullUnitName string) {
	h.T.Helper()
	serviceName := unitNameToService(fullUnitName)
	if h.DBus.StopCount(serviceName) == 0 {
		h.T.Errorf("expected unit %s to be stopped, but StopCount is 0", fullUnitName)
	}
}

// AssertUnitNotStopped asserts that a unit was not stopped.
func (h *Harness) AssertUnitNotStopped(fullUnitName string) {
	h.T.Helper()
	serviceName := unitNameToService(fullUnitName)
	if h.DBus.StopCount(serviceName) > 0 {
		h.T.Errorf("expected unit %s to not be stopped, but StopCount is %d", fullUnitName, h.DBus.StopCount(serviceName))
	}
}

// AssertDaemonReloaded asserts that daemon-reload was called exactly n times.
func (h *Harness) AssertDaemonReloaded(times int) {
	h.T.Helper()
	actual := h.DBus.ReloadCount()
	if actual != times {
		h.T.Errorf("expected daemon-reload to be called %d times, got %d", times, actual)
	}
}

// AssertNoErrors asserts that no results have errors.
func (h *Harness) AssertNoErrors(results []daemon.UnitResult) {
	h.T.Helper()
	for _, r := range results {
		if r.Error {
			h.T.Errorf("unexpected error for unit %s: %s", r.FullName, r.Message)
		}
	}
}

// AssertResultError asserts that a specific unit has an error containing the substring.
func (h *Harness) AssertResultError(results []daemon.UnitResult, fullUnitName, errorContains string) {
	h.T.Helper()
	for _, r := range results {
		if r.FullName == fullUnitName {
			if !r.Error {
				h.T.Errorf("expected error for unit %s, but got none", fullUnitName)
				return
			}
			if !strings.Contains(r.Message, errorContains) {
				h.T.Errorf("error message for %s does not contain %q: %s", fullUnitName, errorContains, r.Message)
			}
			return
		}
	}
	h.T.Errorf("unit %s not found in results", fullUnitName)
}

// AssertResultChanged asserts that a unit was marked as changed.
func (h *Harness) AssertResultChanged(results []daemon.UnitResult, fullUnitName string) {
	h.T.Helper()
	for _, r := range results {
		if r.FullName == fullUnitName {
			if !r.Changed {
				h.T.Errorf("expected unit %s to be changed, but Changed=false", fullUnitName)
			}
			return
		}
	}
	h.T.Errorf("unit %s not found in results", fullUnitName)
}

// AssertResultUnchanged asserts that a unit was not changed.
func (h *Harness) AssertResultUnchanged(results []daemon.UnitResult, fullUnitName string) {
	h.T.Helper()
	for _, r := range results {
		if r.FullName == fullUnitName {
			if r.Changed {
				h.T.Errorf("expected unit %s to be unchanged, but Changed=true (message: %s)", fullUnitName, r.Message)
			}
			return
		}
	}
	h.T.Errorf("unit %s not found in results", fullUnitName)
}

// GetOperations returns recorded D-Bus operations for order verification.
func (h *Harness) GetOperations() []Operation {
	return h.DBus.Operations()
}

// AssertOperationOrder asserts that operations occurred in the specified order.
// Each element should be "type:unit" or just "type" for reload.
func (h *Harness) AssertOperationOrder(expectedOrder ...string) {
	h.T.Helper()

	ops := h.DBus.Operations()

	// Filter to only the operation types we care about (exclude get_props)
	var relevant []string
	for _, op := range ops {
		if op.Type == "get_props" {
			continue
		}
		if op.Unit != "" {
			relevant = append(relevant, op.Type+":"+op.Unit)
		} else {
			relevant = append(relevant, op.Type)
		}
	}

	// Check that expected operations appear in order
	idx := 0
	for _, exp := range expectedOrder {
		found := false
		for ; idx < len(relevant); idx++ {
			if relevant[idx] == exp {
				found = true
				idx++
				break
			}
		}
		if !found {
			h.T.Errorf("expected operation %q not found in order\nexpected: %v\ngot: %v", exp, expectedOrder, relevant)
			return
		}
	}
}

// Reset clears all state for a fresh test.
func (h *Harness) Reset() {
	h.DBus.Reset()
	// Note: Can't easily reset afero.MemMapFs, so create new harness for truly fresh state
}

// WriteUnitFile writes a unit file directly to the mock filesystem (for setup).
func (h *Harness) WriteUnitFile(fullUnitName, content string) {
	h.T.Helper()
	path := filepath.Join(TestQuadletDir, fullUnitName)
	if err := h.FS.MkdirAll(TestQuadletDir, 0755); err != nil {
		h.T.Fatalf("failed to create quadlet dir: %v", err)
	}
	if err := afero.WriteFile(h.FS, path, []byte(content), 0644); err != nil {
		h.T.Fatalf("failed to write unit file: %v", err)
	}
}

// WriteConfigFile writes a config file directly to the mock filesystem (for setup).
func (h *Harness) WriteConfigFile(unitName, filename, content string) {
	h.T.Helper()
	dir := filepath.Join(TestConfigBase, unitName)
	if err := h.FS.MkdirAll(dir, 0755); err != nil {
		h.T.Fatalf("failed to create config dir: %v", err)
	}
	path := filepath.Join(dir, filename)
	if err := afero.WriteFile(h.FS, path, []byte(content), 0644); err != nil {
		h.T.Fatalf("failed to write config file: %v", err)
	}
}

// ReadUnitFile reads a unit file from the mock filesystem.
func (h *Harness) ReadUnitFile(fullUnitName string) string {
	h.T.Helper()
	path := filepath.Join(TestQuadletDir, fullUnitName)
	content, err := afero.ReadFile(h.FS, path)
	if err != nil {
		h.T.Fatalf("failed to read unit file %s: %v", fullUnitName, err)
	}
	return string(content)
}

// APIServer returns a new API server instance for testing.
func (h *Harness) APIServer() *api.Server {
	return api.NewServer(h.Daemon, h.logger)
}
