//go:build integration

package integration

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

// Reaching a fixed point in one pass is the defining property of a converger:
// applying the same specs to the host the last apply produced must do nothing
// at all.
//
// It is also the assertion that catches what after-the-fact checks structurally
// cannot. Every "did apply do the right thing" assertion reads the host through
// the same understanding of the layout that apply used to write it, so a
// mistake shared by both — a unit file written from the wrong spec, content
// written under a filename the plan will not look for, a secret stored without
// the label its own change detection reads — looks correct from every angle
// except the next run's. On the next run it shows up as work that should not
// exist.
//
// These cases are therefore chosen for coverage of the *stores*, not of the
// scenarios: one per kind of state apply writes and later reads back.

// TestSecondApply_NoAction applies twice and requires the second run to be a
// complete no-op.
func TestSecondApply_NoAction(t *testing.T) {
	const dirMount = model.ContainerMountPath("/etc/app/")

	tests := []struct {
		name  string
		opts  []systest.Option
		setup func(env *systest.Env)
	}{
		{
			name: "NewContainer",
			setup: func(env *systest.Env) {
				env.Specs(systest.NewContainer("webapp", "nginx:latest"))
			},
		},
		{
			name: "ContainerImageChanged",
			setup: func(env *systest.Env) {
				env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
				env.Specs(systest.NewContainer("webapp", "nginx:alpine"))
			},
		},
		{
			name: "ContainerConfigFileChanged",
			setup: func(env *systest.Env) {
				env.SeedActive(systest.NewContainer("webapp", "alpine:latest",
					systest.Files(model.NewContainerFileMount("/etc/app.conf", "old", 0))))
				env.SeedConfigFile("webapp", "/etc/app.conf", "old", 0644)
				env.Specs(systest.NewContainer("webapp", "alpine:latest",
					systest.Files(model.NewContainerFileMount("/etc/app.conf", "new", os.FileMode(0600)))))
			},
		},
		{
			name: "ContainerConfigDirChanged",
			opts: []systest.Option{systest.WithOSConfigStore()},
			setup: func(env *systest.Env) {
				oldFiles := []model.ContainerConfigFile{model.NewContainerConfigFile("app.conf", "old", 0)}
				newFiles := []model.ContainerConfigFile{model.NewContainerConfigFile("app.conf", "new", 0)}
				env.SeedActive(systest.NewContainer("webapp", "nginx:latest",
					systest.Dirs(model.NewContainerDirMount(string(dirMount), oldFiles...))))
				env.SeedConfigDir("webapp", dirMount, 1, oldFiles...)
				env.Specs(systest.NewContainer("webapp", "nginx:latest",
					systest.Dirs(model.NewContainerDirMount(string(dirMount), newFiles...))))
			},
		},
		{
			name: "BuildContextChanged",
			setup: func(env *systest.Env) {
				env.SeedBuildContext("myapp", "Containerfile", "FROM ubuntu", 0644)
				env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:latest"))
				env.SeedActive(systest.NewBuiltContainer("webapp", "myapp"))
				env.Specs(
					systest.NewBuild("myapp", "localhost/myapp:latest"),
					systest.NewBuiltContainer("webapp", "myapp"),
				)
			},
		},
		{
			name: "VolumeRecreated",
			setup: func(env *systest.Env) {
				env.SeedActive(systest.NewVolume("data", "old-device", deletable()...))
				env.SeedActive(systest.NewContainer("webapp", "nginx:latest",
					systest.WithVolume("data", "/data")))
				env.Specs(
					systest.NewVolume("data", "tmpfs", deletable()...),
					systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data")),
				)
			},
		},
		{
			// Secrets are the case this suite exists for. Every other store is
			// read back as content, so a single apply can inspect what landed;
			// the secret store is read back as the syslet/hash label, which no
			// assertion on the upsert itself can validate. Tests that seed that
			// label by hand check the comparison against a value they chose —
			// only a second apply checks it against the value apply wrote. Get
			// it wrong and every run re-upserts every key and restarts every
			// container consuming them, forever.
			name: "SecretApplied",
			opts: []systest.Option{systest.WithDecryptor(secretTestDecryptor(t))},
			setup: func(env *systest.Env) {
				env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))
			},
		},
		{
			name: "StaleUnitsPruned",
			setup: func(env *systest.Env) {
				env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
				env.SeedActive(systest.NewContainer("old", "redis:latest", systest.Stale))
				env.SeedUnit(systest.NewVolume("olddata", "tmpfs", deletable()...))
				env.Specs(systest.NewContainer("webapp", "nginx:latest"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := systest.New(t, tt.opts...)
			tt.setup(env)

			env.AssertIdempotent()
		})
	}
}
