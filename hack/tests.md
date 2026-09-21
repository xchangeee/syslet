# Reorganize test helpers: `systest`, `systemdtest`, `testlog`

## Context

`test/integration/testutil_test.go` has grown to 578 lines and is the biggest
file in the integration suite (2940 lines total). Because it is a `_test.go`
file in `package integration`, everything it offers is unexported and reachable
only from that one package — which has three consequences visible in the tests:

1. **Threading noise.** `setupTest*` returns five values in fixed order
   (`ctx, sd, mockConn, mockPodman, raw`, plus `fs` from `setupTest`), and every
   test threads them back into `mustApply`. The `syslet.BuildPlan` argument tail
   (`&systemd.MockJournalReader{}, &systemd.MockQuadletGeneratorRunner{},
   &systemd.MockSystemdAnalyzeRunner{}, nil, nil`) is repeated verbatim ~8 times.
2. **Local re-implementations.** Because the shared helpers cannot be varied,
   three files grew private near-copies: `setupConfigDirTest`
   (`container_config_dir_test.go`, OS-backed config store for symlinks),
   `secretTestSetup` + `buildSecretPlan` (`secret_test.go`, needs a real
   decryptor and a concrete mock podman), and `buildTestPlan` (`volume_test.go`).
3. **Leaky types.** `mockPodmanClient`'s unexported fields (`deletedVolumes`,
   `deletedNetworks`, `deletedImages`, `existingSecrets`) are read/written
   directly from four files after a `mockPodman.(*mockPodmanClient)` downcast;
   `recordingQuadletGenerator.stagedFiles` likewise; and `wantFile` is declared
   in `container_config_file_test.go` but consumed by
   `container_state_restart_test.go` — a cross-file dependency the recent test
   split left behind.

Pulling the file into a real package exposes a second, pre-existing problem:
**test helpers are currently scattered across three locations with no rule
governing which goes where.** Measured on the current tree:

- Of `internal/testutil`'s 19 exported symbols, exactly **two** are used by
  `internal/` unit tests — `NewTestLogger` and `MockQuadletGenerator`, both from
  a single file, `internal/validate/staging_test.go`. The other **17** (all 15
  systemd assertions in `systemd.go`, plus `AssertFileMode` / `AssertFileContent`)
  have **zero** internal callers and are integration-only.
- Meanwhile `MockDBusConn`, `MockJournalReader`, `MockSystemdAnalyzeRunner` and
  `MockQuadletGeneratorRunner` live in the *production* package
  `internal/systemd` (`testing.go`), shipping test scaffolding in the real
  package.
- Three separate types implement the same quadlet-generator interface, in three
  packages: `systemd.MockQuadletGeneratorRunner` (no-op),
  `testutil.MockQuadletGenerator` (writes files), and
  `recordingQuadletGenerator` (records staged filenames).

The outcome we want is one rule, three homes:

| Package | Build tag | Holds | Importable by |
| --- | --- | --- | --- |
| `internal/testlog` | none | the test logger | everything |
| `internal/systemd/systemdtest` | none | every systemd fake | everything |
| `test/integration/systest` | `integration` | the integration harness + its assertions | integration tests only |

`internal/testutil` disappears entirely.

## Constraint that drives the layout

`systest` carries `//go:build integration`, so untagged packages — notably
`internal/validate`'s unit tests — **cannot** import it. Anything an `internal/`
unit test needs must therefore live in an untagged package. That is exactly why
`testlog` and `systemdtest` exist as separate homes rather than being folded
into `systest`.

---

## Part 1 — `internal/systemd/systemdtest`

New untagged package holding every systemd fake. `internal/systemd/testing.go`
is deleted; its contents move here, joined by the two quadlet fakes from
elsewhere:

```go
package systemdtest

// from internal/systemd/testing.go
type MockDBusConn struct{ ... }          // + NewMockDBusConn, SetUnitState
type MockJournalReader struct{ ... }
type MockAnalyzeRunner struct{ ... }     // was MockSystemdAnalyzeRunner

// the quadlet-generator family, now in one place
type NoopQuadletGenerator struct{ ... }      // was systemd.MockQuadletGeneratorRunner
type FakeQuadletGenerator struct{ Fs afero.Fs; Files map[string]string }
                                             // was testutil.MockQuadletGenerator
type RecordingQuadletGenerator struct{ ... } // was integration.recordingQuadletGenerator
```

Two naming notes. `systemdtest.MockSystemdAnalyzeRunner` stutters and `revive`'s
`exported` rule is configured with `sayRepetitiveInsteadOfStutters` in
[.golangci.yaml](.golangci.yaml), so drop the redundant segment →
`MockAnalyzeRunner`. And give the three quadlet fakes one naming family
(`Noop` / `Fake` / `Recording`) so the choice between them is legible at the
call site; this renames `MockQuadletGeneratorRunner` at ~8 sites.

### Resolving the import cycle

**Why `systemdtest` must import `systemd`.** Not for the interfaces — Go
satisfies `DBusConn`, `JournalReader`, `QuadletGeneratorRunner` and
`SystemdAnalyzeRunner` structurally, with no import. The dependency is four
data types the mock bodies reference: `UnitState`, `ActiveState` (plus
`ActiveStateActive` / `ActiveStateInactive`), and `AnalyzeResult`. Narrow, but
unavoidable short of relocating production types to suit test layout — which was
considered and rejected, since `AnalyzeResult` is genuinely systemd-specific and
fits no other package.

So `systemd` must not import `systemdtest`. `internal/systemd/client_test.go` is
an **in-package** test (`package systemd`) using `NewMockDBusConn` at 16 sites —
as written, that is a cycle.

Fix: convert it to the external test package `package systemd_test`. Verified
safe — it touches **no unexported identifiers**; every call is already exported
API (`NewClient`, `client.DaemonReload`, `client.WriteUnitFile`, …), and it
already imports `internal/model`.

Note the incremental cost is smaller than it first appears: those 16
`NewMockDBusConn` sites must change **regardless**, because the mock leaves the
package either way. Going external adds only the package clause plus ~29 further
qualifications (`NewClient` ×15, `ActiveState*` ×9, `UnitState` ×3,
`NewClientWithPaths`, `QuadletUnitDir`) — one mechanical pass, no restructuring.
It also locks in a property the file already satisfies: the client's tests
exercise only its public API.

`analyze_test.go` and `quadlet_test.go` use no mocks and stay `package systemd`.
Mixing in-package and external test packages in one directory is legal Go.

Resulting dependency directions, all acyclic:

```
systemdtest ──> systemd
systemd_test ──> systemd, systemdtest
validate's tests ──> systemdtest, testlog
systest ──> systemdtest, testlog, syslet, filestore, ...
```

## Part 2 — `internal/testlog`

Everything in `internal/testutil` except the two survivors moves to `systest`;
`MockQuadletGenerator` moves to `systemdtest`. That leaves `NewTestLogger`
alone, so rename the package to match its actual scope:

```go
// Package testlog provides the logger used by tests that exercise code paths
// requiring a *slog.Logger. It emits only ERROR-level output, keeping test
// runs quiet while still surfacing genuine failures.
package testlog

func New() *slog.Logger
```

~14 lines. Two call sites: `internal/validate/staging_test.go` and `systest`'s
`Apply`. `internal/testutil/` is then deleted.

## Part 3 — `test/integration/systest`

New directory `test/integration/systest/`, all files carrying
`//go:build integration`. Suggested split:

| File | Contents |
| --- | --- |
| `env.go` | `Env`, `New`, options, `Plan`/`Apply` terminals |
| `seed.go` | `SeedUnit` / `SeedActive` / `SeedConfigFile` / `SeedConfigDir` / `SeedBuildContext` |
| `spec.go` | `NewContainer` / `NewVolume` / `NewNetwork` / `NewBuild` + option funcs |
| `raw.go` | spec→JSON marshalling and loader plumbing (unexported where possible) |
| `podman.go` | `FakePodman` |
| `assert.go` | the 17 assertions absorbed from `internal/testutil`, plus `WantFile` |

### `Env`

```go
// Env is one syslet "host" under test: a filesystem, a mock systemd D-Bus
// connection, a fake podman, and the file managers backing the config and
// build-context stores. Tests seed prior on-host state onto it, declare the
// desired specs, then call Plan or Apply; assertions read back through it.
type Env struct {
    Ctx     context.Context
    Fs      afero.Fs
    Systemd *systemd.Client
    Conn    *systemdtest.MockDBusConn
    Podman  *FakePodman
    Mgrs    filestore.FileManagers
    // t, raw, decryptor, journal reader, quadlet generator unexported
}

func New(t *testing.T, opts ...Option) *Env
```

Options cover every axis the suite varies today:

- `WithFs(afero.Fs)` — replaces the `setupTest` / `setupTestWithFS` pair; `New`
  defaults to `afero.NewMemMapFs()`.
- `WithOSConfigStore()` — OS-backed `filestore.NewContainerConfigFileStoreAt`
  under `t.TempDir()`, absorbing `newConfigStore` + `setupConfigDirTest` from
  [container_config_dir_test.go](test/integration/container_config_dir_test.go).
  Unit files stay on the mem fs; only `Mgrs.Config` becomes OS-backed.
- `WithQuadletGenerator(...)` — for `systemdtest.RecordingQuadletGenerator`
  ([staging_test.go](test/integration/staging_test.go)) and
  `systemdtest.FakeQuadletGenerator` ([diff_test.go](test/integration/diff_test.go)).
- `WithJournalReader(...)` — replaces `testApply`'s variadic
  `jr ...systemd.JournalReader` hack.
- `WithDecryptor(*sops.Decryptor)` — replaces `buildSecretPlan`.

`New` builds the client via `systemd.NewClientWithPaths(conn, fs,
"/etc/containers/systemd")` and registers `t.Cleanup(sd.Close)`, as today.

### Seeding prior state

Replaces `testFixture`. Each method renders internally, removing the current
awkwardness where a test must construct `fs` up-front purely so
`renderContainer(t, fs, oldSpec)` can be passed *into* the fixture literal:

```go
func (e *Env) SeedUnit(spec model.Unit)           // render + WriteUnitFile
func (e *Env) SeedActive(spec model.Unit)         // SeedUnit + mark .service active
func (e *Env) SetUnitState(service, state string) // raw escape hatch
func (e *Env) SeedConfigFile(container, mountPath, content string, mode os.FileMode)
func (e *Env) SeedConfigDir(container string, mountPath model.ContainerMountPath, version int, files ...model.ContainerConfigFile)
func (e *Env) SeedBuildContext(unit, filename, content string, mode os.FileMode)
```

`SeedActive` derives the service name per unit type — `webapp.container` →
`webapp.service`, `data.volume` → `data-volume.service` — so tests stop writing
both strings by hand. Put that derivation in one place; if `internal/model`
already exposes an equivalent, call that rather than re-deriving.

`SeedUnit` subsumes `renderContainer` / `renderVolume` / `renderNetwork` /
`renderBuild` / `renderContainerWithStore` by type-switching on `model.Unit` and
using `e.Mgrs.Config.Resolve` / `e.Mgrs.Build.Resolve` — which is precisely what
`renderContainerWithStore` exists to special-case. Keep an exported
`func (e *Env) Render(spec model.Unit) string` for tests asserting on rendered
content directly.

### Desired specs

```go
func (e *Env) Specs(specs ...model.Unit)  // marshal → api.LoadSpecsReader
func (e *Env) SpecsJSON(lines ...string)  // raw JSON escape hatch
```

`SpecsJSON` is required because there is **no `model.SecretUnit`** — secrets
reach the loader as raw JSON only, which is why `loadResultWithSecret` in
[secret_test.go](test/integration/secret_test.go) hand-rolls its stream. Pair it
with `systest.SecretJSON(name string, ct model.Ciphertext) string`, so secret
tests become `env.SpecsJSON(systest.SecretJSON("myapp", ct))` mixed with
`env.Specs(...)` for container units. Both paths go through the real
`api.LoadSpecsReader`, preserving the property that tests exercise the
production loader.

### Terminals

```go
func (e *Env) Plan() *syslet.ApplyPlan       // t.Fatal on error; does NOT check HasErrors
func (e *Env) MustPlan() *syslet.ApplyPlan   // additionally t.Fatal on plan.HasErrors()
func (e *Env) PlanErr() (*syslet.ApplyPlan, error)
func (e *Env) Apply()                        // t.Fatal on error
func (e *Env) ApplyErr() error
```

This is the payoff: the repeated `syslet.BuildPlan(ctx, fs, mgrs, sd, jr, qg,
analyze, podman, decryptor, raw)` tail collapses to one call site.

Keep `Plan` and `MustPlan` distinct — `buildSecretPlan` fails on
`plan.HasErrors()`, but [diff_test.go](test/integration/diff_test.go) and the
validation paths need plans that *carry* errors. `Apply` must keep passing
`testlog.New()` to `syslet.Apply`.

### `FakePodman`

Replaces `mockPodmanClient`, removing both the downcast and the
unexported-field reads:

```go
type FakePodman struct{ /* unexported slices */ }

func (p *FakePodman) DeletedVolumes() []string
func (p *FakePodman) DeletedNetworks() []string
func (p *FakePodman) DeletedImages() []string
func (p *FakePodman) UpsertedSecrets() []UpsertedSecret
func (p *FakePodman) DeletedSecrets() []string
func (p *FakePodman) SeedSecrets(...podman.SecretMeta)  // seeds ListSecrets
```

Reached as `env.Podman`, already concrete. `SeedSecrets` replaces the six direct
writes to `.existingSecrets` in `secret_test.go`.

`UpsertedSecrets()` / `DeletedSecrets()` have **no readers** today — secret
assertions go through `plan.UpsertPodmanSecrets` / `plan.DeletePodmanSecrets`.
Keep the recording (the fake must implement all of `podman.Interface` anyway,
and it is the only way to assert the apply side) but note it is currently
uncalled surface.

### Assertions

`systest` **absorbs** all 17 integration-only helpers from `internal/testutil` —
the 15 systemd assertions plus `AssertFileMode` / `AssertFileContent` — rather
than forwarding to them. Re-expose the systemd ones as `Env` methods, since
`Env` already holds `Conn` and `Systemd`:

```go
env.AssertRestarted("webapp.service")
env.AssertReloaded()
env.AssertNoStartStop()
env.AssertStarted("a.service", "b.service")
env.AssertUnitExists("webapp.container")
env.AssertUnitAbsent("old.container")
// ... all 15
```

Config-store assertions become methods too, since they encode store layout and
now have a store to ask:

```go
func (e *Env) AssertConfigFileExists(container, mountPath string)
func (e *Env) AssertConfigFileAbsent(container, mountPath string)
func (e *Env) AssertConfigDirEmpty(container string)
func (e *Env) ConfigFilePath(container, mountPath string) string
func (e *Env) AssertFiles(container string, want ...WantFile)
```

Move `wantFile` here as exported `systest.WantFile{MountPath, Mode, Content}`,
resolving the
[container_config_file_test.go](test/integration/container_config_file_test.go) →
[container_state_restart_test.go](test/integration/container_state_restart_test.go)
cross-file dependency.

Keep `AssertFileMode` / `AssertFileContent` **also** as package-level funcs
taking an explicit `fs` — the configDir tests assert against the OS-backed store
rather than `env.Fs`.

### Spec builders

Collapse the eleven `make*Spec*` variants into option-style builders:

```go
func NewContainer(name, image string, opts ...ContainerOpt) *model.ContainerUnit
func NewVolume(name, device string, opts ...UnitOpt) *model.VolumeUnit
func NewNetwork(name, driver string, opts ...UnitOpt) *model.NetworkUnit
func NewBuild(name, imageTag string, opts ...UnitOpt) *model.BuildUnit
```

Options: `Running`, `Stopped`, `Stale`, `Oneshot`, `Reclaim(policy)`,
`Files(...model.ContainerFileMount)`, `Dirs(...model.ContainerDirMount)`,
`WithNetwork(name)`, `WithVolume(volume, path)`, `WithBuild(name)`,
`Option(section, key, value)`.

Default a bare `NewContainer` to `model.DesiredStateRunning` — overwhelmingly
the common case. `Dirs` must keep injecting `[Service] ExecReload=`, which the
validator requires for any container with configDirs (see the comment on
`makeContainerSpecWithDirs`). `WithNetwork` / `WithVolume` / `WithBuild` absorb
the three near-identical private helpers in `network_test.go`, `volume_test.go`
and `build_test.go`. `Option` taking `...string` and calling `model.MultiUV`
makes the `multiUV` helper unnecessary.

### Resulting test shape

```go
func TestContainerImageChanged_RestartsService(t *testing.T) {
 env := systest.New(t)
 env.SeedActive(systest.NewContainer("webapp", "nginx:latest"))
 env.Specs(systest.NewContainer("webapp", "nginx:alpine"))

 env.Apply()

 env.AssertRestarted("webapp.service")
 env.AssertReloaded()
}
```

---

## Part 4 — close the `make check` build-tag gap

Part of `make check` cannot see tagged files. Measured on the current tree, the
gap is **pre-existing** — it already applies to all 12 integration test files —
and narrower than it looks:

| `check` step | Sees tagged files? | Why |
| --- | --- | --- |
| `golangci-lint fmt ./...` | ✅ yes | `.golangci.yaml` sets `run.build-tags: [integration]` |
| `golangci-lint run ./...` | ✅ yes | same; verbose output confirms `[loader] Using build tags: [integration]` |
| `go fix ./...` | ❌ no | untagged |
| `go vet ./...` | ❌ no | untagged |

The two `go` steps skip the directory *silently*: untagged,
`go list ./test/integration/...` reports `matched no packages` and exits 0.

Fix — [Makefile](Makefile), `check` target:

```make
check:
 golangci-lint fmt ./...
 go fix ./...
 go fix -tags=integration ./test/integration/...
 go vet ./...
 go vet -tags=integration ./test/integration/...
 golangci-lint run ./...
```

Both `go fix` and `go vet` accept `-tags` (verified on the toolchain in use,
go1.27.0). Scope the tagged lines to `./test/integration/...` rather than
`./...`, or the untagged lines re-analyze every other package a second time for
no benefit.

---

## Sequencing

Four commits, each independently green — this keeps the mechanical moves
separable from the large test rewrite:

1. **Makefile** — the tagged `go vet` / `go fix` lines. Confirm `make check` is
   green on the *current* tree, so the widened net is proven against existing
   files and any later failure is attributable to the refactor.
2. **`internal/systemd/systemdtest`** — create the package, delete
   `internal/systemd/testing.go`, convert `client_test.go` to `package
   systemd_test`, apply the renames. Update `internal/validate/staging_test.go`
   and the integration files' mock references.
3. **`internal/testlog`** — extract `NewTestLogger` → `testlog.New`, move the 17
   integration-only helpers into a stub `systest/assert.go`, delete
   `internal/testutil/`.
4. **`systest` + test rewrite** — the rest of the package, rewrite all 12 test
   files, delete `testutil_test.go`.

## Files to change

**New:** `internal/systemd/systemdtest/*.go`, `internal/testlog/testlog.go`,
`test/integration/systest/*.go`.

**Deleted:** `internal/testutil/` (all 3 files),
`internal/systemd/testing.go`,
[test/integration/testutil_test.go](test/integration/testutil_test.go).

**Rewritten — all 12 test files** in [test/integration/](test/integration/),
same mechanical pattern each: replace the `setupTest*` + fixture-literal
preamble with `systest.New(t)` + `Seed*` + `Specs`, replace `mustApply(...)`
with `env.Apply()`, replace downcasts with `env.Podman.Deleted*()`, and delete
the file-local duplicate helpers. Three files need more than mechanical work:

- [container_config_dir_test.go](test/integration/container_config_dir_test.go) —
  drop `newConfigStore`, `setupConfigDirTest`, `preWriteConfigDir` in favour of
  `systest.New(t, systest.WithOSConfigStore())` + `env.SeedConfigDir`.
- [secret_test.go](test/integration/secret_test.go) — drop `secretTestSetup`,
  `buildSecretPlan`, `loadResultWithSecret`. **Keep** `secretTestDecryptor`,
  `encryptedCiphertext`, `secretContentHash` in the test package: they read
  `testdata/keys.txt` and `testdata/encrypted.yaml` by *relative* path, resolved
  against the test binary's CWD (`test/integration/`). Moving them into
  `systest` would silently break those paths.
- [diff_test.go](test/integration/diff_test.go) — plan-only, no apply; uses
  `env.Plan()` plus `syslet.DisplayPlan` directly.

**Touched:** [internal/validate/staging_test.go](internal/validate/staging_test.go)
(`testutil.NewTestLogger` → `testlog.New`, `testutil.MockQuadletGenerator` →
`systemdtest.FakeQuadletGenerator`), [internal/systemd/client_test.go](internal/systemd/client_test.go)
(external test package), [Makefile](Makefile).

## Verification

1. **Capture a baseline first**, on the current tree:
   `go test -tags=integration -count=1 -v ./test/integration/... | grep -c '^=== RUN'`
   and the same for `make test`. The counts must match afterwards — this is the
   guard against tests silently disappearing during the rewrite.
2. `make test` — the `internal/` unit tests, which exercise commits 2 and 3
   (`systemdtest`, `testlog`, the `systemd_test` package conversion).
3. `make test-integration` — the real gate for commit 4. Every test must pass
   **unchanged in assertion content**; this is a refactor of scaffolding only,
   so any behavioural difference is a bug in the extraction, not a test to
   update.
4. `make check` — fmt, `go fix`, vet, lint, now including the tagged
   `test/integration/...` lines, so `systest` itself is vetted. Watch for
   `revive`'s `exported` rule on the new packages: every exported symbol needs a
   doc comment, and `sayRepetitiveInsteadOfStutters` is enabled.
5. `make coverage` — confirm the combined report still builds and integration
   coverage has not dropped. `systest` and `systemdtest` will appear in the
   `-coverpkg=./...` denominator, as `internal/testutil` already does.
6. Sanity-check the two non-mem-fs paths explicitly, the most likely to break:
   `go test -tags=integration -run 'ConfigDir' -v` (OS-backed store, symlinks)
   and `go test -tags=integration -run 'Secret' -v` (relative `testdata/` paths).

No red-green cycle applies: no new behaviour is added, and the existing tests
are themselves the regression suite for the change.
