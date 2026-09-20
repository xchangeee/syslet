// Package syslet — secret plan phase.
//
// buildPlanSecrets decrypts all secret specs in the desired set, diffs them
// against the podman secret store, and emits Upsert/Delete ops on the plan.
// It also returns the set of upserted secret names so buildPlanUnitContainer
// can mark affected containers for restart.
//
// Orphan detection uses the syslet/hash label as the ownership marker: any
// podman secret that carries that label but is absent from the desired set is
// scheduled for deletion — including keys left behind by a spec that was
// removed from the zip entirely.
package syslet

import (
	"context"
	"fmt"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/sops"
)

// buildPlanSecrets decrypts secrets, computes the diff against the podman
// secret store, and records UpsertPodmanSecret / DeletePodmanSecret ops.
// Returns the full podman secret names that will be upserted (empty map when
// nothing changes). Returns nil when pc is nil (no podman client; secrets are
// skipped for backward compat with callers that don't need secret support).
func buildPlanSecrets(ctx context.Context, pc podman.Interface, decryptor *sops.Decryptor, plan *ApplyPlan, secrets []model.PodmanSecret) map[string]bool {
	if pc == nil {
		return nil
	}
	if len(secrets) > 0 && decryptor == nil {
		plan.RecordGenericError("secrets present in spec but no decryptor configured (SSH key path not set?)")
		return nil
	}

	for i := range secrets {
		values, err := decryptor.Decrypt(secrets[i].Ciphertext)
		if err != nil {
			plan.RecordGenericError(fmt.Sprintf("secret %q: decryption failed: %v", secrets[i].Name, err))
			return nil
		}
		secrets[i].Values = values
	}

	existing, err := pc.ListSecrets(ctx)
	if err != nil {
		plan.RecordGenericError(fmt.Sprintf("listing podman secrets: %v", err))
		return nil
	}

	existingByName := make(map[string]podman.SecretMeta, len(existing))
	for _, s := range existing {
		existingByName[s.Name] = s
	}

	desired := make(map[string]bool)
	upserted := make(map[string]bool)
	specNames := make([]string, len(secrets))

	for i, secret := range secrets {
		specNames[i] = secret.Name
		for _, key := range secret.Keys {
			fullName := secret.Name + "-" + key
			desired[fullName] = true
			meta, exists := existingByName[fullName]
			if !exists || meta.Labels["syslet/hash"] != secret.ContentHash {
				plan.UpsertPodmanSecret(secret.Name, fullName, secret.Values[key], map[string]string{"syslet/hash": secret.ContentHash})
				upserted[fullName] = true
			}
		}
	}

	// Delete syslet-managed secrets that are no longer in the desired set.
	// The syslet/hash label marks ownership; unmanaged secrets are left alone.
	for _, meta := range existing {
		if desired[meta.Name] {
			continue
		}
		if _, ok := meta.Labels["syslet/hash"]; !ok {
			continue
		}
		plan.DeletePodmanSecret(inferSpecName(meta.Name, specNames), meta.Name)
	}

	return upserted
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
