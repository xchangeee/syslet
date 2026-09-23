//go:build integration

package integration

import (
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/test/integration/systest"
)

func TestNetworkMeaningfulChange_RecreatesAndRestartsContainers(t *testing.T) {
	updated := systest.NewNetwork("frontend", "macvlan")

	env := systest.New(t)
	env.SeedUnit(systest.NewNetwork("frontend", "bridge"))
	env.SetUnitState("frontend-network.service", "active")
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithNetwork("frontend")))
	env.Specs(
		updated,
		systest.NewContainer("webapp", "nginx:latest", systest.WithNetwork("frontend")),
	)

	env.Apply()

	env.AssertStopped("webapp.service", "frontend-network.service")
	env.AssertStarted("webapp.service")
	env.AssertNetworksDeleted("frontend")
	// Driver= must have actually changed on disk; the unit was already there.
	env.AssertUnitMatches(updated)
	env.AssertReloaded()
}

func TestNetworkMetadataOnlyChange_NoRecreation(t *testing.T) {
	env := systest.New(t)
	// Old spec: RemovalAllowed=false.
	env.SeedUnit(systest.NewNetwork("frontend", "bridge"))
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithNetwork("frontend")))
	// New spec: RemovalAllowed=true (metadata-only change), same Driver=bridge.
	env.Specs(
		systest.NewNetwork("frontend", "bridge", systest.Removable),
		systest.NewContainer("webapp", "nginx:latest", systest.WithNetwork("frontend")),
	)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertNoNetworksDeleted()
	env.AssertReloaded()
}

func TestNewNetwork_WritesUnitOnly(t *testing.T) {
	spec := systest.NewNetwork("frontend", "bridge")

	env := systest.New(t)
	env.Specs(spec)

	env.Apply()

	env.AssertUnitMatches(spec)
	env.AssertReloaded()
	env.AssertNoStartStop()
}

func TestStaleNetwork_RemovesUnit(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewNetwork("oldnet", "bridge", systest.Removable))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitAbsent("oldnet.network")
	env.AssertNoStartStop()
	env.AssertReloaded()
}

func TestStaleNetworkWithDeletePolicy_DeletesPodmanNetwork(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewNetwork("oldnet", "bridge",
		systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitAbsent("oldnet.network")
	env.AssertNetworksDeleted("oldnet")
}
