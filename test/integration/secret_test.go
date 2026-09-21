//go:build integration

package integration

// Integration tests for the secret plan+apply lifecycle.
//
// Each test uses real SOPS-encrypted fixtures from test/integration/testdata.
// The fixture contains two keys: "db-password"="hunter2" and "api-key"="s3cr3t"
// (hyphen-separated to satisfy the [a-z0-9-] key name validation rule).
//
// env.Podman.SeedSecrets simulates the on-host state returned by ListSecrets.
// Secrets created by syslet always carry the "syslet/hash" label so orphan
// detection can identify them.
//
// The fixture readers below stay in this package deliberately: they resolve
// testdata/ by a path relative to the test binary's working directory
// (test/integration/). Moving them into systest would silently break that.

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/sops"
	"codeberg.org/xchangeee/syslet/internal/util"
	"codeberg.org/xchangeee/syslet/test/integration/systest"
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

// secretContentHash returns the SHA-256 hex of the standard test ciphertext.
func secretContentHash(t *testing.T) string {
	t.Helper()
	return util.SHA256Hex([]byte(encryptedCiphertext(t)))
}

// newSecretEnv builds an Env wired with the real decryptor, which every test
// declaring a secret spec needs.
func newSecretEnv(t *testing.T) *systest.Env {
	t.Helper()
	return systest.New(t, systest.WithDecryptor(secretTestDecryptor(t)))
}

// hashedSecrets builds on-host secret metadata for the given names, all
// carrying the same syslet/hash label.
func hashedSecrets(hash string, names ...string) []podman.SecretMeta {
	metas := make([]podman.SecretMeta, len(names))
	for i, name := range names {
		metas[i] = podman.SecretMeta{Name: name, Labels: map[string]string{"syslet/hash": hash}}
	}
	return metas
}

func TestNewSecret_UpsertsAllKeys(t *testing.T) {
	env := newSecretEnv(t)
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	env.Apply()

	env.AssertSecretsUpserted("myapp-api-key", "myapp-db-password")
	env.AssertNoSecretsDeleted()
}

// TestSecretUnchanged pins the two halves of the no-op case for a secret whose
// stored hash still matches the desired content. The plan must neither touch the
// podman secret store nor disturb containers that reference the secret: because
// the ciphertext is re-encrypted on every SOPS write, a hash comparison is the
// only thing standing between an untouched spec and a restart of everything that
// consumes it.
func TestSecretUnchanged(t *testing.T) {
	t.Run("NoAction", func(t *testing.T) {
		env := newSecretEnv(t)
		env.Podman.SeedSecrets(hashedSecrets(secretContentHash(t),
			"myapp-api-key", "myapp-db-password")...)
		env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

		env.Apply()

		env.AssertNoSecretsUpserted()
		env.AssertNoSecretsDeleted()
	})

	t.Run("DoesNotRestartContainers", func(t *testing.T) {
		env := newSecretEnv(t)
		webappSpec := systest.NewContainer("webapp", "nginx:latest",
			systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))

		env.Podman.SeedSecrets(hashedSecrets(secretContentHash(t),
			"myapp-api-key", "myapp-db-password")...)
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

func TestSecretContentChanged_UpsertsAllKeys(t *testing.T) {
	env := newSecretEnv(t)
	env.Podman.SeedSecrets(hashedSecrets("oldhash", "myapp-api-key", "myapp-db-password")...)
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
	env.Podman.SeedSecrets(hashedSecrets(secretContentHash(t),
		"myapp-api-key", "myapp-db-password",
		"myapp-old-token", // orphan
	)...)
	env.SpecsJSON(systest.SecretJSON("myapp", encryptedCiphertext(t)))

	env.Apply()

	env.AssertNoSecretsUpserted()
	env.AssertSecretsDeleted("myapp-old-token")
}

func TestSecretSpecRemoved_DeletesAllKeys(t *testing.T) {
	// No secret specs in the input, but syslet-managed secrets exist on host.
	env := systest.New(t)
	env.Podman.SeedSecrets(hashedSecrets("abc", "oldapp-key1", "oldapp-key2")...)
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
// needs a scenario carrying both: two keys whose stored hash no longer matches,
// plus a key that has dropped out of the ciphertext. What each mutation does in
// isolation is covered by TestSecretContentChanged_UpsertsAllKeys and
// TestOrphanSecretKey_DeletesKey.
func TestSecretContentChangedWithOrphanKey_DeletesBeforeUpserting(t *testing.T) {
	env := newSecretEnv(t)
	env.Podman.SeedSecrets(hashedSecrets("oldhash",
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

func TestSecretChanged_RestartsReferencingContainers(t *testing.T) {
	env := newSecretEnv(t)
	// Container references myapp-db-password; hash mismatch → upsert → restart.
	webappSpec := systest.NewContainer("webapp", "nginx:latest",
		systest.Unit(systest.Set("Container", "Secret", "myapp-db-password")))

	env.Podman.SeedSecrets(hashedSecrets("oldhash", "myapp-api-key", "myapp-db-password")...)
	// Seed the unit file so the plan sees the container as existing, not new.
	env.SeedActive(webappSpec)
	env.SpecsJSON(
		systest.SecretJSON("myapp", encryptedCiphertext(t)),
		env.UnitJSON(webappSpec),
	)

	env.Apply()

	env.AssertRestarted("webapp.service")
}
