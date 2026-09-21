//go:build integration

package integration

import (
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd/systemdtest"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
)

// deletable marks a volume as removable with the Delete reclaim policy — the
// combination that lets a meaningful change recreate the backing podman volume.
func deletable() []systest.UnitOpt {
	return []systest.UnitOpt{systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)}
}

func TestNewVolume_WritesUnitOnly(t *testing.T) {
	spec := systest.NewVolume("data", "tmpfs")

	env := systest.New(t)
	env.Specs(spec)

	env.Apply()

	env.AssertUnitMatches(spec)
	env.AssertReloaded()
	env.AssertNoStartStop()
}

// TestVolumeMetadataOnlyChange covers a volume spec whose [X-Syslet] metadata
// changed while Device= — the part podman actually materializes — did not. The
// distinction is what keeps a reclaim-policy edit from being destructive: the
// unit file is rewritten, but the backing podman volume survives and containers
// mounting it keep running. Both halves are asserted here because either one
// failing alone would still lose data or availability.
func TestVolumeMetadataOnlyChange(t *testing.T) {
	// removalAllowed flips from false to true — metadata-only, same Device=.
	t.Run("WritesUnit", func(t *testing.T) {
		updated := systest.NewVolume("data", "tmpfs", systest.Removable)

		env := systest.New(t)
		env.SeedUnit(systest.NewVolume("data", "tmpfs"))
		env.Specs(updated)

		env.Apply()

		env.AssertNoVolumesDeleted()
		// The unit was seeded, so its mere existence proves nothing; only its
		// content shows the metadata change actually reached disk.
		env.AssertUnitMatches(updated)
	})

	t.Run("DoesNotRestartContainers", func(t *testing.T) {
		env := systest.New(t)
		env.SeedUnit(systest.NewVolume("data", "tmpfs"))
		env.SeedActive(systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data")))
		env.Specs(
			systest.NewVolume("data", "tmpfs", systest.Removable),
			systest.NewContainer("webapp", "nginx:latest", systest.WithVolume("data", "/data")),
		)

		env.Apply()

		env.AssertNoStartStop()
	})
}

func TestVolumeMeaningfulChangeWithDeletePolicy_DeletesAndRecreatesUnit(t *testing.T) {
	updated := systest.NewVolume("data", "tmpfs", deletable()...)

	env := systest.New(t)
	// The existing unit must carry the flags, or the change is rejected instead.
	env.SeedActive(systest.NewVolume("data", "old-device", deletable()...))
	env.Specs(updated)

	env.Apply()

	env.AssertVolumesDeleted("data")
	env.AssertUnitMatches(updated)
}

func TestVolumeMeaningfulChangeWithoutDeletePolicy_Errors(t *testing.T) {
	env := systest.New(t)
	env.SeedUnit(systest.NewVolume("data", "old-device"))
	env.Specs(systest.NewVolume("data", "tmpfs")) // no delete policy

	// syslet.Apply refuses a plan carrying errors, so the refusal is observable
	// end-to-end — and asserting the volume survived proves the guard actually
	// protects the data, not merely that a flag was set.
	if err := env.ApplyErr(); err == nil {
		t.Error("expected apply to be refused for meaningful volume change without delete policy")
	}
	env.AssertNoVolumesDeleted()
	env.AssertNoStartStop()
}

// TestVolumeChanged_StagesAllUnitTypes verifies that all rendered unit types
// (container, volume, network, build) appear in the staging directory even when only
// the volume has changed. The quadlet generator needs the full picture to validate correctly.
func TestVolumeChanged_StagesAllUnitTypes(t *testing.T) {
	// The generator reads the staging directory off the same filesystem the Env
	// uses, so it must be built before New.
	fs := afero.NewMemMapFs()
	gen := &systemdtest.RecordingQuadletGenerator{Fs: fs}
	env := systest.New(t, systest.WithFs(fs), systest.WithQuadletGenerator(gen))

	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewVolume("data", "tmpfs")) // removalAllowed=false
	env.SeedUnit(systest.NewNetwork("frontend", "bridge"))
	env.SeedUnit(systest.NewBuild("myapp", "localhost/myapp:latest"))
	env.Specs(
		systest.NewContainer("webapp", "nginx:latest"),
		// removalAllowed=true — metadata-only change, no recreation.
		systest.NewVolume("data", "tmpfs", systest.Removable),
		systest.NewNetwork("frontend", "bridge"),
		systest.NewBuild("myapp", "localhost/myapp:latest"),
	)

	env.Apply()

	systest.AssertStaged(t, gen, "webapp.container", "data.volume", "frontend.network", "myapp.build")
}

func TestStaleVolume_RemovesUnit(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewVolume("olddata", "tmpfs", systest.Removable))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitAbsent("olddata.volume")
	env.AssertNoStartStop()
	env.AssertReloaded()
}

func TestStaleVolumeWithDeletePolicy_DeletesPodmanVolume(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewVolume("olddata", "tmpfs", deletable()...))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertUnitAbsent("olddata.volume")
	env.AssertVolumesDeleted("olddata")
}
