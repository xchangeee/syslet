//go:build integration

package integration

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

// An in-place reload is its own outcome, never interchangeable with a restart:
// a restart costs downtime, a reload does not. Only one thing earns it — a
// change confined to a mounted config *directory*, whose contents can be
// re-synced under a live container through the ..data symlink. Everything else,
// including a file mount, is baked into the container's bind mounts and costs a
// full restart.
//
// These tests run with WithOSConfigStore: the versioned-directory logic uses
// symlinks, which afero.MemMapFs does not support.

// TestContainerConfigDirFileChanges varies the kind of change made to a mounted
// config directory while the container's own unit file stays byte-identical.
// Every variant must take the cheap path: the contents are re-synced and the
// service is reloaded in place, never stopped and started. Restart-worthy
// changes (a new or removed directory, a changed unit) live with the restart
// scenarios, since there the outcome differs rather than the condition.
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
			// The reload is only meaningful if the bytes behind ..data are the
			// new ones: a reload over stale content is worse than none, because
			// it tells the application its config was refreshed when it was not.
			env.AssertConfigDirFiles("webapp", mountPath, tt.newFiles...)
			env.AssertConfigDirVersion("webapp", mountPath, 2)
			// The superseded version is reclaimed, or a host accumulates one
			// directory per deploy forever.
			env.AssertConfigDirVersionsKept("webapp", mountPath, 2)
		})
	}
}
