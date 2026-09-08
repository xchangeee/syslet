package syslet

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"github.com/spf13/afero"
)

// TestStaging_AllUnitTypes_StagedWhenVolumeChanges verifies that all rendered unit types
// (container, volume, network, build) appear in the staging directory even when only
// the volume has changed. The quadlet generator needs the full picture to validate correctly.
func TestStaging_AllUnitTypes_StagedWhenVolumeChanges(t *testing.T) {
	containerSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	oldVolumeSpec := makeVolumeSpec("data", "tmpfs")      // removalAllowed=false
	newVolumeSpec := makeStaleVolumeSpec("data", "tmpfs") // removalAllowed=true — metadata-only change, no recreation
	netSpec := makeNetworkSpec("frontend", "bridge")
	buildSpec := makeBuildSpec("myapp", "localhost/myapp:latest")

	fs := afero.NewMemMapFs()
	ctx, sd, _, _, zipPath := setupTestWithFS(t, fs, testFixture{
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
	if _, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, gen, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath); err != nil {
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
