// Package daemon implements the syslet daemon, which provides synchronous
// CRUD operations for containers, volumes, and networks backed by systemd
// quadlet files. Each Apply* method stores the spec, writes unit/config files,
// reloads systemd, and starts/stops units inline—there is no background
// reconciliation loop.
package daemon

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/store"
	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coder/quartz"
	"github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

const (
	DefaultContainerUnitDirectory   = "/etc/containers/config"
	DefaultContainerConfigDirectory = "/var/syslet/containers/config"
)

// Daemon is the main syslet daemon. It holds shared dependencies and exposes
// synchronous per-type CRUD methods for containers, volumes, and networks.
type Daemon struct {
	clock   quartz.Clock
	logger  *slog.Logger
	systemd *systemd.Client
	store   *store.Store
	config  *ConfigFileManager

	// Host directory referenced in Volume= entries for config mounts
	containerUnitDirectory string
}

// Config holds daemon configuration.
type Config struct {
	Clock quartz.Clock

	// Directory where quadlet container unit files are stored
	ContainerUnitDirectory string

	// Directory where bind-mounted container config files are stored
	ContainerConfigDirectory string
}

// New creates a new daemon.
func New(fs afero.Fs, logger *slog.Logger, sd *systemd.Client, st *store.Store, dcfg Config) *Daemon {
	if dcfg.Clock == nil {
		dcfg.Clock = quartz.NewReal()
	}
	if dcfg.ContainerUnitDirectory == "" {
		dcfg.ContainerUnitDirectory = DefaultContainerUnitDirectory
	}
	if dcfg.ContainerConfigDirectory == "" {
		dcfg.ContainerConfigDirectory = DefaultContainerConfigDirectory
	}
	cfg := NewConfigFileManagerWithPaths(
		fs,
		dcfg.ContainerConfigDirectory,
	)
	return &Daemon{
		clock:                  dcfg.Clock,
		logger:                 logger,
		store:                  st,
		config:                 cfg,
		systemd:                sd,
		containerUnitDirectory: dcfg.ContainerUnitDirectory,
	}
}

// ---------------------------------------------------------------------------
// Container operations
// ---------------------------------------------------------------------------

// ApplyContainer creates or updates a container. It writes the unit file and
// config files to disk, reloads systemd, and starts/stops the container
// according to the desired state. Returns whether anything changed and a
// human-readable message describing what happened.
func (d *Daemon) ApplyContainer(ctx context.Context, spec *pb.ContainerSpec) (bool, string, error) {
	fullName := spec.Name + ".container"

	opts, cfgs, err := d.renderContainerSpec(spec)
	if err != nil {
		return false, "", fmt.Errorf("rendering spec: %w", err)
	}
	newContent := unitContent(opts)

	installed, err := d.systemd.Container().UnitFileExists(fullName)
	if err != nil {
		return false, "", err
	}

	if !installed {
		// New container: write everything, reload, optionally start.
		if err := d.writeConfigs(fullName, cfgs); err != nil {
			return false, "", err
		}
		if err := d.systemd.Container().InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
			return false, "", fmt.Errorf("installing unit file: %w", err)
		}
		if err := d.systemd.DaemonReload(ctx); err != nil {
			return false, "", fmt.Errorf("daemon-reload: %w", err)
		}

		if err := d.store.PutContainer(spec); err != nil {
			return false, "", fmt.Errorf("storing spec: %w", err)
		}

		msg := "created"
		if spec.DesiredState == pb.DesiredState_DESIRED_STATE_RUNNING {
			if err := d.systemd.Container().Start(ctx, fullName); err != nil {
				d.store.UpdateContainerStatus(spec.Name, fmt.Sprintf("failed to start: %v", err), d.clock.Now()) //nolint:errcheck
				return true, "created, failed to start: " + err.Error(), nil
			}
			msg = "created, started"
		}

		d.store.UpdateContainerStatus(spec.Name, "", d.clock.Now()) //nolint:errcheck
		return true, msg, nil
	}

	// Existing container: diff and apply changes.
	unitChanged, err := d.unitFileChanged(fullName, newContent)
	if err != nil {
		return false, "", err
	}

	configChanged := d.configsChanged(fullName, cfgs)

	// Check desired state vs runtime.
	state, err := d.systemd.Container().RuntimeState(ctx, fullName)
	if err != nil {
		return false, "", err
	}

	needsStop := false
	needsStart := false

	if (unitChanged || configChanged) && (state.ActiveState == "active" || state.ActiveState == "activating") {
		needsStop = true
	}

	switch spec.DesiredState {
	case pb.DesiredState_DESIRED_STATE_RUNNING:
		if state.ActiveState != "active" || needsStop {
			needsStart = true
		}
	case pb.DesiredState_DESIRED_STATE_STOPPED:
		if state.ActiveState == "active" || state.ActiveState == "activating" {
			needsStop = true
		}
	}

	changed := unitChanged || configChanged || needsStart || needsStop
	if !changed {
		return false, "up to date", nil
	}

	// Stop before update.
	if needsStop {
		if err := d.systemd.Container().Stop(ctx, fullName); err != nil {
			d.store.UpdateContainerStatus(spec.Name, fmt.Sprintf("failed to stop: %v", err), d.clock.Now()) //nolint:errcheck
			return false, "", fmt.Errorf("stopping container: %w", err)
		}
	}

	if configChanged {
		if err := d.writeConfigs(fullName, cfgs); err != nil {
			return false, "", err
		}
	}

	if unitChanged {
		if err := d.systemd.Container().InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
			return false, "", fmt.Errorf("installing unit file: %w", err)
		}
		if err := d.systemd.DaemonReload(ctx); err != nil {
			return false, "", fmt.Errorf("daemon-reload: %w", err)
		}
	}

	if err := d.store.PutContainer(spec); err != nil {
		return false, "", fmt.Errorf("storing spec: %w", err)
	}

	if needsStart {
		if err := d.systemd.Container().Start(ctx, fullName); err != nil {
			d.store.UpdateContainerStatus(spec.Name, fmt.Sprintf("failed to start: %v", err), d.clock.Now()) //nolint:errcheck
			return true, "updated, failed to start: " + err.Error(), nil
		}
	}

	d.store.UpdateContainerStatus(spec.Name, "", d.clock.Now()) //nolint:errcheck
	return true, buildMessage(unitChanged, configChanged, needsStop, needsStart), nil
}

// GetContainer returns the status of a single container.
func (d *Daemon) GetContainer(ctx context.Context, name string) (*pb.ContainerStatus, error) {
	spec, opStatus, err := d.store.GetContainer(name)
	if err != nil {
		return nil, err
	}
	fullName := spec.Name + ".container"

	state, err := d.systemd.Container().RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	cs := &pb.ContainerStatus{
		Name:         spec.Name,
		DesiredState: spec.DesiredState,
		ActiveState:  pbActiveState(state.ActiveState),
		Enabled:      state.Enabled,
		LastError:    opStatus.LastError,
	}
	if !opStatus.LastApplied.IsZero() {
		cs.LastApplied = opStatus.LastApplied.Format(time.RFC3339)
	}
	cs.ConfigFiles, _ = d.config.ListFiles(fullName)
	return cs, nil
}

// ListContainers returns the status of all managed containers.
func (d *Daemon) ListContainers(ctx context.Context) ([]*pb.ContainerStatus, error) {
	rows, err := d.store.ListContainers()
	if err != nil {
		return nil, err
	}
	var result []*pb.ContainerStatus
	for _, row := range rows {
		fullName := row.Spec.Name + ".container"
		state, err := d.systemd.Container().RuntimeState(ctx, fullName)
		if err != nil {
			d.logger.Error("status error", "container", row.Spec.Name, "error", err)
			continue
		}
		cs := &pb.ContainerStatus{
			Name:         row.Spec.Name,
			DesiredState: row.Spec.DesiredState,
			ActiveState:  pbActiveState(state.ActiveState),
			Enabled:      state.Enabled,
			LastError:    row.Status.LastError,
		}
		if !row.Status.LastApplied.IsZero() {
			cs.LastApplied = row.Status.LastApplied.Format(time.RFC3339)
		}
		cs.ConfigFiles, _ = d.config.ListFiles(fullName)
		result = append(result, cs)
	}
	return result, nil
}

// DeleteContainer stops and removes a container, its unit file, and config files.
func (d *Daemon) DeleteContainer(ctx context.Context, name string) error {
	fullName := name + ".container"

	// Stop if running.
	if err := d.systemd.Container().Stop(ctx, fullName); err != nil {
		d.logger.Warn("stop failed during delete", "container", name, "error", err)
	}

	if err := d.systemd.Container().RemoveUnitFile(fullName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := d.config.RemoveAll(fullName); err != nil {
		d.logger.Warn("config cleanup failed during delete", "container", name, "error", err)
	}

	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}

	return d.store.DeleteContainer(name)
}

// ---------------------------------------------------------------------------
// Volume operations
// ---------------------------------------------------------------------------

// ApplyVolume creates a volume. Volumes are immutable after creation — if the
// spec differs from the installed unit file, an error is returned.
func (d *Daemon) ApplyVolume(ctx context.Context, spec *pb.VolumeSpec) (bool, string, error) {
	fullName := spec.Name + ".volume"

	opts := d.renderSimpleSpec(spec.Options)
	newContent := unitContent(opts)

	installed, err := d.systemd.Volume().UnitFileExists(fullName)
	if err != nil {
		return false, "", err
	}

	if !installed {
		if err := d.systemd.Volume().InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
			return false, "", fmt.Errorf("installing unit file: %w", err)
		}
		if err := d.systemd.DaemonReload(ctx); err != nil {
			return false, "", fmt.Errorf("daemon-reload: %w", err)
		}
		if err := d.store.PutVolume(spec); err != nil {
			return false, "", fmt.Errorf("storing spec: %w", err)
		}
		d.store.UpdateVolumeStatus(spec.Name, "", d.clock.Now()) //nolint:errcheck
		return true, "created", nil
	}

	// Existing volume: check if content changed (immutable).
	unitChanged, err := d.unitFileChangedVolume(fullName, newContent)
	if err != nil {
		return false, "", err
	}
	if unitChanged {
		return false, "", fmt.Errorf("immutable volume %q cannot be modified after creation", spec.Name)
	}

	return false, "up to date", nil
}

// GetVolume returns the status of a single volume.
func (d *Daemon) GetVolume(ctx context.Context, name string) (*pb.VolumeStatus, error) {
	_, opStatus, err := d.store.GetVolume(name)
	if err != nil {
		return nil, err
	}
	fullName := name + ".volume"

	state, err := d.systemd.Volume().RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	vs := &pb.VolumeStatus{
		Name:        name,
		ActiveState: pbActiveState(state.ActiveState),
		Enabled:     state.Enabled,
		LastError:   opStatus.LastError,
	}
	if !opStatus.LastApplied.IsZero() {
		vs.LastApplied = opStatus.LastApplied.Format(time.RFC3339)
	}
	return vs, nil
}

// ListVolumes returns the status of all managed volumes.
func (d *Daemon) ListVolumes(ctx context.Context) ([]*pb.VolumeStatus, error) {
	rows, err := d.store.ListVolumes()
	if err != nil {
		return nil, err
	}
	var result []*pb.VolumeStatus
	for _, row := range rows {
		fullName := row.Spec.Name + ".volume"
		state, err := d.systemd.Volume().RuntimeState(ctx, fullName)
		if err != nil {
			d.logger.Error("status error", "volume", row.Spec.Name, "error", err)
			continue
		}
		vs := &pb.VolumeStatus{
			Name:        row.Spec.Name,
			ActiveState: pbActiveState(state.ActiveState),
			Enabled:     state.Enabled,
			LastError:   row.Status.LastError,
		}
		if !row.Status.LastApplied.IsZero() {
			vs.LastApplied = row.Status.LastApplied.Format(time.RFC3339)
		}
		result = append(result, vs)
	}
	return result, nil
}

// DeleteVolume removes a volume unit file and its store entry.
func (d *Daemon) DeleteVolume(ctx context.Context, name string) error {
	fullName := name + ".volume"

	if err := d.systemd.Volume().RemoveUnitFile(fullName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}
	return d.store.DeleteVolume(name)
}

// ---------------------------------------------------------------------------
// Network operations
// ---------------------------------------------------------------------------

// ApplyNetwork creates a network. Networks are immutable after creation — if
// the spec differs from the installed unit file, an error is returned.
func (d *Daemon) ApplyNetwork(ctx context.Context, spec *pb.NetworkSpec) (bool, string, error) {
	fullName := spec.Name + ".network"

	opts := d.renderSimpleSpec(spec.Options)
	newContent := unitContent(opts)

	installed, err := d.systemd.Network().UnitFileExists(fullName)
	if err != nil {
		return false, "", err
	}

	if !installed {
		if err := d.systemd.Network().InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
			return false, "", fmt.Errorf("installing unit file: %w", err)
		}
		if err := d.systemd.DaemonReload(ctx); err != nil {
			return false, "", fmt.Errorf("daemon-reload: %w", err)
		}
		if err := d.store.PutNetwork(spec); err != nil {
			return false, "", fmt.Errorf("storing spec: %w", err)
		}
		d.store.UpdateNetworkStatus(spec.Name, "", d.clock.Now()) //nolint:errcheck
		return true, "created", nil
	}

	// Existing network: check if content changed (immutable).
	unitChanged, err := d.unitFileChangedNetwork(fullName, newContent)
	if err != nil {
		return false, "", err
	}
	if unitChanged {
		return false, "", fmt.Errorf("immutable network %q cannot be modified after creation", spec.Name)
	}

	return false, "up to date", nil
}

// GetNetwork returns the status of a single network.
func (d *Daemon) GetNetwork(ctx context.Context, name string) (*pb.NetworkStatus, error) {
	_, opStatus, err := d.store.GetNetwork(name)
	if err != nil {
		return nil, err
	}
	fullName := name + ".network"

	state, err := d.systemd.Network().RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	ns := &pb.NetworkStatus{
		Name:        name,
		ActiveState: pbActiveState(state.ActiveState),
		Enabled:     state.Enabled,
		LastError:   opStatus.LastError,
	}
	if !opStatus.LastApplied.IsZero() {
		ns.LastApplied = opStatus.LastApplied.Format(time.RFC3339)
	}
	return ns, nil
}

// ListNetworks returns the status of all managed networks.
func (d *Daemon) ListNetworks(ctx context.Context) ([]*pb.NetworkStatus, error) {
	rows, err := d.store.ListNetworks()
	if err != nil {
		return nil, err
	}
	var result []*pb.NetworkStatus
	for _, row := range rows {
		fullName := row.Spec.Name + ".network"
		state, err := d.systemd.Network().RuntimeState(ctx, fullName)
		if err != nil {
			d.logger.Error("status error", "network", row.Spec.Name, "error", err)
			continue
		}
		ns := &pb.NetworkStatus{
			Name:        row.Spec.Name,
			ActiveState: pbActiveState(state.ActiveState),
			Enabled:     state.Enabled,
			LastError:   row.Status.LastError,
		}
		if !row.Status.LastApplied.IsZero() {
			ns.LastApplied = row.Status.LastApplied.Format(time.RFC3339)
		}
		result = append(result, ns)
	}
	return result, nil
}

// DeleteNetwork removes a network unit file and its store entry.
func (d *Daemon) DeleteNetwork(ctx context.Context, name string) error {
	fullName := name + ".network"

	if err := d.systemd.Network().RemoveUnitFile(fullName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}
	return d.store.DeleteNetwork(name)
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

// renderContainerSpec converts a ContainerSpec into systemd unit options and
// config files. It maps proto options to go-systemd options, generates Volume=
// entries for config file bind mounts, and adds an [Install] section.
func (d *Daemon) renderContainerSpec(spec *pb.ContainerSpec) ([]*unit.UnitOption, []ConfigFile, error) {
	var opts []*unit.UnitOption
	for _, o := range spec.Options {
		opts = append(opts, &unit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}

	var cfgFiles []ConfigFile
	seen := make(map[string]bool)

	for _, ce := range spec.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		if seen[basename] {
			return nil, nil, fmt.Errorf("duplicate config basename %q (from targetVolumePath %q)", basename, ce.TargetVolumePath)
		}
		seen[basename] = true

		hostPath := filepath.Join(d.containerUnitDirectory, spec.Name, basename)
		volumeEntry := fmt.Sprintf("%s:%s:ro", hostPath, ce.TargetVolumePath)

		opts = append(opts, &unit.UnitOption{
			Section: "Container",
			Name:    "Volume",
			Value:   volumeEntry,
		})

		cfgFiles = append(cfgFiles, ConfigFile{
			UnitName: spec.Name,
			Filename: basename,
			Content:  ce.Content,
		})
	}

	// Auto-generate [Install] section for containers.
	opts = append(opts, &unit.UnitOption{
		Section: "Install",
		Name:    "WantedBy",
		Value:   "multi-user.target default.target",
	})

	return opts, cfgFiles, nil
}

// renderSimpleSpec maps proto UnitOptions to go-systemd options for volumes
// and networks (no configs, no [Install] section).
func (d *Daemon) renderSimpleSpec(pbOpts []*pb.UnitOption) []*unit.UnitOption {
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

// ---------------------------------------------------------------------------
// Diffing helpers
// ---------------------------------------------------------------------------

func (d *Daemon) unitFileChanged(fullName string, newContent string) (bool, error) {
	existing, err := d.systemd.Container().ReadInstalledUnit(fullName)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sha256sum([]byte(newContent)) != sha256sum(existing), nil
}

func (d *Daemon) unitFileChangedVolume(fullName string, newContent string) (bool, error) {
	existing, err := d.systemd.Volume().ReadInstalledUnit(fullName)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sha256sum([]byte(newContent)) != sha256sum(existing), nil
}

func (d *Daemon) unitFileChangedNetwork(fullName string, newContent string) (bool, error) {
	existing, err := d.systemd.Network().ReadInstalledUnit(fullName)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sha256sum([]byte(newContent)) != sha256sum(existing), nil
}

func (d *Daemon) configsChanged(fullName string, cfgs []ConfigFile) bool {
	for _, cf := range cfgs {
		changed, err := d.config.IsChanged(fullName, cf.Filename, cf.Content)
		if err != nil || changed {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Writing helpers
// ---------------------------------------------------------------------------

func (d *Daemon) writeConfigs(fullName string, cfgs []ConfigFile) error {
	for _, cf := range cfgs {
		if err := d.config.Write(fullName, cf.Filename, cf.Content); err != nil {
			return fmt.Errorf("writing config %s: %w", cf.Filename, err)
		}
	}
	return nil
}

// unitContent serializes go-systemd options into INI-style unit file content.
func unitContent(opts []*unit.UnitOption) string {
	data, _ := io.ReadAll(unit.Serialize(opts))
	return string(data)
}

// ---------------------------------------------------------------------------
// Status helpers
// ---------------------------------------------------------------------------

func pbActiveState(s string) pb.ActiveState {
	switch s {
	case "active":
		return pb.ActiveState_ACTIVE_STATE_ACTIVE
	case "inactive":
		return pb.ActiveState_ACTIVE_STATE_INACTIVE
	case "failed":
		return pb.ActiveState_ACTIVE_STATE_FAILED
	case "activating":
		return pb.ActiveState_ACTIVE_STATE_ACTIVATING
	case "deactivating":
		return pb.ActiveState_ACTIVE_STATE_DEACTIVATING
	default:
		return pb.ActiveState_ACTIVE_STATE_UNSPECIFIED
	}
}

func buildMessage(unitChanged, configChanged, needsStop, needsStart bool) string {
	var parts []string
	if unitChanged {
		parts = append(parts, "unit updated")
	}
	if configChanged {
		parts = append(parts, "config updated")
	}
	if needsStop && needsStart {
		parts = append(parts, "restarted")
	} else if needsStart {
		parts = append(parts, "started")
	} else if needsStop {
		parts = append(parts, "stopped")
	}
	if len(parts) == 0 {
		return "up to date"
	}
	return strings.Join(parts, ", ")
}
