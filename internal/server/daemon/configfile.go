// Package config manages configuration files associated with container units.
// Container configs go to /etc/containers/config/<unit>/
package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

const (
	DefaultContainerConfigDirectory = "/var/syslet/containers/config"
)

// ConfigFileManager handles config file operations.
type ConfigFileManager struct {
	fs                       afero.Fs
	containerConfigDirectory string
}

// NewConfigFileManager creates a config manager with provided dependencies.
func NewConfigFileManager(fs afero.Fs) *ConfigFileManager {
	return &ConfigFileManager{
		fs:                       fs,
		containerConfigDirectory: DefaultContainerConfigDirectory,
	}
}

// NewConfigFileManagerWithPaths creates a config manager with a custom base path (for testing).
func NewConfigFileManagerWithPaths(fs afero.Fs, containerConfigDirectory string) *ConfigFileManager {
	return &ConfigFileManager{
		fs:                       fs,
		containerConfigDirectory: containerConfigDirectory,
	}
}

// configDir returns the config directory for a given unit.
func (m *ConfigFileManager) configDir(fullUnitName string) string {
	ext := filepath.Ext(fullUnitName)
	unitName := fullUnitName[:len(fullUnitName)-len(ext)]
	return filepath.Join(m.containerConfigDirectory, unitName)
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

// IsChanged checks if a config file differs from what's deployed.
func (m *ConfigFileManager) IsChanged(fullUnitName string, filename, content string) (bool, error) {
	dir := m.configDir(fullUnitName)
	path := filepath.Join(dir, filename)
	existing, err := afero.ReadFile(m.fs, path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	return sha256sum([]byte(content)) != sha256sum(existing), nil
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
