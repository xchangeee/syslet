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
type NetworkDaemon struct {
	logger  *slog.Logger
	systemd *systemd.Client
}

// Config holds daemon configuration.
type NetworkDaemonConfig struct {
}

// New creates a new daemon.
func NewNetworkDaemon(fs afero.Fs, logger *slog.Logger, sd *systemd.Client, dcfg NetworkDaemonConfig) *NetworkDaemon {
	return &NetworkDaemon{
		logger:  logger,
		systemd: sd,
	}
}

// ---------------------------------------------------------------------------
// Network operations
// ---------------------------------------------------------------------------

// ApplyNetworks applies multiple network specs. Networks are immutable after
// creation — if a spec differs from the installed unit file, the result
// contains an error. A single daemon-reload is issued if any new networks
// were created.
func (d *NetworkDaemon) ApplyNetworks(ctx context.Context, specs []*pb.NetworkSpec) ([]*pb.ApplyResult, error) {
	needReload := false
	var results []*pb.ApplyResult

	for _, spec := range specs {
		fullName := spec.Name + ".network"
		opts := d.renderSimpleSpec(spec.Options)
		newContent := unitContent(opts)

		installed, err := d.systemd.Network.UnitFileExists(fullName)
		if err != nil {
			return nil, fmt.Errorf("checking %s: %w", spec.Name, err)
		}

		if !installed {
			if err := d.systemd.Network.InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
				return nil, fmt.Errorf("installing %s: %w", fullName, err)
			}
			needReload = true
			results = append(results, &pb.ApplyResult{Name: spec.Name, Changed: true, Message: "created"})
			continue
		}

		// Existing network: reject if changed.
		changed, err := d.unitFileChangedNetwork(fullName, newContent)
		if err != nil {
			return nil, fmt.Errorf("diffing %s: %w", spec.Name, err)
		}
		if changed {
			msg := fmt.Sprintf("immutable network %q cannot be modified after creation", spec.Name)
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

// GetNetwork returns the status of a single network.
func (d *NetworkDaemon) GetNetwork(ctx context.Context, name string) (*pb.NetworkStatus, error) {
	fullName := name + ".network"

	installed, err := d.systemd.Network.UnitFileExists(fullName)
	if err != nil {
		return nil, err
	}
	if !installed {
		return nil, fmt.Errorf("network %q not found", name)
	}

	state, err := d.systemd.Network.RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	return &pb.NetworkStatus{
		Name:        name,
		ActiveState: pbActiveState(state.ActiveState),
		Enabled:     state.Enabled,
	}, nil
}

// ListNetworks returns the status of all managed networks.
func (d *NetworkDaemon) ListNetworks(ctx context.Context) ([]*pb.NetworkStatus, error) {
	files, err := d.systemd.Network.ListUnitFiles()
	if err != nil {
		return nil, err
	}
	var result []*pb.NetworkStatus
	for _, fullName := range files {
		name := pb.UnitName(fullName)
		state, err := d.systemd.Network.RuntimeState(ctx, fullName)
		if err != nil {
			d.logger.Error("status error", "network", name, "error", err)
			continue
		}
		result = append(result, &pb.NetworkStatus{
			Name:        name,
			ActiveState: pbActiveState(state.ActiveState),
			Enabled:     state.Enabled,
		})
	}
	return result, nil
}

// DeleteNetwork removes a network unit file.
func (d *NetworkDaemon) DeleteNetwork(ctx context.Context, name string) error {
	fullName := name + ".network"
	if err := d.systemd.Network.RemoveUnitFile(fullName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}
	return nil
}

// renderSimpleSpec maps proto UnitOptions to go-systemd options for volumes
// and networks (no configs, no [Install] section).
func (d *NetworkDaemon) renderSimpleSpec(pbOpts []*pb.UnitOption) []*unit.UnitOption {
	var opts []*unit.UnitOption
	for _, o := range pbOpts {
		opts = append(opts, &unit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}
	return opts
}

func (d *NetworkDaemon) unitFileChangedNetwork(fullName string, newContent string) (bool, error) {
	existing, err := d.systemd.Network.ReadInstalledUnit(fullName)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sha256sum([]byte(newContent)) != sha256sum(existing), nil
}
