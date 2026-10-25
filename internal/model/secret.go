package model

// Ciphertext holds SOPS-encrypted content. Must never be logged or displayed.
type Ciphertext []byte

// Plaintext holds a decrypted secret value. Must never be logged or serialized to disk.
type Plaintext string

// PlaintextValues is a map of secret key names to their decrypted values.
type PlaintextValues map[string]Plaintext

// PodmanSecret represents a named group of secrets in podman's native secret store, derived
// from a single SOPS-encrypted YAML file. Each key in the YAML becomes one podman secret
// named <specname>-<key>. Values is populated during the plan phase and never serialized.
// Change detection compares Values against the plaintext podman holds, never the ciphertext:
// SOPS re-encryption changes the ciphertext without changing any value.
type PodmanSecret struct {
	Name       string
	Ciphertext Ciphertext
	Keys       []string        // key names are not encrypted by SOPS
	Values     PlaintextValues // populated during plan phase, never serialized
}
