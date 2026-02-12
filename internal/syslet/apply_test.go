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
	"codeberg.org/xchangeee/syslet/internal/podman"
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

// mockPodmanClient implements podman.Interface for testing.
type mockPodmanClient struct {
	deletedVolumes  []string
	deletedNetworks []string
	deletedImages   []string
}

func newMockPodmanClient() *mockPodmanClient {
	return &mockPodmanClient{
		deletedVolumes:  []string{},
		deletedNetworks: []string{},
		deletedImages:   []string{},
	}
}

func (m *mockPodmanClient) DeleteVolume(ctx context.Context, name string) error {
	m.deletedVolumes = append(m.deletedVolumes, name)
	return nil
}

func (m *mockPodmanClient) DeleteNetwork(ctx context.Context, name string) error {
	m.deletedNetworks = append(m.deletedNetworks, name)
	return nil
}

func (m *mockPodmanClient) DeleteImage(ctx context.Context, tag string) error {
	m.deletedImages = append(m.deletedImages, tag)
	return nil
}

// testFixture helps build test scenarios with specs and existing state.
type testFixture struct {
	specs         []api.Spec
	existingUnits map[string]string // fullName -> content
	existingState map[string]string // service name -> "active"|"inactive"
}

// createZipFromSpecs marshals specs to JSON and creates a zip in the provided filesystem.
func createZipFromSpecs(fs afero.Fs, specs []api.Spec) (string, error) {
	// Create zip in memory
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	for i, spec := range specs {
		// Marshal spec to JSON
		data, err := json.Marshal(map[string]interface{}{
			"type": string(spec.GetType()),
			"name": spec.GetName(),
			"unit": spec.GetUnit(),
		})
		if err != nil {
			_ = w.Close()
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
				_ = w.Close()
				return "", err
			}
		}

		// Write to zip
		filename := fmt.Sprintf("spec-%d.json", i)
		f, err := w.Create(filename)
		if err != nil {
			_ = w.Close()
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			_ = w.Close()
			return "", err
		}
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	// Write zip to filesystem
	zipPath := "/tmp/test-specs.zip"
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		return "", err
	}

	return zipPath, nil
}

// setupTest creates a test environment with mock filesystem, systemd client, and fixtures.
func setupTest(t *testing.T, fixture testFixture) (context.Context, afero.Fs, *systemd.Client, *mockDBusConn, podman.Interface, string) {
	ctx := context.Background()
	fs := afero.NewMemMapFs()
	mockConn := newMockDBusConn()
	mockPodman := newMockPodmanClient()

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
	zipPath, err := createZipFromSpecs(fs, fixture.specs)
	if err != nil {
		t.Fatalf("failed to create zip: %v", err)
	}

	t.Cleanup(func() {
		sd.Close()
	})

	return ctx, fs, sd, mockConn, mockPodman, zipPath
}

func TestApply_NewContainer_DesiredStateRunning(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:latest"),
				},
			},
			DesiredState: "running",
		},
	}

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:latest"),
				},
			},
			DesiredState: "stopped",
		},
	}

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:latest"),
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

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": content,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:alpine"), // Changed from nginx:latest
				},
			},
			DesiredState: "running",
		},
	}

	// Create old unit content with different image
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {
				"Image": api.UV("nginx:latest"),
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

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:latest"),
				},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{
					Content:   "new config content",
					MountPath: "/etc/nginx/nginx.conf",
				},
			},
		},
	}

	// Create old unit without config
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {
				"Image": api.UV("nginx:latest"),
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

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
		},
		&api.ContainerSpec{
			Name: "changed",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:alpine")},
			},
			DesiredState: "running",
		},
		&api.ContainerSpec{
			Name: "new",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("redis:latest")},
			},
			DesiredState: "running",
		},
	}

	// Create existing units
	unchangedRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	unchangedContent, _ := unchangedRendered.SerializeUnitOptions()

	changedOldSpec := &api.ContainerSpec{
		Name: "changed",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")}, // old image
		},
		DesiredState: "running",
	}
	changedRendered, _ := changedOldSpec.Render(containerconfig.DefaultContainerConfigDir)
	changedContent, _ := changedRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
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

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
		},
	}

	// Existing state has two containers, "old" should be removed
	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	oldSpec := &api.ContainerSpec{
		Name: "old",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("redis:latest")},
		},
		RemovalAllowed: true, // Mark for removal
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
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

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{Content: "existing content updated", MountPath: "/etc/existing.conf"},
				{Content: "new content", MountPath: "/etc/new.conf"},
			},
		},
	}

	// Old spec with existing.conf and old.conf
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "running",
		Configs: []api.ConfigEntry{
			{Content: "existing content", MountPath: "/etc/existing.conf"},
			{Content: "old content", MountPath: "/etc/old.conf"},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
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
	_ = cfg.Write("webapp", "existing.conf", "existing content")
	_ = cfg.Write("webapp", "old.conf", "old content")

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
		},
		&api.VolumeSpec{
			Name: "data",
			Unit: map[string]map[string]api.UnitValue{
				"Volume": {"Device": api.UV("tmpfs")},
			},
		},
		&api.NetworkSpec{
			Name: "frontend",
			Unit: map[string]map[string]api.UnitValue{
				"Network": {"Driver": api.UV("bridge")},
			},
		},
	}

	// Existing state with all units unchanged except volume
	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	oldVolumeSpec := &api.VolumeSpec{
		Name: "data",
		Unit: map[string]map[string]api.UnitValue{
			"Volume": {"Device": api.UV("old-device")}, // Changed
		},
	}
	oldVolumeRendered, _ := oldVolumeSpec.Render()
	oldVolumeContent, _ := oldVolumeRendered.SerializeUnitOptions()

	networkRendered, _ := specs[2].(*api.NetworkSpec).Render()
	networkContent, _ := networkRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
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

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "stopped", // Changed from running to stopped
		},
	}

	webappRendered, _ := (&api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "running",
	}).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "stopped",
			Configs: []api.ConfigEntry{
				{Content: "new config", MountPath: "/etc/app.conf"},
			},
		},
	}

	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "stopped",
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "inactive",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
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

func TestApply_NewVolumeAndNetwork(t *testing.T) {
	// Test creating brand new volume and network units (isNew = true in diffSimple)
	specs := []api.Spec{
		&api.VolumeSpec{
			Name: "data",
			Unit: map[string]map[string]api.UnitValue{
				"Volume": {"Device": api.UV("tmpfs")},
			},
		},
		&api.NetworkSpec{
			Name: "frontend",
			Unit: map[string]map[string]api.UnitValue{
				"Network": {"Driver": api.UV("bridge")},
			},
		},
	}

	// No existing units - this is a fresh install
	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs:         specs,
		existingUnits: map[string]string{},
		existingState: map[string]string{},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify volume unit was created
	if !sd.UnitFileExists("data.volume") {
		t.Error("volume unit file was not created")
	}

	// Verify network unit was created
	if !sd.UnitFileExists("frontend.network") {
		t.Error("network unit file was not created")
	}

	// Verify reload was called
	if !mockConn.reloaded {
		t.Error("expected daemon-reload to be called")
	}

	// Verify no containers were started/stopped
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}
	if len(mockConn.stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.stopped)
	}
}

func TestApply_StaleVolumeAndNetworkRemoval(t *testing.T) {
	// Spec with only a container, but existing state has stale volume and network
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
		},
	}

	// Existing state has container, volume, and network, but spec only has container
	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	// Create stale volume WITHOUT ReclaimPolicy (should only remove unit file)
	staleVolumeSpec := &api.VolumeSpec{
		Name: "olddata",
		Unit: map[string]map[string]api.UnitValue{
			"Volume": {"Device": api.UV("tmpfs")},
		},
		RemovalAllowed: true, // Mark for removal
	}
	staleVolumeRendered, _ := staleVolumeSpec.Render()
	staleVolumeContent, _ := staleVolumeRendered.SerializeUnitOptions()

	// Create stale network WITHOUT ReclaimPolicy (should only remove unit file)
	staleNetworkSpec := &api.NetworkSpec{
		Name: "oldnet",
		Unit: map[string]map[string]api.UnitValue{
			"Network": {"Driver": api.UV("bridge")},
		},
		RemovalAllowed: true, // Mark for removal
	}
	staleNetworkRendered, _ := staleNetworkSpec.Render()
	staleNetworkContent, _ := staleNetworkRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
			"olddata.volume":   staleVolumeContent,
			"oldnet.network":   staleNetworkContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify stale volume unit file was removed
	if sd.UnitFileExists("olddata.volume") {
		t.Error("stale volume unit file was not removed")
	}

	// Verify stale network unit file was removed
	if sd.UnitFileExists("oldnet.network") {
		t.Error("stale network unit file was not removed")
	}

	// Verify webapp was not affected
	if sd.UnitFileExists("webapp.container") {
		// Container should still exist
	} else {
		t.Error("webapp container unit should still exist")
	}

	// Verify no containers were stopped/started (webapp unchanged)
	if len(mockConn.stopped) != 0 {
		t.Errorf("expected no stops, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 0 {
		t.Errorf("expected no starts, got: %v", mockConn.started)
	}

	// Verify reload was called (units were deleted)
	if !mockConn.reloaded {
		t.Error("expected daemon-reload to be called")
	}
}

func TestApply_StaleVolumeWithReclaimPolicyDelete(t *testing.T) {
	// Test that stale volumes with ReclaimPolicy=Delete trigger volume deletion
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
		},
	}

	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	// Create stale volume WITH ReclaimPolicy=Delete
	staleVolumeSpec := &api.VolumeSpec{
		Name:           "olddata",
		ReclaimPolicy:  "Delete",
		RemovalAllowed: true, // Mark for removal
		Unit: map[string]map[string]api.UnitValue{
			"Volume": {"Device": api.UV("tmpfs")},
		},
	}
	staleVolumeRendered, _ := staleVolumeSpec.Render()
	staleVolumeContent, _ := staleVolumeRendered.SerializeUnitOptions()

	ctx, fs, sd, _, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
			"olddata.volume":   staleVolumeContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify stale volume unit file was removed
	if sd.UnitFileExists("olddata.volume") {
		t.Error("stale volume unit file was not removed")
	}

	// Verify podman.DeleteVolume was called
	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 1 || mockPc.deletedVolumes[0] != "olddata" {
		t.Errorf("expected DeleteVolume to be called with 'olddata', got: %v", mockPc.deletedVolumes)
	}
}

func TestApply_StaleNetworkWithReclaimPolicyDelete(t *testing.T) {
	// Test that stale networks with ReclaimPolicy=Delete trigger network deletion
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
		},
	}

	webappRendered, _ := specs[0].(*api.ContainerSpec).Render(containerconfig.DefaultContainerConfigDir)
	webappContent, _ := webappRendered.SerializeUnitOptions()

	// Create stale network WITH ReclaimPolicy=Delete
	staleNetworkSpec := &api.NetworkSpec{
		Name:           "oldnet",
		ReclaimPolicy:  "Delete",
		RemovalAllowed: true, // Mark for removal
		Unit: map[string]map[string]api.UnitValue{
			"Network": {"Driver": api.UV("bridge")},
		},
	}
	staleNetworkRendered, _ := staleNetworkSpec.Render()
	staleNetworkContent, _ := staleNetworkRendered.SerializeUnitOptions()

	ctx, fs, sd, _, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": webappContent,
			"oldnet.network":   staleNetworkContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify stale network unit file was removed
	if sd.UnitFileExists("oldnet.network") {
		t.Error("stale network unit file was not removed")
	}

	// Verify podman.DeleteNetwork was called
	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedNetworks) != 1 || mockPc.deletedNetworks[0] != "oldnet" {
		t.Errorf("expected DeleteNetwork to be called with 'oldnet', got: %v", mockPc.deletedNetworks)
	}
}

func TestApply_ConfigRemoval_UnchangedContent(t *testing.T) {
	// Test scenario: container has unchanged config a.conf, but removes b.conf.
	// This tests the second block in configsChanged where we detect stale files
	// without any content changes triggering an early return.
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{Content: "unchanged content", MountPath: "/etc/app/a.conf"},
			},
		},
	}

	// Old spec had both a.conf and b.conf
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "running",
		Configs: []api.ConfigEntry{
			{Content: "unchanged content", MountPath: "/etc/app/a.conf"},
			{Content: "old content", MountPath: "/etc/app/b.conf"},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Write existing config files (both a.conf and b.conf)
	cfg := containerconfig.NewConfigFileManager(fs)
	if err := cfg.Write("webapp", "a.conf", "unchanged content"); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	if err := cfg.Write("webapp", "b.conf", "old content"); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify config files: a.conf should remain, b.conf should be deleted
	files, err := cfg.ListFiles("webapp")
	if err != nil {
		t.Fatalf("failed to list config files: %v", err)
	}

	fileMap := make(map[string]bool)
	for _, f := range files {
		fileMap[f] = true
	}
	if !fileMap["a.conf"] {
		t.Error("a.conf should still exist")
	}
	if fileMap["b.conf"] {
		t.Error("b.conf should be deleted")
	}

	// Verify container was restarted due to config change (b.conf removed)
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "webapp.service" {
		t.Errorf("expected webapp to be stopped, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 1 || mockConn.started[0] != "webapp.service" {
		t.Errorf("expected webapp to be started, got: %v", mockConn.started)
	}
}

func TestApply_AllConfigsRemoved_DeletesConfigDirectory(t *testing.T) {
	// Test scenario: container previously had configs, now has none.
	// The entire config directory should be deleted.
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
			// No configs in the new spec
		},
	}

	// Old spec had configs
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "running",
		Configs: []api.ConfigEntry{
			{Content: "config1", MountPath: "/etc/app/config1.conf"},
			{Content: "config2", MountPath: "/etc/app/config2.conf"},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Write existing config files to simulate previous deployment
	cfg := containerconfig.NewConfigFileManager(fs)
	if err := cfg.Write("webapp", "config1.conf", "config1"); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	if err := cfg.Write("webapp", "config2.conf", "config2"); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Verify configs exist before apply
	files, err := cfg.ListFiles("webapp")
	if err != nil {
		t.Fatalf("failed to list config files before apply: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 config files before apply, got %d", len(files))
	}

	if err := testApply(t, ctx, fs, sd, mockPodman, zipPath); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	// Verify entire config directory was deleted
	files, err = cfg.ListFiles("webapp")
	if err == nil && len(files) > 0 {
		t.Errorf("expected config directory to be deleted, but found files: %v", files)
	}

	// Verify container was restarted due to config change
	if len(mockConn.stopped) != 1 || mockConn.stopped[0] != "webapp.service" {
		t.Errorf("expected webapp to be stopped, got: %v", mockConn.stopped)
	}
	if len(mockConn.started) != 1 || mockConn.started[0] != "webapp.service" {
		t.Errorf("expected webapp to be started, got: %v", mockConn.started)
	}
}

// testLogger returns a no-op logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Only show errors to keep test output clean
	}))
}

// testApply is a helper that builds a plan and applies it.
func testApply(t *testing.T, ctx context.Context, fs afero.Fs, sd *systemd.Client, mockPodman podman.Interface, zipPath string) error {
	plan, err := BuildPlan(ctx, fs, sd, zipPath)
	if err != nil {
		return err
	}
	return Apply(ctx, testLogger(), fs, sd, mockPodman, plan)
}

// TestDisplayDiff_ConfigFileChanges tests that config file diffs are displayed using unified diff format.
func TestDisplayDiff_ConfigFileChanges(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{
					Content:   "server {\n  listen 8080;\n  server_name new.example.com;\n}\n",
					MountPath: "/etc/nginx/nginx.conf",
				},
			},
		},
	}

	// Old spec with different config content
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "running",
		Configs: []api.ConfigEntry{
			{
				Content:   "server {\n  listen 80;\n  server_name old.example.com;\n}\n",
				MountPath: "/etc/nginx/nginx.conf",
			},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, _, _, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Write existing config file
	cfg := containerconfig.NewConfigFileManager(fs)
	_ = cfg.Write("webapp", "nginx.conf", "server {\n  listen 80;\n  server_name old.example.com;\n}\n")

	// Build plan
	plan, err := BuildPlan(ctx, fs, sd, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify plan has config file changes
	if len(plan.WriteConfigs) != 1 {
		t.Fatalf("expected 1 config write operation, got %d", len(plan.WriteConfigs))
	}

	configOp := plan.WriteConfigs[0]
	if configOp.oldContent == "" {
		t.Error("config operation should have oldContent for diff")
	}
	if configOp.oldContent == configOp.content {
		t.Error("config oldContent and new content should be different")
	}

	// Capture diff output
	var buf bytes.Buffer
	DisplayDiff(&buf, plan)
	output := buf.String()

	expected := `
Containers to stop:
  - webapp.container

Config file changes:
--- webapp/nginx.conf (current)
+++ webapp/nginx.conf (new)
@@ -1,4 +1,4 @@
 server {
-  listen 80;
+  listen 8080;
-  server_name old.example.com;
+  server_name new.example.com;
 }

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    config updated, restarted (desired: running)
`

	if output != expected {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", expected, output)
	}
}

// TestDisplayDiff_UnitFileChanges tests that unit file diffs use semantic diff format.
func TestDisplayDiff_UnitFileChanges(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:alpine"), // Changed
				},
			},
			DesiredState: "running",
		},
	}

	// Old spec with different image
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {
				"Image": api.UV("nginx:latest"),
			},
		},
		DesiredState: "running",
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, _, _, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Build plan
	plan, err := BuildPlan(ctx, fs, sd, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify plan has unit file changes
	if len(plan.WriteUnits) != 1 {
		t.Fatalf("expected 1 unit write operation, got %d", len(plan.WriteUnits))
	}

	unitOp := plan.WriteUnits[0]
	if unitOp.oldContent == "" {
		t.Error("unit operation should have oldContent for diff")
	}
	if unitOp.oldContent == unitOp.content {
		t.Error("unit oldContent and new content should be different")
	}

	// Capture diff output
	var buf bytes.Buffer
	DisplayDiff(&buf, plan)
	output := buf.String()

	expected := `
Containers to stop:
  - webapp.container

Unit file changes:

--- webapp.container (current)
+++ webapp.container (new)
- [Container] Image=nginx:latest
+ [Container] Image=nginx:alpine

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, restarted (desired: running)
`

	if output != expected {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", expected, output)
	}
}

// TestDisplayDiff_NewConfigFile tests that new config files are properly labeled.
func TestDisplayDiff_NewConfigFile(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {"Image": api.UV("nginx:latest")},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{
					Content:   "new config content",
					MountPath: "/etc/app/config.yaml",
				},
			},
		},
	}

	// Old spec without any configs
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {"Image": api.UV("nginx:latest")},
		},
		DesiredState: "running",
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, _, _, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Build plan
	plan, err := BuildPlan(ctx, fs, sd, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify plan has config file write with empty oldContent (new file)
	if len(plan.WriteConfigs) != 1 {
		t.Fatalf("expected 1 config write operation, got %d", len(plan.WriteConfigs))
	}

	configOp := plan.WriteConfigs[0]
	if configOp.oldContent != "" {
		t.Errorf("new config file should have empty oldContent, got: %q", configOp.oldContent)
	}

	// Capture diff output
	var buf bytes.Buffer
	DisplayDiff(&buf, plan)
	output := buf.String()

	expected := `
Containers to stop:
  - webapp.container

Config file changes:
--- webapp/config.yaml (current)
+++ webapp/config.yaml (new)
@@ -0,0 +1 @@
+new config content
\ No newline at end of file

Unit file changes:

--- webapp.container (current)
+++ webapp.container (new)
+ [Container] Volume=/etc/containers/config/webapp/config.yaml:/etc/app/config.yaml:ro

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, config updated, restarted (desired: running)
`

	if output != expected {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", expected, output)
	}
}

// TestDisplayDiff_MixedChanges tests diff display with both config and unit file changes.
func TestDisplayDiff_MixedChanges(t *testing.T) {
	specs := []api.Spec{
		&api.ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]api.UnitValue{
				"Container": {
					"Image": api.UV("nginx:alpine"), // Unit changed
				},
			},
			DesiredState: "running",
			Configs: []api.ConfigEntry{
				{
					Content:   "updated config",
					MountPath: "/etc/app/app.conf",
				},
			},
		},
	}

	// Old spec with different unit and config
	oldSpec := &api.ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]api.UnitValue{
			"Container": {
				"Image": api.UV("nginx:latest"),
			},
		},
		DesiredState: "running",
		Configs: []api.ConfigEntry{
			{
				Content:   "old config",
				MountPath: "/etc/app/app.conf",
			},
		},
	}
	oldRendered, _ := oldSpec.Render(containerconfig.DefaultContainerConfigDir)
	oldContent, _ := oldRendered.SerializeUnitOptions()

	ctx, fs, sd, _, _, zipPath := setupTest(t, testFixture{
		specs: specs,
		existingUnits: map[string]string{
			"webapp.container": oldContent,
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	// Write existing config
	cfg := containerconfig.NewConfigFileManager(fs)
	_ = cfg.Write("webapp", "app.conf", "old config")

	// Build plan
	plan, err := BuildPlan(ctx, fs, sd, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	// Verify plan has both config and unit changes
	if len(plan.WriteConfigs) != 1 {
		t.Errorf("expected 1 config write, got %d", len(plan.WriteConfigs))
	}
	if len(plan.WriteUnits) != 1 {
		t.Errorf("expected 1 unit write, got %d", len(plan.WriteUnits))
	}

	// Capture diff output
	var buf bytes.Buffer
	DisplayDiff(&buf, plan)
	output := buf.String()

	expected := `
Containers to stop:
  - webapp.container

Config file changes:
--- webapp/app.conf (current)
+++ webapp/app.conf (new)
@@ -1 +1 @@
-old config
\ No newline at end of file
+updated config
\ No newline at end of file

Unit file changes:

--- webapp.container (current)
+++ webapp.container (new)
- [Container] Image=nginx:latest
+ [Container] Image=nginx:alpine

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, config updated, restarted (desired: running)
`

	if output != expected {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", expected, output)
	}
}
