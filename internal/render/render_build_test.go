package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xchangeee/syslet/internal/model"
)

func TestBuildSpec_Render_AutoGeneratesContainerfilePath(t *testing.T) {
	spec := model.NewBuildUnit(model.BuildUnitRef("myapp"), nil, "FROM alpine\nRUN echo hi", nil, model.ReclaimPolicyRetain)
	ru, err := NewRenderedUnitFromBuild(spec, testBuildResolve("/var/lib/syslet/build"))
	rendered := mustRender(t, ru, err)

	want := filepath.Join("/var/lib/syslet/build/myapp", "Containerfile")
	assertHasOption(t, rendered.UnitOptions, SectionBuild, KeyBuildFile, want)
}

func TestBuildSpec_Render_ContainerfileOverridesUserValue(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myapp"),
		makeUnitOptions(SectionBuild, "File", "/custom/path/Containerfile"),
		"FROM alpine",
		nil,
		model.ReclaimPolicyRetain,
	)
	ru, err := NewRenderedUnitFromBuild(spec, testBuildResolve("/var/lib/syslet/build"))
	rendered := mustRender(t, ru, err)

	want := filepath.Join("/var/lib/syslet/build/myapp", "Containerfile")
	assertUniqueOption(t, rendered.UnitOptions, SectionBuild, KeyBuildFile, want)
}

func TestBuildSpec_Render_WithConfigs(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myapp"),
		nil,
		"FROM alpine",
		[]model.BuildContextFile{
			{Content: "print('hello')", Filename: "app.py"},
			{Content: "key=value", Filename: "config/settings.ini"},
		},
		model.ReclaimPolicyRetain,
	)
	ru, err := NewRenderedUnitFromBuild(spec, testBuildResolve("/var/lib/syslet/build"))
	mustRender(t, ru, err)

	if len(spec.ContextFiles) != 2 {
		t.Fatalf("expected 2 context files, got %d", len(spec.ContextFiles))
	}
	if spec.ContextFiles[0].Filename != "app.py" {
		t.Errorf("expected filename 'app.py', got %q", spec.ContextFiles[0].Filename)
	}
	if spec.ContextFiles[0].Content != "print('hello')" {
		t.Errorf("unexpected content for app.py: %q", spec.ContextFiles[0].Content)
	}
	if spec.ContextFiles[1].Filename != "config/settings.ini" {
		t.Errorf("expected filename 'config/settings.ini', got %q", spec.ContextFiles[1].Filename)
	}
}

func TestBuildSpec_Render_ConfigFileMode(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myapp"),
		nil,
		"FROM alpine",
		[]model.BuildContextFile{
			{Content: "#!/bin/sh\necho hi", Filename: "run.sh", Mode: 0755},
			{Content: "key=value", Filename: "config.ini", Mode: 0644},
		},
		model.ReclaimPolicyRetain,
	)
	ru, err := NewRenderedUnitFromBuild(spec, testBuildResolve("/var/lib/syslet/build"))
	mustRender(t, ru, err)

	if spec.ContextFiles[0].Mode != 0755 {
		t.Errorf("expected mode 0755 for script, got %04o", spec.ContextFiles[0].Mode)
	}
	if spec.ContextFiles[1].Mode != 0644 {
		t.Errorf("expected default mode 0644 for config, got %04o", spec.ContextFiles[1].Mode)
	}
}

func TestBuildSpec_Render_ContentSerializable(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myapp"),
		makeUnitOptions(SectionBuild, "ImageTag", "myapp:latest"),
		"FROM alpine",
		nil,
		model.ReclaimPolicyDelete,
	)
	ru, err := NewRenderedUnitFromBuild(spec, testBuildResolve("/var/lib/syslet/build"))
	rendered := mustRender(t, ru, err)

	if rendered.Content == "" {
		t.Error("expected non-empty serialized Content")
	}
	if !strings.Contains(rendered.Content, "ImageTag=myapp:latest") {
		t.Errorf("expected Content to contain ImageTag=myapp:latest, got:\n%s", rendered.Content)
	}
}

func TestBuildSpec_ReclaimableMixin(t *testing.T) {
	resolve := testBuildResolve("/var/lib/syslet/build")
	testReclaimableMixin(t,
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromBuild(model.NewBuildUnit(
				model.BuildUnitRef("myapp"),
				nil,
				"FROM alpine",
				nil,
				model.ReclaimPolicyDelete,
			), resolve)
		},
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromBuild(model.NewBuildUnit(
				model.BuildUnitRef("myapp"),
				makeUnitOptions(SectionXSyslet, "ReclaimPolicy", "Retain"),
				"FROM alpine",
				nil,
				model.ReclaimPolicyDelete,
			), resolve)
		},
		func() (RenderedUnit, error) {
			return NewRenderedUnitFromBuild(model.NewBuildUnit(
				model.BuildUnitRef("myapp"),
				nil,
				"FROM alpine",
				nil,
				model.ReclaimPolicyRetain,
			), resolve)
		},
	)
}
