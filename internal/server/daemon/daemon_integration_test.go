package daemon_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/testutil"
)

// =============================================================================
// Category E: Reconciliation Loop
// =============================================================================

func TestDaemon_E1_PeriodicReconciliation(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply a spec that will be reconciled
	spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Track reconciliation cycles via start calls
	var startCalls atomic.Int32
	h.DBus.OnStart = func(unit string) {
		startCalls.Add(1)
	}

	// Start daemon in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- h.Daemon.Run(ctx)
	}()

	// Wait for initial reconciliation (runs immediately on start)
	time.Sleep(50 * time.Millisecond)

	initialStarts := startCalls.Load()
	if initialStarts == 0 {
		t.Error("expected initial reconciliation to start the container")
	}

	// Manually stop the unit (simulating drift)
	h.SetUnitStopped("webapp.container")

	// Advance clock by the reconciliation interval
	h.AdvanceTime(10 * time.Second)

	// Give the daemon time to process the tick
	time.Sleep(50 * time.Millisecond)

	// The daemon should have detected drift and restarted the unit
	finalStarts := startCalls.Load()
	if finalStarts <= initialStarts {
		t.Errorf("expected daemon to restart unit after drift, initial=%d, final=%d", initialStarts, finalStarts)
	}

	// Cleanup
	cancel()
	<-done
}

func TestDaemon_E2_ReconciliationDetectsDrift(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Create a running container spec
	spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// First reconciliation - should create and start
	results := h.Reconcile()
	h.AssertNoErrors(results)
	h.AssertUnitStarted("webapp.container")

	// Reset mock state but keep the unit file
	initialStartCount := h.DBus.StartCount("webapp.service")

	// Simulate drift: someone manually stopped the container
	h.SetUnitStopped("webapp.container")

	// Second reconciliation - should detect drift and restart
	results = h.Reconcile()

	// Should have started again
	newStartCount := h.DBus.StartCount("webapp.service")
	if newStartCount <= initialStartCount {
		t.Errorf("expected restart after drift, initial=%d, new=%d", initialStartCount, newStartCount)
	}
}

func TestDaemon_E3_ReconciliationHandlesFailuresGracefully(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply specs
	spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
	spec2 := testutil.ContainerSpec("app2").Image("redis:7").Build()
	if err := h.ApplySpecs(spec1, spec2); err != nil {
		t.Fatalf("ApplySpecs failed: %v", err)
	}

	// First reconciliation - both should be created
	results := h.Reconcile()
	h.AssertNoErrors(results)
	h.AssertUnitStarted("app1.container")
	h.AssertUnitStarted("app2.container")

	// Now inject a reload error
	h.DBus.InjectReloadError(testutil.ErrUnitNotFound)

	// Modify one container to trigger reload
	h.WriteUnitFile("app1.container", `[Container]
Image=nginx:1.24

[Install]
WantedBy=multi-user.target default.target
`)

	spec1Updated := testutil.ContainerSpec("app1").Image("nginx:1.25").Build()
	if err := h.ApplySpec(spec1Updated); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Reconciliation should continue despite reload error
	// (error is logged but doesn't crash)
	results = h.Reconcile()

	// The system should remain functional
	// (specific behavior depends on implementation - we're testing it doesn't panic)
}

func TestDaemon_E4_ImmediateReconcileOnStart(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply a spec before starting daemon
	spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Track start time
	var startTime time.Time
	h.DBus.OnStart = func(unit string) {
		startTime = time.Now()
	}

	// Start daemon
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	daemonStartTime := time.Now()
	go h.Daemon.Run(ctx)

	// Wait a short time
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Reconciliation should happen immediately, not after interval
	if startTime.IsZero() {
		t.Error("expected immediate reconciliation on daemon start")
	}

	// Should happen within 100ms, not after 10s interval
	if startTime.Sub(daemonStartTime) > 100*time.Millisecond {
		t.Error("reconciliation did not happen immediately")
	}
}

func TestDaemon_E5_MultipleReconciliationCycles(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply spec
	spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Track reload count to verify reconciliation runs
	var reloadCount atomic.Int32
	h.DBus.OnReload = func() {
		reloadCount.Add(1)
	}

	// Start daemon
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go h.Daemon.Run(ctx)

	// Wait for initial reconciliation
	time.Sleep(50 * time.Millisecond)

	initialReloads := reloadCount.Load()

	// Advance time through multiple intervals
	for i := 0; i < 3; i++ {
		// Modify spec to force changes
		spec := testutil.ContainerSpec("webapp").
			Image("nginx:latest").
			Environment("CYCLE=" + string(rune('A'+i))).
			Build()
		if err := h.ApplySpec(spec); err != nil {
			t.Fatalf("ApplySpec failed: %v", err)
		}

		h.AdvanceTime(10 * time.Second)
		time.Sleep(50 * time.Millisecond)
	}

	// Should have had multiple reloads
	finalReloads := reloadCount.Load()
	if finalReloads <= initialReloads {
		t.Errorf("expected multiple reconciliation cycles, initial=%d, final=%d",
			initialReloads, finalReloads)
	}

	cancel()
}
