//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

func TestContainerUnchanged_NoAction(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertNoStartStop()
	env.AssertNotReloaded()
}

// TestContainerUnitReordered_NoAction verifies that pure reordering of multi-value
// entries like Network= is not semantically meaningful and must not cause downtime.
func TestContainerUnitReordered_NoAction(t *testing.T) {
	env := systest.New(t)
	spec := systest.NewContainer("webapp", "nginx:latest",
		systest.WithNetwork("backend"), systest.WithNetwork("frontend"))
	canonical := env.Render(spec)

	// Swap the two Network= lines to simulate a file written in a different order.
	reordered := strings.Replace(
		strings.Replace(canonical, "Network=backend.network\n", "__PLACEHOLDER__\n", 1),
		"Network=frontend.network\n", "Network=backend.network\n", 1)
	reordered = strings.Replace(reordered, "__PLACEHOLDER__\n", "Network=frontend.network\n", 1)

	if reordered == canonical {
		t.Fatal("test setup error: reordered content is identical to canonical")
	}

	env.SeedUnitFile("webapp.container", reordered)
	env.SetUnitState("webapp.service", "active")
	env.Specs(spec, systest.NewNetwork("backend", ""), systest.NewNetwork("frontend", ""))

	env.Apply()

	env.AssertNoStartStop()
}

func TestContainerConfigUnchanged_NoAction(t *testing.T) {
	script := "#!/bin/sh\necho hello"
	mountPath := "/usr/local/bin/run.sh"

	spec := systest.NewContainer("webapp", "alpine:latest",
		systest.Files(model.NewContainerFileMount(mountPath, script, os.FileMode(0755))))

	env := systest.New(t)
	env.SeedActive(spec)
	env.SeedConfigFile("webapp", mountPath, script, 0755)
	env.Specs(spec)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertFiles("webapp", systest.WantFile{MountPath: mountPath, Mode: 0755})
}

func TestNewOneshotContainer_WritesUnitOnly(t *testing.T) {
	env := systest.New(t)
	env.Specs(systest.NewContainer("oneshot-container", "alpine:latest", systest.Oneshot))

	env.Apply()

	env.AssertUnitExists("oneshot-container.container")
	env.AssertNoStartStop()
	env.AssertReloaded()
}

func TestOneshotContainerUnitChanged_NoAction(t *testing.T) {
	env := systest.New(t)
	env.SeedUnit(systest.NewContainer("oneshot-container", "alpine:latest", systest.Oneshot))
	env.SetUnitState("oneshot-container.service", "inactive")
	env.Specs(systest.NewContainer("oneshot-container", "alpine:edge", systest.Oneshot))

	env.Apply()

	env.AssertUnitExists("oneshot-container.container")
	env.AssertNoStartStop()
	env.AssertReloaded()
}

func TestStaleOneshotContainer_RemovesUnitWithoutStop(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewContainer("old-oneshot", "alpine:latest", systest.Oneshot, systest.Stale))
	env.SetUnitState("old-oneshot.service", "inactive")
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitAbsent("old-oneshot.container")
	env.AssertUnitExists("webapp.container")
	env.AssertNoStartStop()
	env.AssertReloaded()
}
