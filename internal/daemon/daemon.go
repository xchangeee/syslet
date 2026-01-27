package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"codeberg.org/xchangeee/rsystemd/internal/config"
	"codeberg.org/xchangeee/rsystemd/internal/systemd"
)

const (
	DefaultUnitsDir   = "/etc/rsystemd/units"
	DefaultConfigsDir = "/etc/containers/config"
	DefaultInterval   = 10 * time.Second
)

// Daemon is the main rsystemd daemon.
type Daemon struct {
	reconciler *Reconciler
	unitsDir   string
	configsDir string
	interval   time.Duration
	logger     *slog.Logger
}

// Config holds daemon configuration.
type Config struct {
	UnitsDir   string
	ConfigsDir string
	Interval   time.Duration
}

// New creates a new daemon.
func New(sd *systemd.Client, cfg *config.Manager, logger *slog.Logger, dcfg Config) *Daemon {
	if dcfg.UnitsDir == "" {
		dcfg.UnitsDir = DefaultUnitsDir
	}
	if dcfg.ConfigsDir == "" {
		dcfg.ConfigsDir = DefaultConfigsDir
	}
	if dcfg.Interval == 0 {
		dcfg.Interval = DefaultInterval
	}

	return &Daemon{
		reconciler: NewReconciler(sd, cfg, logger),
		unitsDir:   dcfg.UnitsDir,
		configsDir: dcfg.ConfigsDir,
		interval:   dcfg.Interval,
		logger:     logger,
	}
}

// Run starts the reconciliation loop. It blocks until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("starting rsystemd daemon",
		"units_dir", d.unitsDir,
		"configs_dir", d.configsDir,
		"interval", d.interval,
	)

	// Run immediately on start
	d.reconcileOnce(ctx)

	ticker := time.NewTicker(d.interval)
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

// ReconcileOnce runs a single reconciliation cycle. Exported for API use.
func (d *Daemon) ReconcileOnce(ctx context.Context) []UnitResult {
	return d.reconcileOnce(ctx)
}

func (d *Daemon) reconcileOnce(ctx context.Context) []UnitResult {
	d.logger.Debug("starting reconciliation cycle")

	// Load unit files
	units, err := LoadUnitsFromDir(d.unitsDir)
	if err != nil {
		d.logger.Error("failed to load units", "error", err)
		return nil
	}

	// Load config files from container config dir
	configs, err := LoadConfigsFromDir(d.configsDir)
	if err != nil {
		d.logger.Error("failed to load configs", "error", err)
		return nil
	}

	if len(units) == 0 {
		d.logger.Debug("no managed units found")
		return nil
	}

	// Phase 1: Diff
	plan, err := d.reconciler.Diff(ctx, units, configs)
	if err != nil {
		d.logger.Error("diff failed", "error", err)
		return nil
	}

	// Phase 2: Execute
	results := d.reconciler.Execute(ctx, plan)

	for _, r := range results {
		if r.Error {
			d.logger.Error("reconciliation error", "unit", r.Name, "message", r.Message)
		} else if r.Changed {
			d.logger.Info("reconciled", "unit", r.Name, "message", r.Message)
		} else {
			d.logger.Debug("no changes", "unit", r.Name)
		}
	}

	return results
}

// ApplyUnitsAndConfigs applies a set of units and configs by writing them to
// the managed directories and triggering reconciliation.
func (d *Daemon) ApplyUnitsAndConfigs(ctx context.Context, rawUnits map[string]string, cfgFiles []config.ConfigFile) []UnitResult {
	// Write unit files to units dir
	for name, content := range rawUnits {
		path := filepath.Join(d.unitsDir, name)
		if err := writeFileAtomic(path, []byte(content)); err != nil {
			d.logger.Error("failed to write unit", "unit", name, "error", err)
			return []UnitResult{{Name: name, Error: true, Message: err.Error()}}
		}
	}

	// Write config files to configs dir
	for _, cf := range cfgFiles {
		dir := filepath.Join(d.configsDir, cf.UnitName)
		path := filepath.Join(dir, cf.Filename)
		if err := writeFileAtomic(path, []byte(cf.Content)); err != nil {
			d.logger.Error("failed to write config", "unit", cf.UnitName, "file", cf.Filename, "error", err)
		}
	}

	// Trigger reconciliation
	return d.reconcileOnce(ctx)
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := mkdirAll(dir); err != nil {
		return err
	}
	return writeFile(path, data, 0644)
}
