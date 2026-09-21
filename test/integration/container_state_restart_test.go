//go:build integration

package integration

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
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
		preWrite  []systest.WantFile // configs already on disk, when the case needs them
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
