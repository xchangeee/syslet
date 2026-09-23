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

func TestContainerDesiredStateStopped_StopsService(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest", systest.Stopped))

	env.Apply()

	env.AssertStopped("webapp.service")
	env.AssertNoneStarted()
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
