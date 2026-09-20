package syslet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

// newConfigStore returns a ContainerConfigFileStore backed by a real OS filesystem
// under t.TempDir(). Required for configDir tests because the versioned dir logic
// uses symlinks, which afero.MemMapFs does not support.
func newConfigStore(t *testing.T) *filestore.ContainerConfigFileStore {
	t.Helper()
	return filestore.NewContainerConfigFileStoreAt(afero.NewOsFs(), filepath.Join(t.TempDir(), "config"))
}

// setupConfigDirTest creates a test environment for configDir scenarios.
// It uses a MemMapFs for unit files and the supplied OS-backed store for
// versioned config dirs, so that symlink operations work.
func setupConfigDirTest(t *testing.T, store *filestore.ContainerConfigFileStore, fixture testFixture) (context.Context, afero.Fs, *systemd.Client, *systemd.MockDBusConn, filestore.FileManagers, api.LoadResult) {
	t.Helper()
	memFs := afero.NewMemMapFs()
	mockConn := systemd.NewMockDBusConn()

	quadletDir := "/etc/containers/systemd"
	sd := systemd.NewClientWithPaths(mockConn, memFs, quadletDir)

	for fullName, content := range fixture.existingUnits {
		if err := sd.WriteUnitFile(model.FullUnitName(fullName), []byte(content)); err != nil {
			t.Fatalf("failed to write existing unit %s: %v", fullName, err)
		}
	}
	for serviceName, state := range fixture.existingState {
		mockConn.SetUnitState(serviceName, systemd.ActiveState(state))
	}

	raw, err := loadResultFromSpecs(fixture.specs)
	if err != nil {
		t.Fatalf("failed to load specs: %v", err)
	}

	mgrs := filestore.FileManagers{
		Config: store,
		Build:  filestore.NewBuildContextFileStore(memFs),
	}

	t.Cleanup(func() { sd.Close() })

	return context.Background(), memFs, sd, mockConn, mgrs, raw
}

// preWriteConfigDir simulates a previously-applied configDir state by writing
// versioned files and updating the ..data symlink to point at version.
func preWriteConfigDir(t *testing.T, store *filestore.ContainerConfigFileStore, containerName string, mountPath model.ContainerMountPath, version int, files ...model.ContainerConfigFile) {
	t.Helper()
	ref := model.ContainerUnitRef(containerName)
	for _, f := range files {
		mode := f.Mode
		if mode == 0 {
			mode = 0644
		}
		if err := store.WriteVersionedDirFile(ref, mountPath, version, f.Name, mode, f.Content); err != nil {
			t.Fatalf("preWriteConfigDir WriteVersionedDirFile(%q): %v", f.Name, err)
		}
	}
	if err := store.UpdateDirSymlink(ref, mountPath, version); err != nil {
		t.Fatalf("preWriteConfigDir UpdateDirSymlink: %v", err)
	}
}

// --- Reload scenarios ---

func TestApply_ContainerConfigDir_FilesUnchanged_NoAction(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	files := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "key=value", 0),
	}
	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), files...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, files...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerNotReloaded(t, mockConn)
}

func TestApply_ContainerConfigDir_FileContentChanged_ReloadsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "new", 0),
	}
	oldSpec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), oldFiles...))
	newSpec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{newSpec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerReloaded(t, mockConn, "webapp.service")
}

func TestApply_ContainerConfigDir_MultipleFiles_OneChanged_ReloadsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("a.conf", "unchanged", 0),
		model.NewContainerConfigFile("b.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("a.conf", "unchanged", 0),
		model.NewContainerConfigFile("b.conf", "new", 0),
	}
	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerReloaded(t, mockConn, "webapp.service")
}

func TestApply_ContainerConfigDir_FileAdded_ReloadsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("a.conf", "content", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("a.conf", "content", 0),
		model.NewContainerConfigFile("b.conf", "new file", 0),
	}
	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerReloaded(t, mockConn, "webapp.service")
}

func TestApply_ContainerConfigDir_FileRemoved_ReloadsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("a.conf", "content", 0),
		model.NewContainerConfigFile("b.conf", "to be removed", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("a.conf", "content", 0),
	}
	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerReloaded(t, mockConn, "webapp.service")
}

func TestApply_ContainerConfigDir_FileModeChanged_ReloadsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("run.sh", "#!/bin/sh", os.FileMode(0644)),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("run.sh", "#!/bin/sh", os.FileMode(0755)),
	}
	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerReloaded(t, mockConn, "webapp.service")
}

// --- Restart scenarios ---

func TestApply_ContainerConfigDir_NewDir_RestartsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	newSpec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath),
			model.NewContainerConfigFile("app.conf", "content", 0)))
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{newSpec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
	testutil.AssertContainerNotReloaded(t, mockConn)
}

func TestApply_ContainerConfigDir_NewDirNoFiles_CreatesDir(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	newSpec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath)))
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{newSpec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	// store is OS-backed (see newConfigStore), so the configDir directory lives on
	// the real filesystem, not on memFs (which only holds the rendered unit files).
	hostPath := store.Resolve(model.ContainerUnitRef("webapp"), mountPath)
	info, err := os.Stat(hostPath)
	if err != nil {
		t.Fatalf("expected configDir directory to exist on disk at %s, got error: %v", hostPath, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", hostPath)
	}
	testutil.AssertRestarted(t, mockConn, "webapp.service")
}

func TestApply_ContainerConfigDir_DirRemoved_RestartsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	files := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "content", 0),
	}
	oldSpec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), files...))
	newSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{newSpec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, files...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
	testutil.AssertContainerNotReloaded(t, mockConn)
}

// --- No-action on stopped container ---

func TestApply_ContainerConfigDir_Stopped_FilesChanged_NoAction(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "new", 0),
	}
	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateStopped,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "inactive"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertContainerNotReloaded(t, mockConn)
}

// --- Restart subsumes reload ---

func TestApply_ContainerConfigDir_FilesChangedAndUnitChanged_RestartOnly(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "new", 0),
	}
	oldSpec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), oldFiles...))
	newSpec := makeContainerSpecWithDirs("webapp", "nginx:alpine", model.DesiredStateRunning,
		model.NewContainerDirMount(string(mountPath), newFiles...))

	store := newConfigStore(t)
	ctx, memFs, sd, mockConn, mgrs, raw := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{newSpec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})
	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFiles...)

	mustApplyWithMgrs(t, ctx, memFs, mgrs, sd, newMockPodmanClient(), raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
	testutil.AssertContainerNotReloaded(t, mockConn)
}
