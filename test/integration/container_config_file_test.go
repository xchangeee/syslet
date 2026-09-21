//go:build integration

package integration

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

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
		want   []systest.WantFile
	}{
		{
			name: "NoModeSpecified",
			mounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/app.conf", "key=value", 0),
			},
			want: []systest.WantFile{{MountPath: "/etc/app/app.conf", Mode: 0644, Content: "key=value"}},
		},
		{
			name: "ExplicitMode",
			mounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/usr/local/bin/entrypoint.sh", "#!/bin/sh\necho hello", os.FileMode(0755)),
			},
			want: []systest.WantFile{{MountPath: "/usr/local/bin/entrypoint.sh", Mode: 0755, Content: "#!/bin/sh\necho hello"}},
		},
		{
			name: "MixedModes",
			mounts: []model.ContainerFileMount{
				model.NewContainerFileMount("/etc/app/app.conf", "key=value", 0),
				model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho hi", os.FileMode(0755)),
				model.NewContainerFileMount("/etc/app/secret.conf", "secret", os.FileMode(0600)),
			},
			want: []systest.WantFile{
				{MountPath: "/etc/app/app.conf", Mode: 0644, Content: systest.AnyContent},
				{MountPath: "/usr/local/bin/run.sh", Mode: 0755, Content: systest.AnyContent},
				{MountPath: "/etc/app/secret.conf", Mode: 0600, Content: systest.AnyContent},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := systest.New(t)
			env.Specs(systest.NewContainer("webapp", "alpine:latest", systest.Files(tt.mounts...)))

			env.Apply()

			env.AssertFiles("webapp", tt.want...)
		})
	}
}

// TestContainerConfigFileRemoved_RemovesFile pins that dropping one file mount
// reclaims that file and leaves the others alone. The restart it also costs is
// covered by TestContainerConfigChanges/OneRemoved, alongside the other
// service-transition outcomes.
func TestContainerConfigFileRemoved_RemovesFile(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.Files(
		model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0),
		model.NewContainerFileMount("/etc/app/b.conf", "old content", 0),
	)))
	env.SeedConfigFile("webapp", "/etc/app/a.conf", "unchanged content", 0644)
	env.SeedConfigFile("webapp", "/etc/app/b.conf", "old content", 0644)

	env.Specs(systest.NewContainer("webapp", "nginx:latest",
		systest.Files(model.NewContainerFileMount("/etc/app/a.conf", "unchanged content", 0))))

	env.Apply()

	env.AssertConfigFileExists("webapp", "/etc/app/a.conf")
	env.AssertConfigFileAbsent("webapp", "/etc/app/b.conf")
}
