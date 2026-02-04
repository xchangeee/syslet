// Package daemon implements the syslet daemon, which provides synchronous
// CRUD operations for containers, volumes, and networks backed by systemd
// quadlet files. Each Apply* method stores the specs, computes a diff against
// the installed state, and executes changes in the strict global order:
// stop all changed → write configs → write unit files → single daemon-reload →
// start all that need starting.
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
	containerConfigDirectory string
}

// Config holds daemon configuration.
type Config struct {
	Clock quartz.Clock

	// Directory where bind-mounted container config files are stored
	ContainerConfigDirectory string
}

// New creates a new daemon.
func New(fs afero.Fs, logger *slog.Logger, sd *systemd.Client, st *store.Store, dcfg Config) *Daemon {
	if dcfg.Clock == nil {
		dcfg.Clock = quartz.NewReal()
	}
	if dcfg.ContainerConfigDirectory == "" {
		dcfg.ContainerConfigDirectory = DefaultContainerConfigDirectory
	}
	cfg := NewConfigFileManagerWithPaths(
		fs,
		dcfg.ContainerConfigDirectory,
	)
	return &Daemon{
		clock:                    dcfg.Clock,
		logger:                   logger,
		store:                    st,
		config:                   cfg,
		systemd:                  sd,
		containerConfigDirectory: dcfg.ContainerConfigDirectory,
	}
}

// ---------------------------------------------------------------------------
// Container operations
// ---------------------------------------------------------------------------

// containerChange captures the diff for a single container and carries the
// rendered content needed to apply it.
type containerChange struct {
	spec       *pb.ContainerSpec
	fullName   string
	opts       []*unit.UnitOption
	cfgs       []ConfigFile
	newContent string

	isNew         bool
	unitChanged   bool
	configChanged bool
	needsStop     bool
	needsStart    bool

	// Set when this change is rejected or errored during execution.
	errored bool
	message string
}

// ApplyContainers applies multiple container specs in a single batched
// operation. Changes are executed in strict global order:
//  1. Stop all containers whose unit or config changed (using OLD config)
//  2. Write all config files
//  3. Write all unit files
//  4. Single daemon-reload (if any unit files changed)
//  5. Start all containers that need starting
func (d *Daemon) ApplyContainers(ctx context.Context, specs []*pb.ContainerSpec) ([]*pb.ApplyResult, error) {
	// Phase 1: Diff — compute changes for every spec.
	var changes []*containerChange
	for _, spec := range specs {
		c, err := d.diffContainer(ctx, spec)
		if err != nil {
			return nil, fmt.Errorf("diffing %s: %w", spec.Name, err)
		}
		changes = append(changes, c)
	}

	// Phase 2: Execute — strict global order.

	// 2a. Stop all that need stopping.
	for _, c := range changes {
		if !c.needsStop {
			continue
		}
		d.logger.Info("stopping container", "name", c.fullName)
		if err := d.systemd.Container().Stop(ctx, c.fullName); err != nil {
			d.logger.Error("failed to stop container", "name", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to stop: %v", err)
		}
	}

	// 2b. Write config files.
	for _, c := range changes {
		if c.errored || !c.configChanged {
			continue
		}
		for _, cf := range c.cfgs {
			d.logger.Info("writing config", "container", c.fullName, "file", cf.Filename)
			if err := d.config.Write(c.fullName, cf.Filename, cf.Content); err != nil {
				d.logger.Error("failed to write config", "container", c.fullName, "file", cf.Filename, "error", err)
			}
		}
	}

	// 2c. Write unit files.
	needReload := false
	for _, c := range changes {
		if c.errored || !c.unitChanged {
			continue
		}
		d.logger.Info("installing unit file", "container", c.fullName)
		if err := d.systemd.Container().InstallUnitFile(c.fullName, strings.NewReader(c.newContent)); err != nil {
			d.logger.Error("failed to install unit file", "container", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to install unit file: %v", err)
			continue
		}
		needReload = true
	}

	// 2d. Single daemon-reload.
	if needReload {
		d.logger.Info("daemon-reload")
		if err := d.systemd.DaemonReload(ctx); err != nil {
			d.logger.Error("daemon-reload failed", "error", err)
		}
	}

	// 2e. Start all that need starting.
	for _, c := range changes {
		if c.errored || !c.needsStart {
			continue
		}
		d.logger.Info("starting container", "name", c.fullName)
		if err := d.systemd.Container().Start(ctx, c.fullName); err != nil {
			d.logger.Error("failed to start container", "name", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to start: %v", err)
		}
	}

	// Phase 3: Persist specs and build results.
	now := d.clock.Now()
	var results []*pb.ApplyResult
	for _, c := range changes {
		changed := c.unitChanged || c.configChanged || c.needsStart || c.needsStop
		msg := c.message
		if !c.errored && msg == "" {
			msg = d.containerResultMessage(c)
		}

		if err := d.store.PutContainer(c.spec); err != nil {
			d.logger.Error("failed to store spec", "name", c.spec.Name, "error", err)
		}

		errStr := ""
		if c.errored {
			errStr = c.message
		}
		if err := d.store.UpdateContainerStatus(c.spec.Name, errStr, now); err != nil {
			d.logger.Error("failed to update status", "name", c.spec.Name, "error", err)
		}

		results = append(results, &pb.ApplyResult{
			Name:    c.spec.Name,
			Changed: changed,
			Message: msg,
		})
	}
	return results, nil
}

// diffContainer computes the diff for a single container spec against the
// installed systemd state.
func (d *Daemon) diffContainer(ctx context.Context, spec *pb.ContainerSpec) (*containerChange, error) {
	fullName := spec.Name + ".container"

	opts, cfgs, err := d.renderContainerSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("rendering spec: %w", err)
	}
	newContent := unitContent(opts)

	c := &containerChange{
		spec:       spec,
		fullName:   fullName,
		opts:       opts,
		cfgs:       cfgs,
		newContent: newContent,
	}

	installed, err := d.systemd.Container().UnitFileExists(fullName)
	if err != nil {
		return nil, err
	}

	if !installed {
		c.isNew = true
		c.unitChanged = true
		c.configChanged = len(cfgs) > 0
		if spec.DesiredState == pb.DesiredState_DESIRED_STATE_RUNNING {
			c.needsStart = true
		}
		return c, nil
	}

	// Unit exists — check for changes.
	unitChanged, err := d.unitFileChanged(fullName, newContent)
	if err != nil {
		return nil, err
	}
	c.unitChanged = unitChanged
	c.configChanged = d.configsChanged(fullName, cfgs)

	state, err := d.systemd.Container().RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	// If unit or config changed and currently running, stop first.
	if (c.unitChanged || c.configChanged) && (state.ActiveState == "active" || state.ActiveState == "activating") {
		c.needsStop = true
	}

	switch spec.DesiredState {
	case pb.DesiredState_DESIRED_STATE_RUNNING:
		if state.ActiveState != "active" || c.needsStop {
			c.needsStart = true
		}
	case pb.DesiredState_DESIRED_STATE_STOPPED:
		if state.ActiveState == "active" || state.ActiveState == "activating" {
			c.needsStop = true
		}
	}

	return c, nil
}

func (d *Daemon) containerResultMessage(c *containerChange) string {
	var parts []string
	if c.isNew {
		parts = append(parts, "created")
	}
	if c.unitChanged && !c.isNew {
		parts = append(parts, "unit updated")
	}
	if c.configChanged {
		parts = append(parts, "config updated")
	}
	if c.needsStop && c.needsStart {
		parts = append(parts, "restarted")
	} else if c.needsStart {
		parts = append(parts, "started")
	} else if c.needsStop {
		parts = append(parts, "stopped")
	}
	if len(parts) == 0 {
		return "up to date"
	}
	return strings.Join(parts, ", ")
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

// ApplyVolumes applies multiple volume specs. Volumes are immutable after
// creation — if a spec differs from the installed unit file, the result
// contains an error. A single daemon-reload is issued if any new volumes
// were created.
func (d *Daemon) ApplyVolumes(ctx context.Context, specs []*pb.VolumeSpec) ([]*pb.ApplyResult, error) {
	needReload := false
	now := d.clock.Now()
	var results []*pb.ApplyResult

	for _, spec := range specs {
		fullName := spec.Name + ".volume"
		opts := d.renderSimpleSpec(spec.Options)
		newContent := unitContent(opts)

		installed, err := d.systemd.Volume().UnitFileExists(fullName)
		if err != nil {
			return nil, fmt.Errorf("checking %s: %w", spec.Name, err)
		}

		if !installed {
			if err := d.systemd.Volume().InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
				return nil, fmt.Errorf("installing %s: %w", fullName, err)
			}
			needReload = true
			if err := d.store.PutVolume(spec); err != nil {
				return nil, fmt.Errorf("storing spec %s: %w", spec.Name, err)
			}
			d.store.UpdateVolumeStatus(spec.Name, "", now) //nolint:errcheck
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
			d.store.UpdateVolumeStatus(spec.Name, msg, now) //nolint:errcheck
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

// ApplyNetworks applies multiple network specs. Networks are immutable after
// creation — if a spec differs from the installed unit file, the result
// contains an error. A single daemon-reload is issued if any new networks
// were created.
func (d *Daemon) ApplyNetworks(ctx context.Context, specs []*pb.NetworkSpec) ([]*pb.ApplyResult, error) {
	needReload := false
	now := d.clock.Now()
	var results []*pb.ApplyResult

	for _, spec := range specs {
		fullName := spec.Name + ".network"
		opts := d.renderSimpleSpec(spec.Options)
		newContent := unitContent(opts)

		installed, err := d.systemd.Network().UnitFileExists(fullName)
		if err != nil {
			return nil, fmt.Errorf("checking %s: %w", spec.Name, err)
		}

		if !installed {
			if err := d.systemd.Network().InstallUnitFile(fullName, strings.NewReader(newContent)); err != nil {
				return nil, fmt.Errorf("installing %s: %w", fullName, err)
			}
			needReload = true
			if err := d.store.PutNetwork(spec); err != nil {
				return nil, fmt.Errorf("storing spec %s: %w", spec.Name, err)
			}
			d.store.UpdateNetworkStatus(spec.Name, "", now) //nolint:errcheck
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
			d.store.UpdateNetworkStatus(spec.Name, msg, now) //nolint:errcheck
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

		hostPath := filepath.Join(d.containerConfigDirectory, spec.Name, basename)
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
// Shared helpers
// ---------------------------------------------------------------------------

// unitContent serializes go-systemd options into INI-style unit file content.
func unitContent(opts []*unit.UnitOption) string {
	data, _ := io.ReadAll(unit.Serialize(opts))
	return string(data)
}

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
