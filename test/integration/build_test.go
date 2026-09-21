//go:build integration

package integration

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

func TestStaleBuild_RemovesUnit(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewBuild("oldapp", "oldapp:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitAbsent("oldapp.build")
	env.AssertNoStartStop()
	env.AssertReloaded()
	env.AssertNoImagesDeleted()
}

// TestStaleBuildWithDeletePolicy covers what a stale build unit carrying the
// Delete reclaim policy triggers: the unit file goes away (as it would under any
// policy) and, unlike the default policy, the backing podman image is reclaimed.
// The second outcome guards the blast radius — reclamation must not reach images
// belonging to builds that are still in the desired set.
func TestStaleBuildWithDeletePolicy(t *testing.T) {
	t.Run("DeletesPodmanImage", func(t *testing.T) {
		env := systest.New(t)
		env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
		env.SeedUnit(systest.NewBuild("oldapp", "oldapp:latest", systest.Reclaim(model.ReclaimPolicyDelete)))
		env.Specs(systest.NewContainer("webapp", "nginx:latest"))

		env.Apply()

		env.AssertUnitAbsent("oldapp.build")
		env.AssertImagesDeleted("oldapp:latest")
	})

	t.Run("DeletesOnlyTaggedImage", func(t *testing.T) {
		// myapp is active and referenced by a container; oldapp is stale and unreferenced.
		// Only oldapp's image tag should be deleted; myapp's tag must be left untouched.
		env := systest.New(t)
		env.SeedActive(systest.NewBuiltContainer("webapp", "myapp"))
		env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:latest"))
		env.SeedUnit(systest.NewBuild("oldapp", "oldapp:latest", systest.Reclaim(model.ReclaimPolicyDelete)))
		env.Specs(
			systest.NewBuiltContainer("webapp", "myapp"),
			systest.NewBuild("myapp", "localhost/myapp:latest"),
		)

		env.Apply()

		env.AssertUnitAbsent("oldapp.build")
		env.AssertUnitExists("myapp.build")
		env.AssertImagesDeleted("oldapp:latest")
	})
}

func TestBuildMeaningfulChange_RecreatesAndRestartsContainers(t *testing.T) {
	// New spec changes ImageTag — a meaningful change in the [Build] section.
	env := systest.New(t)
	// Pre-write the same Containerfile so context files are unchanged.
	env.SeedBuildContext("myapp", "Containerfile", "FROM scratch", 0644)
	env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:v1"))
	env.SeedActive(systest.NewBuiltContainer("webapp", "myapp"))
	env.Specs(
		systest.NewBuild("myapp", "localhost/myapp:v2"),
		systest.NewBuiltContainer("webapp", "myapp"),
	)

	env.Apply()

	env.AssertStopped("webapp.service", "myapp-build.service")
	env.AssertStarted("webapp.service")
	env.AssertNoImagesDeleted()
	env.AssertUnitExists("myapp.build")
	env.AssertReloaded()
}

func TestBuildContextFileChange_RecreatesAndRestartsContainers(t *testing.T) {
	// Unit file is unchanged; only the Containerfile content differs.
	env := systest.New(t)
	// Pre-write an old Containerfile so the context file shows as changed.
	env.SeedBuildContext("myapp", "Containerfile", "FROM ubuntu", 0644)
	env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:latest"))
	env.SeedActive(systest.NewBuiltContainer("webapp", "myapp"))
	env.Specs(
		systest.NewBuild("myapp", "localhost/myapp:latest"),
		systest.NewBuiltContainer("webapp", "myapp"),
	)

	env.Apply()

	env.AssertStopped("webapp.service", "myapp-build.service")
	env.AssertStarted("webapp.service")
	env.AssertNoImagesDeleted()
	env.AssertUnitExists("myapp.build")
	// No daemon-reload: the quadlet unit file is unchanged, only the Containerfile changed.
}

func TestBuildMetadataOnlyChange_NoRecreation(t *testing.T) {
	// New spec changes ReclaimPolicy (written to [X-Syslet]) — a metadata-only change.
	env := systest.New(t)
	// Pre-write the same Containerfile so context files are unchanged.
	env.SeedBuildContext("myapp", "Containerfile", "FROM scratch", 0644)
	env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:latest"))
	env.SeedActive(systest.NewBuiltContainer("webapp", "myapp"))
	env.Specs(
		systest.NewBuild("myapp", "localhost/myapp:latest", systest.Reclaim(model.ReclaimPolicyDelete)),
		systest.NewBuiltContainer("webapp", "myapp"),
	)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertNoImagesDeleted()
	env.AssertReloaded()
}
