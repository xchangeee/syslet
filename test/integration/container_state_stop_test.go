//go:build integration

package integration

import (
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

func TestApply_Container_NewWithStoppedState_WritesUnitOnly(t *testing.T) {
	specs := []model.Unit{makeContainerSpec("webapp", "nginx:latest", model.DesiredStateStopped)}
	ctx, fs, sd, mockConn, mockPodman, raw := setupTest(t, testFixture{specs: specs})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertUnitExists(t, sd, "webapp.container")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Container_DesiredStateStopped_StopsService(t *testing.T) {
	spec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateStopped)
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertStopped(t, mockConn, "webapp.service")
	testutil.AssertNoneStarted(t, mockConn)
}

func TestApply_Container_Stale_StopsAndRemovesUnit(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	oldSpec := makeStaleContainerSpec("old", "redis:latest")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"old.container":    renderContainer(t, fs, oldSpec),
		},
		existingState: map[string]string{
			"webapp.service": "active",
			"old.service":    "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertStopped(t, mockConn, "old.service")
	testutil.AssertUnitAbsent(t, sd, "old.container")
	testutil.AssertNoneStarted(t, mockConn)
}
