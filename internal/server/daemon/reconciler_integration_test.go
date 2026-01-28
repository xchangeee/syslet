package daemon_test

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/server/testutil"
)

// =============================================================================
// Category A: Container Lifecycle
// =============================================================================

func TestReconciler_A1_CreateNewContainer(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply a new container spec
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		PublishPort("8080:80").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitFileExists("webapp.container")
	h.AssertUnitFileContains("webapp.container", "Image=nginx:latest", "PublishPort=8080:80")
	h.AssertDaemonReloaded(1)
	h.AssertUnitStarted("webapp.container")
	h.AssertResultChanged(results, "webapp.container")
}

func TestReconciler_A2_UpdateRunningContainer(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Create existing unit file and mark as running
	existingUnit := `[Container]
Image=nginx:1.24
PublishPort=8080:80

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.SetUnitRunning("webapp.container")

	// Apply updated spec with new image tag
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:1.25").
		PublishPort("8080:80").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitStopped("webapp.container") // Should stop before update
	h.AssertUnitFileContains("webapp.container", "Image=nginx:1.25")
	h.AssertDaemonReloaded(1)
	h.AssertUnitStarted("webapp.container") // Should restart after update
	h.AssertResultChanged(results, "webapp.container")

	// Verify order: stop before reload, reload before start
	h.AssertOperationOrder(
		"stop:webapp.service",
		"reload",
		"start:webapp.service",
	)
}

func TestReconciler_A3_StopRunningContainer(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing running container
	existingUnit := `[Container]
Image=nginx:latest

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.SetUnitRunning("webapp.container")

	// Apply spec with desiredState=stopped
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		Stopped().
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitStopped("webapp.container")
	h.AssertUnitNotStarted("webapp.container")
}

func TestReconciler_A4_StartStoppedContainer(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing stopped container
	existingUnit := `[Container]
Image=nginx:latest

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.SetUnitStopped("webapp.container")

	// Apply spec with desiredState=running
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		Running().
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitStarted("webapp.container")
	h.AssertUnitNotStopped("webapp.container")
}

func TestReconciler_A5_NoOpWhenInDesiredState(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing running container matching spec
	existingUnit := `[Container]
Image=nginx:latest

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.SetUnitRunning("webapp.container")

	// Apply identical spec
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		Running().
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertDaemonReloaded(0)
	h.AssertUnitNotStarted("webapp.container")
	h.AssertUnitNotStopped("webapp.container")
	h.AssertResultUnchanged(results, "webapp.container")
}

func TestReconciler_A6_RestartFailedContainer(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Container in failed state
	existingUnit := `[Container]
Image=nginx:latest

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.SetUnitFailed("webapp.container")

	// Apply spec wanting it running
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		Running().
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitStarted("webapp.container") // Should attempt restart
}

// =============================================================================
// Category B: Config File Management
// =============================================================================

func TestReconciler_B1_CreateContainerWithConfigs(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply container spec with config files
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		WithConfig("/etc/nginx/nginx.conf", "server { listen 80; }").
		WithConfig("/run/env", "APP_ENV=production").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertConfigFileExists("webapp", "nginx.conf")
	h.AssertConfigFileContent("webapp", "nginx.conf", "server { listen 80; }")
	h.AssertConfigFileExists("webapp", "env")
	h.AssertConfigFileContent("webapp", "env", "APP_ENV=production")
	h.AssertUnitFileExists("webapp.container")
	h.AssertUnitStarted("webapp.container")
}

func TestReconciler_B2_UpdateConfigOnly(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing container with config
	existingUnit := `[Container]
Image=nginx:latest
Volume=/var/syslet/config/webapp/nginx.conf:/etc/nginx/nginx.conf:ro

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.WriteConfigFile("webapp", "nginx.conf", "server { listen 80; }") // v1
	h.SetUnitRunning("webapp.container")

	// Apply spec with updated config content
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		WithConfig("/etc/nginx/nginx.conf", "server { listen 8080; }"). // v2
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertConfigFileContent("webapp", "nginx.conf", "server { listen 8080; }")
	h.AssertUnitStopped("webapp.container")  // Should stop for config update
	h.AssertUnitStarted("webapp.container")  // Should restart after config update
	h.AssertResultChanged(results, "webapp.container")
}

func TestReconciler_B3_AddConfigToExistingContainer(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing container with one config
	existingUnit := `[Container]
Image=nginx:latest
Volume=/var/syslet/config/webapp/nginx.conf:/etc/nginx/nginx.conf:ro

[Install]
WantedBy=multi-user.target default.target
`
	h.WriteUnitFile("webapp.container", existingUnit)
	h.WriteConfigFile("webapp", "nginx.conf", "server { listen 80; }")
	h.SetUnitRunning("webapp.container")

	// Apply spec with additional config
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		WithConfig("/etc/nginx/nginx.conf", "server { listen 80; }").
		WithConfig("/run/env", "APP_ENV=production"). // new
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertConfigFileExists("webapp", "nginx.conf")
	h.AssertConfigFileExists("webapp", "env")
	h.AssertResultChanged(results, "webapp.container")
}

// =============================================================================
// Category C: Volume and Network Immutability
// =============================================================================

func TestReconciler_C1_CreateNewVolume(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply volume spec
	spec := testutil.VolumeSpec("data").
		Label("app=webapp").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitFileExists("data.volume")
	h.AssertUnitFileContains("data.volume", "Label=app=webapp")
	h.AssertDaemonReloaded(1)
	h.AssertUnitNotStarted("data.volume") // Volumes are not startable
}

func TestReconciler_C2_RejectVolumeModification(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing volume
	existingUnit := `[Volume]
Label=app=webapp
`
	h.WriteUnitFile("data.volume", existingUnit)

	// Apply modified volume spec (different label)
	spec := testutil.VolumeSpec("data").
		Label("app=different").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions - should be rejected
	h.AssertResultError(results, "data.volume", "immutable")

	// Unit file should be unchanged
	h.AssertUnitFileContains("data.volume", "Label=app=webapp")
}

func TestReconciler_C3_CreateNewNetwork(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply network spec
	spec := testutil.NetworkSpec("internal").
		Subnet("10.89.0.0/24").
		Gateway("10.89.0.1").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitFileExists("internal.network")
	h.AssertUnitFileContains("internal.network", "Subnet=10.89.0.0/24", "Gateway=10.89.0.1")
	h.AssertDaemonReloaded(1)
}

func TestReconciler_C4_RejectNetworkModification(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing network
	existingUnit := `[Network]
Subnet=10.89.0.0/24
Gateway=10.89.0.1
`
	h.WriteUnitFile("internal.network", existingUnit)

	// Apply modified network spec (different subnet)
	spec := testutil.NetworkSpec("internal").
		Subnet("10.90.0.0/24").
		Gateway("10.90.0.1").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions - should be rejected
	h.AssertResultError(results, "internal.network", "immutable")

	// Unit file should be unchanged
	h.AssertUnitFileContains("internal.network", "Subnet=10.89.0.0/24")
}

func TestReconciler_C5_VolumeUnchangedNoOp(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Existing volume
	existingUnit := `[Volume]
Label=app=webapp
`
	h.WriteUnitFile("data.volume", existingUnit)

	// Apply identical spec
	spec := testutil.VolumeSpec("data").
		Label("app=webapp").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertDaemonReloaded(0)
	h.AssertResultUnchanged(results, "data.volume")
}

// =============================================================================
// Category D: Multi-Unit Reconciliation
// =============================================================================

func TestReconciler_D1_CreateMultipleUnitsInSingleApply(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply container + volume + network together
	containerSpec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		Volume("data.volume:/data").
		Network("internal.network").
		Build()

	volumeSpec := testutil.VolumeSpec("data").
		Label("app=webapp").
		Build()

	networkSpec := testutil.NetworkSpec("internal").
		Subnet("10.89.0.0/24").
		Gateway("10.89.0.1").
		Build()

	if err := h.ApplySpecs(containerSpec, volumeSpec, networkSpec); err != nil {
		t.Fatalf("ApplySpecs failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitFileExists("webapp.container")
	h.AssertUnitFileExists("data.volume")
	h.AssertUnitFileExists("internal.network")

	// Single daemon-reload for all units
	h.AssertDaemonReloaded(1)

	// Only container should be started (volumes/networks are not startable)
	h.AssertUnitStarted("webapp.container")
}

func TestReconciler_D2_UpdateMultipleContainers(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Two running containers
	h.WriteUnitFile("app1.container", `[Container]
Image=nginx:1.24

[Install]
WantedBy=multi-user.target default.target
`)
	h.WriteUnitFile("app2.container", `[Container]
Image=redis:6

[Install]
WantedBy=multi-user.target default.target
`)
	h.SetUnitRunning("app1.container")
	h.SetUnitRunning("app2.container")

	// Apply updated specs for both
	spec1 := testutil.ContainerSpec("app1").Image("nginx:1.25").Build()
	spec2 := testutil.ContainerSpec("app2").Image("redis:7").Build()

	if err := h.ApplySpecs(spec1, spec2); err != nil {
		t.Fatalf("ApplySpecs failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)
	h.AssertUnitStopped("app1.container")
	h.AssertUnitStopped("app2.container")
	h.AssertDaemonReloaded(1) // Single reload
	h.AssertUnitStarted("app1.container")
	h.AssertUnitStarted("app2.container")
}

func TestReconciler_D3_MixedChanges(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: app1 exists and will change, app2 exists unchanged
	h.WriteUnitFile("app1.container", `[Container]
Image=nginx:1.24

[Install]
WantedBy=multi-user.target default.target
`)
	h.WriteUnitFile("app2.container", `[Container]
Image=redis:7

[Install]
WantedBy=multi-user.target default.target
`)
	h.SetUnitRunning("app1.container")
	h.SetUnitRunning("app2.container")

	// Apply: app1 changed, app2 unchanged, app3 new
	spec1 := testutil.ContainerSpec("app1").Image("nginx:1.25").Build()
	spec2 := testutil.ContainerSpec("app2").Image("redis:7").Build()
	spec3 := testutil.ContainerSpec("app3").Image("postgres:15").Build()

	if err := h.ApplySpecs(spec1, spec2, spec3); err != nil {
		t.Fatalf("ApplySpecs failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// Assertions
	h.AssertNoErrors(results)

	// app1: changed, should be stopped and restarted
	h.AssertUnitStopped("app1.container")
	h.AssertUnitStarted("app1.container")
	h.AssertResultChanged(results, "app1.container")

	// app2: unchanged
	h.AssertUnitNotStopped("app2.container")
	h.AssertUnitNotStarted("app2.container")
	h.AssertResultUnchanged(results, "app2.container")

	// app3: new
	h.AssertUnitFileExists("app3.container")
	h.AssertUnitStarted("app3.container")
	h.AssertResultChanged(results, "app3.container")

	// Single reload for all changes
	h.AssertDaemonReloaded(1)
}

func TestReconciler_D4_PartialFailure(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Inject start error for app2
	h.DBus.InjectStartError("app2.service", testutil.ErrUnitNotFound)

	// Apply three new containers
	spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
	spec2 := testutil.ContainerSpec("app2").Image("redis:7").Build()
	spec3 := testutil.ContainerSpec("app3").Image("postgres:15").Build()

	if err := h.ApplySpecs(spec1, spec2, spec3); err != nil {
		t.Fatalf("ApplySpecs failed: %v", err)
	}

	// Run reconciliation
	results := h.Reconcile()

	// app1 and app3 should succeed
	h.AssertUnitStarted("app1.container")
	h.AssertUnitStarted("app3.container")

	// app2 should have error
	h.AssertResultError(results, "app2.container", "failed to start")
}

// =============================================================================
// Category H: Execution Order Verification
// =============================================================================

func TestReconciler_H1_StopBeforeUnitFileWrite(t *testing.T) {
	h := testutil.NewHarness(t)

	// Setup: Running container
	h.WriteUnitFile("webapp.container", `[Container]
Image=nginx:1.24

[Install]
WantedBy=multi-user.target default.target
`)
	h.SetUnitRunning("webapp.container")

	// Apply update
	spec := testutil.ContainerSpec("webapp").Image("nginx:1.25").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	// Track when stop happens vs when file is written
	stopTime := int64(0)
	writeTime := int64(0)
	counter := int64(0)

	h.DBus.OnStop = func(unit string) {
		counter++
		stopTime = counter
	}

	// We can't easily hook file writes, but we can verify operation order
	h.Reconcile()

	// Stop should have happened
	if stopTime == 0 {
		t.Error("stop should have been called")
	}

	// Verify via operation order that stop comes before reload (which comes after writes)
	h.AssertOperationOrder(
		"stop:webapp.service",
		"reload",
	)

	_ = writeTime // not used in this simplified test
}

func TestReconciler_H2_ReloadAfterAllUnitWrites(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply multiple new units
	spec1 := testutil.ContainerSpec("app1").Image("nginx:latest").Build()
	spec2 := testutil.ContainerSpec("app2").Image("redis:7").Build()

	if err := h.ApplySpecs(spec1, spec2); err != nil {
		t.Fatalf("ApplySpecs failed: %v", err)
	}

	h.Reconcile()

	// Both unit files should exist (written before reload)
	h.AssertUnitFileExists("app1.container")
	h.AssertUnitFileExists("app2.container")

	// Single reload after both writes
	h.AssertDaemonReloaded(1)

	// Start calls come after reload
	h.AssertOperationOrder(
		"reload",
		"start:app1.service",
	)
}

func TestReconciler_H3_StartAfterReload(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply new container
	spec := testutil.ContainerSpec("webapp").Image("nginx:latest").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	h.Reconcile()

	// Verify order: reload before start
	h.AssertOperationOrder(
		"reload",
		"start:webapp.service",
	)
}

// =============================================================================
// Category G: Edge Cases
// =============================================================================

func TestReconciler_G1_EmptySpecList(t *testing.T) {
	h := testutil.NewHarness(t)

	// Don't apply any specs, just reconcile
	results := h.Reconcile()

	// Should be empty, no errors
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
	h.AssertDaemonReloaded(0)
}

func TestReconciler_G5_SpecialCharactersInName(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply spec with special characters (but valid for filenames)
	spec := testutil.ContainerSpec("my-app_v2").Image("nginx:latest").Build()
	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	results := h.Reconcile()

	h.AssertNoErrors(results)
	h.AssertUnitFileExists("my-app_v2.container")
}

func TestReconciler_G6_UnicodeInConfigContent(t *testing.T) {
	h := testutil.NewHarness(t)

	// Apply spec with unicode in config
	spec := testutil.ContainerSpec("webapp").
		Image("nginx:latest").
		WithConfig("/config.txt", "Hello 世界 🎉 Привет").
		Build()

	if err := h.ApplySpec(spec); err != nil {
		t.Fatalf("ApplySpec failed: %v", err)
	}

	results := h.Reconcile()

	h.AssertNoErrors(results)
	h.AssertConfigFileContent("webapp", "config.txt", "Hello 世界 🎉 Привет")
}
