package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContainerSpec_GetMethods(t *testing.T) {
	spec := &ContainerSpec{
		Name: "webapp",
		Unit: map[string]map[string]UnitValue{
			"Container": {"Image": UV("nginx:latest")},
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
		Unit: map[string]map[string]UnitValue{
			"Container": {"Image": UV("nginx:latest")},
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
		Unit: map[string]map[string]UnitValue{
			"Container": {"Image": UV("nginx:latest")},
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
		Unit: map[string]map[string]UnitValue{
			"Container": {"Image": UV("nginx:latest")},
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
		Unit: map[string]map[string]UnitValue{
			"Container": {
				"Image":         UV("nginx:latest"),
				"ContainerName": UV("custom-name"), // User-specified name
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

func TestContainerSpec_Unmarshal_InvalidUnit_Number(t *testing.T) {
	jsonData := []byte(`{
		"name": "webapp",
		"type": "container",
		"unit": {
			"Container": {
				"Port": 8080
			}
		}
	}`)

	var spec ContainerSpec
	err := json.Unmarshal(jsonData, &spec)
	if err == nil {
		t.Error("expected unmarshal to fail with numeric value, but it succeeded")
	}
}

func TestContainerSpec_Unmarshal_InvalidUnit_Boolean(t *testing.T) {
	jsonData := []byte(`{
		"name": "webapp",
		"type": "container",
		"unit": {
			"Container": {
				"Enabled": true
			}
		}
	}`)

	var spec ContainerSpec
	err := json.Unmarshal(jsonData, &spec)
	if err == nil {
		t.Error("expected unmarshal to fail with boolean value, but it succeeded")
	}
}

func TestContainerSpec_Unmarshal_InvalidUnit_ArrayWithNumber(t *testing.T) {
	jsonData := []byte(`{
		"name": "webapp",
		"type": "container",
		"unit": {
			"Container": {
				"Image": [123, 456]
			}
		}
	}`)

	var spec ContainerSpec
	err := json.Unmarshal(jsonData, &spec)
	if err == nil {
		t.Error("expected unmarshal to fail with array of numbers, but it succeeded")
	}
}
