package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/store"
	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	pb "codeberg.org/xchangeee/syslet/proto"
	"github.com/coder/quartz"
	"github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

const (
	DefaultReconcilationInterval  = 10 * time.Second
	DefaultContainerUnitDirectory = "/etc/containers/config"
)

// Daemon is the main syslet daemon.
type Daemon struct {
	clock                  quartz.Clock
	logger                 *slog.Logger
	systemd                *systemd.Client
	store                  *store.Store
	config                 *ConfigFileManager
	reconciler             *Reconciler
	containerUnitDirectory string
	reconcilationInterval  time.Duration
}

// Config holds daemon configuration.
type Config struct {
	Clock                  quartz.Clock
	ContainerUnitDirectory string
	ReconcilationInterval  time.Duration
}

// New creates a new daemon.
func New(fs afero.Fs, logger *slog.Logger, sd *systemd.Client, st *store.Store, dcfg Config) *Daemon {
	if dcfg.Clock == nil {
		dcfg.Clock = quartz.NewReal()
	}
	if dcfg.ContainerUnitDirectory == "" {
		dcfg.ContainerUnitDirectory = DefaultContainerUnitDirectory
	}
	if dcfg.ReconcilationInterval == 0 {
		dcfg.ReconcilationInterval = DefaultReconcilationInterval
	}
	cfg := NewConfigFileManager(fs)
	return &Daemon{
		clock:                  dcfg.Clock,
		logger:                 logger,
		store:                  st,
		config:                 cfg,
		systemd:                sd,
		reconciler:             NewReconciler(sd, cfg, logger),
		containerUnitDirectory: dcfg.ContainerUnitDirectory,
		reconcilationInterval:  dcfg.ReconcilationInterval,
	}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("starting syslet daemon",
		"interval", d.reconcilationInterval,
	)

	// Run immediately on start
	d.reconcileOnce(ctx)

	ticker := d.clock.NewTicker(d.reconcilationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("shutting down")
			return ctx.Err()
		case <-ticker.C:
			d.reconcileOnce(ctx)
		}
	}
}

func (d *Daemon) reconcileOnce(ctx context.Context) []UnitResult {
	d.logger.Debug("starting reconciliation cycle")

	units, err := d.loadUnitsFromStore()
	if err != nil {
		d.logger.Error("failed to load specs from store", "error", err)
		return nil
	}

	if len(units) == 0 {
		d.logger.Debug("no managed units found")
		return nil
	}

	// Phase 1: Diff
	plan, err := d.reconciler.Diff(ctx, units)
	if err != nil {
		d.logger.Error("diff failed", "error", err)
		return nil
	}

	// Phase 2: Execute
	results := d.reconciler.Execute(ctx, plan)

	now := d.clock.Now()
	for _, r := range results {
		if r.Error {
			d.logger.Error("reconciliation error", "unit", r.FullName, "message", r.Message)
		} else if r.Changed {
			d.logger.Info("reconciled", "unit", r.FullName, "message", r.Message)
		} else {
			d.logger.Debug("no changes", "unit", r.FullName)
		}

		errMsg := ""
		if r.Error {
			errMsg = r.Message
		}
		if err := d.store.UpdateReconcileStatus(pb.UnitName(r.FullName), r.Type.ShortName(), errMsg, now); err != nil {
			d.logger.Error("failed to update reconcile status", "unit", r.FullName, "error", err)
		}
	}

	return results
}

// ApplySpecs stores specs in SQLite.
func (d *Daemon) ApplySpecs(ctx context.Context, specs []*pb.UnitSpec) error {
	for _, spec := range specs {
		if err := d.store.Put(spec); err != nil {
			d.logger.Error("failed to store spec", "name", spec.Name, "error", err)
			return err
		}
	}
	return nil
}

// ListUnits returns the status of all managed units, optionally filtered by type.
func (d *Daemon) ListUnits(ctx context.Context, typeFilter pb.UnitType) ([]*pb.UnitStatus, error) {
	loaded, err := d.loadUnitsFromStore()
	if err != nil {
		return nil, err
	}

	var units []*pb.UnitStatus
	for _, ru := range loaded {
		if typeFilter != pb.UnitType_UNIT_TYPE_UNSPECIFIED && ru.Spec.Type != typeFilter {
			continue
		}

		us, err := d.buildUnitStatus(ctx, &ru)
		if err != nil {
			d.logger.Error("status error", "unit", ru.FullName(), "error", err)
			continue
		}
		units = append(units, us)
	}

	return units, nil
}

// GetStatus returns the status of a single unit.
func (d *Daemon) GetStatus(ctx context.Context, fullUnitName string) (*pb.UnitStatus, error) {
	unitType := pb.UnitTypeFromExtension(fullUnitName)
	if unitType == pb.UnitType_UNIT_TYPE_UNSPECIFIED {
		return nil, fmt.Errorf("unknown unit type for %s", fullUnitName)
	}

	ru, err := d.loadUnitFromStore(pb.UnitName(fullUnitName), unitType)
	if err != nil {
		return nil, err
	}

	return d.buildUnitStatus(ctx, ru)
}

// DeleteUnit deletes a unit by name.
func (d *Daemon) DeleteUnit(ctx context.Context, unitName string) error {
	if unitName == "" {
		return fmt.Errorf("unit_name is required")
	}

	// unit_name can be "webapp.container" or just "webapp" — try to resolve
	fullUnitName := unitName
	unitType := pb.UnitTypeFromExtension(fullUnitName)

	if unitType == pb.UnitType_UNIT_TYPE_UNSPECIFIED {
		// Input is a bare unit name (e.g. "webapp"); find its type in the store
		baseName := fullUnitName
		for _, t := range []string{"container", "volume", "network"} {
			_, err := d.store.Get(baseName, t)
			if err == nil {
				unitType = pb.UnitTypeFromExtension(baseName + "." + t)
				fullUnitName = baseName + "." + t
				// Delete from store
				if err := d.store.Delete(baseName, t); err != nil {
					d.logger.Warn("store delete failed", "name", baseName, "error", err)
				}
				break
			}
		}
		if unitType == pb.UnitType_UNIT_TYPE_UNSPECIFIED {
			return fmt.Errorf("unit %q not found", unitName)
		}
	} else {
		baseName := pb.UnitName(fullUnitName)
		typeName := unitType.ShortName()
		if err := d.store.Delete(baseName, typeName); err != nil {
			d.logger.Warn("store delete failed", "name", baseName, "error", err)
		}
	}

	// Stop the unit first
	if unitType.IsStartable() {
		if err := d.systemd.Container().Stop(ctx, fullUnitName); err != nil {
			d.logger.Warn("stop failed during delete", "unit", fullUnitName, "error", err)
		}
	}

	// Remove unit file from systemd
	if err := d.removeUnitFile(fullUnitName, unitType); err != nil {
		return fmt.Errorf("removing unit file: %w", err)
	}

	// Remove config files
	d.config.RemoveAll(fullUnitName)

	// Daemon reload
	if err := d.systemd.DaemonReload(ctx); err != nil {
		d.logger.Warn("daemon-reload failed during delete", "error", err)
	}

	return nil
}

// unitRuntimeState returns systemd runtime state for the unit.
func (d *Daemon) unitRuntimeState(ctx context.Context, fullUnitName string, unitType pb.UnitType) (*systemd.UnitState, error) {
	switch unitType {
	case pb.UnitType_UNIT_TYPE_CONTAINER:
		return d.systemd.Container().RuntimeState(ctx, fullUnitName)
	case pb.UnitType_UNIT_TYPE_VOLUME:
		return d.systemd.Volume().RuntimeState(ctx, fullUnitName)
	case pb.UnitType_UNIT_TYPE_NETWORK:
		return d.systemd.Network().RuntimeState(ctx, fullUnitName)
	default:
		return nil, fmt.Errorf("unsupported unit type: %s", unitType)
	}
}

func (d *Daemon) removeUnitFile(fullUnitName string, unitType pb.UnitType) error {
	switch unitType {
	case pb.UnitType_UNIT_TYPE_CONTAINER:
		return d.systemd.Container().RemoveUnitFile(fullUnitName)
	case pb.UnitType_UNIT_TYPE_VOLUME:
		return d.systemd.Volume().RemoveUnitFile(fullUnitName)
	case pb.UnitType_UNIT_TYPE_NETWORK:
		return d.systemd.Network().RemoveUnitFile(fullUnitName)
	default:
		return fmt.Errorf("unsupported unit type: %s", unitType)
	}
}

func (d *Daemon) loadUnitFromStore(unitName string, unitType pb.UnitType) (*ResolvedUnit, error) {
	spec, err := d.store.Get(unitName, unitType.ShortName())
	if err != nil {
		return nil, err
	}
	return resolveSpec(spec, d.containerUnitDirectory)
}

func (d *Daemon) loadUnitsFromStore() ([]ResolvedUnit, error) {
	specs, err := d.store.List()
	if err != nil {
		return nil, err
	}

	var units []ResolvedUnit
	for _, spec := range specs {
		ru, err := resolveSpec(spec, d.containerUnitDirectory)
		if err != nil {
			return nil, err
		}
		units = append(units, *ru)
	}
	return units, nil
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

func (d *Daemon) buildUnitStatus(ctx context.Context, ru *ResolvedUnit) (*pb.UnitStatus, error) {
	state, err := d.unitRuntimeState(ctx, ru.FullName(), ru.Spec.Type)
	if err != nil {
		return nil, err
	}

	us := &pb.UnitStatus{
		Name:         ru.FullName(),
		Type:         ru.Spec.Type,
		DesiredState: ru.Spec.DesiredState,
		ActiveState:  pbActiveState(state.ActiveState),
		Enabled:      state.Enabled,
		ConfigFiles:  configFilenames(ru.Configs),
	}

	unitName := pb.UnitName(ru.FullName())
	if rs, err := d.store.GetReconcileStatus(unitName, ru.Spec.Type.ShortName()); err == nil {
		if !rs.LastReconciled.IsZero() {
			us.LastReconciled = rs.LastReconciled.Format(time.RFC3339)
		}
		us.Error = rs.Error
	}

	return us, nil
}

// resolveSpec converts a pb.UnitSpec into a ResolvedUnit.
// configBase is the host directory where config files are stored (e.g. /etc/containers/config).
func resolveSpec(spec *pb.UnitSpec, configBase string) (*ResolvedUnit, error) {
	if spec.Type == pb.UnitType_UNIT_TYPE_UNSPECIFIED {
		return nil, fmt.Errorf("unspecified unit type for %q", spec.Name)
	}

	// Map pb options to go-systemd UnitOptions.
	var opts []*unit.UnitOption
	for _, o := range spec.Options {
		opts = append(opts, &unit.UnitOption{
			Section: o.Section,
			Name:    o.Name,
			Value:   o.Value,
		})
	}

	// Process configs: generate Volume= entries and ConfigFile list.
	var cfgFiles []ConfigFile
	seen := make(map[string]bool)

	for _, ce := range spec.Configs {
		basename := filepath.Base(ce.TargetVolumePath)
		if seen[basename] {
			return nil, fmt.Errorf("duplicate config basename %q (from targetVolumePath %q)", basename, ce.TargetVolumePath)
		}
		seen[basename] = true

		hostPath := filepath.Join(configBase, spec.Name, basename)
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

	// Auto-generate [Install] section for startable units.
	if spec.Type.IsStartable() {
		opts = append(opts, &unit.UnitOption{
			Section: "Install",
			Name:    "WantedBy",
			Value:   "multi-user.target default.target",
		})
	}

	return &ResolvedUnit{
		Spec:    spec,
		Options: opts,
		Configs: cfgFiles,
	}, nil
}

func configFilenames(configs []ConfigFile) []string {
	names := make([]string, len(configs))
	for i, c := range configs {
		names[i] = c.Filename
	}
	return names
}
