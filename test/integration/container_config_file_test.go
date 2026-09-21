//go:build integration

package integration

import (
	"os"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

// wantFile is the expected on-disk result for one mounted config file. An empty
// content means the case only cares about the mode.
type wantFile struct {
	mountPath string
	mode      os.FileMode
	content   string
}

// TestContainerConfigFileModes varies how a container spec declares the mode of
// its mounted config files, and pins what lands on disk for each form. The mode
// is the container's only contract with these files — an entrypoint script that
// loses its exec bit, or a secret that gains a readable one, breaks the unit
// without any unit-file change to point at — so every declaration form is
// covered here rather than left to the default.
func TestContainerConfigFileModes(t *testing.T) {
	tests := []struct {
		name   string
		mounts []model.ContainerFileMount
		want   []wantFile
	}{
		{
			name: "NoModeSpecified",
			mounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/app.conf", "key=value", 0),
			},
			want: []wantFile{{"/etc/app/app.conf", 0644, "key=value"}},
		},
		{
			name: "ExplicitMode",
			mounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/usr/local/bin/entrypoint.sh", "#!/bin/sh\necho hello", os.FileMode(0755)),
			},
			want: []wantFile{{"/usr/local/bin/entrypoint.sh", 0755, "#!/bin/sh\necho hello"}},
		},
		{
			name: "MixedModes",
			mounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/app.conf", "key=value", 0),
				model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho hi", os.FileMode(0755)),
				model.NewContainerFileMount("/etc/app/secret.conf", "secret", os.FileMode(0600)),
			},
			want: []wantFile{
				{mountPath: "/etc/app/app.conf", mode: 0644},
				{mountPath: "/usr/local/bin/run.sh", mode: 0755},
				{mountPath: "/etc/app/secret.conf", mode: 0600},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := []model.Unit{
				makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning, tt.mounts...),
			}

			ctx, fs, sd, _, mockPodman, raw := setupTest(t, testFixture{specs: specs})
			mustApply(t, ctx, fs, sd, mockPodman, raw)

			for _, w := range tt.want {
				path := configFilePath("webapp", w.mountPath)
				testutil.AssertFileMode(t, fs, path, w.mode)
				if w.content != "" {
					testutil.AssertFileContent(t, fs, path, w.content)
				}
			}
		})
	}
}

func TestContainerConfigFileModeChanged_UpdatesMode(t *testing.T) {
	script := "#!/bin/sh\necho hello"
	mountPath := "/usr/local/bin/run.sh"

	spec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, os.FileMode(0755)))
	oldSpec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, 0))
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", mountPath, script, 0644)
	testutil.AssertFileMode(t, fs, configFilePath("webapp", mountPath), 0644)

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertFileMode(t, fs, configFilePath("webapp", mountPath), 0755)
	testutil.AssertFileContent(t, fs, configFilePath("webapp", mountPath), script)
}

func TestContainerConfigFileRemoved_RemovesFile(t *testing.T) {
	// Container removes b.conf while a.conf content is unchanged — must still restart.
	spec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0))
	oldSpec := makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0),
		model.NewContainerFileMount("/etc/app/b.conf", "old content", 0))
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", "/etc/app/a.conf", "unchanged content", 0644)
	preWriteConfig(t, fs, "webapp", "/etc/app/b.conf", "old content", 0644)

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	assertConfigFileExists(t, fs, "webapp", "/etc/app/a.conf")
	assertConfigFileAbsent(t, fs, "webapp", "/etc/app/b.conf")
}
