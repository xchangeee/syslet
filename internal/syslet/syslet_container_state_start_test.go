package syslet

import (
	"fmt"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

func TestApply_Container_NewWithRunningState_StartsService(t *testing.T) {
	specs := []model.Unit{makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)}
	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitExists(t, sd, "webapp.container")
	testutil.AssertStarted(t, mockConn, "webapp.service")
	testutil.AssertNoneStopped(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Container_QuadletError_DoesNotStart(t *testing.T) {
	specs := []model.Unit{makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)}

	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})
	mockConn.ReloadErr = fmt.Errorf("daemon-reload failed")
	jr := &systemd.MockJournalReader{
		Messages: []string{"webapp.container: Invalid key 'BadKey' in section Container"},
	}

	err := testApply(t, ctx, fs, sd, mockPodman, zipPath, jr)

	if err == nil {
		t.Error("expected Apply to return an error when daemon-reload fails")
	}
	testutil.AssertNoneStarted(t, mockConn)
}
