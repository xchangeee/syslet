package syslet

import (
	"context"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"
	"github.com/spf13/afero"
)

func buildTestPlan(t *testing.T, ctx context.Context, fs afero.Fs, sd *systemd.Client, zipPath string) (*ApplyPlan, error) {
	t.Helper()
	mgrs := newTestFileManagers(fs)
	return BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath)
}

func makeVolumeSpecWithDelete(name, device string) *model.VolumeUnit {
	return model.NewVolumeUnit(
		model.VolumeUnitRef(name),
		model.UnitOptions{
			"Volume": {model.SectionKey("Device"): model.UV(device)},
		},
		true, model.ReclaimPolicyDelete,
	)
}

func makeContainerSpecWithVolume(name string, state model.DesiredState, volumeName, containerPath string) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Volume"): model.UV(volumeName + ".volume:" + containerPath),
			},
		},
		state, nil, false,
	)
}

func TestApply_Volume_New_WritesUnitOnly(t *testing.T) {
	specs := []model.Unit{makeVolumeSpec("data", "tmpfs")}
	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitExists(t, sd, "data.volume")
	testutil.AssertReloaded(t, mockConn)
	testutil.AssertNoStartStop(t, mockConn)
}

func TestApply_Volume_MetadataOnlyChange_WritesUnit(t *testing.T) {
	// removalAllowed flips from false to true — metadata-only, same Device=.
	newVolumeSpec := makeStaleVolumeSpec("data", "tmpfs")
	oldVolumeSpec := makeVolumeSpec("data", "tmpfs")

	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{newVolumeSpec},
		existingUnits: map[string]string{"data.volume": renderVolume(t, oldVolumeSpec)},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 0 {
		t.Errorf("expected no deleted volumes, got: %v", mockPc.deletedVolumes)
	}
	testutil.AssertUnitExists(t, sd, "data.volume")
}

func TestApply_Volume_MeaningfulChange_WithDeletePolicy_DeletesAndRecreatesUnit(t *testing.T) {
	newVolumeSpec := makeVolumeSpecWithDelete("data", "tmpfs")
	oldVolumeSpec := makeVolumeSpecWithDelete("data", "old-device") // existing unit must carry the flags

	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{newVolumeSpec},
		existingUnits: map[string]string{"data.volume": renderVolume(t, oldVolumeSpec)},
		existingState: map[string]string{"data-volume.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 1 || mockPc.deletedVolumes[0] != "data" {
		t.Errorf("expected DeleteVolume('data'), got: %v", mockPc.deletedVolumes)
	}
	testutil.AssertUnitExists(t, sd, "data.volume")
}

func TestApply_Volume_MeaningfulChange_WithoutDeletePolicy_Errors(t *testing.T) {
	newVolumeSpec := makeVolumeSpec("data", "tmpfs") // no delete policy
	oldVolumeSpec := makeVolumeSpec("data", "old-device")

	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{newVolumeSpec},
		existingUnits: map[string]string{"data.volume": renderVolume(t, oldVolumeSpec)},
	})

	plan, err := buildTestPlan(t, ctx, fs, sd, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	if !plan.HasErrors() {
		t.Error("expected plan to have errors for meaningful volume change without delete policy")
	}

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 0 {
		t.Errorf("expected no deleted volumes, got: %v", mockPc.deletedVolumes)
	}
}

func TestApply_Volume_Stale_RemovesUnit(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	staleVolumeSpec := makeStaleVolumeSpec("olddata", "tmpfs")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"olddata.volume":   renderVolume(t, staleVolumeSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "olddata.volume")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Volume_StaleWithDeletePolicy_DeletesPodmanVolume(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	staleVolumeSpec := makeStaleVolumeSpecWithDelete("olddata", "tmpfs")
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"olddata.volume":   renderVolume(t, staleVolumeSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "olddata.volume")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 1 || mockPc.deletedVolumes[0] != "olddata" {
		t.Errorf("expected DeleteVolume('olddata'), got: %v", mockPc.deletedVolumes)
	}
}
