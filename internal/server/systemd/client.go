// Package systemd wraps the go-systemd D-Bus client for syslet operations.
package systemd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/coreos/go-systemd/v22/dbus"

	"codeberg.org/xchangeee/syslet/internal/server/parser"
)

const (
	QuadletUnitDir = "/etc/containers/systemd"
)

// Client wraps systemd D-Bus operations.
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

// installedPath returns the full path where a unit file is installed.
func (c *Client) installedPath(unitName string, unitType parser.UnitType) string {
	return filepath.Join(c.quadletUnitDir, unitName)
}

// ReadInstalledUnit reads the content of an installed unit file.
func (c *Client) ReadInstalledUnit(unitName string, unitType parser.UnitType) ([]byte, error) {
	path := c.installedPath(unitName, unitType)
	return os.ReadFile(path)
}

// RemoveUnitFile removes an installed unit file.
func (c *Client) RemoveUnitFile(unitName string, unitType parser.UnitType) error {
	path := c.installedPath(unitName, unitType)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// UnitFileExists checks if a unit file is installed.
func (c *Client) UnitFileExists(unitName string, unitType parser.UnitType) bool {
	path := c.installedPath(unitName, unitType)
	_, err := os.Stat(path)
	return err == nil
}

// GetUnitState queries the current state of a unit via D-Bus.
func (c *Client) GetUnitState(ctx context.Context, unitName string, unitType parser.UnitType) (*UnitState, error) {
	// For quadlet containers, the generated service name is the basename + ".service"
	serviceName := unitName
	if unitType == parser.UnitTypeContainer {
		serviceName = parser.UnitBaseName(unitName) + ".service"
	}

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

// DaemonReload calls systemctl daemon-reload.
func (c *Client) DaemonReload(ctx context.Context) error {
	return c.conn.ReloadContext(ctx)
}

// StartUnit starts a unit.
func (c *Client) StartUnit(ctx context.Context, unitName string, unitType parser.UnitType) error {
	serviceName := unitName
	if unitType == parser.UnitTypeContainer {
		serviceName = parser.UnitBaseName(unitName) + ".service"
	}

	ch := make(chan string, 1)
	_, err := c.conn.StartUnitContext(ctx, serviceName, "replace", ch)
	if err != nil {
		return fmt.Errorf("starting %s: %w", serviceName, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("starting %s: job result %s", serviceName, result)
	}
	return nil
}

// StopUnit stops a unit.
func (c *Client) StopUnit(ctx context.Context, unitName string, unitType parser.UnitType) error {
	serviceName := unitName
	if unitType == parser.UnitTypeContainer {
		serviceName = parser.UnitBaseName(unitName) + ".service"
	}

	ch := make(chan string, 1)
	_, err := c.conn.StopUnitContext(ctx, serviceName, "replace", ch)
	if err != nil {
		return fmt.Errorf("stopping %s: %w", serviceName, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("stopping %s: job result %s", serviceName, result)
	}
	return nil
}

// InstallUnitFile writes a unit file to the appropriate directory.
func (c *Client) InstallUnitFile(unitName string, unitType parser.UnitType, content io.Reader) error {
	dir := c.quadletUnitDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating unit dir %s: %w", dir, err)
	}

	data, err := io.ReadAll(content)
	if err != nil {
		return fmt.Errorf("reading content: %w", err)
	}

	path := filepath.Join(dir, unitName)
	return os.WriteFile(path, data, 0644)
}
