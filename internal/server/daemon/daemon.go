package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/server/parser"
	"codeberg.org/xchangeee/syslet/internal/server/spec"
	"codeberg.org/xchangeee/syslet/internal/server/store"
	"codeberg.org/xchangeee/syslet/internal/server/systemd"
	"github.com/coder/quartz"
)

const (
	DefaultInterval   = 10 * time.Second
	DefaultConfigBase = "/etc/containers/config"
)

// Daemon is the main syslet daemon.
type Daemon struct {
	reconciler *Reconciler
	store      *store.Store
	systemd    *systemd.Client
	config     *containerconfig.Manager
	configBase string
	interval   time.Duration
	clock      quartz.Clock
	logger     *slog.Logger
}

// Config holds daemon configuration.
type Config struct {
	ConfigBase string
	Interval   time.Duration
	Clock      quartz.Clock
}

// New creates a new daemon.
func New(sd *systemd.Client, cfg *containerconfig.Manager, st *store.Store, logger *slog.Logger, dcfg Config) *Daemon {
	if dcfg.ConfigBase == "" {
		dcfg.ConfigBase = DefaultConfigBase
	}
	if dcfg.Interval == 0 {
		dcfg.Interval = DefaultInterval
	}
	if dcfg.Clock == nil {
		dcfg.Clock = quartz.NewReal()
	}

	return &Daemon{
		reconciler: NewReconciler(sd, cfg, logger),
		store:      st,
		systemd:    sd,
		config:     cfg,
		configBase: dcfg.ConfigBase,
		interval:   dcfg.Interval,
		clock:      dcfg.Clock,
		logger:     logger,
	}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("starting syslet daemon",
		"interval", d.interval,
	)

	// Run immediately on start
	d.reconcileOnce(ctx)

	ticker := d.clock.NewTicker(d.interval)
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
		if err := d.store.UpdateReconcileStatus(parser.UnitName(r.FullName), r.Type.String(), errMsg, now); err != nil {
			d.logger.Error("failed to update reconcile status", "unit", r.FullName, "error", err)
		}
	}

	return results
}

// ApplySpecs stores specs in SQLite.
func (d *Daemon) ApplySpecs(ctx context.Context, specJSONs []string) error {
	for _, j := range specJSONs {
		var cs spec.ContainerSpec
		if err := json.Unmarshal([]byte(j), &cs); err != nil {
			d.logger.Error("failed to unmarshal spec", "error", err)
			return err
		}
		if err := d.store.Put(cs.Name, cs.Type, j); err != nil {
			d.logger.Error("failed to store spec", "name", cs.Name, "error", err)
			return err
		}
	}
	return nil
}

// GetStatus returns the status of a single unit.
func (d *Daemon) GetStatus(ctx context.Context, fullUnitName string) (*UnitStatus, error) {
	unitType := parser.UnitTypeFromExtension(fullUnitName)
	if unitType == parser.UnitTypeUnknown {
		return nil, fmt.Errorf("unknown unit type for %s", fullUnitName)
	}

	state, err := d.unitRuntimeState(ctx, fullUnitName, unitType)
	if err != nil {
		return nil, err
	}

	// Look up desired state from store
	unitName := parser.UnitName(fullUnitName)
	var desiredState string
	specJSON, err := d.store.Get(unitName, unitType.String())
	if err == nil {
		var cs spec.ContainerSpec
		if err := json.Unmarshal([]byte(specJSON), &cs); err == nil {
			desiredState = cs.DesiredState
		}
	}

	cfgFiles, _ := d.config.ListFiles(fullUnitName)
	us := &UnitStatus{
		Name:         fullUnitName,
		Type:         unitType,
		DesiredState: desiredState,
		ActiveState:  state.ActiveState,
		Enabled:      state.Enabled,
		ConfigFiles:  cfgFiles,
	}

	if rs, err := d.store.GetReconcileStatus(unitName, unitType.String()); err == nil {
		us.LastReconciled = rs.LastReconciled
		us.Error = rs.Error
	}

	return us, nil
}

// ListUnits returns the status of all managed units, optionally filtered by type.
func (d *Daemon) ListUnits(ctx context.Context, typeFilter parser.UnitType) ([]UnitStatus, error) {
	loaded, err := d.loadUnitsFromStore()
	if err != nil {
		return nil, err
	}

	var units []UnitStatus
	for _, uc := range loaded {
		pu := uc.Unit
		if typeFilter != parser.UnitTypeUnknown && pu.Type != typeFilter {
			continue
		}

		state, err := d.unitRuntimeState(ctx, pu.FullName, pu.Type)
		if err != nil {
			d.logger.Error("status error", "unit", pu.FullName, "error", err)
			continue
		}

		cfgFiles, _ := d.config.ListFiles(pu.FullName)

		us := UnitStatus{
			Name:         pu.FullName,
			Type:         pu.Type,
			DesiredState: desiredStateString(pu.DesiredState),
			ActiveState:  state.ActiveState,
			Enabled:      state.Enabled,
			ConfigFiles:  cfgFiles,
		}

		unitName := parser.UnitName(pu.FullName)
		if rs, err := d.store.GetReconcileStatus(unitName, pu.Type.String()); err == nil {
			us.LastReconciled = rs.LastReconciled
			us.Error = rs.Error
		}

		units = append(units, us)
	}

	return units, nil
}

// DeleteUnit deletes a unit by name.
func (d *Daemon) DeleteUnit(ctx context.Context, unitName string) error {
	if unitName == "" {
		return fmt.Errorf("unit_name is required")
	}

	// unit_name can be "webapp.container" or just "webapp" — try to resolve
	fullUnitName := unitName
	unitType := parser.UnitTypeFromExtension(fullUnitName)

	if unitType == parser.UnitTypeUnknown {
		// Input is a bare unit name (e.g. "webapp"); find its type in the store
		baseName := fullUnitName
		for _, t := range []string{"container", "volume", "network"} {
			_, err := d.store.Get(baseName, t)
			if err == nil {
				unitType = parser.UnitTypeFromExtension(baseName + "." + t)
				fullUnitName = baseName + "." + t
				// Delete from store
				if err := d.store.Delete(baseName, t); err != nil {
					d.logger.Warn("store delete failed", "name", baseName, "error", err)
				}
				break
			}
		}
		if unitType == parser.UnitTypeUnknown {
			return fmt.Errorf("unit %q not found", unitName)
		}
	} else {
		baseName := parser.UnitName(fullUnitName)
		typeName := unitType.String()
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

// Returns systemd runtime state for the unit
func (d *Daemon) unitRuntimeState(ctx context.Context, fullUnitName string, unitType parser.UnitType) (*systemd.UnitState, error) {
	switch unitType {
	case parser.UnitTypeContainer:
		return d.systemd.Container().RuntimeState(ctx, fullUnitName)
	case parser.UnitTypeVolume:
		return d.systemd.Volume().RuntimeState(ctx, fullUnitName)
	case parser.UnitTypeNetwork:
		return d.systemd.Network().RuntimeState(ctx, fullUnitName)
	default:
		return nil, fmt.Errorf("unsupported unit type: %s", unitType)
	}
}

func (d *Daemon) removeUnitFile(fullUnitName string, unitType parser.UnitType) error {
	switch unitType {
	case parser.UnitTypeContainer:
		return d.systemd.Container().RemoveUnitFile(fullUnitName)
	case parser.UnitTypeVolume:
		return d.systemd.Volume().RemoveUnitFile(fullUnitName)
	case parser.UnitTypeNetwork:
		return d.systemd.Network().RemoveUnitFile(fullUnitName)
	default:
		return fmt.Errorf("unsupported unit type: %s", unitType)
	}
}

func desiredStateString(ds parser.DesiredState) string {
	switch ds {
	case parser.DesiredStateRunning:
		return "running"
	case parser.DesiredStateStopped:
		return "stopped"
	default:
		return ""
	}
}

func (d *Daemon) loadUnitsFromStore() ([]UnitWithConfigs, error) {
	jsons, err := d.store.List()
	if err != nil {
		return nil, err
	}

	var units []UnitWithConfigs
	for _, j := range jsons {
		var cs spec.ContainerSpec
		if err := json.Unmarshal([]byte(j), &cs); err != nil {
			return nil, err
		}
		pu, cfgFiles, err := spec.Convert(&cs, d.configBase)
		if err != nil {
			return nil, err
		}
		units = append(units, UnitWithConfigs{Unit: pu, Configs: cfgFiles})
	}
	return units, nil
}
