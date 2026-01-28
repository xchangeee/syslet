package parser

import (
	"io"
	"strings"
	"testing"
)

func TestUnitTypeFromExtension(t *testing.T) {
	tests := []struct {
		filename string
		want     UnitType
	}{
		{"webapp.container", UnitTypeContainer},
		{"data.volume", UnitTypeVolume},
		{"net.network", UnitTypeNetwork},
		{"readme.txt", UnitTypeUnknown},
		{"myapp.service", UnitTypeUnknown},
		{"backup.timer", UnitTypeUnknown},
	}
	for _, tt := range tests {
		if got := UnitTypeFromExtension(tt.filename); got != tt.want {
			t.Errorf("UnitTypeFromExtension(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestUnitBaseName(t *testing.T) {
	tests := []struct {
		filename, want string
	}{
		{"webapp.container", "webapp"},
		{"data.volume", "data"},
		{"net.network", "net"},
	}
	for _, tt := range tests {
		if got := UnitBaseName(tt.filename); got != tt.want {
			t.Errorf("UnitBaseName(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}

func TestParseContainerQuadlet(t *testing.T) {
	content := `[Container]
Image=docker.io/library/nginx:latest

[Install]
WantedBy=multi-user.target default.target

[X-Syslet]
DesiredState=running
`
	u, err := Parse("webapp.container", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if u.Type != UnitTypeContainer {
		t.Errorf("Type = %v", u.Type)
	}
	if u.DesiredState != DesiredStateRunning {
		t.Errorf("DesiredState = %v", u.DesiredState)
	}
}

func TestParseVolumeQuadlet(t *testing.T) {
	content := `[Volume]
Label=app=webapp

[X-Syslet]
`
	u, err := Parse("data.volume", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if u.Type != UnitTypeVolume {
		t.Errorf("Type = %v", u.Type)
	}
	if u.DesiredState != DesiredStateUnspecified {
		t.Errorf("DesiredState = %v", u.DesiredState)
	}
}

func TestParseInvalidDesiredState(t *testing.T) {
	content := `[X-Syslet]
DesiredState=invalid
`
	if _, err := Parse("webapp.container", content); err == nil {
		t.Fatal("expected error for invalid DesiredState")
	}
}

func TestParseUnknownKey(t *testing.T) {
	content := `[X-Syslet]
Unknown=value
`
	if _, err := Parse("webapp.container", content); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestParseUnsupportedType(t *testing.T) {
	content := `[Unit]
Description=My App
`
	if _, err := Parse("myapp.service", content); err == nil {
		t.Fatal("expected error for unsupported .service type")
	}
}

func TestSystemdContent(t *testing.T) {
	content := `[Container]
Image=docker.io/library/nginx:latest

[X-Syslet]
DesiredState=running

[Install]
WantedBy=multi-user.target
`
	u, err := Parse("webapp.container", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	b, _ := io.ReadAll(u.SystemdContent())
	got := string(b)

	// Should not contain X-Syslet
	if strings.Contains(got, "X-Syslet") {
		t.Errorf("SystemdContent still contains X-Syslet:\n%s", got)
	}
	if !strings.Contains(got, "Image") {
		t.Errorf("SystemdContent missing Image:\n%s", got)
	}
	if !strings.Contains(got, "WantedBy") {
		t.Errorf("SystemdContent missing WantedBy:\n%s", got)
	}
}

func TestUnitTypeProperties(t *testing.T) {
	if !UnitTypeContainer.IsStartable() {
		t.Error("Container should be startable")
	}
	if !UnitTypeVolume.IsImmutable() {
		t.Error("Volume should be immutable")
	}
	if !UnitTypeNetwork.IsImmutable() {
		t.Error("Network should be immutable")
	}
	if UnitTypeContainer.IsImmutable() {
		t.Error("Container should not be immutable")
	}
	if UnitTypeVolume.IsStartable() {
		t.Error("Volume should not be startable")
	}
}
