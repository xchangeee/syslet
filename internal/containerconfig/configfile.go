package containerconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"codeberg.org/xchangeee/syslet/internal/util"
	"github.com/spf13/afero"
)

const (
	// DefaultContainerConfigDir is where bind-mounted container config files are stored.
	DefaultContainerConfigDir = "/etc/containers/config"
)

// ConfigFileManager handles per-container config file operations.
// Config files are stored at <baseDir>/<containerName>/<filename>.
type ConfigFileManager struct {
	fs      afero.Fs
	baseDir string
}

// NewConfigFileManager creates a config manager with the default base path.
func NewConfigFileManager(fs afero.Fs) *ConfigFileManager {
	return &ConfigFileManager{fs: fs, baseDir: DefaultContainerConfigDir}
}

func (m *ConfigFileManager) BaseDirectory() string {
	return m.baseDir
}

// configDir returns the config directory for a given container name.
func (m *ConfigFileManager) configDir(containerName string) string {
	return filepath.Join(m.baseDir, containerName)
}

// ListContainers returns the names of all container config directories.
func (m *ConfigFileManager) ListContainers() ([]string, error) {
	entries, err := afero.ReadDir(m.fs, m.baseDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ListFiles returns all config filenames for a container.
func (m *ConfigFileManager) ListFiles(containerName string) ([]string, error) {
	dir := m.configDir(containerName)
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
func (m *ConfigFileManager) IsChanged(containerName, filename, content string) (bool, error) {
	path := filepath.Join(m.configDir(containerName), filename)
	existing, err := afero.ReadFile(m.fs, path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s: %w", path, err)
	}
	return util.Sha256hex([]byte(content)) != util.Sha256hex(existing), nil
}

// Read reads a config file for a container and returns its content.
// Returns empty string and nil error if the file doesn't exist.
func (m *ConfigFileManager) Read(containerName, filename string) (string, error) {
	path := filepath.Join(m.configDir(containerName), filename)
	existing, err := afero.ReadFile(m.fs, path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(existing), nil
}

// Write writes a config file for a container.
func (m *ConfigFileManager) Write(containerName, filename, content string) error {
	dir := m.configDir(containerName)
	if err := m.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config dir %s: %w", dir, err)
	}
	path := filepath.Join(dir, filename)
	return afero.WriteFile(m.fs, path, []byte(content), 0644)
}

// RemoveFile removes a single config file for a container.
func (m *ConfigFileManager) RemoveFile(containerName, filename string) error {
	path := filepath.Join(m.configDir(containerName), filename)
	err := m.fs.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RemoveAll removes all config files for a container.
func (m *ConfigFileManager) RemoveAll(containerName string) error {
	dir := m.configDir(containerName)
	err := m.fs.RemoveAll(dir)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
