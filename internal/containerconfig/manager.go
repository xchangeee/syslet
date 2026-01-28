// Package config manages configuration files associated with container units.
// Container configs go to /etc/containers/config/<unit>/
package containerconfig

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"codeberg.org/xchangeee/syslet/internal/parser"
)

const (
	ContainerConfigBase = "/etc/containers/config"
)

// Manager handles config file operations.
type Manager struct {
	containerConfigBase string
}

// ConfigFile represents a config file to deploy.
type ConfigFile struct {
	UnitName string // e.g. "myapp" (without extension)
	Filename string // e.g. "config.yaml"
	Content  string
}

// NewManager creates a config manager with default paths.
func NewManager() *Manager {
	return &Manager{
		containerConfigBase: ContainerConfigBase,
	}
}

// NewManagerWithPaths creates a config manager with a custom base path (for testing).
func NewManagerWithPaths(containerBase string) *Manager {
	return &Manager{
		containerConfigBase: containerBase,
	}
}

// configDir returns the config directory for a given unit.
func (m *Manager) configDir(unitName string) string {
	baseName := parser.UnitBaseName(unitName)
	return filepath.Join(m.containerConfigBase, baseName)
}

// ListFiles returns all config filenames for a unit.
func (m *Manager) ListFiles(unitName string) ([]string, error) {
	dir := m.configDir(unitName)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// ChecksumDir computes a combined checksum of all files in a unit's config directory.
func (m *Manager) ChecksumDir(unitName string) (string, error) {
	dir := m.configDir(unitName)
	h := sha256.New()

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(rel))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// IsChanged checks if a config file differs from what's deployed.
func (m *Manager) IsChanged(unitName string, filename, content string) (bool, error) {
	path := filepath.Join(m.configDir(unitName), filename)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	return sha256Sum([]byte(content)) != sha256Sum(existing), nil
}

// Write writes a config file to its target directory, creating dirs as needed.
func (m *Manager) Write(unitName string, filename, content string) error {
	dir := m.configDir(unitName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, filename)
	return os.WriteFile(path, []byte(content), 0644)
}

// RemoveAll removes all config files for a unit.
func (m *Manager) RemoveAll(unitName string) error {
	dir := m.configDir(unitName)
	err := os.RemoveAll(dir)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func sha256Sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
