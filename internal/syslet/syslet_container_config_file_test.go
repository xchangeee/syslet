package syslet

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"

	"github.com/spf13/afero"
)

func TestApply_ContainerConfigFile_NoModeSpecified_DefaultsTo0644(t *testing.T) {
	specs := []model.Unit{
		makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
			model.NewContainerFileMount("/etc/app/app.conf", "key=value", 0)),
	}

	ctx, fs, sd, _, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})
	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	path := configFilePath("webapp", "/etc/app/app.conf")
	testutil.AssertFileMode(t, fs, path, 0644)
	testutil.AssertFileContent(t, fs, path, "key=value")
}

func TestApply_ContainerConfigFile_ExplicitMode_WrittenWithCorrectPermissions(t *testing.T) {
	specs := []model.Unit{
		makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
			model.NewContainerFileMount("/usr/local/bin/entrypoint.sh", "#!/bin/sh\necho hello", os.FileMode(0755))),
	}

	ctx, fs, sd, _, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})
	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	path := configFilePath("webapp", "/usr/local/bin/entrypoint.sh")
	testutil.AssertFileMode(t, fs, path, 0755)
	testutil.AssertFileContent(t, fs, path, "#!/bin/sh\necho hello")
}

func TestApply_ContainerConfigFile_MixedModes_EachWrittenWithOwnPermissions(t *testing.T) {
	specs := []model.Unit{
		makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
			model.NewContainerFileMount("/etc/app/app.conf", "key=value", 0),
			model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho hi", os.FileMode(0755)),
			model.NewContainerFileMount("/etc/app/secret.conf", "secret", os.FileMode(0600))),
	}

	ctx, fs, sd, _, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})
	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertFileMode(t, fs, configFilePath("webapp", "/etc/app/app.conf"), 0644)
	testutil.AssertFileMode(t, fs, configFilePath("webapp", "/usr/local/bin/run.sh"), 0755)
	testutil.AssertFileMode(t, fs, configFilePath("webapp", "/etc/app/secret.conf"), 0600)
}

func TestApply_ContainerConfigFile_ModeChanged_UpdatesPermissions(t *testing.T) {
	script := "#!/bin/sh\necho hello"
	mountPath := "/usr/local/bin/run.sh"

	spec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, os.FileMode(0755)))
	oldSpec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, 0))
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", mountPath, script, 0644)
	testutil.AssertFileMode(t, fs, configFilePath("webapp", mountPath), 0644)

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertFileMode(t, fs, configFilePath("webapp", mountPath), 0755)
	testutil.AssertFileContent(t, fs, configFilePath("webapp", mountPath), script)
}

func TestApply_ContainerConfigFile_Removed_DeletedFromDisk(t *testing.T) {
	// Container removes b.conf while a.conf content is unchanged — must still restart.
	spec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0))
	oldSpec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0),
		model.NewContainerFileMount("/etc/app/b.conf", "old content", 0))
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", "/etc/app/a.conf", "unchanged content", 0644)
	preWriteConfig(t, fs, "webapp", "/etc/app/b.conf", "old content", 0644)

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	assertConfigFileExists(t, fs, "webapp", "/etc/app/a.conf")
	assertConfigFileAbsent(t, fs, "webapp", "/etc/app/b.conf")
}

func TestApply_Container_Stale_DeletesConfigDirectory(t *testing.T) {
	spec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	oldSpec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/config1.conf", "config1", 0),
		model.NewContainerFileMount("/etc/app/config2.conf", "config2", 0))
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", "/etc/app/config1.conf", "config1", 0644)
	preWriteConfig(t, fs, "webapp", "/etc/app/config2.conf", "config2", 0644)

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	assertConfigDirEmpty(t, fs, "webapp")
}
