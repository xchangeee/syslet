package sops_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/sops"
)

func readFixture(t *testing.T, name string) model.Ciphertext {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("missing testdata/%s — run testdata/generate.sh: %v", name, err)
	}
	return model.Ciphertext(data)
}

func keyFilePath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractKeys_ValidFile_ReturnsKeys(t *testing.T) {
	ct := readFixture(t, "encrypted.yaml")

	keys, err := sops.ExtractKeys(ct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sort.Strings(keys)
	want := []string{"api_key", "db_password"}
	if len(keys) != len(want) {
		t.Fatalf("got keys %v, want %v", keys, want)
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("keys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestExtractKeys_ExcludesSopsMetadataKey(t *testing.T) {
	ct := readFixture(t, "encrypted.yaml")

	keys, err := sops.ExtractKeys(ct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, k := range keys {
		if k == "sops" {
			t.Errorf("sops metadata key must be excluded from results")
		}
	}
}

func TestDecrypt_ValidIdentity_ReturnsPlaintextValues(t *testing.T) {
	ct := readFixture(t, "encrypted.yaml")

	values, err := sops.NewDecryptor(keyFilePath(t, "keys.txt")).Decrypt(ct)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if values["db_password"] != model.Plaintext("hunter2") {
		t.Errorf("db_password = %q, want %q", values["db_password"], "hunter2")
	}
	if values["api_key"] != model.Plaintext("s3cr3t") {
		t.Errorf("api_key = %q, want %q", values["api_key"], "s3cr3t")
	}
}

func TestDecrypt_WrongIdentity_ReturnsError(t *testing.T) {
	ct := readFixture(t, "encrypted.yaml")

	_, err := sops.NewDecryptor(keyFilePath(t, "wrong_keys.txt")).Decrypt(ct)
	if err == nil {
		t.Fatal("expected error for wrong identity, got nil")
	}
}

func TestDecrypt_NestedValue_ReturnsError(t *testing.T) {
	ct := readFixture(t, "encrypted_nested.yaml")

	_, err := sops.NewDecryptor(keyFilePath(t, "keys.txt")).Decrypt(ct)
	if err == nil {
		t.Fatal("expected error for nested YAML value, got nil")
	}
}
