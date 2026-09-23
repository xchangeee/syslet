//go:build integration

package integration

import (
	"testing"

	"github.com/spf13/afero"

	"github.com/xchangeee/syslet/internal/model"
	"github.com/xchangeee/syslet/internal/syslet"
	"github.com/xchangeee/syslet/internal/systemd"
	"github.com/xchangeee/syslet/internal/systemd/systemdtest"
	"github.com/xchangeee/syslet/test/integration/systest"
)

// TestPlanWithSecretChanges_GroupsBySpec verifies that syslet.DisplayPlan groups secret upserts and
// deletes by spec name, hiding every value behind "(secret)" so a plan is safe for shared CI logs.
func TestPlanWithSecretChanges_GroupsBySpec(t *testing.T) {
	tests := []struct {
		name       string
		upserts    []syslet.UpsertPodmanSecretOp
		deletes    []syslet.DeletePodmanSecretOp
		wantOutput string
	}{
		{
			name: "UpsertAndDelete",
			upserts: []syslet.UpsertPodmanSecretOp{
				{SpecName: "myapp", Name: "myapp-db-password", Value: model.Plaintext("s3cr3t"), Labels: map[string]string{"syslet/hash": "abc123"}},
				{SpecName: "myapp", Name: "myapp-api-key", Value: model.Plaintext("newkey"), Labels: map[string]string{"syslet/hash": "def456"}},
			},
			deletes: []syslet.DeletePodmanSecretOp{
				{SpecName: "myapp", Name: "myapp-old-token"},
			},
			wantOutput: `
Secret changes:
  myapp:
    - old-token=(secret)
    + db-password=(secret)
    + api-key=(secret)

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
		{
			name: "UpsertOnly",
			upserts: []syslet.UpsertPodmanSecretOp{
				{SpecName: "infra", Name: "infra-cert", Value: model.Plaintext("pem-data"), Labels: map[string]string{"syslet/hash": "aaa"}},
			},
			wantOutput: `
Secret changes:
  infra:
    + cert=(secret)

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
		{
			name: "DeleteOnly",
			deletes: []syslet.DeletePodmanSecretOp{
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
			upserts: []syslet.UpsertPodmanSecretOp{
				{SpecName: "app1", Name: "app1-token", Value: model.Plaintext("tok1"), Labels: map[string]string{"syslet/hash": "h1"}},
				{SpecName: "app2", Name: "app2-key", Value: model.Plaintext("key2"), Labels: map[string]string{"syslet/hash": "h2"}},
			},
			wantOutput: `
Secret changes:
  app1:
    + token=(secret)
  app2:
    + key=(secret)

Summary:
UNIT                                     STATUS     CHANGES
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &syslet.ApplyPlan{
				UpsertPodmanSecrets: tt.upserts,
				DeletePodmanSecrets: tt.deletes,
			}
			systest.AssertPlanOutput(t, plan, tt.wantOutput)
		})
	}
}

// TestPlanWithSecretChanges_WithShowSecrets_PrintsValues covers the local
// opt-in: new values are printed in plain text, while deleted ones stay
// "(secret)" because the plan never holds their old value.
func TestPlanWithSecretChanges_WithShowSecrets_PrintsValues(t *testing.T) {
	plan := &syslet.ApplyPlan{
		UpsertPodmanSecrets: []syslet.UpsertPodmanSecretOp{
			{SpecName: "myapp", Name: "myapp-db-password", Value: model.Plaintext("s3cr3t")},
		},
		DeletePodmanSecrets: []syslet.DeletePodmanSecretOp{
			{SpecName: "myapp", Name: "myapp-old-token"},
		},
	}
	systest.AssertPlanOutputWithOptions(t, plan, syslet.DisplayOptions{ShowSecrets: true}, `
Secret changes:
  myapp:
    - old-token=(secret)
    + db-password=s3cr3t

Summary:
UNIT                                     STATUS     CHANGES
`)
}

// TestPlanWithValidationErrors_PrintsErrorsOnly verifies that ValidateUnits failures are recorded
// in the plan (not returned as a fatal error), and that syslet.DisplayPlan suppresses
// the diff when the plan contains errors.
func TestPlanWithValidationErrors_PrintsErrorsOnly(t *testing.T) {
	// X-Syslet section is reserved; NoXSysletSection in ValidateUnits rejects this.
	badSpec := model.NewContainerUnit(
		model.ContainerUnitRef("myapp"),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV("nginx:latest")},
			"X-Syslet":  {model.SectionKey("SomeKey"): model.UV("value")},
		},
		model.DesiredStateRunning, nil, false,
	)

	env := systest.New(t)
	env.Specs(badSpec)

	// Plan fatals if BuildPlan itself errored, which is half the contract here:
	// a validation failure must be recorded on the plan, not returned.
	plan := env.Plan()
	systest.AssertPlanHasErrors(t, plan)

	systest.AssertPlanOutputOmits(t, plan, "Unit file changes:")
	systest.AssertPlanOutputContains(t, plan, "X-Syslet")
}

// TestPlanWithStagingErrors_SuppressesDiff verifies that when staging validation records
// errors on an otherwise fully-built plan (with unit file changes), syslet.DisplayPlan
// suppresses the diff and shows only the errors.
func TestPlanWithStagingErrors_SuppressesDiff(t *testing.T) {
	fs := afero.NewMemMapFs()
	az := &systemdtest.MockAnalyzeRunner{
		Result: systemd.AnalyzeResult{
			ExitCode: 0,
			Stderr:   "Invalid memory limit 'asd', ignoring: Invalid argument",
		},
	}
	// Generator writes the generated service file so staging has something to verify.
	gen := &systemdtest.FakeQuadletGenerator{
		Fs:    fs,
		Files: map[string]string{"webapp.service": "[Service]\nExecStart=podman start webapp\n"},
	}

	env := systest.New(t, systest.WithFs(fs),
		systest.WithQuadletGenerator(gen), systest.WithAnalyzeRunner(az))
	env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
	env.Specs(systest.NewContainer("webapp", "nginx:alpine"))

	plan := env.Plan()
	systest.AssertPlanHasErrors(t, plan)

	systest.AssertPlanOutputOmits(t, plan, "Unit file changes:")
	systest.AssertPlanOutputContains(t, plan, "Invalid memory limit")
}

// TestPlanWithConfigDirChanges_ShowsFileDiff verifies that syslet.DisplayPlan shows per-file unified diffs
// for configDir changes. Requires a real OS filesystem because versioned dirs use symlinks.
func TestPlanWithConfigDirChanges_ShowsFileDiff(t *testing.T) {
	const mountPath = "/etc/app/config"

	oldFile := model.NewContainerConfigFile("app.conf", "key=old\n", 0644)
	newFile := model.NewContainerConfigFile("app.conf", "key=new\n", 0644)

	spec := systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(mountPath, newFile)))

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(spec)
	env.SeedConfigDir("webapp", mountPath, 1, oldFile)
	env.Specs(spec)

	plan := env.Plan()

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

	systest.AssertPlanOutput(t, plan, wantOutput)
}

// TestPlanWithConfigDirModeChange_ShowsModeDiff verifies that syslet.DisplayPlan shows a mode-change
// line when only the file permission bits change (content is identical).
func TestPlanWithConfigDirModeChange_ShowsModeDiff(t *testing.T) {
	const mountPath = "/etc/app/config"

	content := "key=value\n"
	oldFile := model.NewContainerConfigFile("app.conf", content, 0644)
	newFile := model.NewContainerConfigFile("app.conf", content, 0755)

	spec := systest.NewContainer("webapp", "nginx:latest",
		systest.Dirs(model.NewContainerDirMount(mountPath, newFile)))

	env := systest.New(t, systest.WithOSConfigStore())
	env.SeedActive(spec)
	env.SeedConfigDir("webapp", mountPath, 1, oldFile)
	env.Specs(spec)

	plan := env.Plan()

	wantOutput := `
ConfigDir changes:
  webapp:/etc/app/config → version 2
  app.conf mode: 0644 → 0755

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    configDir updated, reloaded (desired: running)
`

	systest.AssertPlanOutput(t, plan, wantOutput)
}

func TestPlanDiffOutput(t *testing.T) {
	tests := []struct {
		name       string
		spec       *model.ContainerUnit
		oldSpec    *model.ContainerUnit
		seedConfig map[string]string // mountPath -> content already on disk
		wantOutput string
	}{
		{
			name: "ConfigFileChanges",
			spec: systest.NewContainer("webapp", "nginx:latest", systest.Files(
				model.NewContainerFileMount("/etc/nginx/nginx.conf", "server {\n  listen 8080;\n  server_name new.example.com;\n}\n", 0))),
			oldSpec: systest.NewContainer("webapp", "nginx:latest", systest.Files(
				model.NewContainerFileMount("/etc/nginx/nginx.conf", "server {\n  listen 80;\n  server_name old.example.com;\n}\n", 0))),
			seedConfig: map[string]string{"/etc/nginx/nginx.conf": "server {\n  listen 80;\n  server_name old.example.com;\n}\n"},
			wantOutput: `
Config file changes:
--- webapp:/etc/nginx/nginx.conf (current)
+++ webapp:/etc/nginx/nginx.conf (new)
@@ -1,4 +1,4 @@
 server {
-  listen 80;
-  server_name old.example.com;
+  listen 8080;
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
			spec:    systest.NewContainer("webapp", "nginx:alpine"),
			oldSpec: systest.NewContainer("webapp", "nginx:latest"),
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
			spec: systest.NewContainer("webapp", "nginx:latest", systest.Files(
				model.NewContainerFileMount("/etc/app/config.yaml", "new config content", 0))),
			oldSpec: systest.NewContainer("webapp", "nginx:latest"),
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
			spec: systest.NewContainer("webapp", "nginx:alpine", systest.Files(
				model.NewContainerFileMount("/etc/app/app.conf", "updated config", 0))),
			oldSpec: systest.NewContainer("webapp", "nginx:latest", systest.Files(
				model.NewContainerFileMount("/etc/app/app.conf", "old config", 0))),
			seedConfig: map[string]string{"/etc/app/app.conf": "old config"},
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
			env := systest.New(t)
			env.SeedActive(tt.oldSpec)
			for mountPath, content := range tt.seedConfig {
				env.SeedConfigFile("webapp", mountPath, content, 0644)
			}
			env.Specs(tt.spec)

			plan := env.Plan()

			systest.AssertPlanOutput(t, plan, tt.wantOutput)
		})
	}
}
