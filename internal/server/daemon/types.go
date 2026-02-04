package daemon

import (
	"crypto/sha256"
	"fmt"
)

// ConfigFile represents a config file to deploy alongside a container.
type ConfigFile struct {
	// e.g. "myapp" (without extension)
	UnitName string
	// e.g. "config.yaml"
	Filename string
	Content  string
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
