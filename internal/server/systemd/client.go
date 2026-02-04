// Package systemd wraps the go-systemd D-Bus client for syslet operations.
package systemd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/spf13/afero"
)

const (
	QuadletUnitDir = "/etc/containers/systemd"
)

// DBusConn abstracts the systemd D-Bus connection for dependency injection.
type DBusConn interface {
	Close()
	ReloadContext(ctx context.Context) error
	GetUnitPropertiesContext(ctx context.Context, unit string) (map[string]interface{}, error)
	StartUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error)
	StopUnitContext(ctx context.Context, name string, mode string, ch chan<- string) (int, error)
}

// Client wraps systemd D-Bus operations and unit file management.
type Client struct {
	conn           DBusConn
	fs             afero.Fs
	quadletUnitDir string

	Container *ContainerUnit
	Network   *NetworkUnit
	Volume    *VolumeUnit
}

// UnitState holds the runtime state of a unit.
type UnitState struct {
	ActiveState string // "active", "inactive", "failed", etc.
	Enabled     bool
}

// NewDBusConnection creates a real D-Bus connection to systemd.
// This is the default connection used in production.
func NewDBusConnection(ctx context.Context) (DBusConn, error) {
	return dbus.NewSystemConnectionContext(ctx)
}

// NewClient creates a new systemd client with provided dependencies.
func NewClient(conn DBusConn, fs afero.Fs) *Client {
	c := &Client{
		conn:           conn,
		fs:             fs,
		quadletUnitDir: QuadletUnitDir,
	}

	c.Container = &ContainerUnit{client: c}
	c.Network = &NetworkUnit{client: c}
	c.Volume = &VolumeUnit{client: c}
	return c
}

// NewClientWithPaths creates a client with a custom quadlet directory (for testing).
func NewClientWithPaths(conn DBusConn, fs afero.Fs, quadletDir string) *Client {
	return &Client{
		conn:           conn,
		fs:             fs,
		quadletUnitDir: quadletDir,
	}
}

// Close closes the D-Bus connection.
func (c *Client) Close() {
	c.conn.Close()
}

// DaemonReload calls systemctl daemon-reload.
func (c *Client) DaemonReload(ctx context.Context) error {
	return c.conn.ReloadContext(ctx)
}

// installedPath returns the full path where a unit file is installed.
func (c *Client) installedPath(fullUnitName string) string {
	return filepath.Join(c.quadletUnitDir, fullUnitName)
}

// unitFileExists checks if a unit file is installed.
func (c *Client) unitFileExists(fullUnitName string) bool {
	path := c.installedPath(fullUnitName)
	_, err := c.fs.Stat(path)
	return err == nil
}

// readInstalledUnit reads the content of an installed unit file.
func (c *Client) readInstalledUnit(fullUnitName string) ([]byte, error) {
	path := c.installedPath(fullUnitName)
	return afero.ReadFile(c.fs, path)
}

// installUnitFile writes a unit file to the appropriate directory.
func (c *Client) installUnitFile(fullUnitName string, content io.Reader) error {
	dir := c.quadletUnitDir
	if err := c.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating unit dir %s: %w", dir, err)
	}

	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("reading content: %w", err)
	}

	path := filepath.Join(dir, fullUnitName)
	return afero.WriteFile(c.fs, path, data, 0644)
}

// removeUnitFile removes an installed unit file.
func (c *Client) removeUnitFile(fullUnitName string) error {
	path := c.installedPath(fullUnitName)
	err := c.fs.Remove(path)
	if isNotExist(err) {
		return nil
	}
	return err
}

// listUnitFiles returns the basenames of all files in the quadlet directory
// matching the given extension (e.g. ".container").
func (c *Client) listUnitFiles(ext string) ([]string, error) {
	entries, err := afero.ReadDir(c.fs, c.quadletUnitDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", c.quadletUnitDir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ext {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// isNotExist checks if an error indicates a file does not exist.
func isNotExist(err error) bool {
	return os.IsNotExist(err)
}

// getUnitState queries the current state of a unit via D-Bus using the given service name.
func (c *Client) getUnitState(ctx context.Context, serviceName string) (*UnitState, error) {
	props, err := c.conn.GetUnitPropertiesContext(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("getting properties for %s: %w", serviceName, err)
	}

	activeState, _ := props["ActiveState"].(string)
	unitFileState, _ := props["UnitFileState"].(string)

	return &UnitState{
		ActiveState: activeState,
		Enabled:     unitFileState == "enabled" || unitFileState == "static",
	}, nil
}
