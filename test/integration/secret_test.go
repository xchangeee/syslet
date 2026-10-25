//go:build integration

package integration

// Integration tests for the secret plan+apply lifecycle.
//
// Each test uses real SOPS-encrypted fixtures from test/integration/testdata.
// The fixture contains two keys: "db-password"="hunter2" and "api-key"="s3cr3t"
// (hyphen-separated to satisfy the [a-z0-9-] key name validation rule).
//
// env.Podman.SeedSecrets simulates the on-host state returned by ListSecrets,
// and env.Podman.SeedSecretValues the stored plaintext returned by
// SecretValues. syslet owns podman's whole secret store: whether a key changed
// is decided by comparing its stored plaintext with the decrypted value, and
// every secret absent from the desired set is deleted. Secrets carry no labels;
// the legacy "syslet/hash" label written by earlier versions is dropped.
//
// The fixture readers below stay in this package deliberately: they resolve
// testdata/ by a path relative to the test binary's working directory
// (test/integration/). Moving them into systest would silently break that.

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/internal/sops"
	"github.com/xchangeee/syslet/test/integration/systest"
)

// secretTestDecryptor returns a Decryptor backed by the syslet test key file.
func secretTestDecryptor(t *testing.T) *sops.Decryptor {
	t.Helper()
	p, err := filepath.Abs("testdata/keys.txt")
	if err != nil {
		t.Fatalf("resolving key file path: %v", err)
	}
	return sops.NewDecryptor(p)
}

// encryptedCiphertext reads the syslet SOPS test fixture.
// Keys: "api-key"="s3cr3t", "db-password"="hunter2".
func encryptedCiphertext(t *testing.T) model.Ciphertext {
	t.Helper()
	data, err := afero.ReadFile(afero.NewOsFs(), "testdata/encrypted.yaml")
	if err != nil {
		t.Fatalf("reading fixture — run testdata/generate.sh: %v", err)
	}
	return model.Ciphertext(data)
}

// newSecretEnv builds an Env wired with the real decryptor, which every test
// declaring a secret spec needs.
func newSecretEnv(t *testing.T) *systest.Env {
	t.Helper()
	return systest.New(t, systest.WithDecryptor(secretTestDecryptor(t)))
}

// fixtureValues is the plaintext stored on a host that is already current with
// the encrypted.yaml fixture.
func fixtureValues() map[string]model.Plaintext {
	return map[string]model.Plaintext{
		"myapp-api-key":     "s3cr3t",
		"myapp-db-password": "hunter2",
	}
}

// secretNames builds on-host secret metadata for the given names, unlabeled as
// syslet writes them.
func secretNames(names ...string) []podman.SecretMeta {
	metas := make([]podman.SecretMeta, len(names))
	for i, name := range names {
		metas[i] = podman.SecretMeta{Name: name}
	}
	return metas
}

// legacySecrets builds on-host secret metadata for the given names, carrying
// the syslet/hash label that earlier syslet versions wrote.
func legacySecrets(names ...string) []podman.SecretMeta {
	metas := make([]podman.SecretMeta, len(names))
	for i, name := range names {
		metas[i] = podman.SecretMeta{Name: name, Labels: map[string]string{"syslet/hash": "hash-of-previous-ciphertext"}}
	}
	return metas
}

func TestNewSecret_UpsertsAllKeys(t *testing.T) {
	env := newSecretEnv(t)
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	env.Apply()

	env.AssertSecretsUpserted("myapp-api-key", "myapp-db-password")
	env.AssertSecretValue("myapp-api-key", "s3cr3t")
	env.AssertSecretValue("myapp-db-password", "hunter2")
	env.AssertNoSecretsDeleted()
}

// TestSecretUnchanged pins the two halves of the no-op case for a secret whose
// stored values still match the desired content. The plan must neither touch the
// podman secret store nor disturb containers that reference the secret.
//
// The same holds after a SOPS re-encryption — a new recipient or a rotated key
// produces fresh ciphertext for identical values — because only values are
// compared.
func TestSecretUnchanged(t *testing.T) {
	t.Run("NoAction", func(t *testing.T) {
		env := newSecretEnv(t)
		env.Podman.SeedSecrets(secretNames("myapp-api-key", "myapp-db-password")...)
		env.Podman.SeedSecretValues(fixtureValues())
		env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

		env.Apply()

		env.AssertNoSecretsUpserted()
		env.AssertNoSecretsDeleted()
	})

	t.Run("DoesNotRestartContainers", func(t *testing.T) {
		env := newSecretEnv(t)
		webappSpec := systest.NewContainer("webapp", "nginx:latest",
			systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))

		env.Podman.SeedSecrets(secretNames("myapp-api-key", "myapp-db-password")...)
		env.Podman.SeedSecretValues(fixtureValues())
		// Seed the unit file so the plan sees no unit change.
		env.SeedActive(webappSpec)
		env.SpecsJSON(
			systest.SecretJSON("myapp", encryptedCiphertext(t)),
			env.UnitJSON(webappSpec),
		)

		env.Apply()

		env.AssertNoStartStop()
	})
}

// TestSecretWithLegacyLabel pins the migration away from the syslet/hash label
// earlier versions wrote. A secret still carrying it, with an unchanged value,
// is rewritten once without the label — and because its value is unchanged,
// containers consuming it keep running.
func TestSecretWithLegacyLabel(t *testing.T) {
	setup := func(t *testing.T) *systest.Env {
		t.Helper()
		env := newSecretEnv(t)
		webappSpec := systest.NewContainer("webapp", "nginx:latest",
			systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))

		env.Podman.SeedSecrets(legacySecrets("myapp-api-key", "myapp-db-password")...)
		env.Podman.SeedSecretValues(fixtureValues())
		env.SeedActive(webappSpec)
		env.SpecsJSON(
			systest.SecretJSON("myapp", encryptedCiphertext(t)),
			env.UnitJSON(webappSpec),
		)
		return env
	}

	t.Run("UpsertsAllKeys", func(t *testing.T) {
		env := setup(t)
		env.Apply()
		env.AssertSecretsUpserted("myapp-api-key", "myapp-db-password")
		env.AssertSecretValue("myapp-api-key", "s3cr3t")
		env.AssertSecretUnlabeled("myapp-api-key")
		env.AssertSecretUnlabeled("myapp-db-password")
	})

	t.Run("DoesNotRestartContainers", func(t *testing.T) {
		env := setup(t)
		env.Apply()
		env.AssertNoStartStop()
	})

	t.Run("Idempotent", func(t *testing.T) {
		setup(t).AssertIdempotent()
	})
}

// TestSecretOneKeyChanged pins per-key change detection: a SOPS file groups
// several keys, but only a key whose value actually changed is rewritten, and
// only containers consuming that key are restarted. A container consuming a
// sibling key of the same file stays up.
func TestSecretOneKeyChanged(t *testing.T) {
	setup := func(t *testing.T) *systest.Env {
		t.Helper()
		env := newSecretEnv(t)
		dbConsumer := systest.NewContainer("webapp", "nginx:latest",
			systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))
		apiConsumer := systest.NewContainer("worker", "nginx:latest",
			systest.Unit(systest.Set("Container", "Secret", "myapp-api-key")))

		env.Podman.SeedSecrets(secretNames("myapp-api-key", "myapp-db-password")...)
		env.Podman.SeedSecretValues(map[string]model.Plaintext{
			"myapp-api-key":     "s3cr3t",       // unchanged
			"myapp-db-password": "old-password", // changed to hunter2
		})
		env.SeedActive(dbConsumer)
		env.SeedActive(apiConsumer)
		env.SpecsJSON(
			systest.SecretJSON("myapp", encryptedCiphertext(t)),
			env.UnitJSON(dbConsumer),
			env.UnitJSON(apiConsumer),
		)

		env.Apply()
		return env
	}

	t.Run("UpsertsChangedKeyOnly", func(t *testing.T) {
		env := setup(t)
		env.AssertSecretsUpserted("myapp-db-password")
		env.AssertSecretValue("myapp-db-password", "hunter2")
	})

	t.Run("RestartsConsumingServiceOnly", func(t *testing.T) {
		env := setup(t)
		// AssertRestarted demands webapp be the only unit stopped and started,
		// so it also proves worker stayed up.
		env.AssertRestarted("webapp.service")
	})
}

func TestSecretContentChanged_UpsertsAllKeys(t *testing.T) {
	env := newSecretEnv(t)
	env.Podman.SeedSecrets(secretNames("myapp-api-key", "myapp-db-password")...)
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	env.Apply()

	env.AssertSecretsUpserted("myapp-api-key", "myapp-db-password")
	// The decrypted plaintext must survive all the way to podman, not just the
	// key names — a decryption that silently yielded empty values would
	// otherwise look identical here.
	env.AssertSecretValue("myapp-api-key", "s3cr3t")
	env.AssertSecretValue("myapp-db-password", "hunter2")
	env.AssertNoSecretsDeleted()
}

func TestOrphanSecretKey_DeletesKey(t *testing.T) {
	env := newSecretEnv(t)
	env.Podman.SeedSecrets(secretNames(
		"myapp-api-key", "myapp-db-password",
		"myapp-old-token", // orphan
	)...)
	env.Podman.SeedSecretValues(fixtureValues())
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	env.Apply()

	env.AssertNoSecretsUpserted()
	env.AssertSecretsDeleted("myapp-old-token")
}

func TestSecretSpecRemoved_DeletesAllKeys(t *testing.T) {
	// No secret specs in the input, but secrets exist on host.
	env := systest.New(t)
	env.Podman.SeedSecrets(secretNames("oldapp-key1", "oldapp-key2")...)
	// Input with only a container spec — no secret spec.
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertSecretsDeleted("oldapp-key1", "oldapp-key2")
}

// TestSecretContentChangedWithOrphanKey_DeletesBeforeUpserting pins the one
// property no single-mutation test can observe: when an apply both deletes and
// upserts, every delete is issued first.
//
// That ordering is what makes a key rename safe — deleting first means the old
// and new names are never both present on the host. Producing both mutations
// needs a scenario carrying both: two keys whose stored value no longer matches,
// plus a key that has dropped out of the ciphertext. What each mutation does in
// isolation is covered by TestSecretContentChanged_UpsertsAllKeys and
// TestOrphanSecretKey_DeletesKey.
func TestSecretContentChangedWithOrphanKey_DeletesBeforeUpserting(t *testing.T) {
	env := newSecretEnv(t)
	env.Podman.SeedSecrets(secretNames(
		"myapp-api-key", "myapp-db-password",
		"myapp-old-token", // no longer in the ciphertext
	)...)
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	env.Apply()

	// Guard the premise: without both mutations the ordering check is vacuous.
	env.AssertSecretsUpserted("myapp-api-key", "myapp-db-password")
	env.AssertSecretsDeleted("myapp-old-token")
	env.AssertSecretDeletesPrecedeUpserts()
}

// TestStaleSecret_DeletesRegardlessOfLabels pins that syslet owns podman's
// whole secret store: a secret absent from the desired set is deleted whether
// it was created by hand, by another tool, or by an earlier syslet version.
func TestStaleSecret_DeletesRegardlessOfLabels(t *testing.T) {
	env := systest.New(t)
	env.Podman.SeedSecrets(
		podman.SecretMeta{Name: "hand-made"},                                         // no labels
		podman.SecretMeta{Name: "other-tool", Labels: map[string]string{"app": "x"}}, // foreign label
		legacySecrets("oldapp-key1")[0],                                              // legacy syslet label
	)
	env.Specs(systest.NewContainer("webapp", "nginx:latest"))

	env.Apply()

	env.AssertSecretsDeleted("hand-made", "other-tool", "oldapp-key1")
}

func TestSecretChanged_RestartsReferencingContainers(t *testing.T) {
	env := newSecretEnv(t)
	// Container references myapp-db-password; stored value differs → upsert → restart.
	webappSpec := systest.NewContainer("webapp", "nginx:latest",
		systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))

	env.Podman.SeedSecrets(secretNames("myapp-api-key", "myapp-db-password")...)
	// Seed the unit file so the plan sees the container as existing, not new.
	env.SeedActive(webappSpec)
	env.SpecsJSON(
		systest.SecretJSON("myapp", encryptedCiphertext(t)),
		env.UnitJSON(webappSpec),
	)

	env.Apply()

	env.AssertRestarted("webapp.service")
}
