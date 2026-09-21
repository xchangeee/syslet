//go:build integration

package integration

import (
	"fmt"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd/systemdtest"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

func TestNewRunningContainer_StartsService(t *testing.T) {
	env := systest.New(t)
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitExists("webapp.container")
	env.AssertStarted("webapp.service")
	env.AssertNoneStopped()
	env.AssertReloaded()
}

// TestContainerSpecValidationError covers the guard that stops a plan carrying
// errors from being executed at all.
//
// A validation failure is recorded as a *generic* plan error, which — unlike a
// per-unit error — leaves no errored entry in plan.Results. Apply's trailing
// "did any unit fail" loop therefore cannot see it, and the explicit HasErrors
// check at the top of Apply is the only thing standing between an invalid spec
// and a unit file on disk.
func TestContainerSpecValidationError(t *testing.T) {
	// The X-Syslet section is reserved; ValidateUnits rejects a spec declaring it.
	env := systest.New(t)
	env.Specs(model.NewContainerUnit(
		model.ContainerUnitRef("myapp"),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV("nginx:latest")},
			"X-Syslet":  {model.SectionKey("SomeKey"): model.UV("value")},
		},
		model.DesiredStateRunning, nil, false,
	))

	err := env.ApplyErr()

	t.Run("Errors", func(t *testing.T) {
		if err == nil {
			t.Error("expected apply to be refused for a spec that fails validation")
		}
	})

	t.Run("WritesNoUnit", func(t *testing.T) {
		env.With(t).AssertUnitAbsent("myapp.container")
	})

	t.Run("NoAction", func(t *testing.T) {
		env.With(t).AssertNoStartStop()
		env.With(t).AssertNotReloaded()
	})
}

func TestContainerQuadletError_DoesNotStart(t *testing.T) {
	env := systest.New(t, systest.WithJournalReader(&systemdtest.MockJournalReader{
		Messages: []string{"webapp.container: Invalid key 'BadKey' in section Container"},
	}))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))
	env.Conn.ReloadErr = fmt.Errorf("daemon-reload failed")

	err := env.ApplyErr()

	if err == nil {
		t.Error("expected syslet.Apply to return an error when daemon-reload fails")
	}
	env.AssertNoneStarted()
}
