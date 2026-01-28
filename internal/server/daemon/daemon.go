package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"codeberg.org/xchangeee/syslet/internal/server/containerconfig"
	"codeberg.org/xchangeee/syslet/internal/server/parser"
	"codeberg.org/xchangeee/syslet/internal/server/spec"
	"codeberg.org/xchangeee/syslet/internal/server/store"
	"codeberg.org/xchangeee/syslet/internal/server/systemd"
)

const (
	DefaultInterval   = 10 * time.Second
	DefaultConfigBase = "/etc/containers/config"
)

// Daemon is the main syslet daemon.
type Daemon struct {
	reconciler *Reconciler
	store      *store.Store
	configBase string
	interval   time.Duration
	logger     *slog.Logger
}

// Config holds daemon configuration.
type Config struct {
	ConfigBase string
	Interval   time.Duration
}

// New creates a new daemon.
func New(sd *systemd.Client, cfg *containerconfig.Manager, st *store.Store, logger *slog.Logger, dcfg Config) *Daemon {
	if dcfg.ConfigBase == "" {
		dcfg.ConfigBase = DefaultConfigBase
	}
	if dcfg.Interval == 0 {
		dcfg.Interval = DefaultInterval
	}

	return &Daemon{
		reconciler: NewReconciler(sd, cfg, logger),
		store:      st,
		configBase: dcfg.ConfigBase,
		interval:   dcfg.Interval,
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

	now := time.Now()
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

// Store returns the underlying store (for use by the API server).
func (d *Daemon) Store() *store.Store {
	return d.store
}

// ConfigBase returns the config base path.
func (d *Daemon) ConfigBase() string {
	return d.configBase
}

func (d *Daemon) loadUnitsFromStore() ([]UnitWithConfigs, error) {
	specJSONs, err := d.store.List()
	if err != nil {
		return nil, err
	}

	var units []UnitWithConfigs
	for _, j := range specJSONs {
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
