package syslet

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"
	"github.com/spf13/afero"
)

func makeContainerSpecWithBuild(name string, state model.DesiredState, buildName string) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(buildName + ".build")},
		},
		state, nil, false,
	)
}

func preWriteBuildContext(t *testing.T, fs afero.Fs, unitName, fileName, content string, mode os.FileMode) {
	t.Helper()
	store := filestore.NewBuildContextFileStore(fs)
	if err := store.WriteFile(unitName, fileName, content, mode); err != nil {
		t.Fatalf("preWriteBuildContext(%q, %q): %v", unitName, fileName, err)
	}
}

func TestApply_Build_Stale_RemovesUnit(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	staleBuildSpec := makeStaleBuildSpec("oldapp", "oldapp:latest")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"oldapp.build":     renderBuild(t, fs, staleBuildSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "oldapp.build")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedImages) != 0 {
		t.Errorf("expected no image deletions, got: %v", mockPc.deletedImages)
	}
}

func TestApply_Build_StaleWithDeletePolicy_DeletesPodmanImage(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	staleBuildSpec := makeStaleBuildSpec("oldapp", "oldapp:latest")
	staleBuildSpec = makeStaleBuildSpecWithDelete(staleBuildSpec.Ref().Name(), "oldapp:latest")
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"oldapp.build":     renderBuild(t, fs, staleBuildSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "oldapp.build")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedImages) != 1 || mockPc.deletedImages[0] != "oldapp:latest" {
		t.Errorf("expected DeleteImage('oldapp:latest'), got: %v", mockPc.deletedImages)
	}
}

func TestApply_Build_MeaningfulChange_RecreatesAndRestartsContainers(t *testing.T) {
	// New spec changes ImageTag — a meaningful change in the [Build] section.
	newMyappSpec := makeBuildSpec("myapp", "localhost/myapp:v2")
	webappSpec := makeContainerSpecWithBuild("webapp", model.DesiredStateRunning, "myapp")

	fs := afero.NewMemMapFs()
	oldMyappSpec := makeBuildSpec("myapp", "localhost/myapp:v1")

	// Pre-write the same Containerfile so context files are unchanged.
	preWriteBuildContext(t, fs, "myapp", "Containerfile", "FROM scratch", 0644)

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{newMyappSpec, webappSpec},
		existingUnits: map[string]string{
			"myapp.build":      renderBuild(t, fs, oldMyappSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertStopped(t, mockConn, "webapp.service", "myapp-build.service")
	testutil.AssertStarted(t, mockConn, "webapp.service")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedImages) != 0 {
		t.Errorf("expected no image deletions, got: %v", mockPc.deletedImages)
	}

	testutil.AssertUnitExists(t, sd, "myapp.build")
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Build_ContextFileChange_RecreatesAndRestartsContainers(t *testing.T) {
	// Unit file is unchanged; only the Containerfile content differs.
	myappSpec := makeBuildSpec("myapp", "localhost/myapp:latest")
	webappSpec := makeContainerSpecWithBuild("webapp", model.DesiredStateRunning, "myapp")

	fs := afero.NewMemMapFs()

	// Pre-write an old Containerfile so the context file shows as changed.
	preWriteBuildContext(t, fs, "myapp", "Containerfile", "FROM ubuntu", 0644)

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{myappSpec, webappSpec},
		existingUnits: map[string]string{
			"myapp.build":      renderBuild(t, fs, myappSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertStopped(t, mockConn, "webapp.service", "myapp-build.service")
	testutil.AssertStarted(t, mockConn, "webapp.service")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedImages) != 0 {
		t.Errorf("expected no image deletions, got: %v", mockPc.deletedImages)
	}

	testutil.AssertUnitExists(t, sd, "myapp.build")
	// No daemon-reload: the quadlet unit file is unchanged, only the Containerfile changed.
}

func TestApply_Build_MetadataOnlyChange_NoRecreation(t *testing.T) {
	// New spec changes ReclaimPolicy (written to [X-Syslet]) — a metadata-only change.
	newMyappSpec := makeStaleBuildSpecWithDelete("myapp", "localhost/myapp:latest")
	webappSpec := makeContainerSpecWithBuild("webapp", model.DesiredStateRunning, "myapp")

	fs := afero.NewMemMapFs()
	oldMyappSpec := makeBuildSpec("myapp", "localhost/myapp:latest")

	// Pre-write the same Containerfile so context files are unchanged.
	preWriteBuildContext(t, fs, "myapp", "Containerfile", "FROM scratch", 0644)

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{newMyappSpec, webappSpec},
		existingUnits: map[string]string{
			"myapp.build":      renderBuild(t, fs, oldMyappSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertNoStartStop(t, mockConn)

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedImages) != 0 {
		t.Errorf("expected no image deletions, got: %v", mockPc.deletedImages)
	}

	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Build_StaleWithDeletePolicy_OnlyDeletesSpecificImageTag(t *testing.T) {
	// myapp is active and referenced by a container; oldapp is stale and unreferenced.
	// Only oldapp's image tag should be deleted; myapp's tag must be left untouched.
	webappSpec := makeContainerSpec("webapp", "myapp.build", model.DesiredStateRunning)

	activeBuildSpec := makeBuildSpec("myapp", "localhost/myapp:latest")

	staleBuildSpec := makeStaleBuildSpec("oldapp", "oldapp:latest")
	staleBuildSpec = makeStaleBuildSpecWithDelete(staleBuildSpec.Ref().Name(), "oldapp:latest")

	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec, activeBuildSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"myapp.build":      renderBuild(t, fs, activeBuildSpec),
			"oldapp.build":     renderBuild(t, fs, staleBuildSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "oldapp.build")
	testutil.AssertUnitExists(t, sd, "myapp.build")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedImages) != 1 || mockPc.deletedImages[0] != "oldapp:latest" {
		t.Errorf("expected only DeleteImage('oldapp:latest'), got: %v", mockPc.deletedImages)
	}
}
