package systemd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
)

const volumeExt = ".volume"

// VolumeUnit handles systemd operations for volume units.
// Volumes are immutable after creation and cannot be started or stopped.
type VolumeUnit struct {
	client *Client
}

// qualifyName ensures the unit name has the .volume extension.
func (u *VolumeUnit) qualifyName(name string) (string, error) {
	ext := filepath.Ext(name)
	if ext == "" {
		return name + volumeExt, nil
	}
	if ext != volumeExt {
		return "", fmt.Errorf("expected %s extension, got %q", volumeExt, ext)
	}
	return name, nil
}

// ListUnitFiles returns the basenames of all installed volume unit files.
func (u *VolumeUnit) ListUnitFiles() ([]string, error) {
	return u.client.listUnitFiles(volumeExt)
}

// UnitFileExists checks if a volume unit file is installed.
func (u *VolumeUnit) UnitFileExists(name string) (bool, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return false, err
	}
	return u.client.unitFileExists(full), nil
}

// ReadInstalledUnit reads the content of an installed volume unit file.
func (u *VolumeUnit) ReadInstalledUnit(name string) ([]byte, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return nil, err
	}
	return u.client.readInstalledUnit(full)
}

// InstallUnitFile writes a volume unit file to the appropriate directory.
func (u *VolumeUnit) InstallUnitFile(name string, content io.Reader) error {
	full, err := u.qualifyName(name)
	if err != nil {
		return err
	}
	return u.client.installUnitFile(full, content)
}

// RemoveUnitFile removes an installed volume unit file.
func (u *VolumeUnit) RemoveUnitFile(name string) error {
	full, err := u.qualifyName(name)
	if err != nil {
		return err
	}
	return u.client.removeUnitFile(full)
}

// RuntimeState queries the current state of a volume unit via D-Bus.
func (u *VolumeUnit) RuntimeState(ctx context.Context, name string) (*UnitState, error) {
	full, err := u.qualifyName(name)
	if err != nil {
		return nil, err
	}
	return u.client.getUnitState(ctx, full)
}
