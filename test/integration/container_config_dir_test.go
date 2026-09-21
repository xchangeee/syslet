//go:build integration

package integration

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

// Every test here runs with WithOSConfigStore: the versioned-directory logic
// uses symlinks, which afero.MemMapFs does not support. Only the config store is
// OS-backed; unit files still live on the in-memory filesystem.

// --- Reload scenarios ---

func TestContainerConfigDirUnchanged_NoAction(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	files := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "key=value", 0),
	}
	spec := systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), files...)))

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(spec)
	env.SeedConfigDir("webapp", mountPath, 1, files...)
	env.Specs(spec)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertContainerNotReloaded()
}

// TestContainerConfigDirFileChanges varies the kind of change made to a mounted
// config directory while the container's own unit file stays byte-identical.
// Every variant must take the cheap path: the contents are re-synced and the
// service is reloaded in place, never stopped and started. Restart-worthy
// changes (a new or removed directory, a changed unit) live in their own tests
// below, since there the outcome differs rather than the condition.
func TestContainerConfigDirFileChanges(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")

	tests := []struct {
		name     string
		oldFiles []model.ContainerConfigFile
		newFiles []model.ContainerConfigFile
	}{
		{
			name:     "ContentChanged",
			oldFiles: []model.ContainerConfigFile{model.NewContainerConfigFile("app.conf", "old", 0)},
			newFiles: []model.ContainerConfigFile{model.NewContainerConfigFile("app.conf", "new", 0)},
		},
		{
			name: "OneOfMultipleChanged",
			oldFiles: []model.ContainerConfigFile{
				model.NewContainerConfigFile("a.conf", "unchanged", 0),
				model.NewContainerConfigFile("b.conf", "old", 0),
			},
			newFiles: []model.ContainerConfigFile{
				model.NewContainerConfigFile("a.conf", "unchanged", 0),
				model.NewContainerConfigFile("b.conf", "new", 0),
			},
		},
		{
			name:     "FileAdded",
			oldFiles: []model.ContainerConfigFile{model.NewContainerConfigFile("a.conf", "content", 0)},
			newFiles: []model.ContainerConfigFile{
				model.NewContainerConfigFile("a.conf", "content", 0),
				model.NewContainerConfigFile("b.conf", "new file", 0),
			},
		},
		{
			name: "FileRemoved",
			oldFiles: []model.ContainerConfigFile{
				model.NewContainerConfigFile("a.conf", "content", 0),
				model.NewContainerConfigFile("b.conf", "to be removed", 0),
			},
			newFiles: []model.ContainerConfigFile{model.NewContainerConfigFile("a.conf", "content", 0)},
		},
		{
			name:     "ModeChanged",
			oldFiles: []model.ContainerConfigFile{model.NewContainerConfigFile("run.sh", "#!/bin/sh", os.FileMode(0644))},
			newFiles: []model.ContainerConfigFile{model.NewContainerConfigFile("run.sh", "#!/bin/sh", os.FileMode(0755))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := systest.NewContainer("webapp", "nginx:latest",
				systest.Dirs(model.NewContainerDirMount(string(mountPath), tt.newFiles...)))

			env := systest.New(t, systest.WithOSConfigStore())
			env.SeedActive(spec)
			env.SeedConfigDir("webapp", mountPath, 1, tt.oldFiles...)
			env.Specs(spec)

			env.Apply()

			env.AssertNoStartStop()
			env.AssertContainerReloaded("webapp.service")
		})
	}
}

// --- Restart scenarios ---

func TestNewContainerConfigDir_RestartsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath),
			model.NewContainerConfigFile("app.conf", "content", 0)))))

	env.Apply()

	env.AssertRestarted("webapp.service")
	env.AssertContainerNotReloaded()
}

func TestNewEmptyContainerConfigDir_CreatesDir(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath)))))

	env.Apply()

	env.AssertConfigDirExists("webapp", mountPath)
	env.AssertRestarted("webapp.service")
}

func TestContainerConfigDirRemoved_RestartsService(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	files := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "content", 0),
	}

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), files...))))
	env.SeedConfigDir("webapp", mountPath, 1, files...)
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertRestarted("webapp.service")
	env.AssertContainerNotReloaded()
}

// --- No-action on stopped container ---

func TestStoppedContainerConfigDirChanged_NoAction(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "new", 0),
	}
	spec := systest.NewContainer("webapp", "nginx:latest", systest.Stopped,
		systest.Dirs(model.NewContainerDirMount(string(mountPath), newFiles...)))

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedUnit(spec)
	env.SetUnitState("webapp.service", "inactive")
	env.SeedConfigDir("webapp", mountPath, 1, oldFiles...)
	env.Specs(spec)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertContainerNotReloaded()
}

// --- Restart subsumes reload ---

func TestContainerConfigDirAndUnitChanged_RestartsWithoutReload(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "new", 0),
	}

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), oldFiles...))))
	env.SeedConfigDir("webapp", mountPath, 1, oldFiles...)
	env.Specs(systest.NewContainer("webapp", "nginx:alpine",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), newFiles...))))

	env.Apply()

	env.AssertRestarted("webapp.service")
	env.AssertContainerNotReloaded()
}
