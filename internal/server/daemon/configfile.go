// Package config manages configuration files associated with container units.
// Container configs go to /etc/containers/config/<unit>/
package daemon

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

const (
	ContainerConfigBase = "/var/syslet/config"
)

// ConfigFileManager handles config file operations.
type ConfigFileManager struct {
	fs                  afero.Fs
	containerConfigBase string
}

// NewConfigFileManager creates a config manager with provided dependencies.
func NewConfigFileManager(fs afero.Fs) *ConfigFileManager {
	return &ConfigFileManager{
		fs:                  fs,
		containerConfigBase: ContainerConfigBase,
	}
}

// NewConfigFileManagerWithPaths creates a config manager with a custom base path (for testing).
func NewConfigFileManagerWithPaths(fs afero.Fs, containerBase string) *ConfigFileManager {
	return &ConfigFileManager{
		fs:                  fs,
		containerConfigBase: containerBase,
	}
}

// configDir returns the config directory for a given unit.
func (m *ConfigFileManager) configDir(fullUnitName string) string {
	ext := filepath.Ext(fullUnitName)
	unitName := fullUnitName[:len(fullUnitName)-len(ext)]
	return filepath.Join(m.containerConfigBase, unitName)
}

// ListFiles returns all config filenames for a unit.
func (m *ConfigFileManager) ListFiles(fullUnitName string) ([]string, error) {
	dir := m.configDir(fullUnitName)
	entries, err := afero.ReadDir(m.fs, dir)
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
func (m *ConfigFileManager) ChecksumDir(fullUnitName string) (string, error) {
	dir := m.configDir(fullUnitName)
	h := sha256.New()

	err := afero.Walk(m.fs, dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := afero.ReadFile(m.fs, path)
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
func (m *ConfigFileManager) IsChanged(fullUnitName string, filename, content string) (bool, error) {
	path := filepath.Join(m.configDir(fullUnitName), filename)
	existing, err := afero.ReadFile(m.fs, path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	return sha256Sum([]byte(content)) != sha256Sum(existing), nil
}

// Write writes a config file to its target directory, creating dirs as needed.
func (m *ConfigFileManager) Write(fullUnitName string, filename, content string) error {
	dir := m.configDir(fullUnitName)
	if err := m.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, filename)
	return afero.WriteFile(m.fs, path, []byte(content), 0644)
}

// RemoveAll removes all config files for a unit.
func (m *ConfigFileManager) RemoveAll(fullUnitName string) error {
	dir := m.configDir(fullUnitName)
	err := m.fs.RemoveAll(dir)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func sha256Sum(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}
