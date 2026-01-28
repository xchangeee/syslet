// Package systemd wraps the go-systemd D-Bus client for syslet operations.
package systemd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/dbus"
)

const (
	QuadletUnitDir = "/etc/containers/systemd"
)

// Client wraps systemd D-Bus operations and unit file management.
type Client struct {
	conn           *dbus.Conn
	quadletUnitDir string
}

// UnitState holds the runtime state of a unit.
type UnitState struct {
	ActiveState string // "active", "inactive", "failed", etc.
	Enabled     bool
}

// NewClient creates a new systemd client.
func NewClient(ctx context.Context) (*Client, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting to systemd: %w", err)
	}
	return &Client{
		conn:           conn,
		quadletUnitDir: QuadletUnitDir,
	}, nil
}

// NewClientWithPaths creates a client with a custom quadlet directory (for testing).
func NewClientWithPaths(ctx context.Context, quadletDir string) (*Client, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting to systemd: %w", err)
	}
	return &Client{
		conn:           conn,
		quadletUnitDir: quadletDir,
	}, nil
}

// Close closes the D-Bus connection.
func (c *Client) Close() {
	c.conn.Close()
}

// DaemonReload calls systemctl daemon-reload.
func (c *Client) DaemonReload(ctx context.Context) error {
	return c.conn.ReloadContext(ctx)
}

// Container returns a ContainerUnit resource for container-specific operations.
func (c *Client) Container() *ContainerUnit {
	return &ContainerUnit{client: c}
}

// Volume returns a VolumeUnit resource for volume-specific operations.
func (c *Client) Volume() *VolumeUnit {
	return &VolumeUnit{client: c}
}

// Network returns a NetworkUnit resource for network-specific operations.
func (c *Client) Network() *NetworkUnit {
	return &NetworkUnit{client: c}
}

// installedPath returns the full path where a unit file is installed.
func (c *Client) installedPath(fullUnitName string) string {
	return filepath.Join(c.quadletUnitDir, fullUnitName)
}

// unitFileExists checks if a unit file is installed.
func (c *Client) unitFileExists(fullUnitName string) bool {
	path := c.installedPath(fullUnitName)
	_, err := os.Stat(path)
	return err == nil
}

// readInstalledUnit reads the content of an installed unit file.
func (c *Client) readInstalledUnit(fullUnitName string) ([]byte, error) {
	path := c.installedPath(fullUnitName)
	return os.ReadFile(path)
}

// installUnitFile writes a unit file to the appropriate directory.
func (c *Client) installUnitFile(fullUnitName string, content io.Reader) error {
	dir := c.quadletUnitDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating unit dir %s: %w", dir, err)
	}

	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("reading content: %w", err)
	}

	path := filepath.Join(dir, fullUnitName)
	return os.WriteFile(path, data, 0644)
}

// removeUnitFile removes an installed unit file.
func (c *Client) removeUnitFile(fullUnitName string) error {
	path := c.installedPath(fullUnitName)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
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
