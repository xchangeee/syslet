//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/test/integration/systest"
)

// These tests cover syslet.DisplayResults, the summary an apply prints to
// stdout. They run a real apply rather than only building a plan, because the
// apply rewrites the summary: a unit whose operation failed turns into an error
// row, and that must suppress the "No changes detected" line.

const noChangesLine = "No changes detected. All units are up to date."

// TestApplyWithUnchangedUnits_OmitsThemFromResults verifies that the post-apply
// summary, like the diff, lists only units that changed.
func TestApplyWithUnchangedUnits_OmitsThemFromResults(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.SeedActive(systest.NewContainer("worker", "busybox:latest"))
	env.Specs(
		systest.NewContainer("webapp", "nginx:alpine"),
		systest.NewContainer("worker", "busybox:latest"),
	)

	plan, err := env.ApplyWithPlan()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	systest.AssertResultsOutput(t, plan,
		"webapp.container                         updated    unit updated, restarted (desired: running)\n")
}

// TestApplyWithOnlyUnchangedUnits_PrintsNoChanges verifies that a no-op apply
// still says so, rather than printing nothing.
func TestApplyWithOnlyUnchangedUnits_PrintsNoChanges(t *testing.T) {
	spec := systest.NewContainer("webapp", "nginx:latest")

	env := systest.New(t)
	env.SeedActive(spec)
	env.Specs(spec)

	plan, err := env.ApplyWithPlan()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	systest.AssertResultsOutput(t, plan, noChangesLine+"\n")
}

// TestContainerStartFails_PrintsErrorRow verifies that a unit whose operation
// failed during the apply is reported with the error, not with the status the
// plan gave it.
func TestContainerStartFails_PrintsErrorRow(t *testing.T) {
	env := systest.New(t)
	env.SeedActive(systest.NewContainer("worker", "busybox:latest"))
	env.Specs(
		systest.NewContainer("webapp", "nginx:latest"),
		systest.NewContainer("worker", "busybox:latest"),
	)
	env.Conn.StartErr = fmt.Errorf("unit webapp.service failed to start")

	plan, err := env.ApplyWithPlan()
	if err == nil {
		t.Fatal("expected apply to fail when a unit could not be started")
	}

	systest.AssertResultsOutput(t, plan,
		"webapp.container                         error      starting: starting webapp.service: unit webapp.service failed to start\n")
}

// TestApplyWithPlanErrorsAndUnchangedUnits_DoesNotPrintNoChanges verifies that
// a refused apply, which has no operations to run, doesn't also claim that all
// units are up to date.
func TestApplyWithPlanErrorsAndUnchangedUnits_DoesNotPrintNoChanges(t *testing.T) {
	// X-Syslet section is reserved; NoXSysletSection in ValidateUnits rejects this.
	badSpec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV("nginx:latest")},
			"X-Syslet":  {model.SectionKey("SomeKey"): model.UV("value")},
		},
		model.DesiredStateRunning, nil, false,
	)
	worker := systest.NewContainer("worker", "busybox:latest")

	env := systest.New(t)
	env.SeedActive(worker)
	env.Specs(badSpec, worker)

	plan, err := env.ApplyWithPlan()
	if err == nil {
		t.Fatal("expected apply to refuse a plan with errors")
	}

	systest.AssertResultsOutputOmits(t, plan, noChangesLine)
}
