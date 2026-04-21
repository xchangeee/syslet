// Package systemd wraps the go-systemd D-Bus client and manages quadlet
// unit files on disk. It provides the low-level building blocks that the
// syslet apply logic uses: start/stop containers, daemon-reload, and
// read/write/remove unit files in /etc/containers/systemd/.
package systemd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/spf13/afero"
)

const (
	// QuadletUnitDir is where Podman quadlet files live.
	QuadletUnitDir = "/etc/containers/systemd"
)

// JournalReader reads journal entries produced during a daemon-reload. It is
// injectable so tests can provide a mock without spawning a real journalctl.
type JournalReader interface {
	// QuadletErrorsSince returns all messages logged by quadlet-generator
	// after the given time, or nil if none were found.
	QuadletErrorsSince(ctx context.Context, since time.Time) ([]string, error)
}

// journalctlReader is the real JournalReader that shells out to journalctl.
type journalctlReader struct{}

func (r *journalctlReader) QuadletErrorsSince(ctx context.Context, since time.Time) ([]string, error) {
	sinceStr := since.Local().Format("2006-01-02 15:04:05")
	cmd := exec.CommandContext(ctx,
		"journalctl", "-b", "-t", "quadlet-generator",
		"--since="+sinceStr, "-o", "cat", "--no-pager",
	)
	out, err := cmd.Output()
	if err != nil {
		// journalctl is not present on non-Linux hosts; treat as no entries.
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("journalctl: %w", err)
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

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
	journal        JournalReader
}

// UnitState holds the runtime state of a unit.
type UnitState struct {
	ActiveState string // "active", "inactive", "failed", etc.
	Enabled     bool
}

// NewDBusConnection creates a real D-Bus connection to systemd.
func NewDBusConnection(ctx context.Context) (DBusConn, error) {
	return dbus.NewSystemConnectionContext(ctx)
}

// NewClient creates a new systemd client with provided dependencies.
func NewClient(conn DBusConn, fs afero.Fs) *Client {
	return &Client{
		conn:           conn,
		fs:             fs,
		quadletUnitDir: QuadletUnitDir,
		journal:        &journalctlReader{},
	}
}

// NewClientWithPaths creates a client with a custom quadlet directory (for testing).
func NewClientWithPaths(conn DBusConn, fs afero.Fs, quadletDir string) *Client {
	return &Client{
		conn:           conn,
		fs:             fs,
		quadletUnitDir: quadletDir,
		journal:        &journalctlReader{},
	}
}

// WithJournalReader replaces the journal reader (for testing).
func (c *Client) WithJournalReader(jr JournalReader) *Client {
	c.journal = jr
	return c
}

// QuadletErrorsSince returns messages logged by quadlet-generator after since.
func (c *Client) QuadletErrorsSince(ctx context.Context, since time.Time) ([]string, error) {
	return c.journal.QuadletErrorsSince(ctx, since)
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
		ActiveState: activeState,
		Enabled:     unitFileState == "enabled" || unitFileState == "static",
	}, nil
}

// StartContainer starts a container unit by its quadlet name (e.g. "webapp.container").
// It maps the name to the generated .service unit for D-Bus.
func (c *Client) StartContainer(ctx context.Context, containerName string) error {
	serviceName := containerServiceName(containerName)
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

// StopContainer stops a container unit by its quadlet name (e.g. "webapp.container").
func (c *Client) StopContainer(ctx context.Context, containerName string) error {
	serviceName := containerServiceName(containerName)
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

// ContainerState returns the runtime state of a container, mapping the
// quadlet .container name to the generated .service name for D-Bus queries.
func (c *Client) ContainerState(ctx context.Context, containerName string) (*UnitState, error) {
	return c.RuntimeState(ctx, containerServiceName(containerName))
}

// containerServiceName maps a quadlet container name to its generated
// systemd service name. e.g. "webapp.container" → "webapp.service"
func containerServiceName(containerName string) string {
	base := strings.TrimSuffix(containerName, filepath.Ext(containerName))
	return base + ".service"
}

// ListUnitFiles returns the basenames of all files in the quadlet directory
// matching the given extension (e.g. ".container", ".volume", ".network").
func (c *Client) ListUnitFiles(ext string) ([]string, error) {
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

// ReadUnitFile reads the content of an installed unit file.
func (c *Client) ReadUnitFile(fullUnitName string) ([]byte, error) {
	path := filepath.Join(c.quadletUnitDir, fullUnitName)
	return afero.ReadFile(c.fs, path)
}

// WriteUnitFile writes a unit file to the quadlet directory.
func (c *Client) WriteUnitFile(fullUnitName string, content []byte) error {
	if err := c.fs.MkdirAll(c.quadletUnitDir, 0755); err != nil {
		return fmt.Errorf("creating unit dir %s: %w", c.quadletUnitDir, err)
	}
	path := filepath.Join(c.quadletUnitDir, fullUnitName)
	return afero.WriteFile(c.fs, path, content, 0644)
}

// RemoveUnitFile removes an installed unit file.
func (c *Client) RemoveUnitFile(fullUnitName string) error {
	path := filepath.Join(c.quadletUnitDir, fullUnitName)
	err := c.fs.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// UnitFileExists checks if a unit file is installed.
func (c *Client) UnitFileExists(fullUnitName string) bool {
	path := filepath.Join(c.quadletUnitDir, fullUnitName)
	_, err := c.fs.Stat(path)
	return err == nil
}
