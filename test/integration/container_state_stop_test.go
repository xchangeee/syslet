//go:build integration

package integration

import (
	"testing"

	"github.com/xchangeee/syslet/test/integration/systest"
)

func TestNewStoppedContainer_WritesUnitOnly(t *testing.T) {
	env := systest.New(t)
	env.Specs(systest.NewContainer("webapp", "nginx:latest", systest.Stopped))

	env.Apply()

	env.AssertUnitExists("webapp.container")
	env.AssertNoStartStop()
	env.AssertReloaded()
}

// TestNewStoppedContainer_ServiceAlreadyActive_StopsService is the stopped
// counterpart of TestNewContainer_ServiceAlreadyActive_RestartsService.
func TestNewStoppedContainer_ServiceAlreadyActive_StopsService(t *testing.T) {
	env := systest.New(t)
	env.SetUnitState("webapp.service", "active")
	env.Specs(systest.NewContainer("webapp", "nginx:latest", systest.Stopped))

	env.Apply()

	env.AssertUnitExists("webapp.container")
	env.AssertStopped("webapp.service")
	env.AssertNoneStarted()
}

func TestContainerDesiredStateStopped_StopsService(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest", systest.Stopped))

	env.Apply()

	env.AssertStopped("webapp.service")
	env.AssertNoneStarted()
}

// TestEmptyInput_StaleContainers asserts that an input declaring no specs is a
// desired state like any other: every container whose removal is allowed is
// pruned, and a locked one is kept running.
func TestEmptyInput_StaleContainers(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("old", "redis:latest", systest.Stale))
	env.SeedActive(systest.NewContainer("locked", "nginx:latest"))

	env.Apply()

	t.Run("StopsAndRemovesUnit", func(t *testing.T) {
		env.With(t).AssertStopped("old.service")
		env.With(t).AssertUnitAbsent("old.container")
	})
	t.Run("KeepsLockedUnit", func(t *testing.T) {
		env.With(t).AssertUnitExists("locked.container")
		env.With(t).AssertNotStopped("locked.service")
	})
}

func TestStaleContainer_StopsAndRemovesUnit(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedActive(systest.NewContainer("old", "redis:latest", systest.Stale))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertStopped("old.service")
	env.AssertUnitAbsent("old.container")
	env.AssertNoneStarted()
}
