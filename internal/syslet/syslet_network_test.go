package syslet

import (
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/testutil"
	"github.com/spf13/afero"
)

func makeContainerSpecWithNetwork(name, image string, state model.DesiredState, networkName string) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):   model.UV(image),
				model.SectionKey("Network"): model.UV(networkName + ".network"),
			},
		},
		state, nil, false,
	)
}

func TestApply_Network_MeaningfulChange_RecreatesAndRestartsContainers(t *testing.T) {
	frontendNetworkSpec := makeNetworkSpec("frontend", "macvlan")
	webappSpec := makeContainerSpecWithNetwork("webapp", "nginx:latest", model.DesiredStateRunning, "frontend")

	fs := afero.NewMemMapFs()
	oldFrontendNetworkSpec := makeNetworkSpec("frontend", "bridge")

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{frontendNetworkSpec, webappSpec},
		existingUnits: map[string]string{
			"frontend.network": renderNetwork(t, oldFrontendNetworkSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{
			"webapp.service":           "active",
			"frontend-network.service": "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertStopped(t, mockConn, "webapp.service", "frontend-network.service")
	testutil.AssertStarted(t, mockConn, "webapp.service")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedNetworks) != 1 || mockPc.deletedNetworks[0] != "frontend" {
		t.Errorf("expected DeleteNetwork('frontend'), got: %v", mockPc.deletedNetworks)
	}

	testutil.AssertUnitExists(t, sd, "frontend.network")
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Network_MetadataOnlyChange_NoRecreation(t *testing.T) {
	// New spec: RemovalAllowed=true (metadata-only change), same Driver=bridge
	frontendNetworkSpec := makeStaleNetworkSpec("frontend", "bridge")
	webappSpec := makeContainerSpecWithNetwork("webapp", "nginx:latest", model.DesiredStateRunning, "frontend")

	fs := afero.NewMemMapFs()
	// Old spec: RemovalAllowed=false
	oldFrontendNetworkSpec := makeNetworkSpec("frontend", "bridge")

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{frontendNetworkSpec, webappSpec},
		existingUnits: map[string]string{
			"frontend.network": renderNetwork(t, oldFrontendNetworkSpec),
			"webapp.container": renderContainer(t, fs, webappSpec),
		},
		existingState: map[string]string{
			"webapp.service": "active",
		},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertNoStartStop(t, mockConn)

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedNetworks) != 0 {
		t.Errorf("expected no network deletions, got: %v", mockPc.deletedNetworks)
	}

	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Network_New_WritesUnitOnly(t *testing.T) {
	specs := []model.Unit{makeNetworkSpec("frontend", "bridge")}
	ctx, fs, sd, mockConn, mockPodman, zipPath := setupTest(t, testFixture{specs: specs})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitExists(t, sd, "frontend.network")
	testutil.AssertReloaded(t, mockConn)
	testutil.AssertNoStartStop(t, mockConn)
}

func TestApply_Network_Stale_RemovesUnit(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	staleNetworkSpec := makeStaleNetworkSpec("oldnet", "bridge")
	fs := afero.NewMemMapFs()

	ctx, sd, mockConn, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"oldnet.network":   renderNetwork(t, staleNetworkSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "oldnet.network")
	testutil.AssertNoStartStop(t, mockConn)
	testutil.AssertReloaded(t, mockConn)
}

func TestApply_Network_StaleWithDeletePolicy_DeletesPodmanNetwork(t *testing.T) {
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	staleNetworkSpec := makeStaleNetworkSpecWithDelete("oldnet", "bridge")
	fs := afero.NewMemMapFs()

	ctx, sd, _, mockPodman, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{webappSpec},
		existingUnits: map[string]string{
			"webapp.container": renderContainer(t, fs, webappSpec),
			"oldnet.network":   renderNetwork(t, staleNetworkSpec),
		},
		existingState: map[string]string{"webapp.service": "active"},
	})

	mustApply(t, ctx, fs, sd, mockPodman, zipPath)

	testutil.AssertUnitAbsent(t, sd, "oldnet.network")

	mockPc := mockPodman.(*mockPodmanClient)
	if len(mockPc.deletedNetworks) != 1 || mockPc.deletedNetworks[0] != "oldnet" {
		t.Errorf("expected DeleteNetwork('oldnet'), got: %v", mockPc.deletedNetworks)
	}
}
