// Package systemd wraps the go-systemd D-Bus client and manages quadlet
// unit files on disk. It provides the low-level building blocks that the
// syslet apply logic uses: start/stop containers, daemon-reload, and
// read/write/remove unit files in /etc/containers/systemd/.
package systemd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/model"
)

// Client wraps systemd D-Bus operations and unit file management.
type Client struct {
	conn           DBusConn
	fs             afero.Fs
	quadletUnitDir string
}

// ActiveState is the runtime active state of a systemd unit.
type ActiveState string

const (
	ActiveStateActive     ActiveState = "active"
	ActiveStateInactive   ActiveState = "inactive"
	ActiveStateFailed     ActiveState = "failed"
	ActiveStateActivating ActiveState = "activating"
)

// IsRunning reports whether the unit is currently running or coming up.
func (s ActiveState) IsRunning() bool {
	return s == ActiveStateActive || s == ActiveStateActivating
}

// UnitState holds the runtime state of a unit.
type UnitState struct {
	ActiveState ActiveState
	Enabled     bool
}

// NewClient creates a new systemd client with provided dependencies.
func NewClient(conn DBusConn, fs afero.Fs) *Client {
	return &Client{
		conn:           conn,
		fs:             fs,
		quadletUnitDir: QuadletUnitDir,
	}
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

// RuntimeState queries the current state of a unit via D-Bus.
// For container units, pass the mapped service name (e.g. "webapp.service").
func (c *Client) RuntimeState(ctx context.Context, serviceName string) (*UnitState, error) {
	props, err := c.conn.GetUnitPropertiesContext(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("getting properties for %s: %w", serviceName, err)
	}

	activeState, _ := props["ActiveState"].(string)
	unitFileState, _ := props["UnitFileState"].(string)

	return &UnitState{
		ActiveState: ActiveState(activeState),
		Enabled:     unitFileState == "enabled" || unitFileState == "static",
	}, nil
}

// StartUnit starts the named systemd service.
func (c *Client) StartUnit(ctx context.Context, name model.ServiceUnitName) error {
	ch := make(chan string, 1)
	_, err := c.conn.StartUnitContext(ctx, string(name), "replace", ch)
	if err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("starting %s: job result %s", name, result)
	}
	return nil
}

// StopUnit stops the named systemd service.
func (c *Client) StopUnit(ctx context.Context, name model.ServiceUnitName) error {
	ch := make(chan string, 1)
	_, err := c.conn.StopUnitContext(ctx, string(name), "replace", ch)
	if err != nil {
		return fmt.Errorf("stopping %s: %w", name, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("stopping %s: job result %s", name, result)
	}
	return nil
}

// ContainerState returns the runtime state of a container, mapping the
// base container name to the generated .service name for D-Bus queries.
func (c *Client) ContainerState(ctx context.Context, container model.ContainerUnitRef) (*UnitState, error) {
	return c.RuntimeState(ctx, string(container.ServiceUnitName()))
}

// ReloadUnit sends a reload signal to the named unit (e.g. "webapp.service").
func (c *Client) ReloadUnit(ctx context.Context, name string) error {
	ch := make(chan string, 1)
	_, err := c.conn.ReloadUnitContext(ctx, name, "replace", ch)
	if err != nil {
		return fmt.Errorf("reloading %s: %w", name, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("reloading %s: job result %s", name, result)
	}
	return nil
}

// ListUnitFiles returns the basenames of all files in the quadlet directory
// matching the given extension (e.g. ".container", ".volume", ".network").
func (c *Client) ListUnitFiles(ext string) ([]model.FullUnitName, error) {
	entries, err := afero.ReadDir(c.fs, c.quadletUnitDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", c.quadletUnitDir, err)
	}
	var names []model.FullUnitName
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ext {
			names = append(names, model.FullUnitName(e.Name()))
		}
	}
	return names, nil
}

// ReadUnitFile reads the content of an installed unit file.
func (c *Client) ReadUnitFile(unit model.FullUnitName) ([]byte, error) {
	path := filepath.Join(c.quadletUnitDir, string(unit))
	return afero.ReadFile(c.fs, path)
}

// WriteUnitFile writes a unit file to the quadlet directory.
func (c *Client) WriteUnitFile(unit model.FullUnitName, content []byte) error {
	if err := c.fs.MkdirAll(c.quadletUnitDir, 0755); err != nil {
		return fmt.Errorf("creating unit dir %s: %w", c.quadletUnitDir, err)
	}
	path := filepath.Join(c.quadletUnitDir, string(unit))
	return afero.WriteFile(c.fs, path, content, 0644)
}

// RemoveUnitFile removes an installed unit file.
func (c *Client) RemoveUnitFile(unit model.FullUnitName) error {
	path := filepath.Join(c.quadletUnitDir, string(unit))
	err := c.fs.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// UnitFileExists checks if a unit file is installed.
func (c *Client) UnitFileExists(unit string) bool {
	path := filepath.Join(c.quadletUnitDir, unit)
	_, err := c.fs.Stat(path)
	return err == nil
}
