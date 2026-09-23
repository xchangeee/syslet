//go:build integration

package integration

import (
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/test/integration/systest"
)

// TestStaleResource_ReferencedBySkippedContainer covers a container and the
// resource it uses dropping out of the input together, where the container is
// protected (skipped, left running) and the resource would otherwise be removed
// and reclaimed. validate.PostRender only checks references within the desired
// set, so this is the planner's host-side guard: the resource must be skipped
// whole, since deleting just its unit file already breaks the container's
// quadlet generation on the next daemon-reload.
func TestStaleResource_ReferencedBySkippedContainer(t *testing.T) {
	t.Run("Volume", func(t *testing.T) {
		env := systest.New(t)
		env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data")))
		env.SeedUnit(systest.NewVolume("data", "tmpfs", deletable()...))
		env.Specs()

		plan := env.Plan()
		env.Apply()

		env.AssertUnitExists("data.volume")
		env.AssertNoVolumesDeleted()
		env.AssertNoStartStop()
		systest.AssertPlanOutputContains(t, plan, "still referenced by webapp.container")
	})

	t.Run("Network", func(t *testing.T) {
		env := systest.New(t)
		env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithNetwork("frontend")))
		env.SeedUnit(systest.NewNetwork("frontend", "bridge", systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)))
		env.Specs()

		plan := env.Plan()
		env.Apply()

		env.AssertUnitExists("frontend.network")
		env.AssertNoNetworksDeleted()
		systest.AssertPlanOutputContains(t, plan, "still referenced by webapp.container")
	})

	t.Run("Build", func(t *testing.T) {
		env := systest.New(t)
		env.SeedActive(systest.NewBuiltContainer("webapp", "myapp"))
		env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:latest", systest.Reclaim(model.ReclaimPolicyDelete)))
		env.SeedBuildContext("myapp", "Containerfile", "FROM scratch", 0o644)
		env.Specs()

		plan := env.Plan()
		env.Apply()

		env.AssertUnitExists("myapp.build")
		env.AssertBuildContextFiles("myapp", systest.WantFile{MountPath: "Containerfile", Content: "FROM scratch", Mode: 0o644})
		env.AssertNoImagesDeleted()
		systest.AssertPlanOutputContains(t, plan, "still referenced by webapp.container")
	})
}

// TestStaleVolume_ReferencedByRemovedContainer_DeletesPodmanVolume pins the
// boundary of the guard above: a referencing container that is itself removed
// is stopped before reclamation runs, so it must not hold the volume back.
func TestStaleVolume_ReferencedByRemovedContainer_DeletesPodmanVolume(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data"), systest.Unit(systest.Removable)))
	env.SeedUnit(systest.NewVolume("data", "tmpfs", deletable()...))
	env.Specs()

	env.Apply()

	env.AssertUnitAbsent("data.volume")
	env.AssertVolumesDeleted("data")
}
