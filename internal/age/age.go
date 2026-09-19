// Package age manages age encryption identities derived from SSH keys.
// It handles the key file that caches derived age private keys across runs,
// keeping old keys so secrets encrypted to previous SSH keys survive rotation.
package age

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	agessh "github.com/Mic92/ssh-to-age"
	"github.com/spf13/afero"
)

// IdentityFromSSHKey reads the ed25519 SSH private key at sshKeyPath and derives
// the corresponding age private key string (AGE-SECRET-KEY-... bech32 format).
func IdentityFromSSHKey(fs afero.Fs, sshKeyPath string) (string, error) {
	keyBytes, err := afero.ReadFile(fs, sshKeyPath)
	if err != nil {
		return "", fmt.Errorf("reading SSH key %s: %w", sshKeyPath, err)
	}
	privKey, _, err := agessh.SSHPrivateKeyToAge(keyBytes, nil)
	if err != nil {
		return "", fmt.Errorf("converting SSH key to age: %w", err)
	}
	return *privKey, nil
}

// AppendToKeyFile appends ageKey to keyFilePath (mode 0600) if not already present.
// Returns all age private key strings in the file, including the newly appended one.
// The file is append-only: old identities are kept so secrets encrypted to previous
// SSH keys remain decryptable through key rotations.
func AppendToKeyFile(fs afero.Fs, keyFilePath string, ageKey string) ([]string, error) {
	var existing []string

	data, err := afero.ReadFile(fs, keyFilePath)
	if err != nil && !isNotExist(fs, keyFilePath) {
		return nil, fmt.Errorf("reading key file %s: %w", keyFilePath, err)
	}
	if err == nil {
		existing = parseKeyFile(data)
	}

	if slices.Contains(existing, ageKey) {
		return existing, nil
	}

	f, err := fs.OpenFile(keyFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening key file %s: %w", keyFilePath, err)
	}
	defer func() { _ = f.Close() }()

	line := ageKey + "\n"
	if _, err := f.Write([]byte(line)); err != nil {
		return nil, fmt.Errorf("appending to key file %s: %w", keyFilePath, err)
	}

	return append(existing, ageKey), nil
}

func parseKeyFile(data []byte) []string {
	var keys []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			keys = append(keys, line)
		}
	}
	return keys
}

func isNotExist(fs afero.Fs, path string) bool {
	_, err := fs.Stat(path)
	return err != nil
}
