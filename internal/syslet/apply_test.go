package syslet

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/systemd"

	"github.com/spf13/afero"
)

// mockDBusConn implements systemd.DBusConn for testing.
type mockDBusConn struct {
	unitStates map[string]*systemd.UnitState
	reloaded   bool
	started    []string
	stopped    []string
}

func newMockDBusConn() *mockDBusConn {
	return &mockDBusConn{
		unitStates: make(map[string]*systemd.UnitState),
	}
}

func (m *mockDBusConn) Close() {}

func (m *mockDBusConn) ReloadContext(ctx context.Context) error {
	m.reloaded = true
	return nil
}

func (m *mockDBusConn) GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]interface{}, error) {
	state, ok := m.unitStates[unit]
	if !ok {
		return map[string]interface{}{
			"ActiveState":   "inactive",
			"UnitFileState": "disabled",
		}, nil
	}
	return map[string]interface{}{
		"ActiveState":   state.ActiveState,
		"UnitFileState": "enabled",
	}, nil
}

func (m *mockDBusConn) StartUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	m.started = append(m.started, name)
	if m.unitStates[name] == nil {
		m.unitStates[name] = &systemd.UnitState{}
	}
	m.unitStates[name].ActiveState = "active"
	ch <- "done"
	return 0, nil
}

func (m *mockDBusConn) StopUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error) {
	m.stopped = append(m.stopped, name)
	if m.unitStates[name] == nil {
		m.unitStates[name] = &systemd.UnitState{}
	}
	m.unitStates[name].ActiveState = "inactive"
	ch <- "done"
	return 0, nil
}

func (m *mockDBusConn) setUnitState(serviceName string, activeState string) {
	m.unitStates[serviceName] = &systemd.UnitState{
		ActiveState: activeState,
		Enabled:     true,
	}
}

// testFixture helps build test scenarios with specs and existing state.
type testFixture struct {
	specs         []api.Spec
	existingUnits map[string]string // fullName -> content
	existingState map[string]string // service name -> "active"|"inactive"
}

// createZipFromSpecs marshals specs to JSON and creates a zip in memory.
func createZipFromSpecs(specs []api.Spec) (string, error) {
	// Create a temp file for the zip
	tmpFile, err := os.CreateTemp("", "syslet-test-*.zip")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	w := zip.NewWriter(tmpFile)

	for i, spec := range specs {
		// Marshal spec to JSON
		data, err := json.Marshal(map[string]interface{}{
			"type": string(spec.GetType()),
			"name": spec.GetName(),
			"unit": spec.GetUnit(),
		})
		if err != nil {
			w.Close()
			os.Remove(tmpFile.Name())
			return "", err
		}

		// Add type-specific fields
		switch s := spec.(type) {
		case *api.ContainerSpec:
			fullData := map[string]interface{}{
				"type": "container",
				"name": s.Name,
				"unit": s.Unit,
			}
			if s.DesiredState != "" {
				fullData["desiredState"] = s.DesiredState
			}
			if len(s.Configs) > 0 {
				fullData["configs"] = s.Configs
			}
			data, err = json.Marshal(fullData)
			if err != nil {
				w.Close()
				os.Remove(tmpFile.Name())
				return "", err
			}
		}

		// Write to zip
		filename := fmt.Sprintf("spec-%d.json", i)
		f, err := w.Create(filename)
		if err != nil {
			w.Close()
			os.Remove(tmpFile.Name())
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			w.Close()
			os.Remove(tmpFile.Name())
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// setupTest creates a test environment with mock filesystem, systemd client, and fixtures.
func setupTest(t *testing.T, fixture testFixture) (context.Context, afero.Fs, *systemd.Client, *mockDBusConn, string) {
	ctx := context.Background()
	fs := afero.NewMemMapFs()
	mockConn := newMockDBusConn()

	quadletDir := "/etc/containers/systemd"
	sd := systemd.NewClientWithPaths(mockConn, fs, quadletDir)

	// Write existing unit files
	for fullName, content := range fixture.existingUnits {
		if err := sd.WriteUnitFile(fullName, []byte(content)); err != nil {
			t.Fatalf("failed to write existing unit %s: %v", fullName, err)
		}
	}

	// Set existing runtime state
	for serviceName, state := range fixture.existingState {
		mockConn.setUnitState(serviceName, state)
	}

	// Create zip from specs
	zipPath, err := createZipFromSpecs(fixture.specs)
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}

	t.Cleanup(func() {
		os.Remove(zipPath)
		sd.Close()
	})

	return ctx, fs, sd, mockConn, zipPath
}

func TestApply_NewContainer_DesiredStateRunning(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {
					"Image": "nginx:latest",
				},
			},
			DesiredState: "running",
		},
	}

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{specs: specs})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify unit file was created
	if !sd.UnitFileExists("webapp.container") {
		t.Error("unit file was not created")
	}

	// Verify container was started
	if len(mockConn.started) != 1 || mockConn.started[0] != "webapp.service" {
		t.Errorf("expected container to be started, got started: %v", mockConn.started)
	}

	// Verify no stops
	if len(mockConn.stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.stopped)
	}

	// Verify reload was called
	if !mockConn.reloaded {
		t.Error("expected daemon-reload to be called")
	}
}

func TestApply_NewContainer_DesiredStateStopped(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {
					"Image": "nginx:latest",
				},
			},
			DesiredState: "stopped",
		},
	}

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{specs: specs})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify unit file was created
	if !sd.UnitFileExists("webapp.container") {
		t.Error("unit file was not created")
	}

	// Verify container was NOT started
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}

	// Verify reload was called
	if !mockConn.reloaded {
		t.Error("expected daemon-reload to be called")
	}
}

func TestApply_UnchangedContainer_NoRestart(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {
					"Image": "nginx:latest",
				},
			},
			DesiredState: "running",
		},
	}

	// Render the spec to get the expected unit content
	rendered, err := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	if err != nil {
		t.Fatalf("failed to render spec: %v", err)
	}
	content, err := rendered.SerializeUnitOptions()
	if err != nil {
		t.Fatalf("failed to serialize unit: %v", err)
	}

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": content,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify no stops or starts
	if len(mockConn.stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}

	// Verify no reload (nothing changed)
	if mockConn.reloaded {
		t.Error("expected no daemon-reload for unchanged unit")
	}
}

func TestApply_UnitChanged_RunningContainer_Restart(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {
					"Image": "nginx:alpine", // Changed from nginx:latest
				},
			},
			DesiredState: "running",
		},
	}

	// Create old unit content with different image
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {
				"Image": "nginx:latest",
			},
		},
		DesiredState: "running",
	}
	rendered, err := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	if err != nil {
		t.Fatalf("failed to render old spec: %v", err)
	}
	oldContent, err := rendered.SerializeUnitOptions()
	if err != nil {
		t.Fatalf("failed to serialize old unit: %v", err)
	}

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify container was stopped then started (restart)
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "webapp.service" {
		t.Errorf("expected container to be stopped, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 1 || mockConn.started[0] != "webapp.service" {
		t.Errorf("expected container to be started, got: %v", mockConn.started)
	}

	// Verify reload was called
	if !mockConn.reloaded {
		t.Error("expected daemon-reload to be called")
	}
}

func TestApply_ConfigChanged_RunningContainer_Restart(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {
					"Image": "nginx:latest",
				},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{
					Content:          "new config content",
					TargetVolumePath: "/etc/nginx/nginx.conf",
				},
			},
		},
	}

	// Create old unit without config
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {
				"Image": "nginx:latest",
			},
		},
		DesiredState: "running",
	}
	rendered, err := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	if err != nil {
		t.Fatalf("failed to render old spec: %v", err)
	}
	oldContent, err := rendered.SerializeUnitOptions()
	if err != nil {
		t.Fatalf("failed to serialize old unit: %v", err)
	}

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify container was restarted due to config change
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "webapp.service" {
		t.Errorf("expected container to be stopped, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 1 || mockConn.started[0] != "webapp.service" {
		t.Errorf("expected container to be started, got: %v", mockConn.started)
	}

	// Verify config file was written
	cfg := containerconfig.NewConfigFileManager(fs)
	files, err := cfg.ListFiles("webapp")
	if err != nil {
		t.Fatalf("failed to list config files: %v", err)
	}
	if len(files) != 1 || files[0] != "nginx.conf" {
		t.Errorf("expected config file nginx.conf, got: %v", files)
	}
}

func TestApply_MinimalRestarts_MultipleContainers(t *testing.T) {
	// Three containers: one unchanged, one with unit change, one new
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "unchanged",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:latest"},
			},
			DesiredState: "running",
		},
		&api.ContainerSpec{
			Name: "changed",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:alpine"},
			},
			DesiredState: "running",
		},
		&api.ContainerSpec{
			Name: "new",
			Unit: map[string]map[string]any{
				"Container": {"Image": "redis:latest"},
			},
			DesiredState: "running",
		},
	}

	// Create existing units
	unchangedRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	unchangedContent, _ := unchangedRendered.SerializeUnitOptions()

	changedOldSpec := &api.ContainerSpec{
		Name: "changed",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"}, // old image
		},
		DesiredState: "running",
	}
	changedRendered, _ := changedOldSpec.Render(containerconfig.DefaultContainerConfigDir)
	changedContent, _ := changedRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"unchanged.container": unchangedContent,
			"changed.container":   changedContent,
		},
		existingState: map[string]string{
			"unchanged.service": "active",
			"changed.service":   "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify only "changed" was stopped (not "unchanged")
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "changed.service" {
		t.Errorf("expected only changed.service to be stopped, got: %v", mockConn.stopped)
	}

	// Verify "changed" and "new" were started (not "unchanged")
	if len(mockConn.started) != 2 {
		t.Errorf("expected 2 starts, got %d: %v", len(mockConn.started), mockConn.started)
	}
	startedMap := make(map[string]bool)
	for _, s := range mockConn.started {
		startedMap[s] = true
	}
	if !startedMap["changed.service"] || !startedMap["new.service"] {
		t.Errorf("expected changed.service and new.service to be started, got: %v", mockConn.started)
	}
}

func TestApply_StaleUnitRemoval(t *testing.T) {
	// Spec with only one container
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:latest"},
			},
			DesiredState: "running",
		},
	}

	// Existing state has two containers, "old" should be removed
	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	oldSpec := &api.ContainerSpec{
		Name: "old",
		Unit: map[string]map[string]any{
			"Container": {"Image": "redis:latest"},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
			"old.container":    oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
			"old.service":    "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify old container was stopped
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "old.service" {
		t.Errorf("expected old.service to be stopped, got: %v", mockConn.stopped)
	}

	// Verify old unit file was removed
	if sd.UnitFileExists("old.container") {
		t.Error("stale unit file was not removed")
	}

	// Verify webapp was not restarted
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts (webapp unchanged), got: %v", mockConn.started)
	}
}

func TestApply_ConfigFileAdditionsAndDeletions(t *testing.T) {
	// Spec with updated configs: remove old.conf, add new.conf, keep existing.conf
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:latest"},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{Content: "existing content updated", TargetVolumePath: "/etc/existing.conf"},
				{Content: "new content", TargetVolumePath: "/etc/new.conf"},
			},
		},
	}

	// Old spec with existing.conf and old.conf
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
		DesiredState: "running",
		Configs: []api.ConfigEntry{
			{Content: "existing content", TargetVolumePath: "/etc/existing.conf"},
			{Content: "old content", TargetVolumePath: "/etc/old.conf"},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Write existing config files
	cfg := containerconfig.NewConfigFileManager(fs)
	cfg.Write("webapp", "existing.conf", "existing content")
	cfg.Write("webapp", "old.conf", "old content")

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify config files
	files, err := cfg.ListFiles("webapp")
	if err != nil {
		t.Fatalf("failed to list config files: %v", err)
	}

	// Should have existing.conf and new.conf, but not old.conf
	fileMap := make(map[string]bool)
	for _, f := range files {
		fileMap[f] = true
	}
	if !fileMap["existing.conf"] {
		t.Error("existing.conf should still exist")
	}
	if !fileMap["new.conf"] {
		t.Error("new.conf should be created")
	}
	if fileMap["old.conf"] {
		t.Error("old.conf should be deleted")
	}

	// Verify container was restarted due to config changes
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "webapp.service" {
		t.Errorf("expected webapp to be stopped, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 1 || mockConn.started[0] != "webapp.service" {
		t.Errorf("expected webapp to be started, got: %v", mockConn.started)
	}
}

func TestApply_VolumeAndNetworkChanges_NoContainerRestart(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:latest"},
			},
			DesiredState: "running",
		},
		&api.VolumeSpec{
			Name: "data",
			Unit: map[string]map[string]any{
				"Volume": {"Device": "tmpfs"},
			},
		},
		&api.NetworkSpec{
			Name: "frontend",
			Unit: map[string]map[string]any{
				"Network": {"Driver": "bridge"},
			},
		},
	}

	// Existing state with all units unchanged except volume
	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	oldVolumeSpec := &api.VolumeSpec{
		Name: "data",
		Unit: map[string]map[string]any{
			"Volume": {"Device": "old-device"}, // Changed
		},
	}
	oldVolumeRendered, _ := oldVolumeSpec.Render()
	oldVolumeContent, _ := oldVolumeRendered.SerializeUnitOptions()

	networkRendered, _ := specs[2].(*api.NetworkSpec).Render()
	networkContent, _ := networkRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
			"data.volume":      oldVolumeContent,
			"frontend.network": networkContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify container was NOT restarted (only volume changed)
	if len(mockConn.stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}

	// Verify volume unit was updated
	volumeContent, err := sd.ReadUnitFile("data.volume")
	if err != nil {
		t.Fatalf("failed to read volume unit: %v", err)
	}
	if !bytes.Contains(volumeContent, []byte("tmpfs")) {
		t.Error("volume unit was not updated")
	}

	// Verify reload was called
	if !mockConn.reloaded {
		t.Error("expected daemon-reload to be called")
	}
}

func TestApply_DesiredStateStopped_StopsRunningContainer(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:latest"},
			},
			DesiredState: "stopped", // Changed from running to stopped
		},
	}

	webappRendered, _ := (&api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
		DesiredState: "running",
	}).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify container was stopped
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "webapp.service" {
		t.Errorf("expected webapp to be stopped, got: %v", mockConn.stopped)
	}

	// Verify container was NOT started
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}
}

func TestApply_ConfigOnlyChange_InactiveContainer_NoRestart(t *testing.T) {
	// Container is stopped, config changes, should update config but not start
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]any{
				"Container": {"Image": "nginx:latest"},
			},
			DesiredState: "stopped",
			Configs: []api.ConfigEntry{
				{Content: "new config", TargetVolumePath: "/etc/app.conf"},
			},
		},
	}

	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
		DesiredState: "stopped",
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "inactive",
		},
	})

	if err := Apply(ctx, testLogger(), fs, sd, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify config was written
	cfg := containerconfig.NewConfigFileManager(fs)
	files, _ := cfg.ListFiles("webapp")
	if len(files) != 1 || files[0] != "app.conf" {
		t.Errorf("expected app.conf, got: %v", files)
	}

	// Verify no stops or starts (container is already stopped)
	if len(mockConn.stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}
}

// testLogger returns a no-op logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Only show errors to keep test output clean
	}))
}
