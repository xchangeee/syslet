package validate

import (
	"strings"
	"testing"

	gounit "github.com/coreos/go-systemd/v22/unit"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/render"
)

// --- Helpers ---

func containerUnit(name string, opts ...gounit.UnitOption) render.RenderedUnit {
	return render.RenderedUnit{
		Unit:        model.NewContainerUnit(model.ContainerUnitRef(name), nil, "", nil, false),
		UnitOptions: opts,
	}
}

func volumeUnit(name string) render.RenderedUnit {
	return render.RenderedUnit{
		Unit: model.NewVolumeUnit(model.VolumeUnitRef(name), nil, false, ""),
		UnitOptions: []gounit.UnitOption{
			render.NewUnitOption(render.SectionVolume, render.KeyVolumeName, name),
		},
	}
}

func networkUnit(name string) render.RenderedUnit {
	return render.RenderedUnit{
		Unit: model.NewNetworkUnit(model.NetworkUnitRef(name), nil, false, ""),
		UnitOptions: []gounit.UnitOption{
			render.NewUnitOption(render.SectionNetwork, render.KeyNetworkName, name),
		},
	}
}

func buildUnit(name string, opts ...gounit.UnitOption) render.RenderedUnit {
	return render.RenderedUnit{
		Unit:        model.NewBuildUnit(model.BuildUnitRef(name), nil, "FROM scratch", nil, ""),
		UnitOptions: opts,
	}
}

func containerVolumeOpt(value string) gounit.UnitOption {
	return render.NewUnitOption(render.SectionContainer, render.KeyContainerVolume, value)
}

func containerNetworkOpt(value string) gounit.UnitOption {
	return render.NewUnitOption(render.SectionContainer, render.KeyContainerNetwork, value)
}

func containerImageOpt(value string) gounit.UnitOption {
	return render.NewUnitOption(render.SectionContainer, render.KeyContainerImage, value)
}

func buildImageTagOpt(value string) gounit.UnitOption {
	return render.NewUnitOption(render.SectionBuild, render.KeyBuildImageTag, value)
}

// --- parseVolumeReference ---

func TestParseVolumeReference_NamedVolume_ReturnsName(t *testing.T) {
	got := parseVolumeReference("webapp-data.volume:/data")
	if got != "webapp-data" {
		t.Errorf("got %q, want %q", got, "webapp-data")
	}
}

func TestParseVolumeReference_BindMount_ReturnsEmpty(t *testing.T) {
	got := parseVolumeReference("/host/path:/data")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestParseVolumeReference_NoColon_ReturnsEmpty(t *testing.T) {
	got := parseVolumeReference("webapp-data.volume")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// --- parseNetworkReference ---

func TestParseNetworkReference_NamedNetwork_ReturnsName(t *testing.T) {
	got := parseNetworkReference("mynet.network")
	if got != "mynet" {
		t.Errorf("got %q, want %q", got, "mynet")
	}
}

func TestParseNetworkReference_HostNetwork_ReturnsEmpty(t *testing.T) {
	got := parseNetworkReference("host")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// --- parseBuildReference ---

func TestParseBuildReference_BuildUnit_ReturnsName(t *testing.T) {
	got := parseBuildReference("myapp.build")
	if got != "myapp" {
		t.Errorf("got %q, want %q", got, "myapp")
	}
}

func TestParseBuildReference_RegistryImage_ReturnsEmpty(t *testing.T) {
	got := parseBuildReference("nginx:latest")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// --- validateVolumeReferences ---

func TestValidateVolumeReferences_ReferencedVolumeExists_ReturnsNil(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerVolumeOpt("webapp-data.volume:/data")),
	}
	volumes := []render.RenderedUnit{volumeUnit("webapp-data")}

	if err := validateContainerVolumeReferencesExist(containers, volumes); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateVolumeReferences_UndefinedVolume_ReturnsError(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerVolumeOpt("missing-data.volume:/data")),
	}
	volumes := []render.RenderedUnit{volumeUnit("other-vol")}

	err := validateContainerVolumeReferencesExist(containers, volumes)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing-data") {
		t.Errorf("error should mention the missing volume name, got: %v", err)
	}
}

func TestValidateVolumeReferences_BindMount_Ignored(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerVolumeOpt("/host/data:/data")),
	}

	if err := validateContainerVolumeReferencesExist(containers, nil); err != nil {
		t.Errorf("bind mount should not be validated against volumes, got: %v", err)
	}
}

func TestValidateVolumeReferences_NoContainers_ReturnsNil(t *testing.T) {
	if err := validateContainerVolumeReferencesExist(nil, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- validateNetworkReferences ---

func TestValidateNetworkReferences_ReferencedNetworkExists_ReturnsNil(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerNetworkOpt("mynet.network")),
	}
	networks := []render.RenderedUnit{networkUnit("mynet")}

	if err := validateContainerNetworkReferencesExist(containers, networks); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateNetworkReferences_UndefinedNetwork_ReturnsError(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerNetworkOpt("missing.network")),
	}

	err := validateContainerNetworkReferencesExist(containers, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention the network name, got: %v", err)
	}
}

func TestValidateNetworkReferences_HostMode_Ignored(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerNetworkOpt("host")),
	}

	if err := validateContainerNetworkReferencesExist(containers, nil); err != nil {
		t.Errorf("host network should not be validated, got: %v", err)
	}
}

// --- validateBuildReferences ---

func TestValidateBuildReferences_ReferencedBuildExists_ReturnsNil(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerImageOpt("myapp.build")),
	}
	builds := []render.RenderedUnit{buildUnit("myapp", buildImageTagOpt("localhost/myapp:latest"))}

	if err := validateContainerBuildReferencesExist(containers, builds); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateBuildReferences_UndefinedBuild_ReturnsError(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerImageOpt("missing.build")),
	}

	err := validateContainerBuildReferencesExist(containers, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention the build name, got: %v", err)
	}
}

func TestValidateBuildReferences_RegistryImage_Ignored(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp", containerImageOpt("nginx:latest")),
	}

	if err := validateContainerBuildReferencesExist(containers, nil); err != nil {
		t.Errorf("registry image should not be validated against builds, got: %v", err)
	}
}

// --- validateNoVolumeConflicts ---

func TestValidateNoVolumeConflicts_DistinctDestinations_ReturnsNil(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp",
			containerVolumeOpt("data.volume:/data"),
			containerVolumeOpt("logs.volume:/logs"),
		),
	}

	if err := validateContainerNoConflictingVolumeDstPaths(containers); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateNoVolumeConflicts_DuplicateDestination_ReturnsError(t *testing.T) {
	containers := []render.RenderedUnit{
		containerUnit("webapp",
			containerVolumeOpt("data.volume:/data"),
			containerVolumeOpt("/host/other:/data"),
		),
	}

	err := validateContainerNoConflictingVolumeDstPaths(containers)
	if err == nil {
		t.Fatal("expected error for duplicate destination, got nil")
	}
	if !strings.Contains(err.Error(), "/data") {
		t.Errorf("error should mention the conflicting destination, got: %v", err)
	}
}

func TestValidateNoVolumeConflicts_NoVolumes_ReturnsNil(t *testing.T) {
	if err := validateContainerNoConflictingVolumeDstPaths([]render.RenderedUnit{containerUnit("webapp")}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- validateBuildImageTags ---

func TestValidateBuildImageTags_ValidTag_ReturnsNil(t *testing.T) {
	builds := []render.RenderedUnit{
		buildUnit("myapp", buildImageTagOpt("localhost/myapp:latest")),
	}

	if err := validateBuildImageTagsUseLocalhostRegistry(builds); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateBuildImageTags_MissingTag_ReturnsError(t *testing.T) {
	builds := []render.RenderedUnit{buildUnit("myapp")}

	err := validateBuildImageTagsUseLocalhostRegistry(builds)
	if err == nil {
		t.Fatal("expected error for missing ImageTag, got nil")
	}
	if !strings.Contains(err.Error(), "ImageTag") {
		t.Errorf("error should mention ImageTag, got: %v", err)
	}
}

func TestValidateBuildImageTags_WrongPrefix_ReturnsError(t *testing.T) {
	builds := []render.RenderedUnit{
		buildUnit("myapp", buildImageTagOpt("docker.io/myapp:latest")),
	}

	err := validateBuildImageTagsUseLocalhostRegistry(builds)
	if err == nil {
		t.Fatal("expected error for wrong ImageTag prefix, got nil")
	}
	if !strings.Contains(err.Error(), "localhost/myapp:") {
		t.Errorf("error should mention expected prefix, got: %v", err)
	}
}

func TestValidateBuildImageTags_WrongBuildName_ReturnsError(t *testing.T) {
	builds := []render.RenderedUnit{
		buildUnit("myapp", buildImageTagOpt("localhost/otherapp:latest")),
	}

	err := validateBuildImageTagsUseLocalhostRegistry(builds)
	if err == nil {
		t.Fatal("expected error when ImageTag name mismatches build name, got nil")
	}
	if !strings.Contains(err.Error(), "localhost/myapp:") {
		t.Errorf("error should mention expected prefix, got: %v", err)
	}
}

func TestValidateBuildImageTags_NoBuilds_ReturnsNil(t *testing.T) {
	if err := validateBuildImageTagsUseLocalhostRegistry(nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ValidateRenderedUnits ---

func TestPostRender_AllValid_ReturnsNil(t *testing.T) {
	result := render.RenderResult{
		Containers: []render.RenderedUnit{
			containerUnit("webapp",
				containerVolumeOpt("data.volume:/data"),
				containerNetworkOpt("mynet.network"),
				containerImageOpt("myapp.build"),
			),
		},
		Volumes:  []render.RenderedUnit{volumeUnit("data")},
		Networks: []render.RenderedUnit{networkUnit("mynet")},
		Builds:   []render.RenderedUnit{buildUnit("myapp", buildImageTagOpt("localhost/myapp:latest"))},
	}

	if err := PostRender(result); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPostRender_UndefinedVolume_ReturnsError(t *testing.T) {
	result := render.RenderResult{
		Containers: []render.RenderedUnit{
			containerUnit("webapp", containerVolumeOpt("missing.volume:/data")),
		},
	}

	if err := PostRender(result); err == nil {
		t.Error("expected error for undefined volume, got nil")
	}
}

func TestPostRender_VolumeConflict_ReturnsError(t *testing.T) {
	result := render.RenderResult{
		Containers: []render.RenderedUnit{
			containerUnit("webapp",
				containerVolumeOpt("a.volume:/data"),
				containerVolumeOpt("b.volume:/data"),
			),
		},
		Volumes: []render.RenderedUnit{volumeUnit("a"), volumeUnit("b")},
	}

	if err := PostRender(result); err == nil {
		t.Error("expected error for volume conflict, got nil")
	}
}
