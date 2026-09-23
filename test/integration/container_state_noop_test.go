//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/test/integration/systest"
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
	env.AssertFiles("webapp", systest.WantFile{MountPath: mountPath, Mode: 0755, Content: script})
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

// --- ConfigDir scenarios that cost nothing ---
//
// These run with WithOSConfigStore: versioned directories use symlinks, which
// afero.MemMapFs does not support.

func TestContainerConfigDirUnchanged_NoAction(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	files := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "key=value", 0),
	}
	spec := systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), files...)))

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(spec)
	env.SeedConfigDir("webapp", mountPath, 1, files...)
	env.Specs(spec)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertContainerNotReloaded()
	// An unchanged configDir must not be rewritten into a new version: doing so
	// would churn the ..data symlink on every deploy and, since a new version is
	// what triggers the reload, reload the container for nothing.
	env.AssertConfigDirVersion("webapp", mountPath, 1)
	env.AssertConfigDirFiles("webapp", mountPath, files...)
}

// TestStoppedContainerConfigDirChanged_NoAction covers a configDir change on a
// container that is not running.
//
// "No action" means no service transition. The new content is still written to
// disk and the version advanced, so the container starts against current config
// whenever it is next started; only the reload that would have pushed it into a
// live container is skipped.
func TestStoppedContainerConfigDirChanged_NoAction(t *testing.T) {
	mountPath := model.ContainerMountPath("/etc/app/")
	oldFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "old", 0),
	}
	newFiles := []model.ContainerConfigFile{
		model.NewContainerConfigFile("app.conf", "new", 0),
	}
	spec := systest.NewContainer("webapp", "nginx:latest", systest.Stopped,
		systest.Dirs(model.NewContainerDirMount(string(mountPath), newFiles...)))

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedUnit(spec)
	env.SetUnitState("webapp.service", "inactive")
	env.SeedConfigDir("webapp", mountPath, 1, oldFiles...)
	env.Specs(spec)

	env.Apply()

	env.AssertNoStartStop()
	env.AssertContainerNotReloaded()
	env.AssertConfigDirFiles("webapp", mountPath, newFiles...)
	env.AssertConfigDirVersion("webapp", mountPath, 2)
}
