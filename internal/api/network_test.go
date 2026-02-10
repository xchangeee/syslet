package api

import (
	"bytes"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/systemd"
	gounit "github.com/coreos/go-systemd/v22/unit"
	"github.com/spf13/afero"
)

func TestNetworkSpec_GetMethods(t *testing.T) {
	spec := &NetworkSpec{
		Name: "frontend",
		Unit: map[string]map[string]any{
			"Network": {"Driver": "bridge"},
		},
	}

	if spec.GetName() != "frontend" {
		t.Errorf("expected name 'frontend', got %q", spec.GetName())
	}
	if spec.GetType() != SpecTypeNetwork {
		t.Errorf("expected type %q, got %q", SpecTypeNetwork, spec.GetType())
	}
	if spec.FullUnitName() != "frontend.network" {
		t.Errorf("expected 'frontend.network', got %q", spec.FullUnitName())
	}
}

func TestNetworkSpec_Render_BasicNetwork(t *testing.T) {
	spec := &NetworkSpec{
		Name: "frontend",
		Unit: map[string]map[string]any{
			"Network": {"Driver": "bridge"},
		},
	}

	rendered, err := spec.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify NetworkName is set
	hasNetworkName := false
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Network" && opt.Name == "NetworkName" && opt.Value == "frontend" {
			hasNetworkName = true
		}
	}
	if !hasNetworkName {
		t.Error("expected NetworkName=frontend to be set")
	}
}

func TestNetworkSpec_Render_WithReclaimPolicy(t *testing.T) {
	spec := &NetworkSpec{
		Name: "frontend",
		Unit: map[string]map[string]any{
			"Network": {"Driver": "bridge"},
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

func TestNetworkSpec_Render_NetworkNameNotOverridden(t *testing.T) {
	spec := &NetworkSpec{
		Name: "frontend",
		Unit: map[string]map[string]any{
			"Network": {
				"NetworkName": "custom-network", // User-specified name
			},
		},
	}

	rendered, err := spec.Render()
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Verify user-specified NetworkName is preserved
	networkNameCount := 0
	for _, opt := range rendered.UnitOptions {
		if opt.Section == "Network" && opt.Name == "NetworkName" {
			networkNameCount++
			if opt.Value != "custom-network" {
				t.Errorf("expected custom-network, got %q", opt.Value)
			}
		}
	}
	if networkNameCount != 1 {
		t.Errorf("expected exactly 1 NetworkName, got %d", networkNameCount)
	}
}

func TestNetworkSpec_ShouldDeleteOnRemoval_WithDelete(t *testing.T) {
	spec := &NetworkSpec{Name: "frontend"}

	// Create a unit file with ReclaimPolicy=Delete
	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content with X-Syslet section
	opts := []*gounit.UnitOption{
		{Section: "X-Syslet", Name: "ReclaimPolicy", Value: "Delete"},
		{Section: "Network", Name: "NetworkName", Value: "frontend"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("frontend.network", contentBytes.Bytes())

	if !spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return true for Delete policy")
	}
}

func TestNetworkSpec_ShouldDeleteOnRemoval_WithRetain(t *testing.T) {
	spec := &NetworkSpec{Name: "frontend"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content with ReclaimPolicy=Retain
	opts := []*gounit.UnitOption{
		{Section: "X-Syslet", Name: "ReclaimPolicy", Value: "Retain"},
		{Section: "Network", Name: "NetworkName", Value: "frontend"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("frontend.network", contentBytes.Bytes())

	if spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return false for Retain policy")
	}
}

func TestNetworkSpec_ShouldDeleteOnRemoval_NoPolicy(t *testing.T) {
	spec := &NetworkSpec{Name: "frontend"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content without ReclaimPolicy
	opts := []*gounit.UnitOption{
		{Section: "Network", Name: "NetworkName", Value: "frontend"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("frontend.network", contentBytes.Bytes())

	if spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return false when no policy is set")
	}
}

func TestNetworkSpec_ShouldDeleteOnRemoval_FileNotFound(t *testing.T) {
	spec := &NetworkSpec{Name: "nonexistent"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	if spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to return false when file doesn't exist")
	}
}

func TestNetworkSpec_ShouldDeleteOnRemoval_CaseInsensitive(t *testing.T) {
	spec := &NetworkSpec{Name: "frontend"}

	fs := afero.NewMemMapFs()
	mockConn := &mockDBusConn{}
	sd := systemd.NewClient(mockConn, fs)

	// Create unit content with lowercase "delete"
	opts := []*gounit.UnitOption{
		{Section: "X-Syslet", Name: "ReclaimPolicy", Value: "delete"},
		{Section: "Network", Name: "NetworkName", Value: "frontend"},
	}
	content := gounit.Serialize(opts)
	contentBytes := new(bytes.Buffer)
	contentBytes.ReadFrom(content)

	sd.WriteUnitFile("frontend.network", contentBytes.Bytes())

	if !spec.ShouldDeleteOnRemoval(sd) {
		t.Error("expected ShouldDeleteOnRemoval to be case-insensitive")
	}
}
