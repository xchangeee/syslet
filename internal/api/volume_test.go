package api

import (
	"bytes"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/systemd"
	gounit "github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

func TestVolumeSpec_GetMethods(t *testing.T) {
	spec := &VolumeSpec{
		Name: "data",
		Unit: map[string]map[string]any{
			"Volume": {"Device": "tmpfs"},
		},
	}

	if spec.GetName() != "data" {
		t.Errorf("expected name 'data', got %q", spec.GetName())
	}
	if spec.GetType() != SpecTypeVolume {
		t.Errorf("expected type %q, got %q", SpecTypeVolume, spec.GetType())
	}
	if spec.FullUnitName() != "data.volume" {
		t.Errorf("expected 'data.volume', got %q", spec.FullUnitName())
	}
}

func TestVolumeSpec_Render_BasicVolume(t *testing.T) {
	spec := &VolumeSpec{
		Name: "data",
		Unit: map[string]map[string]any{
			"Volume": {"Device": "tmpfs"},
		},
	}

	rendered, err := spec.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify VolumeName is set
	hasVolumeName := false
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Volume" && opt.Name == "VolumeName" && opt.Value == "data" {
			hasVolumeName = true
		}
	}
	if !hasVolumeName {
		t.Error("expected VolumeName=data to be set")
	}
}

func TestVolumeSpec_Render_WithReclaimPolicy(t *testing.T) {
	spec := &VolumeSpec{
		Name: "data",
		Unit: map[string]map[string]any{
			"Volume": {"Device": "tmpfs"},
		},
		ReclaimPolicy: "Delete",
	}

	rendered, err := spec.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify ReclaimPolicy is in X-Syslet section and comes first
	if len(rendered.UnitOptions) == 0 {
		t.Fatal("expected at least one unit option")
	}
	firstOpt := rendered.UnitOptions[0]
	if firstOpt.Section != "X-Syslet" || firstOpt.Name != "ReclaimPolicy" || firstOpt.Value != "Delete" {
		t.Errorf("expected first option to be X-Syslet.ReclaimPolicy=Delete, got %v.%v=%v",
			firstOpt.Section, firstOpt.Name, firstOpt.Value)
	}
}

func TestVolumeSpec_Render_VolumeNameNotOverridden(t *testing.T) {
	spec := &VolumeSpec{
		Name: "data",
		Unit: map[string]map[string]any{
			"Volume": {
				"VolumeName": "custom-volume", // User-specified name
			},
		},
	}

	rendered, err := spec.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify user-specified VolumeName is preserved
	volumeNameCount := 0
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Volume" && opt.Name == "VolumeName" {
			volumeNameCount++
			if opt.Value != "custom-volume" {
				t.Errorf("expected custom-volume, got %q", opt.Value)
			}
		}
	}
	if volumeNameCount != 1 {
		t.Errorf("expected exactly 1 VolumeName, got %d", volumeNameCount)
	}
}

func TestVolumeSpec_ShouldDeleteOnRemoval_WithDelete(t *testing.T) {
	spec := &VolumeSpec{Name: "data"}

	// Create a unit file with ReclaimPolicy=Delete
	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content with X-Syslet section
	opts := []*gounit.UnitOption{
		{Section: "X-Syslet", Name: "ReclaimPolicy", Value: "Delete"},
		{Section: "Volume", Name: "VolumeName", Value: "data"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("data.volume", contentBytes.Bytes())

	if !spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return true for Delete policy")
	}
}

func TestVolumeSpec_ShouldDeleteOnRemoval_WithRetain(t *testing.T) {
	spec := &VolumeSpec{Name: "data"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content with ReclaimPolicy=Retain
	opts := []*gounit.UnitOption{
		{Section: "X-Syslet", Name: "ReclaimPolicy", Value: "Retain"},
		{Section: "Volume", Name: "VolumeName", Value: "data"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("data.volume", contentBytes.Bytes())

	if spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return false for Retain policy")
	}
}

func TestVolumeSpec_ShouldDeleteOnRemoval_NoPolicy(t *testing.T) {
	spec := &VolumeSpec{Name: "data"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content without ReclaimPolicy
	opts := []*gounit.UnitOption{
		{Section: "Volume", Name: "VolumeName", Value: "data"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("data.volume", contentBytes.Bytes())

	if spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return false when no policy is set")
	}
}

func TestVolumeSpec_ShouldDeleteOnRemoval_FileNotFound(t *testing.T) {
	spec := &VolumeSpec{Name: "nonexistent"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	if spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return false when file doesn't exist")
	}
}

func TestVolumeSpec_ShouldDeleteOnRemoval_CaseInsensitive(t *testing.T) {
	spec := &VolumeSpec{Name: "data"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content with uppercase "DELETE"
	opts := []*gounit.UnitOption{
		{Section: "X-Syslet", Name: "ReclaimPolicy", Value: "DELETE"},
		{Section: "Volume", Name: "VolumeName", Value: "data"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("data.volume", contentBytes.Bytes())

	if !spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to be case-insensitive")
	}
}
