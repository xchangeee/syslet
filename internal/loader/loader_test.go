package loader

import (
	"os"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/model"
)

// --- desiredState parsing ---

func TestDesiredState_DefaultsToStopped(t *testing.T) {
	units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{Name: "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	c := units[0].(*model.ContainerUnit)
	if c.DesiredState != model.DesiredStateStopped {
		t.Errorf("expected DesiredStateStopped, got %q", c.DesiredState)
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
		units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{Name: "c", DesiredState: tc.input}}})
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
	_, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{Name: "c", DesiredState: "restart"}}})
	if err == nil {
		t.Fatal("expected error for invalid desiredState")
	}
}

// --- reclaimPolicy parsing ---

func TestReclaimPolicy_DefaultsToRetain(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (model.ReclaimPolicy, error)
	}{
		{"volume", func() (model.ReclaimPolicy, error) {
			units, err := Parse(api.LoadResult{Volumes: []api.RawVolumeSpec{{Name: "v"}}})
			if err != nil {
				return "", err
			}
			return units[0].(*model.VolumeUnit).ReclaimPolicy, nil
		}},
		{"network", func() (model.ReclaimPolicy, error) {
			units, err := Parse(api.LoadResult{Networks: []api.RawNetworkSpec{{Name: "n"}}})
			if err != nil {
				return "", err
			}
			return units[0].(*model.NetworkUnit).ReclaimPolicy, nil
		}},
		{"build", func() (model.ReclaimPolicy, error) {
			units, err := Parse(api.LoadResult{Builds: []api.RawBuildSpec{{Name: "b"}}})
			if err != nil {
				return "", err
			}
			return units[0].(*model.BuildUnit).ReclaimPolicy, nil
		}},
	}
	for _, tc := range cases {
		policy, err := tc.fn()
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if policy != model.ReclaimPolicyRetain {
			t.Errorf("%s: expected ReclaimPolicyRetain, got %q", tc.name, policy)
		}
	}
}

func TestReclaimPolicy_Delete(t *testing.T) {
	units, err := Parse(api.LoadResult{Volumes: []api.RawVolumeSpec{{Name: "v", ReclaimPolicy: "Delete"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := units[0].(*model.VolumeUnit).ReclaimPolicy; got != model.ReclaimPolicyDelete {
		t.Errorf("expected ReclaimPolicyDelete, got %q", got)
	}
}

func TestReclaimPolicy_InvalidReturnsError(t *testing.T) {
	_, err := Parse(api.LoadResult{Volumes: []api.RawVolumeSpec{{Name: "v", ReclaimPolicy: "GC"}}})
	if err == nil {
		t.Fatal("expected error for invalid reclaimPolicy")
	}
}

// --- mode/perm parsing ---

func TestMode_DefaultsTo0644(t *testing.T) {
	units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
		Name:        "c",
		ConfigFiles: []api.RawConfigFileEntry{{MountPath: "/etc/f", Content: "x"}},
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
	units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
		Name:        "c",
		ConfigFiles: []api.RawConfigFileEntry{{MountPath: "/etc/f", Mode: "0600", Content: "x"}},
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
	_, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
		Name:        "c",
		ConfigFiles: []api.RawConfigFileEntry{{MountPath: "/etc/f", Mode: "rwx", Content: "x"}},
	}}})
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

// --- field mapping ---

func TestContainerUnit_FieldsAreMapped(t *testing.T) {
	units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
		Name:           "web",
		DesiredState:   "running",
		RemovalAllowed: true,
		ConfigFiles: []api.RawConfigFileEntry{
			{MountPath: "/etc/app.conf", Mode: "0640", Content: "cfg"},
		},
		ConfigDirs: []api.RawConfigDirEntry{{
			MountPath: "/etc/app/",
			Files:     []api.RawConfigDirFile{{Name: "a.conf", Content: "ac", Mode: "0600"}},
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
	units, err := Parse(api.LoadResult{Builds: []api.RawBuildSpec{{
		Name:          "img",
		Containerfile: "FROM scratch",
		ReclaimPolicy: "Delete",
		ContextFiles: []api.RawBuildFileEntry{
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
	units, err := Parse(api.LoadResult{Volumes: []api.RawVolumeSpec{{
		Name:           "data",
		RemovalAllowed: true,
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
	units, err := Parse(api.LoadResult{Networks: []api.RawNetworkSpec{{
		Name:           "net",
		RemovalAllowed: true,
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
	units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
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
	units, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
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
	_, err := Parse(api.LoadResult{Containers: []api.RawContainerSpec{{
		Name: "c",
		Unit: map[string]map[string]any{"Container": {"Image": 42}},
	}}})
	if err == nil {
		t.Fatal("expected error for non-string unit option value")
	}
}

// --- mixed unit types ---

func TestParse_AllUnitTypesProduced(t *testing.T) {
	units, err := Parse(api.LoadResult{
		Containers: []api.RawContainerSpec{{Name: "c"}},
		Volumes:    []api.RawVolumeSpec{{Name: "v"}},
		Networks:   []api.RawNetworkSpec{{Name: "n"}},
		Builds:     []api.RawBuildSpec{{Name: "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 4 {
		t.Fatalf("expected 4 units, got %d", len(units))
	}
}
