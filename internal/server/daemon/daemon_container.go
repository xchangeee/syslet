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
	"path/filepath"
	"strings"

	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

// Daemon is the main syslet daemon. It holds shared dependencies and exposes
// synchronous per-type CRUD methods for containers, volumes, and networks.
type ContainerDaemon struct {
	logger  *slog.Logger
	systemd *systemd.Client
	config  *ConfigFileManager

	// Host directory referenced in Volume= entries for config mounts
	containerConfigDirectory string
}

// Config holds daemon configuration.
type ContainerDaemonConfig struct {
	// Directory where bind-mounted container config files are stored
	ContainerConfigDirectory string
}

// New creates a new daemon.
func NewContainerDaemon(fs afero.Fs, logger *slog.Logger, sd *systemd.Client, dcfg ContainerDaemonConfig) *ContainerDaemon {
	if dcfg.ContainerConfigDirectory == "" {
		dcfg.ContainerConfigDirectory = DefaultContainerConfigDirectory
	}
	cfg := NewConfigFileManagerWithPaths(
		fs,
		dcfg.ContainerConfigDirectory,
	)
	return &ContainerDaemon{
		logger:                   logger,
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

// ConfigFile represents a config file to deploy alongside a container.
type ConfigFile struct {
	// e.g. "myapp" (without extension)
	UnitName string
	// e.g. "config.yaml"
	Filename string
	Content  string
}

// ApplyContainers applies multiple container specs in a single batched
// operation. Changes are executed in strict global order:
//  1. Stop all containers whose unit or config changed (using OLD config)
//  2. Write all config files
//  3. Write all unit files
//  4. Single daemon-reload (if any unit files changed)
//  5. Start all containers that need starting
func (d *ContainerDaemon) ApplyContainers(ctx context.Context, specs []*pb.ContainerSpec) ([]*pb.ApplyResult, error) {
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
		if err := d.systemd.Container.Stop(ctx, c.fullName); err != nil {
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
		if err := d.systemd.Container.InstallUnitFile(c.fullName, strings.NewReader(c.newContent)); err != nil {
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
		if err := d.systemd.Container.Start(ctx, c.fullName); err != nil {
			d.logger.Error("failed to start container", "name", c.fullName, "error", err)
			c.errored = true
			c.message = fmt.Sprintf("failed to start: %v", err)
		}
	}

	// Build results.
	var results []*pb.ApplyResult
	for _, c := range changes {
		changed := c.unitChanged || c.configChanged || c.needsStart || c.needsStop
		msg := c.message
		if !c.errored && msg == "" {
			msg = d.containerResultMessage(c)
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
func (d *ContainerDaemon) diffContainer(ctx context.Context, spec *pb.ContainerSpec) (*containerChange, error) {
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

	installed, err := d.systemd.Container.UnitFileExists(fullName)
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

	state, err := d.systemd.Container.RuntimeState(ctx, fullName)
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

func (d *ContainerDaemon) containerResultMessage(c *containerChange) string {
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
func (d *ContainerDaemon) GetContainer(ctx context.Context, name string) (*pb.ContainerStatus, error) {
	fullName := name + ".container"

	installed, err := d.systemd.Container.UnitFileExists(fullName)
	if err != nil {
		return nil, err
	}
	if !installed {
		return nil, fmt.Errorf("container %q not found", name)
	}

	state, err := d.systemd.Container.RuntimeState(ctx, fullName)
	if err != nil {
		return nil, err
	}

	cs := &pb.ContainerStatus{
		Name:        name,
		ActiveState: pbActiveState(state.ActiveState),
		Enabled:     state.Enabled,
	}
	cs.ConfigFiles, _ = d.config.ListFiles(fullName)
	return cs, nil
}

// ListContainers returns the status of all managed containers.
func (d *ContainerDaemon) ListContainers(ctx context.Context) ([]*pb.ContainerStatus, error) {
	files, err := d.systemd.Container.ListUnitFiles()
	if err != nil {
		return nil, err
	}
	var result []*pb.ContainerStatus
	for _, fullName := range files {
		name := pb.UnitName(fullName)
		state, err := d.systemd.Container.RuntimeState(ctx, fullName)
		if err != nil {
			d.logger.Error("status error", "container", name, "error", err)
			continue
		}
		cs := &pb.ContainerStatus{
			Name:        name,
			ActiveState: pbActiveState(state.ActiveState),
			Enabled:     state.Enabled,
		}
		cs.ConfigFiles, _ = d.config.ListFiles(fullName)
		result = append(result, cs)
	}
	return result, nil
}

// DeleteContainer stops and removes a container, its unit file, and config files.
func (d *ContainerDaemon) DeleteContainer(ctx context.Context, name string) error {
	fullName := name + ".container"

	if err := d.systemd.Container.Stop(ctx, fullName); err != nil {
		d.logger.Warn("stop failed during delete", "container", name, "error", err)
	}
	if err := d.systemd.Container.RemoveUnitFile(fullName); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}
	if err := d.config.RemoveAll(fullName); err != nil {
		d.logger.Warn("config cleanup failed during delete", "container", name, "error", err)
	}
	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}
	return nil
}

// renderContainerSpec converts a ContainerSpec into systemd unit options and
// config files. It maps proto options to go-systemd options, generates Volume=
// entries for config file bind mounts, and adds an [Install] section.
func (d *ContainerDaemon) renderContainerSpec(spec *pb.ContainerSpec) ([]*unit.UnitOption, []ConfigFile, error) {
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

func (d *ContainerDaemon) unitFileChanged(fullName string, newContent string) (bool, error) {
	existing, err := d.systemd.Container.ReadInstalledUnit(fullName)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return sha256sum([]byte(newContent)) != sha256sum(existing), nil
}

func (d *ContainerDaemon) configsChanged(fullName string, cfgs []ConfigFile) bool {
	for _, cf := range cfgs {
		changed, err := d.config.IsChanged(fullName, cf.Filename, cf.Content)
		if err != nil || changed {
			return true
		}
	}
	return false
}
