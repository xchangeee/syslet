// Package util provides shared low-level helpers (hashing, filename generation).
package util

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
)

func SHA256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}

// BasenameWithHashSuffix returns a collision-safe local filename for a container config file.
// Two configs with the same basename but different mount paths (e.g. /etc/app/conf and
// /etc/other/conf) would otherwise overwrite each other. The hash suffix disambiguates them
// while keeping the name human-readable.
func BasenameWithHashSuffix(path string) string {
	return filepath.Base(path) + "-" + SHA256Hex([]byte(path))[:8]
}
