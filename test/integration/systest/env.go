//go:build integration

// Package systest is the harness for syslet's integration tests.
//
// It models one syslet "host" as an Env: a filesystem, a mock systemd D-Bus
// connection, a fake podman, and the file managers backing the config and
// build-context stores. A test seeds the prior on-host state onto the Env,
// declares the desired specs, runs a terminal (Plan or Apply), and reads the
// result back through the Env's assertions.
//
// The package carries //go:build integration because it wires together syslet,
// the loader and the filestores — the whole production apply path — and is only
// meaningful to the tagged suite in test/integration. Fakes that untagged unit
// tests also need live in internal/systemd/systemdtest instead.
package systest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/sops"
	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/systemd/systemdtest"
	"codeberg.org/xchangeee/syslet/internal/testlog"
)

// quadletDir is where the harness installs unit files, matching the path the
// production client uses on a real host.
const quadletDir = "/etc/containers/systemd"

// Env is one syslet "host" under test. Tests seed prior on-host state onto it,
// declare the desired specs, then call a terminal; assertions read back through
// it.
//
// The exported fields are the pieces tests legitimately reach into directly.
// The collaborators that only the terminals need — the loaded specs, the
// decryptor, the journal reader and the quadlet generator — stay unexported so
// that varying them goes through an Option and stays visible at New.
//
// There is deliberately no stored context: the containedctx linter forbids one,
// and no test has needed to cancel a plan or apply. The terminals below each
// take a fresh background context instead.
type Env struct {
	Fs      afero.Fs
	Systemd *systemd.Client
	Conn    *systemdtest.MockDBusConn
	Podman  *FakePodman
	Mgrs    filestore.FileManagers

	t         *testing.T
	raw       api.LoadResult
	decryptor *sops.Decryptor
	journal   systemd.JournalReader
	generator systemd.QuadletGeneratorRunner
	analyze   systemd.AnalyzeRunner
}

// Option customizes an Env at construction. Every axis the suite varies is an
// Option, so a test's deviation from the default host is legible on the New
// line rather than buried in a bespoke setup function.
type Option func(*envConfig)

// envConfig holds the choices an Option makes, before New has built anything.
// It is separate from Env because some options (the filesystem, the config
// store) must be decided before the client and the stores can be constructed.
type envConfig struct {
	fs            afero.Fs
	osConfigStore bool
	journal       systemd.JournalReader
	generator     systemd.QuadletGeneratorRunner
	analyze       systemd.AnalyzeRunner
	decryptor     *sops.Decryptor
}

// WithFs replaces the default in-memory filesystem, for tests that need to
// share one filesystem across several construction steps.
func WithFs(fs afero.Fs) Option {
	return func(c *envConfig) { c.fs = fs }
}

// WithOSConfigStore backs the container config store with a real OS filesystem
// under t.TempDir(). Required by configDir tests: the versioned-directory logic
// uses symlinks, which afero.MemMapFs does not support. Only Mgrs.Config
// becomes OS-backed — unit files stay on the in-memory filesystem.
func WithOSConfigStore() Option {
	return func(c *envConfig) { c.osConfigStore = true }
}

// WithQuadletGenerator swaps the no-op generator for one that records or
// fabricates staged files.
func WithQuadletGenerator(gen systemd.QuadletGeneratorRunner) Option {
	return func(c *envConfig) { c.generator = gen }
}

// WithJournalReader swaps the empty journal for one returning canned quadlet
// errors, used to drive the failure paths of daemon-reload.
func WithJournalReader(jr systemd.JournalReader) Option {
	return func(c *envConfig) { c.journal = jr }
}

// WithAnalyzeRunner swaps the clean `systemd-analyze verify` result for one
// reporting warnings, driving the post-render validation failure path.
func WithAnalyzeRunner(az systemd.AnalyzeRunner) Option {
	return func(c *envConfig) { c.analyze = az }
}

// WithDecryptor supplies a real SOPS decryptor, required by any test whose
// specs include a secret.
func WithDecryptor(d *sops.Decryptor) Option {
	return func(c *envConfig) { c.decryptor = d }
}

// New builds a host with an in-memory filesystem, an empty systemd, and a fake
// podman holding no resources. The systemd client is closed on test cleanup.
func New(t *testing.T, opts ...Option) *Env {
	t.Helper()

	cfg := &envConfig{
		journal:   &systemdtest.MockJournalReader{},
		generator: &systemdtest.NoopQuadletGenerator{},
		analyze:   &systemdtest.MockAnalyzeRunner{},
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.fs == nil {
		cfg.fs = afero.NewMemMapFs()
	}

	conn := systemdtest.NewMockDBusConn()
	sd := systemd.NewClientWithPaths(conn, cfg.fs, quadletDir)
	t.Cleanup(sd.Close)

	configStore := filestore.NewContainerConfigFileStore(cfg.fs)
	if cfg.osConfigStore {
		configStore = filestore.NewContainerConfigFileStoreAt(
			afero.NewOsFs(), filepath.Join(t.TempDir(), "config"))
	}

	return &Env{
		Fs:      cfg.fs,
		Systemd: sd,
		Conn:    conn,
		Podman:  NewFakePodman(),
		Mgrs: filestore.FileManagers{
			Config: configStore,
			Build:  filestore.NewBuildContextFileStore(cfg.fs),
		},
		t:         t,
		decryptor: cfg.decryptor,
		journal:   cfg.journal,
		generator: cfg.generator,
		analyze:   cfg.analyze,
	}
}

// With returns a shallow copy of the Env bound to t, sharing all of the
// original's recorded state.
//
// Use it inside a subtest that asserts on an Env built by the parent. Without
// it the assertions report against the parent's *testing.T, so a failing
// subtest is marked PASS while its parent fails — the failure is real but
// attributed to the wrong place.
func (e *Env) With(t *testing.T) *Env {
	t.Helper()
	sub := *e
	sub.t = t
	return &sub
}

// --- Terminals ---
//
// These are the single place syslet.BuildPlan and syslet.Apply are called from,
// collapsing the long, invariant argument tail that every test used to repeat.
//
// Which terminal a test uses is not a free choice. Apply runs BuildPlan first
// and refuses a plan carrying errors, so it already asserts everything a plan
// assertion would and then proves the decision was carried out. Default to it.
//
// Reach for Plan only when the plan itself is the subject and there is nothing
// on the host to observe — today that means the syslet.DisplayPlan tests in
// diff_test.go, which render a plan rather than execute one.

// Plan builds the plan, failing the test if plan construction itself errors.
// It deliberately does not check plan.HasErrors: the diff and validation tests
// need a plan that carries errors in order to render it.
func (e *Env) Plan() *syslet.ApplyPlan {
	e.t.Helper()
	plan, err := e.PlanErr()
	if err != nil {
		e.t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// PlanErr builds the plan and returns any construction error to the caller.
func (e *Env) PlanErr() (*syslet.ApplyPlan, error) {
	e.t.Helper()
	return syslet.BuildPlan(context.Background(), e.Fs, e.Mgrs, e.Systemd, e.journal, e.generator,
		e.analyze, e.Podman, e.decryptor, e.raw)
}

// Apply builds and applies the plan, failing the test on any error.
func (e *Env) Apply() {
	e.t.Helper()
	if err := e.ApplyErr(); err != nil {
		e.t.Fatalf("Apply failed: %v", err)
	}
}

// ApplyErr builds and applies the plan, returning the first error to the
// caller. Use when the error itself is the assertion.
func (e *Env) ApplyErr() error {
	e.t.Helper()
	plan, err := e.PlanErr()
	if err != nil {
		return err
	}
	return syslet.Apply(context.Background(), testlog.New(), e.Systemd, e.journal, e.Podman, e.Mgrs, plan)
}
