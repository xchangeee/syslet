//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"
)

func buildTestPlan(t *testing.T, ctx context.Context, fs afero.Fs, sd *systemd.Client, raw api.LoadResult) (*syslet.ApplyPlan, error) {
	t.Helper()
	mgrs := newTestFileManagers(fs)
	return syslet.BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, raw)
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

func TestNewVolume_WritesUnitOnly(t *testing.T) {
	specs := []model.Unit{makeVolumeSpec("data", "tmpfs")}
	ctx, fs, sd, mockConn, mockPodman, raw := setupTest(t, testFixture{specs: specs})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertUnitExists(t, sd, "data.volume")
	testutil.AssertReloaded(t, mockConn)
	testutil.AssertNoStartStop(t, mockConn)
}

// TestVolumeMetadataOnlyChange covers a volume spec whose [X-Syslet] metadata
// changed while Device= — the part podman actually materializes — did not. The
// distinction is what keeps a reclaim-policy edit from being destructive: the
// unit file is rewritten, but the backing podman volume survives and containers
// mounting it keep running. Both halves are asserted here because either one
// failing alone would still lose data or availability.
func TestVolumeMetadataOnlyChange(t *testing.T) {
	// removalAllowed flips from false to true — metadata-only, same Device=.
	t.Run("WritesUnit", func(t *testing.T) {
		newVolumeSpec := makeStaleVolumeSpec("data", "tmpfs")
		oldVolumeSpec := makeVolumeSpec("data", "tmpfs")

		fs := afero.NewMemMapFs()

		ctx, sd, _, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
			specs:         []model.Unit{newVolumeSpec},
			existingUnits: map[string]string{"data.volume": renderVolume(t, oldVolumeSpec)},
		})

		mustApply(t, ctx, fs, sd, mockPodman, raw)

		mockPc := mockPodman.(*mockPodmanClient)
		if len(mockPc.deletedVolumes) != 0 {
			t.Errorf("expected no deleted volumes, got: %v", mockPc.deletedVolumes)
		}
		testutil.AssertUnitExists(t, sd, "data.volume")
	})

	t.Run("DoesNotRestartContainers", func(t *testing.T) {
		newVolumeSpec := makeStaleVolumeSpec("data", "tmpfs")
		oldVolumeSpec := makeVolumeSpec("data", "tmpfs")
		webappSpec := makeContainerSpecWithVolume("webapp", model.DesiredStateRunning, "data", "/data")
		fs := afero.NewMemMapFs()

		ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
			specs: []model.Unit{newVolumeSpec, webappSpec},
			existingUnits: map[string]string{
				"data.volume":      renderVolume(t, oldVolumeSpec),
				"webapp.container": renderContainer(t, fs, webappSpec),
			},
			existingState: map[string]string{"webapp.service": "active"},
		})

		mustApply(t, ctx, fs, sd, mockPodman, raw)

		testutil.AssertNoStartStop(t, mockConn)
	})
}

func TestVolumeMeaningfulChangeWithDeletePolicy_DeletesAndRecreatesUnit(t *testing.T) {
	newVolumeSpec := makeVolumeSpecWithDelete("data", "tmpfs")
	oldVolumeSpec := makeVolumeSpecWithDelete("data", "old-device") // existing unit must carry the flags

	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{newVolumeSpec},
		existingUnits: map[string]string{"data.volume": renderVolume(t, oldVolumeSpec)},
		existingState: map[string]string{"data-volume.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 1 || mockPc.deletedVolumes[0] != "data" {
		t.Errorf("expected DeleteVolume('data'), got: %v", mockPc.deletedVolumes)
	}
	testutil.AssertUnitExists(t, sd, "data.volume")
}

func TestVolumeMeaningfulChangeWithoutDeletePolicy_Errors(t *testing.T) {
	newVolumeSpec := makeVolumeSpec("data", "tmpfs") // no delete policy
	oldVolumeSpec := makeVolumeSpec("data", "old-device")

	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{newVolumeSpec},
		existingUnits: map[string]string{"data.volume": renderVolume(t, oldVolumeSpec)},
	})

	plan, err := buildTestPlan(t, ctx, fs, sd, raw)
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

// TestVolumeChanged_StagesAllUnitTypes verifies that all rendered unit types
// (container, volume, network, build) appear in the staging directory even when only
// the volume has changed. The quadlet generator needs the full picture to validate correctly.
func TestVolumeChanged_StagesAllUnitTypes(t *testing.T) {
	containerSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	oldVolumeSpec := makeVolumeSpec("data", "tmpfs")      // removalAllowed=false
	newVolumeSpec := makeStaleVolumeSpec("data", "tmpfs") // removalAllowed=true — metadata-only change, no recreation
	netSpec := makeNetworkSpec("frontend", "bridge")
	buildSpec := makeBuildSpec("myapp", "localhost/myapp:latest")

	fs := afero.NewMemMapFs()
	ctx, sd, _, _, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{containerSpec, newVolumeSpec, netSpec, buildSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, containerSpec),
			"data.volume":      renderVolume(t, oldVolumeSpec),
			"frontend.network": renderNetwork(t, netSpec),
			"myapp.build":      renderBuild(t, fs, buildSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	gen := &recordingQuadletGenerator{fs: fs}
	mgrs := newTestFileManagers(fs)
	if _, err := syslet.BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, gen, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, raw); err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	staged := make(map[string]bool, len(gen.stagedFiles))
	for _, f := range gen.stagedFiles {
		staged[f] = true
	}
	for _, want := range []string{"webapp.container", "data.volume", "frontend.network", "myapp.build"} {
		if !staged[want] {
			t.Errorf("expected %s to be staged", want)
		}
	}
}

func TestStaleVolume_RemovesUnit(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	staleVolumeSpec := makeStaleVolumeSpec("olddata", "tmpfs")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"olddata.volume":   renderVolume(t, staleVolumeSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertUnitAbsent(t, sd, "olddata.volume")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestStaleVolumeWithDeletePolicy_DeletesPodmanVolume(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	staleVolumeSpec := makeStaleVolumeSpecWithDelete("olddata", "tmpfs")
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, raw := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"olddata.volume":   renderVolume(t, staleVolumeSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, raw)

	testutil.AssertUnitAbsent(t, sd, "olddata.volume")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedVolumes) != 1 || mockPc.deletedVolumes[0] != "olddata" {
		t.Errorf("expected DeleteVolume('olddata'), got: %v", mockPc.deletedVolumes)
	}
}
