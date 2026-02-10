// Package daemon implements the syslet daemon, which provides synchronous
// CRUD operations for containers, volumes, and networks backed by systemd
// quadlet files. Each Apply* method computes a diff against the installed
// state, and executes changes in the strict global order:
// stop all changed → write configs → write unit files → single daemon-reload →
// start all that need starting.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

// Daemon is the main syslet daemon. It holds shared dependencies and exposes
// synchronous per-type CRUD methods for containers, volumes, and networks.
type VolumeDaemon struct {
	logger  *slog.Logger
	systemd *systemd.Client
}

// Config holds daemon configuration.
type VolumeDaemonConfig struct {
}

// New creates a new daemon.
func NewVolumeDaemon(fs afero.Fs, logger *slog.Logger, sd *systemd.Client, dcfg VolumeDaemonConfig) *VolumeDaemon {
	return &VolumeDaemon{
		logger:  logger,
		systemd: sd,
	}
}

// ApplyVolumes applies multiple volume specs. Volumes are immutable after
// creation — if a spec differs from the installed unit file, the result
// contains an error. A single daemon-reload is issued if any new volumes
// were created.
func (d *VolumeDaemon) ApplyVolumes(ctx context.Context, specs []*pb.VolumeSpec) ([]*pb.ApplyResult, error) {
	needReload := false
	var results []*pb.ApplyResult

	for _, spec := range specs {
		fullName := spec.Name + ".volume"
		var opts []*unit.UnitOption
		for _, o := range spec.Options {
			opts = append(opts, &unit.UnitOption{
				Section: o.Section,
				Name:    o.Name,
				Value:   o.Value,
			})
		}
		newContent := unitContent(opts)

		installed, err := d.systemd.Volume.UnitFileExists(fullName)
		if err != nil {
			return nil, fmt.Errorf("checking %s: %w", spec.Name, err)
		}

		if !installed {
			if err := d.systemd.Volume.InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
				return nil, fmt.Errorf("installing %s: %w", fullName, err)
			}
			needReload = true
			results = append(results, &pb.ApplyResult{Name: spec.Name, Changed: true, Message: "created"})
			continue
		}

		// Existing volume: reject if changed.
		changed, err := d.unitFileChangedVolume(fullName, newContent)
		if err != nil {
			return nil, fmt.Errorf("diffing %s: %w", spec.Name, err)
		}
		if changed {
			msg := fmt.Sprintf("immutable volume %q cannot be modified after creation", spec.Name)
			results = append(results, &pb.ApplyResult{Name: spec.Name, Changed: false, Message: msg})
			continue
		}

		results = append(results, &pb.ApplyResult{Name: spec.Name, Changed: false, Message: "up to date"})
	}

	if needReload {
		if err := d.systemd.DaemonReload(ctx); err != nil {
			d.logger.Error("daemon-reload failed", "error", err)
		}
	}

	return results, nil
}

// GetVolume returns the status of a single volume.
func (d *VolumeDaemon) GetVolume(ctx context.Context, name string) (*pb.VolumeStatus, error) {
	fullName := name + ".volume"

	installed, err := d.systemd.Volume.UnitFileExists(fullName)
	if err != nil {
		return nil, err
	}
	if !installed {
		return nil, fmt.Errorf("volume %q not found", name)
	}

	state, err := d.systemd.Volume.RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	return &pb.VolumeStatus{
		Name:        name,
		ActiveState: pbActiveState(state.ActiveState),
		Enabled:     state.Enabled,
	}, nil
}

// ListVolumes returns the status of all managed volumes.
func (d *VolumeDaemon) ListVolumes(ctx context.Context) ([]*pb.VolumeStatus, error) {
	files, err := d.systemd.Volume.ListUnitFiles()
	if err != nil {
		return nil, err
	}
	var result []*pb.VolumeStatus
	for _, fullName := range files {
		name := pb.UnitName(fullName)
		state, err := d.systemd.Volume.RuntimeState(ctx, fullName)
		if err != nil {
			d.logger.Error("status error", "volume", name, "error", err)
			continue
		}
		result = append(result, &pb.VolumeStatus{
			Name:        name,
			ActiveState: pbActiveState(state.ActiveState),
			Enabled:     state.Enabled,
		})
	}
	return result, nil
}

// DeleteVolume removes a volume unit file.
func (d *VolumeDaemon) DeleteVolume(ctx context.Context, name string) error {
	fullName := name + ".volume"
	if err := d.systemd.Volume.RemoveUnitFile(fullName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}
	return nil
}

func (d *VolumeDaemon) unitFileChangedVolume(fullName string, newContent string) (bool, error) {
	existing, err := d.systemd.Volume.ReadInstalledUnit(fullName)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sha256sum([]byte(newContent)) != sha256sum(existing), nil
}
