package syslet

// Integration tests for the secret plan+apply lifecycle.
//
// Each test uses real SOPS-encrypted fixtures from internal/syslet/testdata.
// The fixture contains two keys: "db-password"="hunter2" and "api-key"="s3cr3t"
// (hyphen-separated to satisfy the [a-z0-9-] key name validation rule).
//
// The mock podman client's existingSecrets field simulates the on-host state
// returned by ListSecrets. Secrets created by syslet always carry the
// "syslet/hash" label so orphan detection can identify them.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/sops"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/util"
	"github.com/spf13/afero"
)

// secretTestDecryptor returns a Decryptor backed by the syslet test key file.
func secretTestDecryptor(t *testing.T) *sops.Decryptor {
	t.Helper()
	p, err := filepath.Abs("testdata/keys.txt")
	if err != nil {
		t.Fatalf("resolving key file path: %v", err)
	}
	return sops.NewDecryptor(p)
}

// encryptedCiphertext reads the syslet SOPS test fixture.
// Keys: "api-key"="s3cr3t", "db-password"="hunter2".
func encryptedCiphertext(t *testing.T) model.Ciphertext {
	t.Helper()
	data, err := afero.ReadFile(afero.NewOsFs(), "testdata/encrypted.yaml")
	if err != nil {
		t.Fatalf("reading fixture — run testdata/generate.sh: %v", err)
	}
	return model.Ciphertext(data)
}

// secretContentHash returns the SHA-256 hex of the standard test ciphertext.
func secretContentHash(t *testing.T) string {
	t.Helper()
	return util.SHA256Hex([]byte(encryptedCiphertext(t)))
}

// createZipWithSecret builds a zip containing one secret spec and optional extra
// unit specs, writes it to the in-memory FS, and returns its path.
func createZipWithSecret(t *testing.T, fs afero.Fs, specName string, ciphertext model.Ciphertext, extraSpecs []model.Unit) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	secretJSON, err := json.Marshal(map[string]any{
		"type":       "secret",
		"name":       specName,
		"ciphertext": string(ciphertext),
	})
	if err != nil {
		t.Fatalf("marshal secret spec: %v", err)
	}
	f, err := w.Create("secret.json")
	if err != nil {
		t.Fatalf("create secret entry: %v", err)
	}
	if _, err := f.Write(secretJSON); err != nil {
		t.Fatalf("write secret entry: %v", err)
	}

	for i, spec := range extraSpecs {
		data, err := marshalSpecToRaw(spec)
		if err != nil {
			t.Fatalf("marshal spec %d: %v", i, err)
		}
		entry, err := w.Create(filepath.Join("spec", filepath.FromSlash("unit-"+string(rune('0'+i))+".json")))
		if err != nil {
			t.Fatalf("create unit entry %d: %v", i, err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatalf("write unit entry %d: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	zipPath := "/tmp/secret-test.zip"
	if err := afero.WriteFile(fs, zipPath, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	return zipPath
}

// secretTestSetup initialises the standard in-memory test environment.
func secretTestSetup(t *testing.T) (context.Context, afero.Fs, *systemd.Client, *systemd.MockDBusConn, *mockPodmanClient) {
	t.Helper()
	ctx := context.Background()
	fs := afero.NewMemMapFs()
	mockConn := systemd.NewMockDBusConn()
	sd := systemd.NewClientWithPaths(mockConn, fs, "/etc/containers/systemd")
	t.Cleanup(sd.Close)
	mockPodman := newMockPodmanClient()
	return ctx, fs, sd, mockConn, mockPodman
}

// buildSecretPlan calls BuildPlan with a real decryptor and the mock podman client.
func buildSecretPlan(t *testing.T, ctx context.Context, fs afero.Fs, sd *systemd.Client, mockPodman *mockPodmanClient, decryptor *sops.Decryptor, zipPath string) *ApplyPlan {
	t.Helper()
	mgrs := newTestFileManagers(fs)
	plan, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, mockPodman, decryptor, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.HasErrors() {
		t.Fatalf("unexpected plan errors: %v", plan.Errors)
	}
	return plan
}

// sortedUpsertNames extracts and sorts the Name field from UpsertPodmanSecrets.
func sortedUpsertNames(ops []UpsertPodmanSecretOp) []string {
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = op.Name
	}
	sort.Strings(names)
	return names
}

func TestSecret_New_UpsertsAllKeys(t *testing.T) {
	ctx, fs, sd, _, mockPodman := secretTestSetup(t)
	ct := encryptedCiphertext(t)
	zipPath := createZipWithSecret(t, fs, "myapp", ct, nil)

	plan := buildSecretPlan(t, ctx, fs, sd, mockPodman, secretTestDecryptor(t), zipPath)

	got := sortedUpsertNames(plan.UpsertPodmanSecrets)
	want := []string{"myapp-api-key", "myapp-db-password"}
	if len(got) != len(want) {
		t.Fatalf("upserts: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("upsert[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if len(plan.DeletePodmanSecrets) != 0 {
		t.Errorf("unexpected deletes: %v", plan.DeletePodmanSecrets)
	}
}

func TestSecret_Unchanged_NoAction(t *testing.T) {
	ctx, fs, sd, _, mockPodman := secretTestSetup(t)
	ct := encryptedCiphertext(t)
	hash := secretContentHash(t)

	mockPodman.existingSecrets = []podman.PodmanSecretMeta{
		{Name: "myapp-api-key", Labels: map[string]string{"syslet/hash": hash}},
		{Name: "myapp-db-password", Labels: map[string]string{"syslet/hash": hash}},
	}
	zipPath := createZipWithSecret(t, fs, "myapp", ct, nil)

	plan := buildSecretPlan(t, ctx, fs, sd, mockPodman, secretTestDecryptor(t), zipPath)

	if len(plan.UpsertPodmanSecrets) != 0 {
		t.Errorf("expected no upserts, got %v", plan.UpsertPodmanSecrets)
	}
	if len(plan.DeletePodmanSecrets) != 0 {
		t.Errorf("expected no deletes, got %v", plan.DeletePodmanSecrets)
	}
}

func TestSecret_ContentChanged_UpsertsAllKeys(t *testing.T) {
	ctx, fs, sd, _, mockPodman := secretTestSetup(t)
	ct := encryptedCiphertext(t)

	mockPodman.existingSecrets = []podman.PodmanSecretMeta{
		{Name: "myapp-api-key", Labels: map[string]string{"syslet/hash": "oldhash"}},
		{Name: "myapp-db-password", Labels: map[string]string{"syslet/hash": "oldhash"}},
	}
	zipPath := createZipWithSecret(t, fs, "myapp", ct, nil)

	plan := buildSecretPlan(t, ctx, fs, sd, mockPodman, secretTestDecryptor(t), zipPath)

	if len(plan.UpsertPodmanSecrets) != 2 {
		t.Errorf("expected 2 upserts (hash mismatch), got %d: %v", len(plan.UpsertPodmanSecrets), plan.UpsertPodmanSecrets)
	}
	if len(plan.DeletePodmanSecrets) != 0 {
		t.Errorf("unexpected deletes: %v", plan.DeletePodmanSecrets)
	}
}

func TestSecret_OrphanKey_DeletesKey(t *testing.T) {
	ctx, fs, sd, _, mockPodman := secretTestSetup(t)
	ct := encryptedCiphertext(t)
	hash := secretContentHash(t)

	mockPodman.existingSecrets = []podman.PodmanSecretMeta{
		{Name: "myapp-api-key", Labels: map[string]string{"syslet/hash": hash}},
		{Name: "myapp-db-password", Labels: map[string]string{"syslet/hash": hash}},
		{Name: "myapp-old-token", Labels: map[string]string{"syslet/hash": hash}}, // orphan
	}
	zipPath := createZipWithSecret(t, fs, "myapp", ct, nil)

	plan := buildSecretPlan(t, ctx, fs, sd, mockPodman, secretTestDecryptor(t), zipPath)

	if len(plan.UpsertPodmanSecrets) != 0 {
		t.Errorf("expected no upserts, got %v", plan.UpsertPodmanSecrets)
	}
	if len(plan.DeletePodmanSecrets) != 1 || plan.DeletePodmanSecrets[0].Name != "myapp-old-token" {
		t.Errorf("expected delete of myapp-old-token, got %v", plan.DeletePodmanSecrets)
	}
}

func TestSecret_SpecRemoved_DeletesAllKeys(t *testing.T) {
	// No secret specs in the zip, but syslet-managed secrets exist on host.
	ctx, fs, sd, _, mockPodman := secretTestSetup(t)

	mockPodman.existingSecrets = []podman.PodmanSecretMeta{
		{Name: "oldapp-key1", Labels: map[string]string{"syslet/hash": "abc"}},
		{Name: "oldapp-key2", Labels: map[string]string{"syslet/hash": "abc"}},
	}

	// Zip with only a container spec — no secret spec.
	webappSpec := makeContainerSpec("webapp", "nginx:latest", model.DesiredStateRunning)
	zipPath, err := createZipFromSpecs(fs, []model.Unit{webappSpec})
	if err != nil {
		t.Fatalf("createZipFromSpecs: %v", err)
	}

	mgrs := newTestFileManagers(fs)
	plan, err := BuildPlan(ctx, fs, mgrs, sd, &systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, mockPodman, nil, zipPath)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.HasErrors() {
		t.Fatalf("unexpected plan errors: %v", plan.Errors)
	}

	deleted := make([]string, len(plan.DeletePodmanSecrets))
	for i, op := range plan.DeletePodmanSecrets {
		deleted[i] = op.Name
	}
	sort.Strings(deleted)
	want := []string{"oldapp-key1", "oldapp-key2"}
	if len(deleted) != len(want) {
		t.Fatalf("expected deletes %v, got %v", want, deleted)
	}
	for i := range want {
		if deleted[i] != want[i] {
			t.Errorf("delete[%d] = %q, want %q", i, deleted[i], want[i])
		}
	}
}

func TestSecret_Changed_RestartsReferencingContainers(t *testing.T) {
	ctx, fs, sd, mockConn, mockPodman := secretTestSetup(t)
	ct := encryptedCiphertext(t)

	// Container references myapp-db-password; hash mismatch → upsert → restart.
	webappSpec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Secret"): model.UV("myapp-db-password"),
			},
		},
		model.DesiredStateRunning, nil, false,
	)

	mockPodman.existingSecrets = []podman.PodmanSecretMeta{
		{Name: "myapp-api-key", Labels: map[string]string{"syslet/hash": "oldhash"}},
		{Name: "myapp-db-password", Labels: map[string]string{"syslet/hash": "oldhash"}},
	}

	// Pre-write the unit file so the plan sees the container as existing, not new.
	webappContent := renderContainer(t, fs, webappSpec)
	if err := sd.WriteUnitFile("webapp.container", []byte(webappContent)); err != nil {
		t.Fatalf("pre-write unit file: %v", err)
	}
	mockConn.SetUnitState("webapp.service", systemd.ActiveStateActive)

	zipPath := createZipWithSecret(t, fs, "myapp", ct, []model.Unit{webappSpec})
	plan := buildSecretPlan(t, ctx, fs, sd, mockPodman, secretTestDecryptor(t), zipPath)

	var stopped, started bool
	for _, op := range plan.StopSystemdServices {
		if op.ref.FullName() == "webapp.container" {
			stopped = true
		}
	}
	for _, op := range plan.StartSystemdServices {
		if op.ref.FullName() == "webapp.container" {
			started = true
		}
	}
	if !stopped || !started {
		t.Errorf("expected webapp to be restarted (stopped=%v started=%v)", stopped, started)
	}
}

func TestSecret_Unchanged_NoContainerRestart(t *testing.T) {
	ctx, fs, sd, mockConn, mockPodman := secretTestSetup(t)
	ct := encryptedCiphertext(t)
	hash := secretContentHash(t)

	webappSpec := model.NewContainerUnit(
		model.ContainerUnitRef("webapp"),
		model.UnitOptions{
			"Container": {
				model.SectionKey("Image"):  model.UV("nginx:latest"),
				model.SectionKey("Secret"): model.UV("myapp-db-password"),
			},
		},
		model.DesiredStateRunning, nil, false,
	)

	mockPodman.existingSecrets = []podman.PodmanSecretMeta{
		{Name: "myapp-api-key", Labels: map[string]string{"syslet/hash": hash}},
		{Name: "myapp-db-password", Labels: map[string]string{"syslet/hash": hash}},
	}

	// Pre-write the unit file so the plan sees no unit change.
	webappContent := renderContainer(t, fs, webappSpec)
	if err := sd.WriteUnitFile("webapp.container", []byte(webappContent)); err != nil {
		t.Fatalf("pre-write unit file: %v", err)
	}
	mockConn.SetUnitState("webapp.service", systemd.ActiveStateActive)

	zipPath := createZipWithSecret(t, fs, "myapp", ct, []model.Unit{webappSpec})
	plan := buildSecretPlan(t, ctx, fs, sd, mockPodman, secretTestDecryptor(t), zipPath)

	if len(plan.StopSystemdServices) != 0 || len(plan.StartSystemdServices) != 0 {
		t.Errorf("expected no restart: stops=%v starts=%v", plan.StopSystemdServices, plan.StartSystemdServices)
	}
}
