// Package podmantest provides the fake podman implementation used by tests.
//
// It is the podman-side counterpart to internal/systemd/systemdtest, and exists
// for the same reasons: test scaffolding stays out of the production
// internal/podman package, while remaining importable by everything that needs
// it — internal/ unit tests, podman's own external test package, and the tagged
// integration harness in test/integration/systest. The package carries no build
// tag for exactly that reason, since an untagged unit test cannot import a
// package tagged `integration`.
//
// The dependency runs one way only: podmantest imports podman for the data
// types the fake accepts and hands back (Interface, SecretMeta); podman must
// never import podmantest. Interface itself is satisfied structurally, so
// FakePodman does not name it.
package podmantest

import (
	"context"
	"maps"
	"slices"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/podman"
	"github.com/xchangeee/syslet/test/oplog"
)

// podmanChannel labels this fake's entries on the shared oplog timeline.
const podmanChannel = "podman"

// UpsertedSecret is one recorded call to UpsertSecret.
type UpsertedSecret struct {
	Name   string
	Value  model.Plaintext
	Labels map[string]string
}

// SecretOp is which kind of secret mutation a SecretCall records.
type SecretOp string

const (
	// SecretDelete is a call to DeleteSecret.
	SecretDelete SecretOp = "delete"
	// SecretUpsert is a call to UpsertSecret.
	SecretUpsert SecretOp = "upsert"
)

// SecretCall is one secret mutation, recorded in arrival order.
//
// Upserts and deletes are also recorded separately, but only this interleaved
// log can show their relative order — and the order is load-bearing: apply
// deletes before it upserts so that a key rename never leaves the old and new
// names present at the same time.
type SecretCall struct {
	Op   SecretOp
	Name string
}

// FakePodman implements podman.Interface in memory.
//
// It is both a recorder and a model of the host. As a recorder it captures
// every destructive and mutating call, so that the apply phase — which, unlike
// the plan, leaves no inspectable artifact — can still be asserted on. As a
// model it keeps the secret store up to date as those calls arrive, so that
// ListSecrets after an apply reports what that apply actually did.
//
// The second role is what makes idempotency testable. A pure recorder always
// reports the host as it was seeded, so a second apply would re-derive the same
// work and the harness could never tell a converging apply from a thrashing
// one. It also closes the loop on the syslet/hash label: the plan's no-op
// decision compares against the label a previous apply wrote, and only a
// stateful store lets a test exercise that comparison against a real write
// rather than a hand-seeded one.
//
// Volumes, networks and images are recorded but not modeled, because the plan
// never reads them back — it derives their fate from unit files alone.
//
// Recorded calls are read back through the accessor methods rather than by
// reaching into fields, so tests never need a type assertion on
// podman.Interface.
type FakePodman struct {
	deletedVolumes  []string
	deletedNetworks []string
	deletedImages   []string
	upsertedSecrets []UpsertedSecret
	deletedSecrets  []string
	secretCalls     []SecretCall
	existingSecrets []podman.SecretMeta

	// Log, when set, receives every mutation in arrival order alongside the
	// effects recorded by the other fakes sharing it.
	Log *oplog.Log

	// FailOn makes a named operation return an error, keyed "<op>:<name>" —
	// for example "delete-volume:data" or "upsert-secret:myapp-api-key".
	//
	// Reclamation and secret mutation run through syslet's best-effort exec
	// path, which logs a failure and carries on rather than failing the apply.
	// That choice is deliberate — a leaked volume should not fail a deploy that
	// otherwise converged, and the next run retries — but it is only a
	// deliberate choice if a test pins it.
	FailOn map[string]error
}

// NewFakePodman creates a fake holding no resources.
func NewFakePodman() *FakePodman {
	return &FakePodman{
		deletedVolumes:  []string{},
		deletedNetworks: []string{},
		deletedImages:   []string{},
	}
}

// SeedSecrets stages the secrets ListSecrets will report, simulating secrets
// already present on the host before the plan runs.
func (p *FakePodman) SeedSecrets(secrets ...podman.SecretMeta) {
	p.existingSecrets = secrets
}

// DeletedVolumes returns the volume names passed to DeleteVolume, in order.
func (p *FakePodman) DeletedVolumes() []string { return p.deletedVolumes }

// DeletedNetworks returns the network names passed to DeleteNetwork, in order.
func (p *FakePodman) DeletedNetworks() []string { return p.deletedNetworks }

// DeletedImages returns the image tags passed to DeleteImage, in order.
func (p *FakePodman) DeletedImages() []string { return p.deletedImages }

// UpsertedSecrets returns the secrets passed to UpsertSecret, in order.
func (p *FakePodman) UpsertedSecrets() []UpsertedSecret { return p.upsertedSecrets }

// DeletedSecrets returns the secret names passed to DeleteSecret, in order.
func (p *FakePodman) DeletedSecrets() []string { return p.deletedSecrets }

// SecretCalls returns every secret mutation in arrival order, upserts and
// deletes interleaved.
func (p *FakePodman) SecretCalls() []SecretCall { return p.secretCalls }

// ExistingSecrets returns the modeled secret store as ListSecrets would report
// it, reflecting every upsert and delete applied so far.
func (p *FakePodman) ExistingSecrets() []podman.SecretMeta { return p.existingSecrets }

// ResetRecordings clears the recorded calls while leaving the modeled host
// state intact, so that a second apply can be asserted on in isolation.
func (p *FakePodman) ResetRecordings() {
	p.deletedVolumes = []string{}
	p.deletedNetworks = []string{}
	p.deletedImages = []string{}
	p.upsertedSecrets = nil
	p.deletedSecrets = nil
	p.secretCalls = nil
}

// fail returns the configured error for an operation, if any.
func (p *FakePodman) fail(op, name string) error {
	return p.FailOn[op+":"+name]
}

// --- podman.Interface ---

// DeleteVolume records the deletion.
func (p *FakePodman) DeleteVolume(_ context.Context, name string) error {
	if err := p.fail("delete-volume", name); err != nil {
		return err
	}
	p.Log.Record(podmanChannel, "delete-volume", name)
	p.deletedVolumes = append(p.deletedVolumes, name)
	return nil
}

// DeleteNetwork records the deletion.
func (p *FakePodman) DeleteNetwork(_ context.Context, name string) error {
	if err := p.fail("delete-network", name); err != nil {
		return err
	}
	p.Log.Record(podmanChannel, "delete-network", name)
	p.deletedNetworks = append(p.deletedNetworks, name)
	return nil
}

// DeleteImage records the deletion.
func (p *FakePodman) DeleteImage(_ context.Context, tag string) error {
	if err := p.fail("delete-image", tag); err != nil {
		return err
	}
	p.Log.Record(podmanChannel, "delete-image", tag)
	p.deletedImages = append(p.deletedImages, tag)
	return nil
}

// UpsertSecret records the upsert and applies it to the modeled store, so a
// later ListSecrets reports the name and labels this call wrote.
func (p *FakePodman) UpsertSecret(_ context.Context, name string, value model.Plaintext, labels map[string]string) error {
	if err := p.fail("upsert-secret", name); err != nil {
		return err
	}
	p.Log.Record(podmanChannel, "upsert-secret", name)
	p.upsertedSecrets = append(p.upsertedSecrets, UpsertedSecret{Name: name, Value: value, Labels: labels})
	p.secretCalls = append(p.secretCalls, SecretCall{Op: SecretUpsert, Name: name})

	stored := podman.SecretMeta{Name: name, Labels: copyLabels(labels)}
	for i, existing := range p.existingSecrets {
		if existing.Name == name {
			p.existingSecrets[i] = stored
			return nil
		}
	}
	p.existingSecrets = append(p.existingSecrets, stored)
	return nil
}

// DeleteSecret records the deletion and removes the secret from the modeled
// store.
func (p *FakePodman) DeleteSecret(_ context.Context, name string) error {
	if err := p.fail("delete-secret", name); err != nil {
		return err
	}
	p.Log.Record(podmanChannel, "delete-secret", name)
	p.deletedSecrets = append(p.deletedSecrets, name)
	p.secretCalls = append(p.secretCalls, SecretCall{Op: SecretDelete, Name: name})

	p.existingSecrets = slices.DeleteFunc(p.existingSecrets, func(m podman.SecretMeta) bool {
		return m.Name == name
	})
	return nil
}

// ListSecrets returns the modeled secret store.
func (p *FakePodman) ListSecrets(_ context.Context) ([]podman.SecretMeta, error) {
	return p.existingSecrets, nil
}

// copyLabels copies a label map, so that a caller mutating the map it passed to
// UpsertSecret cannot retroactively change what the store reports.
func copyLabels(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
