// Package sops wraps the SOPS decrypt library for age-encrypted YAML files.
// ExtractKeys reads key names from the SOPS YAML metadata without decrypting.
// Decryptor decrypts ciphertext using the age key file configured at construction time.
package sops

import (
	"fmt"
	"os"

	"github.com/getsops/sops/v3/decrypt"
	"gopkg.in/yaml.v3"

	"codeberg.org/xchangeee/syslet/internal/model"
)

// ExtractKeys reads the top-level key names from a SOPS-encrypted YAML file
// without decrypting. The .sops metadata key is excluded.
func ExtractKeys(ct model.Ciphertext) ([]string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(ct, &doc); err != nil {
		return nil, fmt.Errorf("parsing SOPS YAML: %w", err)
	}
	keys := make([]string, 0, len(doc))
	for k := range doc {
		if k != "sops" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Decryptor decrypts SOPS age-encrypted YAML files using the age keys at keyFilePath.
// A single Decryptor is constructed once per run (keyed to the host key file) and
// reused across all secret specs.
type Decryptor struct {
	keyFilePath string
}

// NewDecryptor returns a Decryptor backed by the age key file at keyFilePath.
func NewDecryptor(keyFilePath string) *Decryptor {
	return &Decryptor{keyFilePath: keyFilePath}
}

// Decrypt decrypts a SOPS age-encrypted YAML file. Returns a flat key-value map;
// nested YAML values are rejected. The .sops metadata key is excluded.
func (d *Decryptor) Decrypt(ct model.Ciphertext) (model.PlaintextValues, error) {
	tmp, err := os.CreateTemp("", "syslet-sops-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(ct); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("writing ciphertext to temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing temp file: %w", err)
	}

	prev := os.Getenv("SOPS_AGE_KEY_FILE")
	_ = os.Setenv("SOPS_AGE_KEY_FILE", d.keyFilePath)
	plainBytes, err := decrypt.File(tmpPath, "yaml")
	_ = os.Setenv("SOPS_AGE_KEY_FILE", prev)
	if err != nil {
		return nil, fmt.Errorf("SOPS decryption failed: %w", err)
	}

	return parsePlaintext(plainBytes)
}

func parsePlaintext(data []byte) (model.PlaintextValues, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing decrypted YAML: %w", err)
	}
	values := make(model.PlaintextValues, len(doc))
	for k, v := range doc {
		if k == "sops" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("secret key %q has non-string value (nested YAML not allowed)", k)
		}
		values[k] = model.Plaintext(s)
	}
	return values, nil
}
