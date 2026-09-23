//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/test/integration/systest"
)

func TestContainerImageChanged_RestartsService(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:alpine"))

	env.Apply()

	env.AssertRestarted("webapp.service")
	env.AssertReloaded()
}

// TestContainerConfigChanges varies how a running container's single-file config
// mounts differ from what is installed. Unlike a config *directory*, whose
// contents can be re-synced under a live container (see
// TestContainerConfigDirFileChanges), a file mount is baked into the container's
// bind mounts, so every variant — added, removed, or merely re-moded — must cost
// a full restart. Cases that assert on disk state do so because the restart
// alone would not prove the new bytes ever reached the file.
func TestContainerConfigChanges(t *testing.T) {
	tests := []struct {
		name      string
		image     string
		oldMounts []model.ContainerFileMount
		newMounts []model.ContainerFileMount
		preWrite  []systest.WantFile // configFiles already on disk, when the case needs them
		want      []systest.WantFile
		// wantEmptyConfigDir asserts the container's config directory was cleaned
		// out, not merely unreferenced — removal must reclaim the files too.
		wantEmptyConfigDir bool
	}{
		{
			name:      "Added",
			image:     "nginx:latest",
			oldMounts: nil,
			newMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/nginx/nginx.conf", "new config content", 0),
			},
			want: []systest.WantFile{{MountPath: "/etc/nginx/nginx.conf", Mode: 0644, Content: "new config content"}},
		},
		{
			name:  "ContentAndModeChanged",
			image: "alpine:latest",
			oldMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho old", 0),
			},
			newMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho new", os.FileMode(0755)),
			},
			preWrite: []systest.WantFile{{MountPath: "/usr/local/bin/run.sh", Mode: 0644, Content: "#!/bin/sh\necho old"}},
			want:     []systest.WantFile{{MountPath: "/usr/local/bin/run.sh", Mode: 0755, Content: "#!/bin/sh\necho new"}},
		},
		{
			name:  "ModeChanged",
			image: "alpine:latest",
			oldMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho hello", 0),
			},
			newMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho hello", os.FileMode(0755)),
			},
			preWrite: []systest.WantFile{{MountPath: "/usr/local/bin/run.sh", Mode: 0644, Content: "#!/bin/sh\necho hello"}},
			want:     []systest.WantFile{{MountPath: "/usr/local/bin/run.sh", Mode: 0755, Content: "#!/bin/sh\necho hello"}},
		},
		{
			// Removing one config restarts even though the remaining config is unchanged.
			name:  "OneRemoved",
			image: "nginx:latest",
			oldMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0),
				model.NewContainerFileMount("/etc/app/b.conf", "old content", 0),
			},
			newMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0),
			},
			preWrite: []systest.WantFile{
				{MountPath: "/etc/app/a.conf", Mode: 0644, Content: "unchanged content"},
				{MountPath: "/etc/app/b.conf", Mode: 0644, Content: "old content"},
			},
			// b.conf must be gone: AssertFiles is an exact set, so naming only
			// a.conf asserts the removal reclaimed the file too.
			want: []systest.WantFile{
				{MountPath: "/etc/app/a.conf", Mode: 0644, Content: "unchanged content"},
			},
		},
		{
			name:  "AllRemoved",
			image: "nginx:latest",
			oldMounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/config1.conf", "config1", 0),
				model.NewContainerFileMount("/etc/app/config2.conf", "config2", 0),
			},
			newMounts: nil,
			preWrite: []systest.WantFile{
				{MountPath: "/etc/app/config1.conf", Mode: 0644, Content: "config1"},
				{MountPath: "/etc/app/config2.conf", Mode: 0644, Content: "config2"},
			},
			wantEmptyConfigDir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := systest.New(t)
			env.SeedActive(systest.NewContainer("webapp", tt.image, systest.Files(tt.oldMounts...)))
			for _, pw := range tt.preWrite {
				env.SeedConfigFile("webapp", pw.MountPath, pw.Content, pw.Mode)
			}
			// Guard the premise before acting on it: a case that seeds the state
			// it means to change, but seeds it wrong, asserts nothing afterwards
			// — ModeChanged in particular would be comparing 0755 against 0755.
			env.AssertFiles("webapp", tt.preWrite...)

			env.Specs(systest.NewContainer("webapp", tt.image, systest.Files(tt.newMounts...)))

			env.Apply()

			env.AssertRestarted("webapp.service")
			env.AssertFiles("webapp", tt.want...)

			if tt.wantEmptyConfigDir {
				env.AssertConfigDirEmpty("webapp")
			}
		})
	}
}

func TestOneOfMultipleContainersChanged_RestartsOnlyChanged(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("unchanged", "nginx:latest"))
	env.SeedActive(systest.NewContainer("changed", "nginx:latest"))
	env.Specs(
		systest.NewContainer("unchanged", "nginx:latest"),
		systest.NewContainer("changed", "nginx:alpine"),
		systest.NewContainer("new", "redis:latest"),
	)

	env.Apply()

	env.AssertStopped("changed.service")
	env.AssertStarted("changed.service", "new.service")
}

func TestStoppedContainerConfigChanged_NoAction(t *testing.T) {
	env := systest.New(t)
	env.SeedUnit(systest.NewContainer("webapp", "nginx:latest", systest.Stopped))
	env.SetUnitState("webapp.service", "inactive")
	env.Specs(systest.NewContainer("webapp", "nginx:latest", systest.Stopped,
		systest.Files(model.NewContainerFileMount("/etc/app.conf", "new config", 0))))

	env.Apply()

	env.AssertFiles("webapp", systest.WantFile{MountPath: "/etc/app.conf", Mode: 0644, Content: "new config"})
	env.AssertNoStartStop()
}

func TestVolumeRecreated_RestartsContainers(t *testing.T) {
	// Container referencing a recreated volume must be stopped and restarted.
	env := systest.New(t)
	// The existing unit must carry the delete policy, or the change is rejected
	// rather than applied.
	env.SeedActive(systest.NewVolume("data", "old-device",
		systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)))
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data")))
	env.Specs(
		systest.NewVolume("data", "tmpfs", systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)),
		systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data")),
	)

	env.Apply()

	env.AssertStopped("webapp.service", "data-volume.service")
	env.AssertStarted("webapp.service")
}

func TestNetworkChanged_DoesNotRestartUnreferencedContainers(t *testing.T) {
	// Container does NOT reference the changed network — it must not be restarted.
	// The network service itself is stopped for recreation; the container is unaffected.
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewNetwork("frontend", "macvlan"))
	env.Specs(
		systest.NewContainer("webapp", "nginx:latest"),
		systest.NewNetwork("frontend", "bridge"),
	)

	env.Apply()

	env.AssertStopped("frontend-network.service")
	env.AssertNotStopped("webapp.service")
	env.AssertNoneStarted()
	env.AssertReloaded()
}

// --- ConfigDir scenarios that cost a restart ---
//
// A configDir's *contents* can be re-synced under a live container (see
// container_state_reload_test.go). Adding or removing the directory itself
// cannot: it changes the container's bind mounts, which only exist at creation.
// These tests run with WithOSConfigStore, since versioned directories use
// symlinks that afero.MemMapFs does not support.

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
	env.AssertConfigDirFiles("webapp", mountPath,
		model.NewContainerConfigFile("app.conf", "content", 0))
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
	env.AssertConfigDirEmpty("webapp")
	// The directory itself must go, not just its files: AssertConfigDirEmpty
	// lists files and a configDir is a directory, so it alone cannot see a
	// group left behind.
	env.AssertConfigDirAbsent("webapp", mountPath)
}

// TestContainerConfigDirAndUnitChanged_RestartsWithoutReload pins that a
// restart subsumes the reload rather than being issued alongside it. Reloading
// a container that is about to be stopped and started is wasted work at best,
// and at worst pushes config into an instance that is about to disappear.
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
	// The restart subsumes the reload, but the new content must still be on
	// disk — the container is about to start against it.
	env.AssertConfigDirFiles("webapp", mountPath, newFiles...)
}
