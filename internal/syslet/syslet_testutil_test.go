package syslet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/model"
	"codeberg.org/xchangeee/syslet/internal/podman"
	"codeberg.org/xchangeee/syslet/internal/render"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/testutil"
	"codeberg.org/xchangeee/syslet/internal/util"
)

// recordingQuadletGenerator records the filenames found in the units input directory
// when Run is called. Used to verify which unit files are staged for validation.
type recordingQuadletGenerator struct {
	fs          afero.Fs
	stagedFiles []string
}

func (g *recordingQuadletGenerator) Run(_ context.Context, unitsDir, _, _, _ string) (int, error) {
	entries, _ := afero.ReadDir(g.fs, unitsDir)
	for _, e := range entries {
		if !e.IsDir() {
			g.stagedFiles = append(g.stagedFiles, e.Name())
		}
	}
	return 0, nil
}

// multiUV creates a UnitValue with multiple string values (e.g. for Network=, Volume=).
func multiUV(_ *testing.T, values ...string) model.UnitValue {
	return model.MultiUV(values...)
}

// --- Spec fixture helpers ---

func makeContainerSpec(name, image string, state model.DesiredState) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(image)},
		},
		state, nil, false,
	)
}

func makeContainerSpecWithConfigs(name, image string, state model.DesiredState, configs ...model.ContainerFileMount) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(image)},
		},
		state, configs, false,
	)
}

// makeContainerSpecWithDirs builds a ContainerUnit with DirMounts.
// [Service] ExecReload= is required by the validator for any container with configDirs.
func makeContainerSpecWithDirs(name, image string, state model.DesiredState, dirs ...model.ContainerDirMount) *model.ContainerUnit {
	return model.NewContainerUnitWithDirs(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(image)},
			"Service":   {model.SectionKey("ExecReload"): model.UV("/bin/kill -HUP $MAINPID")},
		},
		state, nil, dirs, false,
	)
}

// renderContainerWithStore renders a ContainerUnit using the given store's Resolve function.
// Use this when the rendered Volume= paths must match those produced by a non-default store.
func renderContainerWithStore(t *testing.T, store *filestore.ContainerConfigFileStore, spec *model.ContainerUnit) string {
	t.Helper()
	rendered, err := render.NewRenderedUnitFromContainer(spec, store.Resolve)
	if err != nil {
		t.Fatalf("renderContainerWithStore: %v", err)
	}
	return rendered.Content
}

// testApplyWithMgrs builds a plan and applies it using the supplied FileManagers.
// Use when the config store requires a real OS filesystem (e.g. for configDir symlink support).
func testApplyWithMgrs(t *testing.T, ctx context.Context, fs afero.Fs, mgrs filestore.FileManagers, sd *systemd.Client, mockPodman podman.Interface, raw api.LoadResult) error {
	t.Helper()
	jr := &systemd.MockJournalReader{}
	plan, err := BuildPlan(ctx, fs, mgrs, sd, jr, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, raw)
	if err != nil {
		return err
	}
	return Apply(ctx, testutil.NewTestLogger(), sd, jr, mockPodman, mgrs, plan)
}

// mustApplyWithMgrs calls testApplyWithMgrs and fails the test on any error.
func mustApplyWithMgrs(t *testing.T, ctx context.Context, fs afero.Fs, mgrs filestore.FileManagers, sd *systemd.Client, mockPodman podman.Interface, raw api.LoadResult) {
	t.Helper()
	if err := testApplyWithMgrs(t, ctx, fs, mgrs, sd, mockPodman, raw); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
}

func makeVolumeSpec(name, device string) *model.VolumeUnit {
	return model.NewVolumeUnit(
		model.VolumeUnitRef(name),
		model.UnitOptions{
			"Volume": {model.SectionKey("Device"): model.UV(device)},
		},
		false, model.ReclaimPolicyRetain,
	)
}

func makeNetworkSpec(name, driver string) *model.NetworkUnit {
	return model.NewNetworkUnit(
		model.NetworkUnitRef(name),
		model.UnitOptions{
			"Network": {model.SectionKey("Driver"): model.UV(driver)},
		},
		false, model.ReclaimPolicyRetain,
	)
}

func makeOneshotContainerSpec(name, image string, state model.DesiredState) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(image)},
			"Service":   {model.SectionKey("Type"): model.UV("oneshot")},
		},
		state, nil, false,
	)
}

func makeStaleContainerSpec(name, image string) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(image)},
		},
		model.DesiredStateStopped, nil, true,
	)
}

func makeStaleOneshotContainerSpec(name, image string) *model.ContainerUnit {
	return model.NewContainerUnit(
		model.ContainerUnitRef(name),
		model.UnitOptions{
			"Container": {model.SectionKey("Image"): model.UV(image)},
			"Service":   {model.SectionKey("Type"): model.UV("oneshot")},
		},
		model.DesiredStateStopped, nil, true,
	)
}

func makeStaleVolumeSpec(name, device string) *model.VolumeUnit {
	return model.NewVolumeUnit(
		model.VolumeUnitRef(name),
		model.UnitOptions{
			"Volume": {model.SectionKey("Device"): model.UV(device)},
		},
		true, model.ReclaimPolicyRetain,
	)
}

func makeStaleNetworkSpec(name, driver string) *model.NetworkUnit {
	return model.NewNetworkUnit(
		model.NetworkUnitRef(name),
		model.UnitOptions{
			"Network": {model.SectionKey("Driver"): model.UV(driver)},
		},
		true, model.ReclaimPolicyRetain,
	)
}

func makeBuildSpec(name, imageTag string) *model.BuildUnit {
	return model.NewBuildUnit(
		model.BuildUnitRef(name),
		model.UnitOptions{
			"Build": {model.SectionKey("ImageTag"): model.UV(imageTag)},
		},
		"FROM scratch", nil, model.ReclaimPolicyRetain,
	)
}

func makeStaleBuildSpec(name, imageTag string) *model.BuildUnit {
	return makeBuildSpec(name, imageTag)
}

func makeStaleVolumeSpecWithDelete(name, device string) *model.VolumeUnit {
	return model.NewVolumeUnit(
		model.VolumeUnitRef(name),
		model.UnitOptions{
			"Volume": {model.SectionKey("Device"): model.UV(device)},
		},
		true, model.ReclaimPolicyDelete,
	)
}

func makeStaleNetworkSpecWithDelete(name, driver string) *model.NetworkUnit {
	return model.NewNetworkUnit(
		model.NetworkUnitRef(name),
		model.UnitOptions{
			"Network": {model.SectionKey("Driver"): model.UV(driver)},
		},
		true, model.ReclaimPolicyDelete,
	)
}

func makeStaleBuildSpecWithDelete(name, imageTag string) *model.BuildUnit {
	return model.NewBuildUnit(
		model.BuildUnitRef(name),
		model.UnitOptions{
			"Build": {model.SectionKey("ImageTag"): model.UV(imageTag)},
		},
		"FROM scratch", nil, model.ReclaimPolicyDelete,
	)
}

// newTestFileManagers creates a FileManagers backed by an in-memory filesystem.
func newTestFileManagers(fs afero.Fs) filestore.FileManagers {
	return filestore.FileManagers{
		Config: filestore.NewContainerConfigFileStore(fs),
		Build:  filestore.NewBuildContextFileStore(fs),
	}
}

// renderContainer renders a ContainerUnit and returns its content, failing the test on error.
func renderContainer(t *testing.T, fs afero.Fs, spec *model.ContainerUnit) string {
	t.Helper()
	store := filestore.NewContainerConfigFileStore(fs)
	rendered, err := render.NewRenderedUnitFromContainer(spec, store.Resolve)
	if err != nil {
		t.Fatalf("render.RenderContainer: %v", err)
	}
	return rendered.Content
}

// renderVolume renders a VolumeUnit and returns its content, failing the test on error.
func renderVolume(t *testing.T, spec *model.VolumeUnit) string {
	t.Helper()
	rendered, err := render.NewRenderedUnitFromVolume(spec)
	if err != nil {
		t.Fatalf("render.RenderVolume: %v", err)
	}
	return rendered.Content
}

// renderNetwork renders a NetworkUnit and returns its content, failing the test on error.
func renderNetwork(t *testing.T, spec *model.NetworkUnit) string {
	t.Helper()
	rendered, err := render.NewRenderedUnitFromNetwork(spec)
	if err != nil {
		t.Fatalf("render.RenderNetwork: %v", err)
	}
	return rendered.Content
}

// renderBuild renders a BuildUnit and returns its content, failing the test on error.
func renderBuild(t *testing.T, fs afero.Fs, spec *model.BuildUnit) string {
	t.Helper()
	rendered, err := render.NewRenderedUnitFromBuild(spec, filestore.NewBuildContextFileStore(fs).Resolve)
	if err != nil {
		t.Fatalf("render.RenderBuild: %v", err)
	}
	return rendered.Content
}

// --- Mock types ---

type upsertedSecret struct {
	name   string
	value  model.Plaintext
	labels map[string]string
}

// mockPodmanClient implements podman.Interface for testing.
type mockPodmanClient struct {
	deletedVolumes  []string
	deletedNetworks []string
	deletedImages   []string
	upsertedSecrets []upsertedSecret
	deletedSecrets  []string
	existingSecrets []podman.SecretMeta
}

func newMockPodmanClient() *mockPodmanClient {
	return &mockPodmanClient{
		deletedVolumes:  []string{},
		deletedNetworks: []string{},
		deletedImages:   []string{},
	}
}

func (m *mockPodmanClient) DeleteVolume(_ context.Context, name string) error {
	m.deletedVolumes = append(m.deletedVolumes, name)
	return nil
}

func (m *mockPodmanClient) DeleteNetwork(_ context.Context, name string) error {
	m.deletedNetworks = append(m.deletedNetworks, name)
	return nil
}

func (m *mockPodmanClient) DeleteImage(_ context.Context, tag string) error {
	m.deletedImages = append(m.deletedImages, tag)
	return nil
}

func (m *mockPodmanClient) UpsertSecret(_ context.Context, name string, value model.Plaintext, labels map[string]string) error {
	m.upsertedSecrets = append(m.upsertedSecrets, upsertedSecret{name: name, value: value, labels: labels})
	return nil
}

func (m *mockPodmanClient) DeleteSecret(_ context.Context, name string) error {
	m.deletedSecrets = append(m.deletedSecrets, name)
	return nil
}

func (m *mockPodmanClient) ListSecrets(_ context.Context) ([]podman.SecretMeta, error) {
	return m.existingSecrets, nil
}

// --- Test fixture and setup ---

// testFixture helps build test scenarios with specs and existing state.
type testFixture struct {
	specs         []model.Unit
	existingUnits map[string]string // fullName -> content
	existingState map[string]string // service name -> "active"|"inactive"
}

// unitOptionsToRaw converts model.UnitOptions to the raw map format expected by the loader.
func unitOptionsToRaw(opts model.UnitOptions) map[string]map[string]any {
	raw := make(map[string]map[string]any)
	for section, keys := range opts {
		raw[string(section)] = make(map[string]any)
		for key, uv := range keys {
			values := uv.Values()
			if len(values) == 1 {
				raw[string(section)][string(key)] = values[0]
			} else {
				strs := make([]string, len(values))
				copy(strs, values)
				raw[string(section)][string(key)] = strs
			}
		}
	}
	return raw
}

// marshalSpecToRaw serializes a model.Spec to the raw JSON format read by the loader.
func marshalSpecToRaw(spec model.Unit) ([]byte, error) {
	raw := map[string]any{
		"type": string(spec.Ref().UnitType()),
		"name": spec.Ref().Name(),
		"unit": unitOptionsToRaw(spec.Options()),
	}
	switch s := spec.(type) {
	case *model.ContainerUnit:
		if s.DesiredState != "" {
			raw["desiredState"] = string(s.DesiredState)
		}
		if s.RemovalAllowed {
			raw["removalAllowed"] = true
		}
		if len(s.FileMounts) > 0 {
			configs := make([]map[string]any, len(s.FileMounts))
			for i, c := range s.FileMounts {
				entry := map[string]any{
					"mountPath": string(c.FullPath()),
					"content":   c.File.Content,
				}
				if c.File.Mode != 0 {
					entry["mode"] = fmt.Sprintf("%04o", c.File.Mode)
				}
				configs[i] = entry
			}
			raw["configs"] = configs
		}
		if len(s.DirMounts) > 0 {
			configDirs := make([]map[string]any, len(s.DirMounts))
			for i, cd := range s.DirMounts {
				files := make([]map[string]any, len(cd.Files))
				for j, f := range cd.Files {
					fe := map[string]any{"name": f.Name, "content": f.Content}
					if f.Mode != 0 {
						fe["mode"] = fmt.Sprintf("%04o", f.Mode)
					}
					files[j] = fe
				}
				configDirs[i] = map[string]any{
					"mountPath": string(cd.Directory),
					"files":     files,
				}
			}
			raw["configDirs"] = configDirs
		}
	case *model.VolumeUnit:
		if s.RemovalAllowed {
			raw["removalAllowed"] = true
		}
		if s.ReclaimPolicy != "" {
			raw["reclaimPolicy"] = string(s.ReclaimPolicy)
		}
	case *model.NetworkUnit:
		if s.RemovalAllowed {
			raw["removalAllowed"] = true
		}
		if s.ReclaimPolicy != "" {
			raw["reclaimPolicy"] = string(s.ReclaimPolicy)
		}
	case *model.BuildUnit:
		raw["containerfile"] = s.Containerfile
		if s.ReclaimPolicy != "" {
			raw["reclaimPolicy"] = string(s.ReclaimPolicy)
		}
		if len(s.ContextFiles) > 0 {
			configs := make([]map[string]any, len(s.ContextFiles))
			for i, c := range s.ContextFiles {
				entry := map[string]any{
					"filename": string(c.Filename),
					"content":  c.Content,
				}
				if c.Mode != 0 {
					entry["mode"] = fmt.Sprintf("%04o", c.Mode)
				}
				configs[i] = entry
			}
			raw["configs"] = configs
		}
	}
	return json.Marshal(raw)
}

// loadResultFromSpecs marshals specs into a JSON stream and runs it through the
// real api.LoadSpecsReader, so tests exercise the same loader path production
// uses for stdin and persisted config.json input.
func loadResultFromSpecs(specs []model.Unit) (api.LoadResult, error) {
	var buf bytes.Buffer
	for _, spec := range specs {
		data, err := marshalSpecToRaw(spec)
		if err != nil {
			return api.LoadResult{}, err
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return api.LoadResult{}, nil
	}
	return api.LoadSpecsReader(&buf)
}

// setupTestWithFS creates a test environment using the provided filesystem.
func setupTestWithFS(t *testing.T, fs afero.Fs, fixture testFixture) (context.Context, *systemd.Client, *systemd.MockDBusConn, podman.Interface, api.LoadResult) {
	t.Helper()
	ctx := context.Background()
	mockConn := systemd.NewMockDBusConn()
	mockPodman := newMockPodmanClient()

	quadletDir := "/etc/containers/systemd"
	sd := systemd.NewClientWithPaths(mockConn, fs, quadletDir)

	for fullName, content := range fixture.existingUnits {
		if err := sd.WriteUnitFile(model.FullUnitName(fullName), []byte(content)); err != nil {
			t.Fatalf("failed to write existing unit %s: %v", fullName, err)
		}
	}

	for serviceName, state := range fixture.existingState {
		mockConn.SetUnitState(serviceName, systemd.ActiveState(state))
	}

	raw, err := loadResultFromSpecs(fixture.specs)
	if err != nil {
		t.Fatalf("failed to load specs: %v", err)
	}

	t.Cleanup(func() {
		sd.Close()
	})

	return ctx, sd, mockConn, mockPodman, raw
}

// setupTest creates a test environment with a fresh in-memory filesystem.
func setupTest(t *testing.T, fixture testFixture) (context.Context, afero.Fs, *systemd.Client, *systemd.MockDBusConn, podman.Interface, api.LoadResult) {
	t.Helper()
	fs := afero.NewMemMapFs()
	ctx, sd, mockConn, mockPodman, raw := setupTestWithFS(t, fs, fixture)
	return ctx, fs, sd, mockConn, mockPodman, raw
}

// testApply is a helper that builds a plan and applies it.
func testApply(_ *testing.T, ctx context.Context, fs afero.Fs, sd *systemd.Client, mockPodman podman.Interface, raw api.LoadResult, jr ...systemd.JournalReader) error {
	mgrs := newTestFileManagers(fs)
	var journalReader systemd.JournalReader = &systemd.MockJournalReader{}
	if len(jr) > 0 {
		journalReader = jr[0]
	}
	plan, err := BuildPlan(ctx, fs, mgrs, sd, journalReader, &systemd.MockQuadletGeneratorRunner{}, &systemd.MockSystemdAnalyzeRunner{}, nil, nil, raw)
	if err != nil {
		return err
	}
	return Apply(ctx, testutil.NewTestLogger(), sd, journalReader, mockPodman, mgrs, plan)
}

// mustApply builds a plan and applies it, failing the test on any error.
func mustApply(t *testing.T, ctx context.Context, fs afero.Fs, sd *systemd.Client, mockPodman podman.Interface, raw api.LoadResult, jr ...systemd.JournalReader) {
	t.Helper()
	if err := testApply(t, ctx, fs, sd, mockPodman, raw, jr...); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
}

// configFilePath returns the path of a config file on the test filesystem.
func configFilePath(containerName, mountPath string) string {
	return fmt.Sprintf("%s/%s/%s", filestore.DefaultContainerConfigDir, containerName, util.BasenameWithHashSuffix(mountPath))
}

// preWriteConfig writes a config file to the test filesystem, simulating an already-applied state.
func preWriteConfig(t *testing.T, fs afero.Fs, containerName, mountPath, content string, mode os.FileMode) {
	t.Helper()
	store := filestore.NewContainerConfigFileStore(fs)
	if err := store.WriteFile(model.ContainerUnitRef(containerName), model.ContainerMountPath(mountPath), content, mode); err != nil {
		t.Fatalf("preWriteConfig(%q, %q): %v", containerName, mountPath, err)
	}
}

// assertConfigFileExists checks that a config file for a container exists on the test filesystem.
func assertConfigFileExists(t *testing.T, fs afero.Fs, containerName, mountPath string) {
	t.Helper()
	path := configFilePath(containerName, mountPath)
	if _, err := fs.Stat(path); err != nil {
		t.Errorf("expected config file %q to exist: %v", path, err)
	}
}

// assertConfigFileAbsent checks that a config file for a container does not exist on the test filesystem.
func assertConfigFileAbsent(t *testing.T, fs afero.Fs, containerName, mountPath string) {
	t.Helper()
	path := configFilePath(containerName, mountPath)
	if _, err := fs.Stat(path); err == nil {
		t.Errorf("expected config file %q to be absent", path)
	}
}

// assertConfigDirEmpty checks that no config files remain for a container.
func assertConfigDirEmpty(t *testing.T, fs afero.Fs, containerName string) {
	t.Helper()
	store := filestore.NewContainerConfigFileStore(fs)
	files, err := store.ListFiles(model.ContainerUnitRef(containerName))
	if err == nil && len(files) > 0 {
		t.Errorf("expected config directory for %q to be empty, found: %v", containerName, files)
	}
}
