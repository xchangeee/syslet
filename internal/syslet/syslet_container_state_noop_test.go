package syslet

import (
	"os"
	"strings"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"

	"github.com/spf13/afero"
)

func TestApply_Container_Unchanged_NoAction(t *testing.T) {
	spec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertNotReloaded(t, mockConn)
}

// TestApply_Container_UnitReordered_NoAction verifies that pure reordering of multi-value
// entries like Network= is not semantically meaningful and must not cause downtime.
func TestApply_Container_UnitReordered_NoAction(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):   model.UV("nginx:latest"),
				model.SectionKey("Network"): multiUV(t, "backend.network", "frontend.network"),
			},
		},
		model.DesiredStateRunning, nil, false,
	)
	fs := afero.NewMemMapFs()
	canonical := renderContainer(t, fs, spec)

	// Swap the two Network= lines to simulate a file written in a different order.
	reordered := strings.Replace(
		strings.Replace(canonical, "Network=backend.network\n", "__PLACEHOLDER__\n", 1),
		"Network=frontend.network\n", "Network=backend.network\n", 1)
	reordered = strings.Replace(reordered, "__PLACEHOLDER__\n", "Network=frontend.network\n", 1)

	if reordered == canonical {
		t.Fatal("test setup error: reordered content is identical to canonical")
	}

	ctx, sd, _, _, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec, model.NewNetworkUnit(model.NetworkUnitRef("backend"), nil, false, ""), model.NewNetworkUnit(model.NetworkUnitRef("frontend"), nil, false, "")},
		existingUnits: map[string]string{"webapp.container": reordered},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mgrs := newTestFileManagers(fs)
	plan, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	if len(plan.StopSystemdServices) != 0 {
		t.Errorf("reordering should not stop the container, got StopContainers: %v", plan.StopSystemdServices)
	}
	if len(plan.StartSystemdServices) != 0 {
		t.Errorf("reordering should not start the container, got StartContainers: %v", plan.StartSystemdServices)
	}
}

func TestApply_Container_ConfigUnchanged_NoAction(t *testing.T) {
	script := "#!/bin/sh\necho hello"
	mountPath := "/usr/local/bin/run.sh"

	spec := makeContainerSpecWithConfigs("webapp", "alpine:latest", model.DesiredStateRunning,
		model.NewContainerFileMount(mountPath, script, os.FileMode(0755)))
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfig(t, fs, "webapp", mountPath, script, 0755)

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertFileMode(t, fs, configFilePath("webapp", mountPath), 0755)
}

func TestApply_OneshotContainer_New_WritesUnitOnly(t *testing.T) {
	specs := []model.Unit{makeOneshotContainerSpec("oneshot-container", "alpine:latest", model.DesiredStateRunning)}
	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitExists(t, sd, "oneshot-container.container")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_OneshotContainer_UnitChanged_NoAction(t *testing.T) {
	spec := makeOneshotContainerSpec("oneshot-container", "alpine:edge", model.DesiredStateRunning)
	oldSpec := makeOneshotContainerSpec("oneshot-container", "alpine:latest", model.DesiredStateRunning)
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"oneshot-container.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"oneshot-container.service": "inactive"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitExists(t, sd, "oneshot-container.container")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_OneshotContainer_Stale_RemovesUnitWithoutStop(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	staleSpec := makeStaleOneshotContainerSpec("old-oneshot", "alpine:latest")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container":      renderContainer(t, fs, webappSpec),
			"old-oneshot.container": renderContainer(t, fs, staleSpec),
		},
		existingState: map[string]string{
			"webapp.service":      "active",
			"old-oneshot.service": "inactive",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "old-oneshot.container")
	testutil.AssertUnitExists(t, sd, "webapp.container")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Volume_MetadataOnlyChange_ContainerNotRestarted(t *testing.T) {
	// Container referencing a volume whose metadata changed must not be restarted.
	newVolumeSpec := makeStaleVolumeSpec("data", "tmpfs") // removalAllowed flips, same Device=
	oldVolumeSpec := makeVolumeSpec("data", "tmpfs")
	webappSpec := makeContainerSpecWithVolume("webapp", model.DesiredStateRunning, "data", "/data")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{newVolumeSpec, webappSpec},
		existingUnits: map[string]string{
			"data.volume":      renderVolume(t, oldVolumeSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertNoStartStop(t, mockConn)
}
