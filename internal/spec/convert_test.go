package spec

import (
	"io"
	"strings"
	"testing"

	"codeberg.org/xchangeee/rsystemd/internal/parser"
)

func TestConvert_BasicContainer(t *testing.T) {
	s := &ContainerSpec{
		Name:         "webapp",
		Type:         "container",
		DesiredState: "running",
		Unit: map[string]map[string]any{
			"Container": {
				"Image":       "docker.io/library/nginx:latest",
				"PublishPort": []any{"8080:80"},
			},
		},
	}

	pu, cfgFiles, err := Convert(s, "/etc/containers/config")
	if err != nil {
		t.Fatal(err)
	}

	if pu.Name != "webapp.container" {
		t.Errorf("got name %q, want %q", pu.Name, "webapp.container")
	}
	if pu.Type != parser.UnitTypeContainer {
		t.Errorf("got type %v, want container", pu.Type)
	}
	if pu.DesiredState != parser.DesiredStateRunning {
		t.Errorf("got desired state %v, want running", pu.DesiredState)
	}
	if len(cfgFiles) != 0 {
		t.Errorf("expected no config files, got %d", len(cfgFiles))
	}

	// Check that Install section was auto-generated.
	content, _ := io.ReadAll(pu.SystemdContent())
	out := string(content)
	if !strings.Contains(out, "WantedBy=multi-user.target default.target") {
		t.Errorf("missing auto-generated Install section in:\n%s", out)
	}
	// X-Rsystemd should be stripped by SystemdContent.
	if strings.Contains(out, "X-Rsystemd") {
		t.Errorf("SystemdContent should not contain X-Rsystemd:\n%s", out)
	}
}

func TestConvert_ConfigVolumeInjection(t *testing.T) {
	s := &ContainerSpec{
		Name:         "myapp",
		Type:         "container",
		DesiredState: "running",
		Unit: map[string]map[string]any{
			"Container": {
				"Image": "myimage:latest",
			},
		},
		Configs: []ConfigEntry{
			{Content: "server {}", TargetVolumePath: "/etc/nginx/nginx.conf"},
			{Content: "KEY=val", TargetVolumePath: "/app/env"},
		},
	}

	pu, cfgFiles, err := Convert(s, "/etc/containers/config")
	if err != nil {
		t.Fatal(err)
	}

	if len(cfgFiles) != 2 {
		t.Fatalf("expected 2 config files, got %d", len(cfgFiles))
	}

	if cfgFiles[0].Filename != "nginx.conf" {
		t.Errorf("config[0] filename = %q, want %q", cfgFiles[0].Filename, "nginx.conf")
	}
	if cfgFiles[1].Filename != "env" {
		t.Errorf("config[1] filename = %q, want %q", cfgFiles[1].Filename, "env")
	}

	// Check volume entries in raw content.
	if !strings.Contains(pu.RawContent, "/etc/containers/config/myapp/nginx.conf:/etc/nginx/nginx.conf:ro") {
		t.Errorf("missing nginx.conf volume entry in:\n%s", pu.RawContent)
	}
	if !strings.Contains(pu.RawContent, "/etc/containers/config/myapp/env:/app/env:ro") {
		t.Errorf("missing env volume entry in:\n%s", pu.RawContent)
	}
}

func TestConvert_DuplicateConfigBasename(t *testing.T) {
	s := &ContainerSpec{
		Name: "dup",
		Type: "container",
		Unit: map[string]map[string]any{
			"Container": {"Image": "x"},
		},
		Configs: []ConfigEntry{
			{Content: "a", TargetVolumePath: "/foo/conf.yaml"},
			{Content: "b", TargetVolumePath: "/bar/conf.yaml"},
		},
	}

	_, _, err := Convert(s, "/etc/containers/config")
	if err == nil {
		t.Fatal("expected error for duplicate basename, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate config basename") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConvert_Volume(t *testing.T) {
	s := &ContainerSpec{
		Name: "mydata",
		Type: "volume",
		Unit: map[string]map[string]any{
			"Volume": {
				"User":  "1000",
				"Group": "1000",
			},
		},
	}

	pu, cfgFiles, err := Convert(s, "/etc/containers/config")
	if err != nil {
		t.Fatal(err)
	}

	if pu.Name != "mydata.volume" {
		t.Errorf("got name %q, want %q", pu.Name, "mydata.volume")
	}
	if pu.Type != parser.UnitTypeVolume {
		t.Errorf("got type %v, want volume", pu.Type)
	}
	if len(cfgFiles) != 0 {
		t.Errorf("expected no config files for volume")
	}

	// Volumes should NOT have Install section.
	content, _ := io.ReadAll(pu.SystemdContent())
	out := string(content)
	if strings.Contains(out, "WantedBy") {
		t.Errorf("volume should not have Install section:\n%s", out)
	}
}
