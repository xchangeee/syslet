package api

import (
	"strings"
	"testing"
)

func TestContainerSpec_GetMethods(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
	}

	if spec.GetName() != "webapp" {
		t.Errorf("expected name 'webapp', got %q", spec.GetName())
	}
	if spec.GetType() != SpecTypeContainer {
		t.Errorf("expected type %q, got %q", SpecTypeContainer, spec.GetType())
	}
	if spec.FullUnitName() != "webapp.container" {
		t.Errorf("expected 'webapp.container', got %q", spec.FullUnitName())
	}
}

func TestContainerSpec_Render_BasicContainer(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
		DesiredState: "running",
	}

	rendered, err := spec.Render("/etc/containers/config")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify ContainerName is set
	hasContainerName := false
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Container" && opt.Name == "ContainerName" && opt.Value == "webapp" {
			hasContainerName = true
		}
	}
	if !hasContainerName {
		t.Error("expected ContainerName=webapp to be set")
	}

	// Verify Install section is added for running state
	hasInstall := false
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Install" && opt.Name == "WantedBy" {
			hasInstall = true
		}
	}
	if !hasInstall {
		t.Error("expected Install section for running container")
	}
}

func TestContainerSpec_Render_DesiredStateStopped(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
		DesiredState: "stopped",
	}

	rendered, err := spec.Render("/etc/containers/config")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify Install section is NOT added for stopped state
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Install" {
			t.Error("expected no Install section for stopped container")
		}
	}
}

func TestContainerSpec_Render_WithConfigs(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {"Image": "nginx:latest"},
		},
		Configs: []ConfigEntry{
			{
				Content:          "config content",
				TargetVolumePath: "/etc/nginx/nginx.conf",
			},
			{
				Content:          "another config",
				TargetVolumePath: "/etc/app/config.yaml",
			},
		},
	}

	rendered, err := spec.Render("/etc/containers/config")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify config bind mounts were added
	volumeCount := 0
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Container" && opt.Name == "Volume" {
			volumeCount++
			// Verify mount is read-only
			if !strings.Contains(opt.Value, ":ro") {
				t.Errorf("expected volume mount to be read-only, got %q", opt.Value)
			}
		}
	}
	if volumeCount != 2 {
		t.Errorf("expected 2 config volume mounts, got %d", volumeCount)
	}
}

func TestContainerSpec_Render_ContainerNameNotOverridden(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {
				"Image":         "nginx:latest",
				"ContainerName": "custom-name", // User-specified name
			},
		},
	}

	rendered, err := spec.Render("/etc/containers/config")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify user-specified ContainerName is preserved
	containerNameCount := 0
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Container" && opt.Name == "ContainerName" {
			containerNameCount++
			if opt.Value != "custom-name" {
				t.Errorf("expected custom-name, got %q", opt.Value)
			}
		}
	}
	if containerNameCount != 1 {
		t.Errorf("expected exactly 1 ContainerName, got %d", containerNameCount)
	}
}

func TestContainerSpec_Render_InvalidUnit(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]any{
			"Container": {
				"Image": []any{123}, // Invalid: array with non-string
			},
		},
	}

	_, err := spec.Render("/etc/containers/config")
	if err == nil {
		t.Error("expected Render to fail with invalid unit data")
	}
}
