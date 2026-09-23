package age_test

import (
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/age"
)

// sshKeyFixture reads testdata/id_ed25519 from the real filesystem.
func sshKeyFixture(t *testing.T) []byte {
	t.Helper()
	fs := afero.NewOsFs()
	data, err := afero.ReadFile(fs, "testdata/id_ed25519")
	if err != nil {
		t.Fatalf("missing testdata/id_ed25519 — run testdata/generate.sh: %v", err)
	}
	return data
}

func TestIdentityFromSSHKey_ValidKey_ReturnsIdentity(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "id_ed25519", sshKeyFixture(t), 0o600); err != nil {
		t.Fatal(err)
	}

	key, err := age.IdentityFromSSHKey(fs, "id_ed25519")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(key, "AGE-SECRET-KEY-") {
		t.Errorf("expected AGE-SECRET-KEY-... prefix, got %q", key)
	}
}

func TestIdentityFromSSHKey_InvalidKey_ReturnsError(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := afero.WriteFile(fs, "bad_key", []byte("not a valid ssh key"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := age.IdentityFromSSHKey(fs, "bad_key")
	if err == nil {
		t.Fatal("expected error for invalid SSH key, got nil")
	}
}

func TestAppendToKeyFile_Absent_AppendsAndReturnsAll(t *testing.T) {
	fs := afero.NewMemMapFs()
	const keyA = "AGE-SECRET-KEY-1AAAA"
	const keyB = "AGE-SECRET-KEY-1BBBB"

	// First append creates the file with keyA.
	keys, err := age.AppendToKeyFile(fs, "keys.txt", keyA)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	if len(keys) != 1 || keys[0] != keyA {
		t.Errorf("expected [%s], got %v", keyA, keys)
	}

	// Second append adds keyB.
	keys, err = age.AppendToKeyFile(fs, "keys.txt", keyB)
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if len(keys) != 2 || keys[0] != keyA || keys[1] != keyB {
		t.Errorf("expected [%s %s], got %v", keyA, keyB, keys)
	}
}

func TestAppendToKeyFile_Present_NoOpAndReturnsAll(t *testing.T) {
	fs := afero.NewMemMapFs()
	const keyA = "AGE-SECRET-KEY-1AAAA"

	if _, err := age.AppendToKeyFile(fs, "keys.txt", keyA); err != nil {
		t.Fatalf("initial append: %v", err)
	}

	keys, err := age.AppendToKeyFile(fs, "keys.txt", keyA)
	if err != nil {
		t.Fatalf("duplicate append: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 key after duplicate append, got %d: %v", len(keys), keys)
	}

	data, _ := afero.ReadFile(fs, "keys.txt")
	if strings.Count(string(data), keyA) != 1 {
		t.Errorf("key written more than once to file:\n%s", data)
	}
}
