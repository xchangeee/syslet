package render

import (
	"strings"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
)

func TestContainerSpec_Render_BasicContainer(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		makeUnitOptions(SectionContainer, "Image", "nginx:latest"),
		model.DesiredStateRunning,
		nil, false,
	)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)

	assertHasOption(t, rendered.UnitOptions, SectionContainer, KeyContainerName, "webapp")
	assertHasOption(t, rendered.UnitOptions, SectionService, KeyServiceRestart, ServiceRestartAlways)
	assertHasOption(t, rendered.UnitOptions, SectionInstall, KeyInstallWantedBy, InstallMultiUserTarget)
}

func TestContainerSpec_Render_DesiredStateRunning_OverridesUserRestart(t *testing.T) {
	opts := model.UnitOptions{
		SectionContainer: {model.SectionKey("Image"): model.UV("nginx:latest")},
		SectionService:   {model.SectionKey("Restart"): model.UV("on-failure")},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), opts, model.DesiredStateRunning, nil, false)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionService, KeyServiceRestart, "always")
}

func TestContainerSpec_Render_DesiredStateStopped(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		makeUnitOptions(SectionContainer, "Image", "nginx:latest"),
		model.DesiredStateStopped,
		nil, false,
	)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertNoOption(t, rendered.UnitOptions, SectionInstall, KeyInstallWantedBy)
	assertNoOption(t, rendered.UnitOptions, SectionService, KeyServiceRestart)
}

func TestContainerSpec_Render_DesiredStateOneshot(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("job"),
		makeUnitOptions(SectionContainer, "Image", "alpine:latest"),
		model.DesiredStateOneshot,
		nil, false,
	)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertNoOption(t, rendered.UnitOptions, SectionInstall, KeyInstallWantedBy)
	assertNoOption(t, rendered.UnitOptions, SectionService, KeyServiceRestart)
	assertHasOption(t, rendered.UnitOptions, SectionService, KeyServiceType, ServiceTypeOneshot)
}

func TestContainerSpec_Render_DesiredStateOneshot_OverridesUserType(t *testing.T) {
	opts := model.UnitOptions{
		SectionContainer: {model.SectionKey("Image"): model.UV("alpine:latest")},
		SectionService:   {model.SectionKey("Type"): model.UV("notify")},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("job"), opts, model.DesiredStateOneshot, nil, false)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionService, KeyServiceType, "oneshot")
}

func TestContainerSpec_Render_WithConfig_Mode(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		makeUnitOptions(SectionContainer, "Image", "alpine:latest"),
		model.DesiredStateStopped,
		[]model.ContainerFileMount{
			model.NewContainerFileMount("/usr/local/bin/run.sh", "#!/bin/sh\necho hi", 0755),
			model.NewContainerFileMount("/etc/app.conf", "data", 0644),
		},
		false,
	)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	mustRender(t, ru, err)

	if spec.FileMounts[0].File.Mode != 0755 {
		t.Errorf("expected mode 0755 for script, got %04o", spec.FileMounts[0].File.Mode)
	}
	if spec.FileMounts[1].File.Mode != 0644 {
		t.Errorf("expected default mode 0644 for config, got %04o", spec.FileMounts[1].File.Mode)
	}
}

func TestContainerSpec_Render_WithConfigs(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		makeUnitOptions(SectionContainer, "Image", "nginx:latest"),
		model.DesiredStateStopped,
		[]model.ContainerFileMount{
			model.NewContainerFileMount("/etc/nginx/nginx.conf", "config content", 0),
			model.NewContainerFileMount("/etc/app/config.yaml", "another config", 0),
		},
		false,
	)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)

	volumes := filterOptions(rendered.UnitOptions, SectionContainer, KeyContainerVolume)
	if len(volumes) != 2 {
		t.Errorf("expected 2 config volume mounts, got %d", len(volumes))
	}
	for _, v := range volumes {
		if !strings.Contains(v.Value, ":ro,Z") {
			t.Errorf("expected volume mount to be read-only, got %q", v.Value)
		}
	}
}

func TestContainerSpec_Render_ContainerNameIsEnforced(t *testing.T) {
	opts := model.UnitOptions{
		SectionContainer: {
			model.SectionKey("Image"):         model.UV("nginx:latest"),
			model.SectionKey("ContainerName"): model.UV("custom-name"),
		},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), opts, model.DesiredStateStopped, nil, false)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionContainer, KeyContainerName, "webapp")
}

func TestContainerSpec_Render_AutoGeneratedDescription(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		makeUnitOptions(SectionContainer, "Image", "nginx:latest"),
		model.DesiredStateStopped, nil, false,
	)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertHasOption(t, rendered.UnitOptions, SectionUnit, KeyUnitDescription, "webapp container")
}

func TestContainerSpec_Render_DescriptionNotOverridden(t *testing.T) {
	opts := model.UnitOptions{
		SectionUnit:      {model.SectionKey("Description"): model.UV("Custom webapp description")},
		SectionContainer: {model.SectionKey("Image"): model.UV("nginx:latest")},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), opts, model.DesiredStateStopped, nil, false)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionUnit, KeyUnitDescription, "Custom webapp description")
}

func TestContainerSpec_Render_RestartForcedForRunning(t *testing.T) {
	opts := model.UnitOptions{
		SectionService:   {model.SectionKey("Restart"): model.UV("on-failure")},
		SectionContainer: {model.SectionKey("Image"): model.UV("nginx:latest")},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), opts, model.DesiredStateRunning, nil, false)
	ru, err := RenderContainer(spec, testContainerResolve("/etc/containers/config"))
	rendered := mustRender(t, ru, err)
	assertUniqueOption(t, rendered.UnitOptions, SectionService, KeyServiceRestart, "always")
}

func TestContainerSpec_PrunableMixin(t *testing.T) {
	resolve := testContainerResolve("/etc/containers/config")
	testPrunableMixin(t,
		func() (RenderedUnit, error) {
			return RenderContainer(model.NewContainerUnit(
				model.ContainerUnitRef("webapp"),
				makeUnitOptions(SectionContainer, "Image", "nginx:latest"),
				model.DesiredStateStopped, nil, true,
			), resolve)
		},
		func() (RenderedUnit, error) {
			opts := model.UnitOptions{
				SectionContainer: {model.SectionKey("Image"): model.UV("nginx:latest")},
				SectionXSyslet:   {model.SectionKey("RemovalAllowed"): model.UV("false")},
			}
			return RenderContainer(model.NewContainerUnit(
				model.ContainerUnitRef("webapp"), opts, "", nil, true,
			), resolve)
		},
		func() (RenderedUnit, error) {
			return RenderContainer(model.NewContainerUnit(
				model.ContainerUnitRef("webapp"),
				makeUnitOptions(SectionContainer, "Image", "nginx:latest"),
				model.DesiredStateStopped, nil, false,
			), resolve)
		},
	)
}
