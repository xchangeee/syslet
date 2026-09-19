package syslet

import (
	"os"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

func TestApply_Container_ImageChanged_RestartsService(t *testing.T) {
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

func TestApply_Container_ConfigAdded_RestartsService(t *testing.T) {
	spec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/nginx/nginx.conf", "new config content", 0))
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
	path := configFilePath("webapp", "/etc/nginx/nginx.conf")
	testutil.AssertFileContent(t, fs, path, "new config content")
	testutil.AssertFileMode(t, fs, path, 0644)
}

func TestApply_Container_ConfigContentAndModeChanged_RestartsService(t *testing.T) {
	mountPath := "/usr/local/bin/run.sh"

	spec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, "#!/bin/sh\necho new", os.FileMode(0755)))
	oldSpec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, "#!/bin/sh\necho old", 0))
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", mountPath, "#!/bin/sh\necho old", 0644)

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
	path := configFilePath("webapp", mountPath)
	testutil.AssertFileMode(t, fs, path, 0755)
	testutil.AssertFileContent(t, fs, path, "#!/bin/sh\necho new")
}

func TestApply_Container_ConfigModeChanged_RestartsService(t *testing.T) {
	script := "#!/bin/sh\necho hello"
	mountPath := "/usr/local/bin/run.sh"

	spec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, os.FileMode(0755)))
	oldSpec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, 0))
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", mountPath, script, 0644)

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
}

func TestApply_Container_ConfigRemoved_RestartsService(t *testing.T) {
	// Removing a config file triggers a restart even when remaining configs are unchanged.
	spec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0))
	oldSpec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0),
		model.NewContainerFileMount("/etc/app/b.conf", "old content", 0))
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
}

func TestApply_Container_AllConfigsRemoved_RestartsService(t *testing.T) {
	spec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	oldSpec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/config1.conf", "config1", 0),
		model.NewContainerFileMount("/etc/app/config2.conf", "config2", 0))
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertRestarted(t, mockConn, "webapp.service")
}

func TestApply_MultipleContainers_OneChanged_RestartsOnlyChanged(t *testing.T) {
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

func TestApply_StoppedContainer_ConfigChanged_NoAction(t *testing.T) {
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

func TestApply_Volume_Recreated_ContainerRestarted(t *testing.T) {
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

func TestApply_Network_Changed_UnreferencedContainerNotRestarted(t *testing.T) {
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
