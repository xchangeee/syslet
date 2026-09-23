package loader

import (
	"os"
	"testing"

	"github.com/xchangeee/syslet/internal/api"
	v1 "github.com/xchangeee/syslet/internal/api/v1"
	"github.com/xchangeee/syslet/internal/model"
)

// --- version dispatch ---

// TestParse_ConvertsV1 verifies that the public entry points pick up the specs
// of the v1 field of api.LoadResult.
func TestParse_ConvertsV1(t *testing.T) {
	raw := api.LoadResult{V1: v1.Specs{
		Containers: []v1.Container{{Name: "c", Unit: noOpts}},
		Volumes:    []v1.Volume{{Name: "v", Unit: noOpts}},
	}}
	units, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Errorf("expected 2 units, got %d", len(units))
	}
	secrets, err := ParseSecrets(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(secrets) != 0 {
		t.Errorf("expected 0 secrets, got %d", len(secrets))
	}
}

// noOpts is an empty but present unit map; a spec without one is rejected.
var noOpts = map[string]map[string]any{}

// --- desiredState parsing ---

// TestDesiredState_Omitted_ReturnsRunning verifies that an omitted desiredState
// matches the CUE default in #SysdefDefaults.
func TestDesiredState_Omitted_ReturnsRunning(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{Name: "c", Unit: noOpts}}})
	if err != nil {
		t.Fatal(err)
	}
	c := units[0].(*model.ContainerUnit)
	if c.DesiredState != model.DesiredStateRunning {
		t.Errorf("expected DesiredStateRunning, got %q", c.DesiredState)
	}
}

func TestDesiredState_AllValues(t *testing.T) {
	cases := []struct {
		input string
		want  model.DesiredState
	}{
		{"stopped", model.DesiredStateStopped},
		{"running", model.DesiredStateRunning},
		{"oneshot", model.DesiredStateOneshot},
		{"Running", model.DesiredStateRunning}, // case-insensitive
		{"STOPPED", model.DesiredStateStopped},
	}
	for _, tc := range cases {
		units, err := parseV1(v1.Specs{Containers: []v1.Container{{Name: "c", Unit: noOpts, DesiredState: tc.input}}})
		if err != nil {
			t.Errorf("input %q: unexpected error: %v", tc.input, err)
			continue
		}
		got := units[0].(*model.ContainerUnit).DesiredState
		if got != tc.want {
			t.Errorf("input %q: got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestDesiredState_InvalidReturnsError(t *testing.T) {
	_, err := parseV1(v1.Specs{Containers: []v1.Container{{Name: "c", Unit: noOpts, DesiredState: "restart"}}})
	if err == nil {
		t.Fatal("expected error for invalid desiredState")
	}
}

// --- removalAllowed parsing ---

// TestRemovalAllowed_Omitted_ReturnsTrue verifies that an omitted
// removalAllowed matches the CUE default in #SysdefDefaults.
func TestRemovalAllowed_Omitted_ReturnsTrue(t *testing.T) {
	units, err := parseV1(v1.Specs{
		Containers: []v1.Container{{Name: "c", Unit: noOpts}},
		Volumes:    []v1.Volume{{Name: "v", Unit: noOpts}},
		Networks:   []v1.Network{{Name: "n", Unit: noOpts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !units[0].(*model.ContainerUnit).RemovalAllowed {
		t.Error("Container: expected RemovalAllowed true")
	}
	if !units[1].(*model.VolumeUnit).RemovalAllowed {
		t.Error("Volume: expected RemovalAllowed true")
	}
	if !units[2].(*model.NetworkUnit).RemovalAllowed {
		t.Error("Network: expected RemovalAllowed true")
	}
}

func TestRemovalAllowed_ExplicitFalse_ReturnsFalse(t *testing.T) {
	units, err := parseV1(v1.Specs{Volumes: []v1.Volume{{Name: "v", Unit: noOpts, RemovalAllowed: new(false)}}})
	if err != nil {
		t.Fatal(err)
	}
	if units[0].(*model.VolumeUnit).RemovalAllowed {
		t.Error("expected RemovalAllowed false")
	}
}

// --- reclaimPolicy parsing ---

func TestReclaimPolicy_Omitted_ReturnsDelete(t *testing.T) {
	cases := []struct {
		name  string
		specs v1.Specs
		get   func(model.Unit) model.ReclaimPolicy
	}{
		{"Volume", v1.Specs{Volumes: []v1.Volume{{Name: "v", Unit: noOpts}}},
			func(u model.Unit) model.ReclaimPolicy { return u.(*model.VolumeUnit).ReclaimPolicy }},
		{"Network", v1.Specs{Networks: []v1.Network{{Name: "n", Unit: noOpts}}},
			func(u model.Unit) model.ReclaimPolicy { return u.(*model.NetworkUnit).ReclaimPolicy }},
		{"Build", v1.Specs{Builds: []v1.Build{{Name: "b", Unit: noOpts}}},
			func(u model.Unit) model.ReclaimPolicy { return u.(*model.BuildUnit).ReclaimPolicy }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			units, err := parseV1(tc.specs)
			if err != nil {
				t.Fatal(err)
			}
			if got := tc.get(units[0]); got != model.ReclaimPolicyDelete {
				t.Errorf("expected ReclaimPolicyDelete, got %q", got)
			}
		})
	}
}

func TestReclaimPolicy_Retain_ReturnsRetain(t *testing.T) {
	units, err := parseV1(v1.Specs{Volumes: []v1.Volume{{Name: "v", Unit: noOpts, ReclaimPolicy: "Retain"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := units[0].(*model.VolumeUnit).ReclaimPolicy; got != model.ReclaimPolicyRetain {
		t.Errorf("expected ReclaimPolicyRetain, got %q", got)
	}
}

func TestReclaimPolicy_Delete(t *testing.T) {
	units, err := parseV1(v1.Specs{Volumes: []v1.Volume{{Name: "v", Unit: noOpts, ReclaimPolicy: "Delete"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := units[0].(*model.VolumeUnit).ReclaimPolicy; got != model.ReclaimPolicyDelete {
		t.Errorf("expected ReclaimPolicyDelete, got %q", got)
	}
}

func TestReclaimPolicy_InvalidReturnsError(t *testing.T) {
	_, err := parseV1(v1.Specs{Volumes: []v1.Volume{{Name: "v", Unit: noOpts, ReclaimPolicy: "GC"}}})
	if err == nil {
		t.Fatal("expected error for invalid reclaimPolicy")
	}
}

// --- mode/perm parsing ---

func TestMode_DefaultsTo0644(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name:        "c",
		Unit:        noOpts,
		ConfigFiles: []v1.ConfigFileEntry{{MountPath: "/etc/f", Content: "x"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := units[0].(*model.ContainerUnit).FileMounts[0]
	if cfg.File.Mode != 0644 {
		t.Errorf("expected mode 0644, got %04o", cfg.File.Mode)
	}
}

func TestMode_ExplicitOctal(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name:        "c",
		Unit:        noOpts,
		ConfigFiles: []v1.ConfigFileEntry{{MountPath: "/etc/f", Mode: "0600", Content: "x"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := units[0].(*model.ContainerUnit).FileMounts[0]
	if cfg.File.Mode != os.FileMode(0600) {
		t.Errorf("expected mode 0600, got %04o", cfg.File.Mode)
	}
}

func TestMode_InvalidOctalReturnsError(t *testing.T) {
	_, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name:        "c",
		Unit:        noOpts,
		ConfigFiles: []v1.ConfigFileEntry{{MountPath: "/etc/f", Mode: "rwx", Content: "x"}},
	}}})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

// --- field mapping ---

func TestContainerUnit_FieldsAreMapped(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name:           "web",
		Unit:           noOpts,
		DesiredState:   "running",
		RemovalAllowed: new(true),
		ConfigFiles: []v1.ConfigFileEntry{
			{MountPath: "/etc/app.conf", Mode: "0640", Content: "cfg"},
		},
		ConfigDirs: []v1.ConfigDirEntry{{
			MountPath: "/etc/app/",
			Files:     []v1.ConfigDirFile{{Name: "a.conf", Content: "ac", Mode: "0600"}},
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	c := units[0].(*model.ContainerUnit)
	if c.Ref().Name() != "web" {
		t.Errorf("Name: got %q", c.Ref())
	}
	if c.DesiredState != model.DesiredStateRunning {
		t.Errorf("DesiredState: got %q", c.DesiredState)
	}
	if !c.RemovalAllowed {
		t.Error("RemovalAllowed: expected true")
	}
	cfg := c.FileMounts[0]
	if cfg.FullPath() != "/etc/app.conf" || cfg.File.Content != "cfg" || cfg.File.Mode != 0640 {
		t.Errorf("Config: got mountPath=%q content=%q mode=%04o", cfg.FullPath(), cfg.File.Content, cfg.File.Mode)
	}
	dir := c.DirMounts[0]
	if dir.Directory != "/etc/app/" {
		t.Errorf("ConfigDir mountPath: got %q", dir.Directory)
	}
	f := dir.Files[0]
	if f.Name != "a.conf" || f.Content != "ac" || f.Mode != 0600 {
		t.Errorf("ConfigDir file: got name=%q content=%q mode=%04o", f.Name, f.Content, f.Mode)
	}
}

func TestBuildUnit_FieldsAreMapped(t *testing.T) {
	units, err := parseV1(v1.Specs{Builds: []v1.Build{{
		Name:          "img",
		Unit:          noOpts,
		Containerfile: "FROM scratch",
		ReclaimPolicy: "Delete",
		ContextFiles: []v1.BuildFileEntry{
			{Filename: "app.conf", Mode: "0600", Content: "c"},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	b := units[0].(*model.BuildUnit)
	if b.Ref().Name() != "img" {
		t.Errorf("Name: got %q", b.Ref())
	}
	if b.Containerfile != "FROM scratch" {
		t.Errorf("Containerfile: got %q", b.Containerfile)
	}
	if b.ReclaimPolicy != model.ReclaimPolicyDelete {
		t.Errorf("ReclaimPolicy: got %q", b.ReclaimPolicy)
	}
	cfg := b.ContextFiles[0]
	if cfg.Filename != "app.conf" || cfg.Content != "c" || cfg.Mode != 0600 {
		t.Errorf("Config: got filename=%q content=%q mode=%04o", cfg.Filename, cfg.Content, cfg.Mode)
	}
}

func TestVolumeUnit_FieldsAreMapped(t *testing.T) {
	units, err := parseV1(v1.Specs{Volumes: []v1.Volume{{
		Name:           "data",
		Unit:           noOpts,
		RemovalAllowed: new(true),
		ReclaimPolicy:  "Delete",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	v := units[0].(*model.VolumeUnit)
	if v.Ref().Name() != "data" {
		t.Errorf("Name: got %q", v.Ref())
	}
	if !v.RemovalAllowed {
		t.Error("RemovalAllowed: expected true")
	}
	if v.ReclaimPolicy != model.ReclaimPolicyDelete {
		t.Errorf("ReclaimPolicy: got %q", v.ReclaimPolicy)
	}
}

func TestNetworkUnit_FieldsAreMapped(t *testing.T) {
	units, err := parseV1(v1.Specs{Networks: []v1.Network{{
		Name:           "net",
		Unit:           noOpts,
		RemovalAllowed: new(true),
		ReclaimPolicy:  "Delete",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	n := units[0].(*model.NetworkUnit)
	if n.Ref().Name() != "net" {
		t.Errorf("Name: got %q", n.Ref())
	}
	if !n.RemovalAllowed {
		t.Error("RemovalAllowed: expected true")
	}
	if n.ReclaimPolicy != model.ReclaimPolicyDelete {
		t.Errorf("ReclaimPolicy: got %q", n.ReclaimPolicy)
	}
}

// --- unitOptions conversion ---

func TestUnitOptions_StringValue(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name: "c",
		Unit: map[string]map[string]any{"Container": {"Image": "nginx:latest"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	opts := units[0].Options()
	v, ok := opts["Container"]["Image"]
	if !ok {
		t.Fatal("expected Container.Image in options")
	}
	if vals := v.Values(); len(vals) != 1 || vals[0] != "nginx:latest" {
		t.Errorf("Container.Image: got %v", vals)
	}
}

func TestUnitOptions_StringArrayValue(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name: "c",
		Unit: map[string]map[string]any{"Container": {"Volume": []any{"data:/data", "cfg:/cfg"}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	v := units[0].Options()["Container"]["Volume"]
	if vals := v.Values(); len(vals) != 2 || vals[0] != "data:/data" || vals[1] != "cfg:/cfg" {
		t.Errorf("Container.Volume: got %v", vals)
	}
}

func TestUnitOptions_InvalidValueReturnsError(t *testing.T) {
	_, err := parseV1(v1.Specs{Containers: []v1.Container{{
		Name: "c",
		Unit: map[string]map[string]any{"Container": {"Image": 42}},
	}}})
	if err == nil {
		t.Fatal("expected error for non-string unit option value")
	}
}

// TestUnitOptions_MissingUnit_ReturnsError verifies that every spec type
// rendering to a quadlet unit requires a unit map, matching unit! in #UnitSpec.
func TestUnitOptions_MissingUnit_ReturnsError(t *testing.T) {
	cases := map[string]v1.Specs{
		"Container": {Containers: []v1.Container{{Name: "c"}}},
		"Volume":    {Volumes: []v1.Volume{{Name: "v"}}},
		"Network":   {Networks: []v1.Network{{Name: "n"}}},
		"Build":     {Builds: []v1.Build{{Name: "b"}}},
	}
	for name, specs := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseV1(specs); err == nil {
				t.Fatal("expected error for missing unit")
			}
		})
	}
}

func TestUnitOptions_EmptyUnit_ReturnsNoOptions(t *testing.T) {
	units, err := parseV1(v1.Specs{Containers: []v1.Container{{Name: "c", Unit: noOpts}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(units[0].Options()) != 0 {
		t.Errorf("expected no options, got %v", units[0].Options())
	}
}

// --- mixed unit types ---

func TestParse_AllUnitTypesProduced(t *testing.T) {
	units, err := parseV1(v1.Specs{
		Containers: []v1.Container{{Name: "c", Unit: noOpts}},
		Volumes:    []v1.Volume{{Name: "v", Unit: noOpts}},
		Networks:   []v1.Network{{Name: "n", Unit: noOpts}},
		Builds:     []v1.Build{{Name: "b", Unit: noOpts}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 4 {
		t.Fatalf("expected 4 units, got %d", len(units))
	}
}
