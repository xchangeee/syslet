//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/test/integration/systest"
)

// syslet.Apply treats its operations as two classes, and the difference is a
// deliberate policy rather than an accident of how the code grew:
//
//   - Operations on a unit — stopping, starting, writing its files — record the
//     failure against that unit, and apply reports failure at the end.
//   - Reclamation of a podman resource is best-effort: a failure is logged and
//     the apply carries on, because a volume that could not be removed should
//     not fail a deploy that otherwise converged, and the next run retries it.
//
// The second is the surprising one, and a policy nothing exercises is
// indistinguishable from a bug. These tests pin both halves.

// TestContainerStartFails_Errors covers the per-unit failure path: a service
// that will not start must fail the apply rather than be reported as converged.
func TestContainerStartFails_Errors(t *testing.T) {
	env := systest.New(t)
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))
	env.Conn.StartErr = fmt.Errorf("unit webapp.service failed to start")

	if err := env.ApplyErr(); err == nil {
		t.Error("expected apply to fail when a unit could not be started")
	}
	// The unit file is still installed: the host is left in the state the plan
	// intended, and only the transition failed.
	env.AssertUnitExists("webapp.container")
}

// TestPodmanReclaimFails covers the best-effort path. A stale volume whose
// backing podman volume cannot be removed leaves the volume behind, but the
// apply itself succeeds and the operator learns about it from the log.
func TestPodmanReclaimFails(t *testing.T) {
	env := systest.New(t)
	env.Podman.FailOn = map[string]error{
		"delete-volume:olddata": fmt.Errorf("volume is in use"),
	}
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedUnit(systest.NewVolume("olddata", "tmpfs",
		systest.Removable, systest.Reclaim(model.ReclaimPolicyDelete)))
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	err := env.ApplyErr()

	t.Run("ReturnsNoError", func(t *testing.T) {
		if err != nil {
			t.Errorf("expected a failed reclaim not to fail the apply, got: %v", err)
		}
	})

	t.Run("RemovesUnit", func(t *testing.T) {
		// The unit is pruned regardless: reclamation is a separate outcome from
		// the unit file, and a retry next run needs the spec gone either way.
		env.With(t).AssertUnitAbsent("olddata.volume")
	})

	t.Run("LogsFailure", func(t *testing.T) {
		if !env.Logs.Contains("volume is in use") {
			t.Errorf("expected the reclaim failure to be logged, got:\n%s", env.Logs.String())
		}
	})
}

// TestSecretUpsertFails_ReturnsNoError covers the same policy for the secret
// store, which apply also treats as best-effort.
func TestSecretUpsertFails_ReturnsNoError(t *testing.T) {
	env := newSecretEnv(t)
	env.Podman.FailOn = map[string]error{
		"upsert-secret:myapp-api-key": fmt.Errorf("secret store unavailable"),
	}
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	if err := env.ApplyErr(); err != nil {
		t.Errorf("expected a failed secret upsert not to fail the apply, got: %v", err)
	}
	if !env.Logs.Contains("secret store unavailable") {
		t.Errorf("expected the upsert failure to be logged, got:\n%s", env.Logs.String())
	}
	// The key that did not fail is still written: apply does not abandon the
	// remaining work when one operation fails.
	env.AssertSecretsUpserted("myapp-db-password")
}
