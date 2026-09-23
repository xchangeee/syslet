package syslet

import (
	"fmt"
	"log/slog"

	"github.com/xchangeee/syslet/internal/model"
)

// applyRunner groups the logger and plan so Apply loops can call exec/execUnit
// without repeating the info/error log pair and the optional RecordError call.
type applyRunner struct {
	logger *slog.Logger
	plan   *ApplyPlan
}

// exec logs action, calls fn, and on error logs "<action> failed" with attrs plus "error".
func (r *applyRunner) exec(action string, fn func() error, attrs ...any) {
	r.logger.Info(action, attrs...)
	if err := fn(); err != nil {
		r.logger.Error(action+" failed", append(attrs, "error", err)...)
	}
}

// execUnit is like exec but also calls plan.RecordError on failure.
func (r *applyRunner) execUnit(action string, unit model.FullUnitName, fn func() error, attrs ...any) {
	r.logger.Info(action, attrs...)
	if err := fn(); err != nil {
		r.logger.Error(action+" failed", append(attrs, "error", err)...)
		r.plan.RecordError(unit, fmt.Sprintf("%s: %v", action, err))
	}
}
