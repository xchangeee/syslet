package systemd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"codeberg.org/xchangeee/syslet/internal/server/parser"
)

const containerExt = ".container"

// ContainerUnit handles systemd operations for container units.
// Container quadlet files (.container) generate .service units,
// so D-Bus operations use the mapped service name.
type ContainerUnit struct {
	client *Client
}

// qualifyName ensures the unit name has the .container extension.
// If no extension is present, .container is appended.
// If the extension is wrong, an error is returned.
func (u *ContainerUnit) qualifyName(name string) (string, error) {
	ext := filepath.Ext(name)
	if ext == "" {
		return name + containerExt, nil
	}
	if ext != containerExt {
		return "", fmt.Errorf("expected %s extension, got %q", containerExt, ext)
	}
	return name, nil
}

// serviceName maps a quadlet container name to its generated systemd service name.
// e.g. "webapp.container" → "webapp.service"
func (u *ContainerUnit) serviceName(fullUnitName string) string {
	return parser.UnitName(fullUnitName) + ".service"
}

// UnitFileExists checks if a container unit file is installed.
func (u *ContainerUnit) UnitFileExists(name string) (bool, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return false, err
	}
	return u.client.unitFileExists(full), nil
}

// ReadInstalledUnit reads the content of an installed container unit file.
func (u *ContainerUnit) ReadInstalledUnit(name string) ([]byte, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return nil, err
	}
	return u.client.readInstalledUnit(full)
}

// InstallUnitFile writes a container unit file to the appropriate directory.
func (u *ContainerUnit) InstallUnitFile(name string, content io.Reader) error {
	full, err := u.qualifyName(name)
	if err != nil {
		return err
	}
	return u.client.installUnitFile(full, content)
}

// RemoveUnitFile removes an installed container unit file.
func (u *ContainerUnit) RemoveUnitFile(name string) error {
	full, err := u.qualifyName(name)
	if err != nil {
		return err
	}
	return u.client.removeUnitFile(full)
}

// GetState queries the current state of a container unit via D-Bus.
func (u *ContainerUnit) GetState(ctx context.Context, fullUnitName string) (*UnitState, error) {
	return u.client.getUnitState(ctx, u.serviceName(fullUnitName))
}

// Start starts a container unit.
func (u *ContainerUnit) Start(ctx context.Context, fullUnitName string) error {
	serviceName := u.serviceName(fullUnitName)
	ch := make(chan string, 1)
	_, err := u.client.conn.StartUnitContext(ctx, serviceName, "replace", ch)
	if err != nil {
		return fmt.Errorf("starting %s: %w", serviceName, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("starting %s: job result %s", serviceName, result)
	}
	return nil
}

// Stop stops a container unit.
func (u *ContainerUnit) Stop(ctx context.Context, fullUnitName string) error {
	serviceName := u.serviceName(fullUnitName)
	ch := make(chan string, 1)
	_, err := u.client.conn.StopUnitContext(ctx, serviceName, "replace", ch)
	if err != nil {
		return fmt.Errorf("stopping %s: %w", serviceName, err)
	}
	result := <-ch
	if result != "done" {
		return fmt.Errorf("stopping %s: job result %s", serviceName, result)
	}
	return nil
}
