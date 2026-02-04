package systemd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
)

const networkExt = ".network"

// NetworkUnit handles systemd operations for network units.
// Networks are immutable after creation and cannot be started or stopped.
type NetworkUnit struct {
	client *Client
}

// qualifyName ensures the unit name has the .network extension.
func (u *NetworkUnit) qualifyName(name string) (string, error) {
	ext := filepath.Ext(name)
	if ext == "" {
		return name + networkExt, nil
	}
	if ext != networkExt {
		return "", fmt.Errorf("expected %s extension, got %q", networkExt, ext)
	}
	return name, nil
}

// ListUnitFiles returns the basenames of all installed network unit files.
func (u *NetworkUnit) ListUnitFiles() ([]string, error) {
	return u.client.listUnitFiles(networkExt)
}

// UnitFileExists checks if a network unit file is installed.
func (u *NetworkUnit) UnitFileExists(name string) (bool, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return false, err
	}
	return u.client.unitFileExists(full), nil
}

// ReadInstalledUnit reads the content of an installed network unit file.
func (u *NetworkUnit) ReadInstalledUnit(name string) ([]byte, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return nil, err
	}
	return u.client.readInstalledUnit(full)
}

// InstallUnitFile writes a network unit file to the appropriate directory.
func (u *NetworkUnit) InstallUnitFile(name string, content io.Reader) error {
	full, err := u.qualifyName(name)
	if err != nil {
		return err
	}
	return u.client.installUnitFile(full, content)
}

// RemoveUnitFile removes an installed network unit file.
func (u *NetworkUnit) RemoveUnitFile(name string) error {
	full, err := u.qualifyName(name)
	if err != nil {
		return err
	}
	return u.client.removeUnitFile(full)
}

// RuntimeState queries the current state of a network unit via D-Bus.
func (u *NetworkUnit) RuntimeState(ctx context.Context, name string) (*UnitState, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return nil, err
	}
	return u.client.getUnitState(ctx, full)
}
