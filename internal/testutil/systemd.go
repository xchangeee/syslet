package testutil

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/systemd"
)

// AssertRestarted checks that exactly one unit was stopped and then started.
func AssertRestarted(t *testing.T, mockConn *systemd.MockDBusConn, unit string) {
	t.Helper()
	if len(mockConn.Stopped) != 1 || mockConn.Stopped[0] != unit {
		t.Errorf("expected %s to be stopped, got: %v", unit, mockConn.Stopped)
	}
	if len(mockConn.Started) != 1 || mockConn.Started[0] != unit {
		t.Errorf("expected %s to be started, got: %v", unit, mockConn.Started)
	}
}

// AssertNoStartStop checks that no units were started or stopped.
func AssertNoStartStop(t *testing.T, mockConn *systemd.MockDBusConn) {
	t.Helper()
	AssertNoneStopped(t, mockConn)
	AssertNoneStarted(t, mockConn)
}

// AssertNoneStarted checks that no units were started.
func AssertNoneStarted(t *testing.T, mockConn *systemd.MockDBusConn) {
	t.Helper()
	if len(mockConn.Started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.Started)
	}
}

// AssertNoneStopped checks that no units were stopped.
func AssertNoneStopped(t *testing.T, mockConn *systemd.MockDBusConn) {
	t.Helper()
	if len(mockConn.Stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.Stopped)
	}
}

// AssertNotStopped checks that the given unit was not stopped.
func AssertNotStopped(t *testing.T, mockConn *systemd.MockDBusConn, unit string) {
	t.Helper()
	for _, s := range mockConn.Stopped {
		if s == unit {
			t.Errorf("expected %s not to be stopped, but it was (stopped: %v)", unit, mockConn.Stopped)
			return
		}
	}
}

// AssertStarted checks that exactly the given units were started (order-independent).
func AssertStarted(t *testing.T, mockConn *systemd.MockDBusConn, units ...string) {
	t.Helper()
	if len(mockConn.Started) != len(units) {
		t.Errorf("expected %d starts %v, got: %v", len(units), units, mockConn.Started)
		return
	}
	startedSet := make(map[string]bool, len(mockConn.Started))
	for _, s := range mockConn.Started {
		startedSet[s] = true
	}
	for _, u := range units {
		if !startedSet[u] {
			t.Errorf("expected %s to be started, got: %v", u, mockConn.Started)
		}
	}
}

// AssertStopped checks that exactly the given units were stopped (order-independent).
func AssertStopped(t *testing.T, mockConn *systemd.MockDBusConn, units ...string) {
	t.Helper()
	if len(mockConn.Stopped) != len(units) {
		t.Errorf("expected %d stops %v, got: %v", len(units), units, mockConn.Stopped)
		return
	}
	stoppedSet := make(map[string]bool, len(mockConn.Stopped))
	for _, s := range mockConn.Stopped {
		stoppedSet[s] = true
	}
	for _, u := range units {
		if !stoppedSet[u] {
			t.Errorf("expected %s to be stopped, got: %v", u, mockConn.Stopped)
		}
	}
}

// AssertReloaded checks that daemon-reload was called.
func AssertReloaded(t *testing.T, mockConn *systemd.MockDBusConn) {
	t.Helper()
	if !mockConn.Reloaded {
		t.Error("expected daemon-reload to be called")
	}
}

// AssertNotReloaded checks that daemon-reload was not called.
func AssertNotReloaded(t *testing.T, mockConn *systemd.MockDBusConn) {
	t.Helper()
	if mockConn.Reloaded {
		t.Error("expected no daemon-reload")
	}
}

// AssertContainerReloaded checks that exactly the given unit was reloaded in-place
// (via ReloadUnitContext, not daemon-reload). Distinct from AssertReloaded which checks
// the systemd daemon-reload flag.
func AssertContainerReloaded(t *testing.T, mockConn *systemd.MockDBusConn, unit string) {
	t.Helper()
	if len(mockConn.ReloadedUnits) != 1 || mockConn.ReloadedUnits[0] != unit {
		t.Errorf("expected container reload of %s, got: %v", unit, mockConn.ReloadedUnits)
	}
}

// AssertContainerNotReloaded checks that no unit was reloaded in-place via ReloadUnitContext.
func AssertContainerNotReloaded(t *testing.T, mockConn *systemd.MockDBusConn) {
	t.Helper()
	if len(mockConn.ReloadedUnits) != 0 {
		t.Errorf("expected no container reload, got: %v", mockConn.ReloadedUnits)
	}
}

// AssertUnitExists checks that a unit file is present on disk.
func AssertUnitExists(t *testing.T, sd *systemd.Client, name string) {
	t.Helper()
	if !sd.UnitFileExists(name) {
		t.Errorf("expected unit file %s to exist", name)
	}
}

// AssertUnitAbsent checks that a unit file is not present on disk.
func AssertUnitAbsent(t *testing.T, sd *systemd.Client, name string) {
	t.Helper()
	if sd.UnitFileExists(name) {
		t.Errorf("expected unit file %s to not exist", name)
	}
}
