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
// tests also need live beside the package they fake instead — see
// internal/systemd/systemdtest and internal/podman/podmantest. What stays here
// is what genuinely cannot leave: the recording filesystem and the phase
// ranking, which are about the wiring rather than about one collaborator.
package systest

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"codeberg.org/xchangeee/syslet/internal/api"
	"codeberg.org/xchangeee/syslet/internal/filestore"
	"codeberg.org/xchangeee/syslet/internal/podman/podmantest"
	"codeberg.org/xchangeee/syslet/internal/sops"
	"codeberg.org/xchangeee/syslet/internal/syslet"
	"codeberg.org/xchangeee/syslet/internal/systemd"
	"codeberg.org/xchangeee/syslet/internal/systemd/systemdtest"
	"codeberg.org/xchangeee/syslet/test/log"
	"codeberg.org/xchangeee/syslet/test/oplog"
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
	Podman  *podmantest.FakePodman
	Mgrs    filestore.FileManagers

	// Log is the one ordered timeline every fake appends to. Apply resets it
	// before running and checks it afterwards, which is what makes the phase
	// boundaries in syslet.Apply assertable at all; see order.go.
	Log *oplog.Log

	// Logs captures what apply logged. Some outcomes — the quadlet generator's
	// errors after a failed daemon-reload, a best-effort reclamation that did
	// not happen — reach the operator only as log records, so a test needs to
	// read them back.
	Logs *testlog.Capture

	t         *testing.T
	raw       api.LoadResult
	decryptor *sops.Decryptor
	journal   systemd.JournalReader
	generator systemd.QuadletGeneratorRunner
	analyze   systemd.AnalyzeRunner
	logger    *slog.Logger

	// configFs is the filesystem behind Mgrs.Config, which is not Fs whenever
	// WithOSConfigStore is in play. Assertions that read the config store
	// directly — listing configDir versions, say — must go through it rather
	// than guessing which filesystem holds the bytes.
	configFs afero.Fs
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

	// One log, shared by every fake: the systemd connection, the fake podman,
	// and the filesystems behind all three stores. Recording into separate
	// buckets would show that each effect happened but never that they happened
	// in the order Apply's phases promise.
	log := &oplog.Log{}

	// The config store's directory is decided here rather than read back from
	// the store, because the recorder has to classify paths before the store
	// that owns them exists.
	configDir := filestore.DefaultContainerConfigDir
	configBaseFs := cfg.fs
	if cfg.osConfigStore {
		configDir = filepath.Join(t.TempDir(), "config")
		configBaseFs = afero.NewOsFs()
	}
	rec := newFsRecorder(log, quadletDir, configDir, filestore.DefaultBuildContextDir)

	unitFs := newRecordingFs(cfg.fs, rec)
	conn := systemdtest.NewMockDBusConn()
	conn.Log = log
	sd := systemd.NewClientWithPaths(conn, unitFs, quadletDir)
	t.Cleanup(sd.Close)

	configFs := newRecordingFs(configBaseFs, rec)
	configStore := filestore.NewContainerConfigFileStoreAt(configFs, configDir)

	pc := podmantest.NewFakePodman()
	pc.Log = log

	logger, capture := testlog.NewCapture()

	return &Env{
		Fs:      unitFs,
		Systemd: sd,
		Conn:    conn,
		Podman:  pc,
		Mgrs: filestore.FileManagers{
			Config: configStore,
			Build:  filestore.NewBuildContextFileStore(newRecordingFs(cfg.fs, rec)),
		},
		Log:       log,
		Logs:      capture,
		t:         t,
		decryptor: cfg.decryptor,
		journal:   cfg.journal,
		generator: cfg.generator,
		analyze:   cfg.analyze,
		logger:    logger,
		configFs:  configFs,
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
//
// Every apply is also checked for phase order: the effects it produced must
// follow the sequence syslet.Apply documents — stops, then file content, then
// secrets, then unit files, then reclamation, then daemon-reload, then reloads
// and starts. That check is automatic rather than opt-in because it protects
// invariants no individual test would think to restate, and because a test
// that forgets it silently loses the protection. See order.go.
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
	// Seeding writes through the same stores and the same filesystem that apply
	// does, so the timeline is cleared here — after planning, immediately before
	// the effects under test — rather than at construction.
	e.Log.Reset()
	applyErr := syslet.Apply(context.Background(), e.logger, e.Systemd, e.journal, e.Podman, e.Mgrs, plan)
	e.assertPhaseOrder()
	return applyErr
}

// ResetRecordings clears everything the fakes recorded while leaving the host
// state — unit files, config files, active services, the podman secret store —
// exactly as the last apply left it.
//
// This is the seam that makes a second apply assertable: the host carries over,
// the record of how it got there does not.
func (e *Env) ResetRecordings() {
	e.t.Helper()
	e.Log.Reset()
	e.Logs.Reset()
	e.Conn.Started = nil
	e.Conn.Stopped = nil
	e.Conn.ReloadedUnits = nil
	e.Conn.Reloaded = false
	e.Podman.ResetRecordings()
}

// AssertIdempotent applies, then applies again against the state the first
// apply left behind, and fails unless the second run changes nothing at all.
//
// Reaching a fixed point in one pass is the defining property of a converger,
// and it is the assertion that catches a whole class of bugs no single
// after-the-fact check does: a unit file written with the wrong content, a
// secret upserted without the syslet/hash label its own change detection reads,
// a configDir whose files never made it to disk. Each of those looks correct in
// isolation and betrays itself on the second run, as work that should not exist.
func (e *Env) AssertIdempotent() {
	e.t.Helper()
	e.Apply()
	e.ResetRecordings()
	e.Apply()
	e.AssertNoEffects()
}
