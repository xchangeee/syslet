//go:build integration

package systest

import (
	"context"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/podman"
)

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

// FakePodman implements podman.Interface in memory. It records every
// destructive and mutating call so that the apply phase — which, unlike the
// plan, leaves no inspectable artifact — can still be asserted on, and serves
// a seeded secret list so plan-time orphan detection has host state to compare
// against.
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

// --- podman.Interface ---

// DeleteVolume records the deletion.
func (p *FakePodman) DeleteVolume(_ context.Context, name string) error {
	p.deletedVolumes = append(p.deletedVolumes, name)
	return nil
}

// DeleteNetwork records the deletion.
func (p *FakePodman) DeleteNetwork(_ context.Context, name string) error {
	p.deletedNetworks = append(p.deletedNetworks, name)
	return nil
}

// DeleteImage records the deletion.
func (p *FakePodman) DeleteImage(_ context.Context, tag string) error {
	p.deletedImages = append(p.deletedImages, tag)
	return nil
}

// UpsertSecret records the upsert.
func (p *FakePodman) UpsertSecret(_ context.Context, name string, value model.Plaintext, labels map[string]string) error {
	p.upsertedSecrets = append(p.upsertedSecrets, UpsertedSecret{Name: name, Value: value, Labels: labels})
	p.secretCalls = append(p.secretCalls, SecretCall{Op: SecretUpsert, Name: name})
	return nil
}

// DeleteSecret records the deletion.
func (p *FakePodman) DeleteSecret(_ context.Context, name string) error {
	p.deletedSecrets = append(p.deletedSecrets, name)
	p.secretCalls = append(p.secretCalls, SecretCall{Op: SecretDelete, Name: name})
	return nil
}

// ListSecrets returns the secrets staged by SeedSecrets.
func (p *FakePodman) ListSecrets(_ context.Context) ([]podman.SecretMeta, error) {
	return p.existingSecrets, nil
}
