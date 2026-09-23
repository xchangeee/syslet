package systemd_test

import (
	"context"
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/systemd"
	"github.com/xchangeee/syslet/internal/systemd/systemdtest"
)

func TestClient_DaemonReload(t *testing.T) {
	ctx := context.Background()
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	err := client.DaemonReload(ctx)
	if err != nil {
		t.Fatalf("DaemonReload failed: %v", err)
	}
	if !mockConn.Reloaded {
		t.Error("expected daemon to be reloaded")
	}
}

func TestClient_RuntimeState_Active(t *testing.T) {
	ctx := context.Background()
	mockConn := systemdtest.NewMockDBusConn()
	mockConn.UnitStates["webapp.service"] = &systemd.UnitState{
		ActiveState: systemd.ActiveStateActive,
		Enabled:     true,
	}
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	state, err := client.RuntimeState(ctx, "webapp.service")
	if err != nil {
		t.Fatalf("RuntimeState failed: %v", err)
	}
	if state.ActiveState != systemd.ActiveStateActive {
		t.Errorf("expected active state, got %q", state.ActiveState)
	}
	if !state.Enabled {
		t.Error("expected unit to be enabled")
	}
}

func TestClient_RuntimeState_Inactive(t *testing.T) {
	ctx := context.Background()
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Unit doesn't exist in mock, should return default inactive state
	state, err := client.RuntimeState(ctx, "nonexistent.service")
	if err != nil {
		t.Fatalf("RuntimeState failed: %v", err)
	}
	if state.ActiveState != systemd.ActiveStateInactive {
		t.Errorf("expected inactive state, got %q", state.ActiveState)
	}
	if state.Enabled {
		t.Error("expected unit to not be enabled")
	}
}

func TestClient_StartUnit(t *testing.T) {
	ctx := context.Background()
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	err := client.StartUnit(ctx, model.ContainerUnitRef("webapp").ServiceUnitName())
	if err != nil {
		t.Fatalf("StartUnit failed: %v", err)
	}

	// Verify the service was started (not the container unit)
	if len(mockConn.Started) != 1 || mockConn.Started[0] != "webapp.service" {
		t.Errorf("expected webapp.service to be started, got %v", mockConn.Started)
	}
}

func TestClient_StopUnit(t *testing.T) {
	ctx := context.Background()
	mockConn := systemdtest.NewMockDBusConn()
	mockConn.UnitStates["webapp.service"] = &systemd.UnitState{ActiveState: systemd.ActiveStateActive}
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	err := client.StopUnit(ctx, model.ContainerUnitRef("webapp").ServiceUnitName())
	if err != nil {
		t.Fatalf("StopUnit failed: %v", err)
	}

	if len(mockConn.Stopped) != 1 || mockConn.Stopped[0] != "webapp.service" {
		t.Errorf("expected webapp.service to be stopped, got %v", mockConn.Stopped)
	}
}

func TestClient_ContainerState(t *testing.T) {
	ctx := context.Background()
	mockConn := systemdtest.NewMockDBusConn()
	mockConn.UnitStates["myapp.service"] = &systemd.UnitState{
		ActiveState: systemd.ActiveStateActive,
		Enabled:     true,
	}
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	state, err := client.ContainerState(ctx, model.ContainerUnitRef("myapp"))
	if err != nil {
		t.Fatalf("ContainerState failed: %v", err)
	}
	if state.ActiveState != systemd.ActiveStateActive {
		t.Errorf("expected active state, got %q", state.ActiveState)
	}
}

func TestClient_WriteUnitFile(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	content := []byte("[Container]\nImage=nginx:latest\n")
	err := client.WriteUnitFile("webapp.container", content)
	if err != nil {
		t.Fatalf("WriteUnitFile failed: %v", err)
	}

	// Verify file was written
	readContent, err := afero.ReadFile(fs, "/etc/containers/systemd/webapp.container")
	if err != nil {
		t.Fatalf("failed to read unit file: %v", err)
	}
	if string(readContent) != string(content) {
		t.Errorf("expected %q, got %q", string(content), string(readContent))
	}
}

func TestClient_WriteUnitFile_CreatesDirectory(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Directory doesn't exist yet
	err := client.WriteUnitFile("webapp.container", []byte("content"))
	if err != nil {
		t.Fatalf("WriteUnitFile failed: %v", err)
	}

	// Verify directory was created
	exists, err := afero.DirExists(fs, systemd.QuadletUnitDir)
	if err != nil {
		t.Fatalf("failed to check directory: %v", err)
	}
	if !exists {
		t.Error("expected quadlet directory to be created")
	}
}

func TestClient_ReadUnitFile(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Write a unit file first
	expectedContent := []byte("[Container]\nImage=redis:latest\n")
	_ = client.WriteUnitFile("cache.container", expectedContent)

	// Read it back
	content, err := client.ReadUnitFile("cache.container")
	if err != nil {
		t.Fatalf("ReadUnitFile failed: %v", err)
	}
	if string(content) != string(expectedContent) {
		t.Errorf("expected %q, got %q", string(expectedContent), string(content))
	}
}

func TestClient_RemoveUnitFile(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Write a unit file
	_ = client.WriteUnitFile("old.container", []byte("content"))

	// Remove it
	err := client.RemoveUnitFile("old.container")
	if err != nil {
		t.Fatalf("RemoveUnitFile failed: %v", err)
	}

	// Verify it's gone
	if client.UnitFileExists("old.container") {
		t.Error("expected unit file to be removed")
	}
}

func TestClient_RemoveUnitFile_NonexistentOK(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Removing nonexistent file should not error
	err := client.RemoveUnitFile("nonexistent.container")
	if err != nil {
		t.Fatalf("RemoveUnitFile of nonexistent file failed: %v", err)
	}
}

func TestClient_UnitFileExists(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// File doesn't exist
	if client.UnitFileExists("webapp.container") {
		t.Error("expected UnitFileExists to return false for nonexistent file")
	}

	// Write file
	_ = client.WriteUnitFile("webapp.container", []byte("content"))

	// Now it exists
	if !client.UnitFileExists("webapp.container") {
		t.Error("expected UnitFileExists to return true for existing file")
	}
}

func TestClient_ListUnitFiles_Empty(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Directory doesn't exist yet
	files, err := client.ListUnitFiles(".container")
	if err != nil {
		t.Fatalf("ListUnitFiles failed: %v", err)
	}
	if files != nil {
		t.Errorf("expected nil for nonexistent directory, got %v", files)
	}
}

func TestClient_ListUnitFiles_ByExtension(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Write different types of unit files
	_ = client.WriteUnitFile("webapp.container", []byte("content"))
	_ = client.WriteUnitFile("db.container", []byte("content"))
	_ = client.WriteUnitFile("data.volume", []byte("content"))
	_ = client.WriteUnitFile("frontend.network", []byte("content"))

	// List only containers
	containers, err := client.ListUnitFiles(".container")
	if err != nil {
		t.Fatalf("ListUnitFiles failed: %v", err)
	}
	if len(containers) != 2 {
		t.Errorf("expected 2 containers, got %d: %v", len(containers), containers)
	}

	// List only volumes
	volumes, err := client.ListUnitFiles(".volume")
	if err != nil {
		t.Fatalf("ListUnitFiles failed: %v", err)
	}
	if len(volumes) != 1 || volumes[0] != "data.volume" {
		t.Errorf("expected [data.volume], got %v", volumes)
	}

	// List only networks
	networks, err := client.ListUnitFiles(".network")
	if err != nil {
		t.Fatalf("ListUnitFiles failed: %v", err)
	}
	if len(networks) != 1 || networks[0] != "frontend.network" {
		t.Errorf("expected [frontend.network], got %v", networks)
	}
}

func TestClient_ListUnitFiles_IgnoresDirectories(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	client := systemd.NewClient(mockConn, fs)

	// Write a unit file
	_ = client.WriteUnitFile("webapp.container", []byte("content"))

	// Create a subdirectory
	_ = fs.MkdirAll("/etc/containers/systemd/subdir", 0755)

	// Should only list the file, not the directory
	files, err := client.ListUnitFiles(".container")
	if err != nil {
		t.Fatalf("ListUnitFiles failed: %v", err)
	}
	if len(files) != 1 || files[0] != "webapp.container" {
		t.Errorf("expected [webapp.container], got %v", files)
	}
}

func TestClient_CustomQuadletDir(t *testing.T) {
	mockConn := systemdtest.NewMockDBusConn()
	fs := afero.NewMemMapFs()
	customDir := "/custom/path/systemd"
	client := systemd.NewClientWithPaths(mockConn, fs, customDir)

	// Write a unit file
	_ = client.WriteUnitFile("test.container", []byte("content"))

	// Verify it was written to custom directory
	content, err := afero.ReadFile(fs, "/custom/path/systemd/test.container")
	if err != nil {
		t.Fatalf("failed to read from custom directory: %v", err)
	}
	if string(content) != "content" {
		t.Error("file not written to custom directory")
	}
}

func TestContainerName_ServiceUnitName(t *testing.T) {
	tests := []struct {
		name     model.ContainerUnitRef
		expected model.ServiceUnitName
	}{
		{"webapp", "webapp.service"},
		{"myapp", "myapp.service"},
		{"multi.dot.name", "multi.dot.name.service"},
	}

	for _, tt := range tests {
		t.Run(string(tt.name), func(t *testing.T) {
			if got := tt.name.ServiceUnitName(); got != tt.expected {
				t.Errorf("ContainerName(%q).ServiceUnitName() = %q, want %q", tt.name, got, tt.expected)
			}
		})
	}
}
