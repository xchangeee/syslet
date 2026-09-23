//go:build integration

package integration

import (
	"path/filepath"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/internal/sops"
	"github.com/xchangeee/syslet/test/integration/systest"
)

// Every apply in this suite is checked for phase order automatically, by the
// Apply terminal in systest. That is deliberate — the ordering protects
// invariants no individual test would think to restate, and an opt-in check is
// one a test can silently forget.
//
// The cost is that the invariant appears nowhere a reader can see it. This file
// pays that back: it drives one apply through every phase at once and states
// the boundaries out loud. It is also the test that fails first if the phases in
// syslet.Apply are rearranged without updating the ranking they are checked
// against, since a scenario missing a phase cannot detect a change to it.

// TestApplyTouchingEveryPhase_RunsInPhaseOrder builds a single apply that
// produces an effect in every phase, and pins the boundaries between them.
func TestApplyTouchingEveryPhase_RunsInPhaseOrder(t *testing.T) {
	const mountPath = model.ContainerMountPath("/etc/app/")

	keyPath, err := filepath.Abs("testdata/keys.txt")
	if err != nil {
		t.Fatalf("resolving key file path: %v", err)
	}

	env := systest.New(t,
		systest.WithOSConfigStore(),
		systest.WithDecryptor(sops.NewDecryptor(keyPath)))

	// webapp changes image: stop, unit write, start.
	webapp := systest.NewContainer("webapp", "nginx:alpine",
		systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest",
		systest.Unit(systest.Set("Container", "Secret", "myapp-db-password"))))

	// sidecar changes only its configDir: content write, then an in-place reload.
	oldFiles := []model.ContainerConfigFile{model.NewContainerConfigFile("app.conf", "old", 0)}
	newFiles := []model.ContainerConfigFile{model.NewContainerConfigFile("app.conf", "new", 0)}
	sidecar := systest.NewContainer("sidecar", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), newFiles...)))
	env.SeedActive(systest.NewContainer("sidecar", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(string(mountPath), oldFiles...))))
	env.SeedConfigDir("sidecar", mountPath, 1, oldFiles...)

	// A stale volume with the Delete policy: unit removal plus podman reclaim.
	env.SeedUnit(systest.NewVolume("olddata", "tmpfs",
		systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)))

	// Secrets whose stored hash is stale, plus one key that has dropped out:
	// a delete and an upsert in the same apply.
	env.Podman.SeedSecrets(hashedSecrets("stale-hash",
		"myapp-api-key", "myapp-db-password", "myapp-old-token")...)

	env.SpecsJSON(
		systest.SecretJSON("myapp", encryptedCiphertext(t)),
		env.UnitJSON(webapp),
		env.UnitJSON(sidecar),
	)

	env.Apply()

	// Guard the premise: an ordering assertion over a phase that produced no
	// effect passes vacuously, so check every phase actually fired.
	env.AssertStopped("webapp.service")
	env.AssertStarted("webapp.service")
	env.AssertContainerReloaded("sidecar.service")
	env.AssertSecretsDeleted("myapp-old-token")
	env.AssertSecretsUpserted("myapp-api-key", "myapp-db-password")
	env.AssertVolumesDeleted("olddata")
	env.AssertUnitAbsent("olddata.volume")
	env.AssertReloaded()

	// A container is stopped before its unit file is rewritten, so systemd
	// never has a unit changed underneath a running service.
	env.AssertHappensBefore("stop", "webapp.service", "write-unit", "webapp.container")

	// Config content lands while the consumer is down, never under it.
	env.AssertHappensBefore("stop", "webapp.service", "write-config", "")

	// A renamed key is deleted before the new name is written, so both names
	// are never present at once.
	env.AssertHappensBefore("delete-secret", "myapp-old-token", "upsert-secret", "myapp-api-key")

	// systemd regenerates before it is asked to act: unit files are written
	// before the daemon-reload, and nothing starts or reloads before it.
	env.AssertHappensBefore("write-unit", "webapp.container", "daemon-reload", "")
	env.AssertHappensBefore("daemon-reload", "", "start", "webapp.service")
	env.AssertHappensBefore("daemon-reload", "", "reload-unit", "sidecar.service")
}

// TestApplyWithNothingToDo_NoAction pins the floor of the phase check: an apply
// over a converged host produces no effects at all, on any channel.
func TestApplyWithNothingToDo_NoAction(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedSecrets(podman.SecretMeta{Name: "unmanaged"})
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertNoEffects()
}
