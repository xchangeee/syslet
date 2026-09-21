//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

func TestContainerImageChanged_RestartsService(t *testing.T) {
	spec := makeContainerSpec("webapp", "nginx:alpine", model.DesiredStateRunning)
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
	testutil.AssertReloaded(t, mockConn)
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
		preWrite  []wantFile // configs already on disk, when the case needs them
		want      []wantFile
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
			want: []wantFile{{"/etc/nginx/nginx.conf", 0644, "new config content"}},
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
			preWrite: []wantFile{{"/usr/local/bin/run.sh", 0644, "#!/bin/sh\necho old"}},
			want:     []wantFile{{"/usr/local/bin/run.sh", 0755, "#!/bin/sh\necho new"}},
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
			preWrite: []wantFile{{"/usr/local/bin/run.sh", 0644, "#!/bin/sh\necho hello"}},
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
			preWrite: []wantFile{
				{"/etc/app/config1.conf", 0644, "config1"},
				{"/etc/app/config2.conf", 0644, "config2"},
			},
			wantEmptyConfigDir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := makeContainerSpecWithMounts("webapp", tt.image, tt.newMounts)
			oldSpec := makeContainerSpecWithMounts("webapp", tt.image, tt.oldMounts)
			fs := afero.NewMemMapFs()

			ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
				specs:         []model.Unit{spec},
				existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
				existingState: map[string]string{"webapp.service": "active"},
			})

			for _, pw := range tt.preWrite {
				preWriteConfig(t, fs, "webapp", pw.mountPath, pw.content, pw.mode)
			}

			mustApply(t, ctx, fs, sd, mockPodman, raw)

			testutil.AssertRestarted(t, mockConn, "webapp.service")

			for _, w := range tt.want {
				path := configFilePath("webapp", w.mountPath)
				testutil.AssertFileMode(t, fs, path, w.mode)
				testutil.AssertFileContent(t, fs, path, w.content)
			}

			if tt.wantEmptyConfigDir {
				assertConfigDirEmpty(t, fs, "webapp")
			}
		})
	}
}

func TestOneOfMultipleContainersChanged_RestartsOnlyChanged(t *testing.T) {
	unchangedSpec := makeContainerSpec("unchanged", "nginx:latest", model.DesiredStateRunning)
	changedSpec := makeContainerSpec("changed", "nginx:alpine", model.DesiredStateRunning)
	newSpec := makeContainerSpec("new", "redis:latest", model.DesiredStateRunning)
	changedOldSpec := makeContainerSpec("changed", "nginx:latest", model.DesiredStateRunning)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{unchangedSpec, changedSpec, newSpec},
		existingUnits: map[string]string{
			"unchanged.container": renderContainer(t, fs, unchangedSpec),
			"changed.container":   renderContainer(t, fs, changedOldSpec),
		},
		existingState: map[string]string{
			"unchanged.service": "active",
			"changed.service":   "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertStopped(t, mockConn, "changed.service")
	testutil.AssertStarted(t, mockConn, "changed.service", "new.service")
}

func TestStoppedContainerConfigChanged_NoAction(t *testing.T) {
	spec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateStopped,
		model.NewContainerFileMount("/etc/app.conf", "new config", 0))
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateStopped)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "inactive"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	path := configFilePath("webapp", "/etc/app.conf")
	testutil.AssertFileContent(t, fs, path, "new config")
	testutil.AssertFileMode(t, fs, path, 0644)
	testutil.AssertNoStartStop(t, mockConn)
}

func TestVolumeRecreated_RestartsContainers(t *testing.T) {
	// Container referencing a recreated volume must be stopped and restarted.
	newVolumeSpec := makeVolumeSpecWithDelete("data", "tmpfs")
	oldVolumeSpec := makeVolumeSpecWithDelete("data", "old-device") // existing unit must carry the flags
	webappSpec := makeContainerSpecWithVolume("webapp", model.DesiredStateRunning, "data", "/data")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{newVolumeSpec, webappSpec},
		existingUnits: map[string]string{
			"data.volume":      renderVolume(t, oldVolumeSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{
			"webapp.service":      "active",
			"data-volume.service": "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertStopped(t, mockConn, "webapp.service", "data-volume.service")
	testutil.AssertStarted(t, mockConn, "webapp.service")
}

func TestNetworkChanged_DoesNotRestartUnreferencedContainers(t *testing.T) {
	// Container does NOT reference the changed network — it must not be restarted.
	// The network service itself is stopped for recreation; the container is unaffected.
	containerSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	networkSpec := makeNetworkSpec("frontend", "bridge")
	oldNetworkSpec := makeNetworkSpec("frontend", "macvlan")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{containerSpec, networkSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, containerSpec),
			"frontend.network": renderNetwork(t, oldNetworkSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertStopped(t, mockConn, "frontend-network.service")
	testutil.AssertNotStopped(t, mockConn, "webapp.service")
	testutil.AssertNoneStarted(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}
