// Package syslet — secret plan phase.
//
// buildPlanSecrets decrypts all secret specs in the desired set, diffs them
// against the podman secret store, and emits Upsert/Delete ops on the plan.
// It also returns the set of changed secret names so buildPlanUnitContainer
// can mark affected containers for restart.
//
// Change detection works per key and on plaintext: a key is changed when
// podman holds no value for it yet or holds a different value. The stored
// values are read back from podman (podman.Interface.SecretValues) rather than
// inferred from the ciphertext, because SOPS produces fresh ciphertext on every
// re-encryption — adding a recipient or rotating a key would otherwise rewrite
// every secret and restart every container consuming one, although no value
// changed.
//
// syslet owns podman's whole secret store: any podman secret absent from the
// desired set is scheduled for deletion — including keys left behind by a spec
// that was removed from the zip entirely, and secrets syslet never created.
//
// Secrets are written without labels. Earlier versions labeled every secret
// with syslet/hash; a secret still carrying that label but holding the desired
// value is rewritten once to drop it, without counting as a change.
package syslet

import (
	"context"
	"fmt"
	"strings"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/internal/sops"
)

// legacyHashLabel is the label earlier syslet versions wrote on every secret.
// It is only ever read, to find secrets that still need to have it removed.
const legacyHashLabel = "syslet/hash"

// buildPlanSecrets decrypts secrets, computes the diff against the podman
// secret store, and records UpsertPodmanSecret / DeletePodmanSecret ops.
// Returns the full podman secret names whose value changes (empty map when
// nothing changes); upserts that only drop the legacy label are not included. Returns nil when pc is nil (no podman client; secrets are
// skipped for backward compat with callers that don't need secret support).
func buildPlanSecrets(ctx context.Context, pc podman.Interface, decryptor *sops.Decryptor, planner *planner, secrets []model.PodmanSecret) map[string]bool {
	if pc == nil {
		return nil
	}
	if len(secrets) > 0 && decryptor == nil {
		// main only passes a nil decryptor when the default SSH key is missing
		// and no keys are cached; it has already logged a warning naming the path.
		planner.recordGenericError("secrets present in spec but no age key available (no SSH key at the default sshKeyPath and no cached keys)")
		return nil
	}

	for i := range secrets {
		values, err := decryptor.Decrypt(secrets[i].Ciphertext)
		if err != nil {
			planner.recordGenericError(fmt.Sprintf("secret %q: decryption failed: %v", secrets[i].Name, err))
			return nil
		}
		secrets[i].Values = values
	}

	existing, err := pc.ListSecrets(ctx)
	if err != nil {
		planner.recordGenericError(fmt.Sprintf("listing podman secrets: %v", err))
		return nil
	}

	existingByName := make(map[string]podman.SecretMeta, len(existing))
	for _, s := range existing {
		existingByName[s.Name] = s
	}

	stored, err := pc.SecretValues(ctx, existingDesiredSecrets(secrets, existingByName))
	if err != nil {
		planner.recordGenericError(fmt.Sprintf("reading podman secret values: %v", err))
		return nil
	}

	desired := make(map[string]bool)
	changed := make(map[string]bool)
	specNames := make([]string, len(secrets))

	for i, secret := range secrets {
		specNames[i] = secret.Name
		for _, key := range secret.Keys {
			fullName := secret.Name + "-" + key
			desired[fullName] = true
			current, ok := stored[fullName]
			if !ok || current != secret.Values[key] {
				planner.UpsertPodmanSecret(secret.Name, fullName, secret.Values[key], false)
				changed[fullName] = true
				continue
			}
			if _, legacy := existingByName[fullName].Labels[legacyHashLabel]; legacy {
				planner.UpsertPodmanSecret(secret.Name, fullName, secret.Values[key], true)
			}
		}
	}

	// Delete every secret that is no longer in the desired set.
	for _, meta := range existing {
		if desired[meta.Name] {
			continue
		}
		planner.DeletePodmanSecret(inferSpecName(meta.Name, specNames), meta.Name)
	}

	return changed
}

// existingDesiredSecrets returns the full names of desired keys that already
// exist in podman — the only secrets whose stored values are worth reading.
// Absent keys are upserted regardless.
func existingDesiredSecrets(secrets []model.PodmanSecret, existingByName map[string]podman.SecretMeta) []string {
	var names []string
	for _, secret := range secrets {
		for _, key := range secret.Keys {
			fullName := secret.Name + "-" + key
			if _, exists := existingByName[fullName]; exists {
				names = append(names, fullName)
			}
		}
	}
	return names
}

// inferSpecName returns the longest spec name whose "<name>-" prefix matches
// secretName. Returns "" when no known spec claims the secret (e.g. the spec
// was removed from the zip).
func inferSpecName(secretName string, specNames []string) string {
	best := ""
	for _, sn := range specNames {
		if strings.HasPrefix(secretName, sn+"-") && len(sn) > len(best) {
			best = sn
		}
	}
	return best
}
