package syslet

import (
	"bytes"
	"strings"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"

	"github.com/spf13/afero"
)

// TestDisplayPlan_SecretChanges verifies that DisplayPlan groups secret upserts and
// deletes by spec name, showing plaintext new values and "(secret)" for old values.
func TestDisplayPlan_SecretChanges(t *testing.T) {
	tests := []struct {
		name       string
		upserts    []UpsertPodmanSecretOp
		deletes    []DeletePodmanSecretOp
		wantOutput string
	}{
		{
			name: "UpsertAndDelete",
			upserts: []UpsertPodmanSecretOp{
				{SpecName: "myapp", Name: "myapp-db-password", Value: model.Plaintext("s3cr3t"), Labels: map[string]string{"syslet/hash": "abc123"}},
				{SpecName: "myapp", Name: "myapp-api-key", Value: model.Plaintext("newkey"), Labels: map[string]string{"syslet/hash": "def456"}},
			},
			deletes: []DeletePodmanSecretOp{
				{SpecName: "myapp", Name: "myapp-old-token"},
			},
			wantOutput: `
Secret changes:
  myapp:
    - old-token=(secret)
    + db-password=s3cr3t
    + api-key=newkey

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
		{
			name: "UpsertOnly",
			upserts: []UpsertPodmanSecretOp{
				{SpecName: "infra", Name: "infra-cert", Value: model.Plaintext("pem-data"), Labels: map[string]string{"syslet/hash": "aaa"}},
			},
			wantOutput: `
Secret changes:
  infra:
    + cert=pem-data

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
		{
			name: "DeleteOnly",
			deletes: []DeletePodmanSecretOp{
				{SpecName: "infra", Name: "infra-old-cert"},
			},
			wantOutput: `
Secret changes:
  infra:
    - old-cert=(secret)

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
		{
			name: "MultipleSpecs",
			upserts: []UpsertPodmanSecretOp{
				{SpecName: "app1", Name: "app1-token", Value: model.Plaintext("tok1"), Labels: map[string]string{"syslet/hash": "h1"}},
				{SpecName: "app2", Name: "app2-key", Value: model.Plaintext("key2"), Labels: map[string]string{"syslet/hash": "h2"}},
			},
			wantOutput: `
Secret changes:
  app1:
    + token=tok1
  app2:
    + key=key2

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &ApplyPlan{
				UpsertPodmanSecrets: tt.upserts,
				DeletePodmanSecrets: tt.deletes,
			}
			var buf bytes.Buffer
			DisplayPlan(&buf, plan)
			if got := buf.String(); got != tt.wantOutput {
				t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", tt.wantOutput, got)
			}
		})
	}
}

// TestValidationErrorsInPlan verifies that ValidateUnits failures are recorded
// in the plan (not returned as a fatal error), and that DisplayPlan suppresses
// the diff when the plan contains errors.
func TestValidationErrorsInPlan(t *testing.T) {
	// X-Syslet section is reserved; NoXSysletSection in ValidateUnits rejects this.
	badSpec := model.NewContainerUnit(
		model.ContainerUnitRef("myapp"),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV("nginx:latest")},
			"X-Syslet":  {model.SectionKey("SomeKey"): model.UV("value")},
		},
		model.DesiredStateRunning, nil, false,
	)

	fs := afero.NewMemMapFs()
	ctx, sd, _, _, zipPath := setupTestWithFS(t, fs, testFixture{
		specs: []model.Unit{badSpec},
	})

	mgrs := newTestFileManagers(fs)
	plan, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan returned fatal error (want nil): %v", err)
	}
	if plan == nil {
		t.Fatal("BuildPlan returned nil plan")
	}
	if !plan.HasErrors() {
		t.Error("plan.HasErrors() == false, want true")
	}

	var buf bytes.Buffer
	DisplayPlan(&buf, plan)
	out := buf.String()

	if strings.Contains(out, "Unit file changes:") {
		t.Error("DisplayPlan must not show diff when plan has errors")
	}
	if !strings.Contains(out, "X-Syslet") {
		t.Errorf("DisplayPlan should include the validation error message, got:\n%s", out)
	}
}

// TestStagingErrorsSuppressDiff verifies that when staging validation records
// errors on an otherwise fully-built plan (with unit file changes), DisplayPlan
// suppresses the diff and shows only the errors.
func TestStagingErrorsSuppressDiff(t *testing.T) {
	spec := makeContainerSpec("webapp", "nginx:alpine", model.DesiredStateRunning)
	oldSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)

	fs := afero.NewMemMapFs()
	ctx, sd, _, _, zipPath := setupTestWithFS(t, fs, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, oldSpec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	az := &systemd.MockSystemdAnalyzeRunner{
		Result: systemd.AnalyzeResult{
			ExitCode: 0,
			Stderr:   "Invalid memory limit 'asd', ignoring: Invalid argument",
		},
	}
	// Generator writes the generated service file so staging has something to verify.
	gen := &testutil.MockQuadletGenerator{
		Fs:    fs,
		Files: map[string]string{"webapp.service": "[Service]\nExecStart=podman start webapp\n"},
	}

	mgrs := newTestFileManagers(fs)
	plan, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, gen, az, nil, nil, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan returned fatal error: %v", err)
	}
	if !plan.HasErrors() {
		t.Error("plan.HasErrors() == false, want true")
	}

	var buf bytes.Buffer
	DisplayPlan(&buf, plan)
	out := buf.String()

	if strings.Contains(out, "Unit file changes:") {
		t.Error("DisplayPlan must not show diff when plan has errors")
	}
	if !strings.Contains(out, "Invalid memory limit") {
		t.Errorf("DisplayPlan should show the staging error, got:\n%s", out)
	}
}

// TestDisplayDiff_ConfigDir verifies that DisplayPlan shows per-file unified diffs
// for configDir changes. Requires a real OS filesystem because versioned dirs use symlinks.
func TestDisplayDiff_ConfigDir(t *testing.T) {
	const mountPath = "/etc/app/config"

	store := newConfigStore(t)

	oldFile := model.NewContainerConfigFile("app.conf", "key=old\n", 0644)
	newFile := model.NewContainerConfigFile("app.conf", "key=new\n", 0644)

	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(mountPath, newFile))

	ctx, memFs, sd, _, mgrs, zipPath := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFile)

	plan, err := BuildPlan(ctx, memFs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	var buf bytes.Buffer
	DisplayPlan(&buf, plan)
	out := buf.String()

	wantOutput := `
ConfigDir changes:
  webapp:/etc/app/config → version 2
--- app.conf (current)
+++ app.conf (new)
@@ -1 +1 @@
-key=old
+key=new

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    configDir updated, reloaded (desired: running)
`

	if out != wantOutput {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", wantOutput, out)
	}
}

// TestDisplayDiff_ConfigDir_ModeChange verifies that DisplayPlan shows a mode-change
// line when only the file permission bits change (content is identical).
func TestDisplayDiff_ConfigDir_ModeChange(t *testing.T) {
	const mountPath = "/etc/app/config"

	store := newConfigStore(t)

	content := "key=value\n"
	oldFile := model.NewContainerConfigFile("app.conf", content, 0644)
	newFile := model.NewContainerConfigFile("app.conf", content, 0755)

	spec := makeContainerSpecWithDirs("webapp", "nginx:latest", model.DesiredStateRunning,
		model.NewContainerDirMount(mountPath, newFile))

	ctx, memFs, sd, _, mgrs, zipPath := setupConfigDirTest(t, store, testFixture{
		specs:         []model.Unit{spec},
		existingUnits: map[string]string{"webapp.container": renderContainerWithStore(t, store, spec)},
		existingState: map[string]string{"webapp.service": "active"},
	})

	preWriteConfigDir(t, store, "webapp", mountPath, 1, oldFile)

	plan, err := BuildPlan(ctx, memFs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan failed: %v", err)
	}

	var buf bytes.Buffer
	DisplayPlan(&buf, plan)
	out := buf.String()

	wantOutput := `
ConfigDir changes:
  webapp:/etc/app/config → version 2
  app.conf mode: 0644 → 0755

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    configDir updated, reloaded (desired: running)
`

	if out != wantOutput {
		t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", wantOutput, out)
	}
}

func TestDisplayDiff(t *testing.T) {
	tests := []struct {
		name           string
		spec           *model.ContainerUnit
		oldSpec        *model.ContainerUnit
		preWriteConfig map[string]string // mountPath -> content to pre-write on fs
		wantOutput     string
	}{
		{
			name: "ConfigFileChanges",
			spec: makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
				model.NewContainerFileMount("/etc/nginx/nginx.conf", "server {\n  listen 8080;\n  server_name new.example.com;\n}\n", 0)),
			oldSpec: makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
				model.NewContainerFileMount("/etc/nginx/nginx.conf", "server {\n  listen 80;\n  server_name old.example.com;\n}\n", 0)),
			preWriteConfig: map[string]string{"/etc/nginx/nginx.conf": "server {\n  listen 80;\n  server_name old.example.com;\n}\n"},
			wantOutput: `
Config file changes:
--- webapp:/etc/nginx/nginx.conf (current)
+++ webapp:/etc/nginx/nginx.conf (new)
@@ -1,4 +1,4 @@
 server {
-  listen 80;
+  listen 8080;
-  server_name old.example.com;
+  server_name new.example.com;
 }

Services to stop:
  - webapp.container

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    config updated, restarted (desired: running)
`,
		},
		{
			name:    "UnitFileChanges",
			spec:    makeContainerSpec("webapp", "nginx:alpine", model.DesiredStateRunning),
			oldSpec: makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning),
			wantOutput: `
Unit file changes:

--- webapp.container
changed:
  [Container] Image
    old: nginx:latest
    new: nginx:alpine

Services to stop:
  - webapp.container

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, restarted (desired: running)
`,
		},
		{
			name: "NewConfigFile",
			spec: makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
				model.NewContainerFileMount("/etc/app/config.yaml", "new config content", 0)),
			oldSpec: makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning),
			wantOutput: `
Unit file changes:

--- webapp.container
added:
  [Container] Volume=/etc/containers/config/webapp/config.yaml-4f166d9b:/etc/app/config.yaml:ro,Z

Config file changes:
--- webapp:/etc/app/config.yaml (current)
+++ webapp:/etc/app/config.yaml (new)
@@ -0,0 +1 @@
+new config content
\ No newline at end of file

Services to stop:
  - webapp.container

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, config updated, restarted (desired: running)
`,
		},
		{
			name: "MixedChanges",
			spec: makeContainerSpecWithConfigs("webapp", "nginx:alpine", model.DesiredStateRunning,
				model.NewContainerFileMount("/etc/app/app.conf", "updated config", 0)),
			oldSpec: makeContainerSpecWithConfigs("webapp", "nginx:latest", model.DesiredStateRunning,
				model.NewContainerFileMount("/etc/app/app.conf", "old config", 0)),
			preWriteConfig: map[string]string{"/etc/app/app.conf": "old config"},
			wantOutput: `
Unit file changes:

--- webapp.container
changed:
  [Container] Image
    old: nginx:latest
    new: nginx:alpine

Config file changes:
--- webapp:/etc/app/app.conf (current)
+++ webapp:/etc/app/app.conf (new)
@@ -1 +1 @@
-old config
\ No newline at end of file
+updated config
\ No newline at end of file

Services to stop:
  - webapp.container

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, config updated, restarted (desired: running)
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()

			ctx, sd, _, _, zipPath := setupTestWithFS(t, fs, testFixture{
				specs:         []model.Unit{tt.spec},
				existingUnits: map[string]string{"webapp.container": renderContainer(t, fs, tt.oldSpec)},
				existingState: map[string]string{"webapp.service": "active"},
			})

			for mountPath, content := range tt.preWriteConfig {
				preWriteConfig(t, fs, "webapp", mountPath, content, 0644)
			}

			mgrs := newTestFileManagers(fs)
			plan, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, zipPath)
			if err != nil {
				t.Fatalf("BuildPlan failed: %v", err)
			}

			var buf bytes.Buffer
			DisplayPlan(&buf, plan)
			if got := buf.String(); got != tt.wantOutput {
				t.Errorf("output mismatch\nExpected:\n%s\nGot:\n%s", tt.wantOutput, got)
			}
		})
	}
}
