//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/xchangeee/syslet/test/integration/systest"
)

// These tests cover syslet.DisplayReport, the summary an apply prints to
// stdout. They run a real apply rather than only building a plan, because the
// report differs from the plan: a unit whose operation failed turns into an
// error row.

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

	report, err := env.ApplyWithReport()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	systest.AssertReportOutput(t, report,
		"webapp.container                         updated    unit updated, restarted (desired: running)\n")
}

// TestApplyWithOnlyUnchangedUnits_PrintsNoChanges verifies that a no-op apply
// still says so, rather than printing nothing.
func TestApplyWithOnlyUnchangedUnits_PrintsNoChanges(t *testing.T) {
	spec := systest.NewContainer("webapp", "nginx:latest")

	env := systest.New(t)
	env.SeedActive(spec)
	env.Specs(spec)

	report, err := env.ApplyWithReport()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	systest.AssertReportOutput(t, report, noChangesLine+"\n")
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

	report, err := env.ApplyWithReport()
	if err == nil {
		t.Fatal("expected apply to fail when a unit could not be started")
	}

	systest.AssertReportOutput(t, report,
		"webapp.container                         error      starting: starting webapp.service: unit webapp.service failed to start\n")
}
