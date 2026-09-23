package validate

import (
	"errors"
	"strings"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
)

func TestValidateUnits_EmptyInputs(t *testing.T) {
	if err := validateUnits(nil, nil, nil); err != nil {
		t.Errorf("validateSpecs(nil, nil, nil) unexpected error: %v", err)
	}
	if err := validateUnits([]model.Unit{}, nil, nil); err != nil {
		t.Errorf("validateSpecs(empty, nil, nil) unexpected error: %v", err)
	}
}

func TestValidateUnits_AllValidatorsExercised(t *testing.T) {
	specs := []model.Unit{
		model.NewContainerUnit(model.ContainerUnitRef("a"), nil, "", nil, false),
		model.NewContainerUnit(model.ContainerUnitRef("b"), nil, "", nil, false),
	}

	calls := map[string]int{}

	if err := validateUnits(
		specs,
		[]CollectionValidator{
			func(_ []model.Unit) error { calls["col1"]++; return nil },
			func(_ []model.Unit) error { calls["col2"]++; return nil },
		},
		[]UnitValidator{
			func(_ model.Unit) error { calls["spec1"]++; return nil },
			func(_ model.Unit) error { calls["spec2"]++; return nil },
		},
	); err != nil {
		t.Fatalf("validateSpecs() unexpected error: %v", err)
	}

	for _, key := range []string{"col1", "col2"} {
		if calls[key] != 1 {
			t.Errorf("collection validator %q called %d times, want 1", key, calls[key])
		}
	}
	for _, key := range []string{"spec1", "spec2"} {
		if calls[key] != len(specs) {
			t.Errorf("spec validator %q called %d times, want %d", key, calls[key], len(specs))
		}
	}
}

func TestValidateUnits_CollectionValidatorError(t *testing.T) {
	sentinel := errors.New("collection error")
	specs := []model.Unit{model.NewContainerUnit(model.ContainerUnitRef("x"), nil, "", nil, false)}

	err := validateUnits(
		specs,
		[]CollectionValidator{func(_ []model.Unit) error { return sentinel }},
		nil,
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestValidateUnits_UnitValidatorError(t *testing.T) {
	sentinel := errors.New("spec error")
	specs := []model.Unit{model.NewContainerUnit(model.ContainerUnitRef("x"), nil, "", nil, false)}

	err := validateUnits(
		specs,
		nil,
		[]UnitValidator{func(_ model.Unit) error { return sentinel }},
	)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestNoXSysletSection_NoXSyslet_ReturnsNil(t *testing.T) {
	opts := model.UnitOptions{
		"Container": {model.SectionKey("Image"): model.UV("nginx:latest")},
		"Service":   {model.SectionKey("Restart"): model.UV("always")},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), opts, "", nil, false)

	if err := NoXSysletSection(spec); err != nil {
		t.Errorf("NoXSysletSection() should not error with valid spec, got: %v", err)
	}
}

func TestNoXSysletSection_ContainerWithXSyslet_ReturnsError(t *testing.T) {
	opts := model.UnitOptions{
		"X-Syslet":  {model.SectionKey("RemovalAllowed"): model.UV("true")},
		"Container": {model.SectionKey("Image"): model.UV("nginx:latest")},
	}
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), opts, "", nil, false)

	err := NoXSysletSection(spec)
	if err == nil {
		t.Fatal("NoXSysletSection() should error when X-Syslet section is provided")
	}
	if !strings.Contains(err.Error(), "X-Syslet") {
		t.Errorf("error should mention 'X-Syslet', got: %v", err)
	}
	if !strings.Contains(err.Error(), "reserved for internal use") {
		t.Errorf("error should mention 'reserved for internal use', got: %v", err)
	}
}

func TestBuildConfigFilenames_NonBuildSpec_ReturnsNil(t *testing.T) {
	spec := model.NewContainerUnit(model.ContainerUnitRef("webapp"), nil, "", nil, false)
	if err := BuildConfigFilenames(spec); err != nil {
		t.Errorf("BuildConfigFilenames() should not error for non-build spec, got: %v", err)
	}
}

func TestBuildConfigFilenames_RelativePath_ReturnsNil(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myimage"),
		nil,
		"FROM scratch",
		[]model.BuildContextFile{{Filename: "config/app.conf"}},
		"",
	)
	if err := BuildConfigFilenames(spec); err != nil {
		t.Errorf("BuildConfigFilenames() should not error for relative filename, got: %v", err)
	}
}

func TestBuildConfigFilenames_EmptyFilename_ReturnsError(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myimage"),
		nil,
		"FROM scratch",
		[]model.BuildContextFile{{Filename: ""}},
		"",
	)
	err := BuildConfigFilenames(spec)
	if err == nil {
		t.Fatal("BuildConfigFilenames() should error for empty filename")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %v", err)
	}
}

func TestBuildConfigFilenames_AbsolutePath_ReturnsError(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myimage"),
		nil,
		"FROM scratch",
		[]model.BuildContextFile{{Filename: "/etc/config.conf"}},
		"",
	)
	err := BuildConfigFilenames(spec)
	if err == nil {
		t.Fatal("BuildConfigFilenames() should error for absolute filename")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should mention 'absolute', got: %v", err)
	}
	if !strings.Contains(err.Error(), "/etc/config.conf") {
		t.Errorf("error should include the offending path, got: %v", err)
	}
}

func TestBuildConfigFilenames_PathTraversal_ReturnsError(t *testing.T) {
	spec := model.NewBuildUnit(
		model.BuildUnitRef("myimage"),
		nil,
		"FROM scratch",
		[]model.BuildContextFile{{Filename: "../secrets/key"}},
		"",
	)
	err := BuildConfigFilenames(spec)
	if err == nil {
		t.Fatal("BuildConfigFilenames() should error for filename containing '..'")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("error should mention '..', got: %v", err)
	}
}

func TestContainerConfigDirMountPaths_AbsolutePath_ReturnsNil(t *testing.T) {
	spec := model.NewContainerUnitWithDirs(
		model.ContainerUnitRef("webapp"), nil, "", nil,
		[]model.ContainerDirMount{model.NewContainerDirMount("/etc/app/")},
		false,
	)
	if err := ContainerConfigDirMountPaths(spec); err != nil {
		t.Errorf("ContainerConfigDirMountPaths() unexpected error: %v", err)
	}
}

func TestContainerConfigDirMountPaths_RelativePath_ReturnsError(t *testing.T) {
	spec := model.NewContainerUnitWithDirs(
		model.ContainerUnitRef("webapp"), nil, "", nil,
		[]model.ContainerDirMount{model.NewContainerDirMount("etc/app/")},
		false,
	)
	err := ContainerConfigDirMountPaths(spec)
	if err == nil {
		t.Fatal("ContainerConfigDirMountPaths() should error for relative mountPath")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should mention 'absolute', got: %v", err)
	}
}

func TestContainerConfigDirMountPaths_EmptyPath_ReturnsError(t *testing.T) {
	spec := model.NewContainerUnitWithDirs(
		model.ContainerUnitRef("webapp"), nil, "", nil,
		[]model.ContainerDirMount{model.NewContainerDirMount("")},
		false,
	)
	err := ContainerConfigDirMountPaths(spec)
	if err == nil {
		t.Fatal("ContainerConfigDirMountPaths() should error for empty mountPath")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %v", err)
	}
}

func TestContainerConfigDirMountPaths_PathTraversal_ReturnsError(t *testing.T) {
	spec := model.NewContainerUnitWithDirs(
		model.ContainerUnitRef("webapp"), nil, "", nil,
		[]model.ContainerDirMount{model.NewContainerDirMount("/etc/../app/")},
		false,
	)
	err := ContainerConfigDirMountPaths(spec)
	if err == nil {
		t.Fatal("ContainerConfigDirMountPaths() should error for mountPath containing '..'")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("error should mention '..', got: %v", err)
	}
}

func TestContainerNoOverlappingMountPaths_NonContainerSpec_ReturnsNil(t *testing.T) {
	spec := model.NewBuildUnit(model.BuildUnitRef("myimage"), nil, "FROM scratch", nil, "")
	if err := ContainerNoOverlappingMountPaths(spec); err != nil {
		t.Errorf("ContainerNoOverlappingMountPaths() should not error for non-container spec, got: %v", err)
	}
}

func TestContainerNoOverlappingMountPaths_DisjointPaths_ReturnsNil(t *testing.T) {
	// /etc/app2 and /etc/app.conf share a string prefix with /etc/app but are
	// siblings, not children, so they must not be reported as overlapping.
	spec := model.NewContainerUnitWithDirs(
		model.ContainerUnitRef("webapp"), nil, "",
		[]model.ContainerFileMount{
			model.NewContainerFileMount("/etc/app.conf", "", 0),
			model.NewContainerFileMount("/run/env", "", 0),
		},
		[]model.ContainerDirMount{
			model.NewContainerDirMount("/etc/app/"),
			model.NewContainerDirMount("/etc/app2/"),
		},
		false,
	)
	if err := ContainerNoOverlappingMountPaths(spec); err != nil {
		t.Errorf("ContainerNoOverlappingMountPaths() unexpected error: %v", err)
	}
}

func TestContainerNoOverlappingMountPaths_OverlapKinds(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		dirs  []string
	}{
		{"DuplicateConfigFiles", []string{"/etc/app.conf", "/etc/app.conf"}, nil},
		{"DuplicateConfigDirs", nil, []string{"/etc/app/", "/etc/app"}},
		{"NestedConfigDirs", nil, []string{"/etc/app/", "/etc/app/conf.d/"}},
		{"NestedConfigDirsReversed", nil, []string{"/etc/app/conf.d/", "/etc/app/"}},
		{"ConfigFileUnderConfigDir", []string{"/etc/app/app.conf"}, []string{"/etc/app/"}},
		{"ConfigFileEqualsConfigDir", []string{"/etc/app"}, []string{"/etc/app/"}},
		{"ConfigDirUnderConfigFile", []string{"/etc/app"}, []string{"/etc/app/conf.d/"}},
		{"RootConfigDir", []string{"/etc/app.conf"}, []string{"/"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var files []model.ContainerFileMount
			for _, f := range tc.files {
				files = append(files, model.NewContainerFileMount(f, "", 0))
			}
			var dirs []model.ContainerDirMount
			for _, d := range tc.dirs {
				dirs = append(dirs, model.NewContainerDirMount(d))
			}
			spec := model.NewContainerUnitWithDirs(model.ContainerUnitRef("webapp"), nil, "", files, dirs, false)
			err := ContainerNoOverlappingMountPaths(spec)
			if err == nil {
				t.Fatalf("ContainerNoOverlappingMountPaths() should error for files=%v dirs=%v", tc.files, tc.dirs)
			}
			if !strings.Contains(err.Error(), "overlap") {
				t.Errorf("error should mention 'overlap', got: %v", err)
			}
		})
	}
}

func TestContainerConfigDirFilenames_PlainName_ReturnsNil(t *testing.T) {
	spec := model.NewContainerUnitWithDirs(
		model.ContainerUnitRef("webapp"), nil, "", nil,
		[]model.ContainerDirMount{model.NewContainerDirMount("/etc/app/",
			model.NewContainerConfigFile("app.conf", "", 0),
			model.NewContainerConfigFile("other.conf", "", 0),
		)},
		false,
	)
	if err := ContainerConfigDirFilenames(spec); err != nil {
		t.Errorf("ContainerConfigDirFilenames() unexpected error: %v", err)
	}
}

func TestContainerConfigDirFilenames_InvalidNames_ReturnError(t *testing.T) {
	cases := []struct{ name, file string }{
		{"path separator", "sub/app.conf"},
		{"dot", "."},
		{"absolute", "/app.conf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := model.NewContainerUnitWithDirs(
				model.ContainerUnitRef("webapp"), nil, "", nil,
				[]model.ContainerDirMount{model.NewContainerDirMount("/etc/app/",
					model.NewContainerConfigFile(tc.file, "", 0),
				)},
				false,
			)
			if err := ContainerConfigDirFilenames(spec); err == nil {
				t.Fatalf("ContainerConfigDirFilenames() should error for file name %q, got nil", tc.file)
			}
		})
	}
}

func TestContainerConfigMountPaths_NonContainerSpec_ReturnsNil(t *testing.T) {
	spec := model.NewBuildUnit(model.BuildUnitRef("myimage"), nil, "FROM scratch", nil, "")
	if err := ContainerConfigMountPaths(spec); err != nil {
		t.Errorf("ContainerConfigMountPaths() should not error for non-container spec, got: %v", err)
	}
}

func TestContainerConfigMountPaths_AbsolutePath_ReturnsNil(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		nil, "",
		[]model.ContainerFileMount{model.NewContainerFileMount("/etc/config.conf", "", 0)},
		false,
	)
	if err := ContainerConfigMountPaths(spec); err != nil {
		t.Errorf("ContainerConfigMountPaths() should not error for absolute mountPath, got: %v", err)
	}
}

func TestContainerConfigMountPaths_EmptyPath_ReturnsError(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		nil, "",
		[]model.ContainerFileMount{{Directory: ""}},
		false,
	)
	err := ContainerConfigMountPaths(spec)
	if err == nil {
		t.Fatal("ContainerConfigMountPaths() should error for empty mountPath")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %v", err)
	}
}

func TestContainerConfigMountPaths_RelativePath_ReturnsError(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		nil, "",
		[]model.ContainerFileMount{model.NewContainerFileMount("etc/config.conf", "", 0)},
		false,
	)
	err := ContainerConfigMountPaths(spec)
	if err == nil {
		t.Fatal("ContainerConfigMountPaths() should error for relative mountPath")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should mention 'absolute', got: %v", err)
	}
	if !strings.Contains(err.Error(), "etc/config.conf") {
		t.Errorf("error should include the offending path, got: %v", err)
	}
}

func TestContainerConfigMountPaths_PathTraversal_ReturnsError(t *testing.T) {
	spec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		nil, "",
		[]model.ContainerFileMount{{Directory: "/etc/..", File: model.ContainerConfigFile{Name: "config.conf"}}},
		false,
	)
	err := ContainerConfigMountPaths(spec)
	if err == nil {
		t.Fatal("ContainerConfigMountPaths() should error for mountPath containing '..'")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("error should mention '..', got: %v", err)
	}
}

func TestValidateSecretKeyNames_ValidKey_ReturnsNil(t *testing.T) {
	s := model.PodmanSecret{Name: "db", Keys: []string{"password", "username", "db-host"}}
	if err := SecretKeyNames(s); err != nil {
		t.Errorf("SecretKeyNames() unexpected error: %v", err)
	}
}

func TestValidateSecretKeyNames_InvalidChars_ReturnsError(t *testing.T) {
	cases := []struct {
		name string
		keys []string
	}{
		{"uppercase", []string{"Password"}},
		{"underscore", []string{"db_password"}},
		{"dot", []string{"db.password"}},
		{"space", []string{"db password"}},
		{"empty", []string{""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := model.PodmanSecret{Name: "db", Keys: tc.keys}
			if err := SecretKeyNames(s); err == nil {
				t.Errorf("SecretKeyNames() should error for keys %v, got nil", tc.keys)
			}
		})
	}
}

func TestValidateSecretReferences_ValidRef_ReturnsNil(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"password"}}}
	container := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Secret"): model.UV("db-password"),
			},
		},
		"", nil, false,
	)
	validator := SecretReferences(secrets)
	if err := validator([]model.Unit{container}); err != nil {
		t.Errorf("SecretReferences() unexpected error: %v", err)
	}
}

func TestValidateSecretReferences_SpecAbsent_ReturnsError(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"password"}}}
	container := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Secret"): model.UV("cache-password"),
			},
		},
		"", nil, false,
	)
	validator := SecretReferences(secrets)
	if err := validator([]model.Unit{container}); err == nil {
		t.Error("SecretReferences() should error for absent spec")
	}
}

func TestValidateSecretReferences_KeyAbsent_ReturnsError(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"password"}}}
	container := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Secret"): model.UV("db-username"),
			},
		},
		"", nil, false,
	)
	validator := SecretReferences(secrets)
	if err := validator([]model.Unit{container}); err == nil {
		t.Error("SecretReferences() should error for absent key")
	}
}

func TestValidateSecretReferences_ParsesNameBeforeComma(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"password"}}}
	container := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Secret"): model.UV("db-password,uid=0,gid=0"),
			},
		},
		"", nil, false,
	)
	validator := SecretReferences(secrets)
	if err := validator([]model.Unit{container}); err != nil {
		t.Errorf("SecretReferences() unexpected error: %v", err)
	}
}

func TestPreRender_WithSecretInvalidKey_ReturnsError(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"INVALID_KEY"}}}
	units := []model.Unit{model.NewContainerUnit(model.ContainerUnitRef("app"), nil, "", nil, false)}
	if err := PreRender(units, secrets); err == nil {
		t.Error("PreRender() should error for secret with invalid key name")
	}
}

func TestPreRender_WithSecretBadReference_ReturnsError(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"password"}}}
	container := model.NewContainerUnit(
		model.ContainerUnitRef("app"),
		model.UnitOptions{
			"Container": {model.SectionKey("Secret"): model.UV("db-missing")},
		},
		"", nil, false,
	)
	if err := PreRender([]model.Unit{container}, secrets); err == nil {
		t.Error("PreRender() should error for container referencing unknown secret key")
	}
}

func TestPreRender_WithValidSecrets_ReturnsNil(t *testing.T) {
	secrets := []model.PodmanSecret{{Name: "db", Keys: []string{"password"}}}
	container := model.NewContainerUnit(
		model.ContainerUnitRef("app"),
		model.UnitOptions{
			"Container": {model.SectionKey("Secret"): model.UV("db-password")},
		},
		"", nil, false,
	)
	if err := PreRender([]model.Unit{container}, secrets); err != nil {
		t.Errorf("PreRender() unexpected error: %v", err)
	}
}
