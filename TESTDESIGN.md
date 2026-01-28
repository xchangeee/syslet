# syslet Integration Test Design

This document outlines the integration test strategy for syslet, a declarative container reconciliation daemon.

## Goals

1. Verify the reconciliation loop correctly manages container, volume, and network lifecycle
2. Validate the two-phase reconciliation algorithm (diff → execute)
3. Ensure strict execution ordering (stop → configs → units → reload → start)
4. Test immutability enforcement for volumes and networks
5. Validate gRPC API behavior end-to-end
6. Achieve confidence the system works without requiring real Podman/systemd

## Test Infrastructure

### Mock Dependencies

| Component | Real Implementation | Test Mock | Purpose |
|-----------|---------------------|-----------|---------|
| Filesystem | `afero.OsFs` | `afero.MemMapFs` | Unit file and config file operations |
| D-Bus | `dbus.Conn` | `MockDBusConn` | systemd unit control and state queries |
| Time | `quartz.NewReal()` | `quartz.NewMock()` | Control reconciliation timing |
| SQLite | File-based DB | In-memory `:memory:` | Spec persistence |
| Journal | `journalctl` subprocess | `MockJournal` | Log streaming |

### MockDBusConn

A mock implementation of the `DBusConn` interface that simulates systemd behavior:

```go
type MockDBusConn struct {
    mu     sync.Mutex
    units  map[string]*MockUnit  // unit name → state
    reloads int                  // daemon-reload count

    // Error injection
    reloadErr     error
    startErr      map[string]error
    stopErr       map[string]error
    getPropsErr   map[string]error
}

type MockUnit struct {
    ActiveState    string  // "active", "inactive", "failed", "activating", "deactivating"
    UnitFileState  string  // "enabled", "disabled", "static"
    StartCount     int
    StopCount      int
}
```

**Behavior:**
- `GetUnitPropertiesContext()` returns `ActiveState` and `UnitFileState` from `MockUnit`
- `StartUnitContext()` transitions unit to "active", increments `StartCount`
- `StopUnitContext()` transitions unit to "inactive", increments `StopCount`
- `ReloadContext()` increments `reloads` counter
- All operations can return injected errors for failure testing

### Test Harness

```go
type TestHarness struct {
    Daemon     *daemon.Daemon
    Reconciler *daemon.Reconciler
    Store      *store.Store
    Systemd    *systemd.Client
    Config     *containerconfig.Manager
    API        *api.Server

    // Mocks
    FS         afero.Fs
    DBus       *MockDBusConn
    Clock      *quartz.Mock

    // Helpers
    t          *testing.T
}

func NewTestHarness(t *testing.T) *TestHarness
func (h *TestHarness) ApplySpec(specJSON string) error
func (h *TestHarness) Reconcile() []daemon.UnitResult
func (h *TestHarness) AdvanceTime(d time.Duration)
func (h *TestHarness) AssertUnitFileExists(name string)
func (h *TestHarness) AssertUnitFileContent(name, expected string)
func (h *TestHarness) AssertConfigFileExists(unit, filename string)
func (h *TestHarness) AssertUnitStarted(name string)
func (h *TestHarness) AssertUnitStopped(name string)
func (h *TestHarness) AssertDaemonReloaded(times int)
```

---

## Test Categories

### 1. Unit Tests (existing)

Already covered in `parser/parser_test.go` and `spec/convert_test.go`. These test pure logic without I/O.

### 2. Component Integration Tests

Test individual components with mocked dependencies.

### 3. Reconciler Integration Tests

Test the full reconciliation algorithm with all mocks wired together.

### 4. API Integration Tests

Test gRPC endpoints through the full stack.

---

## Test Scenarios

### Category A: Container Lifecycle

#### A1. Create new container

**Scenario:** Apply a container spec when no unit exists.

**Setup:**
- Empty filesystem (no unit files)
- DBus returns "unit not found" for state queries

**Input:**
```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "nginx:latest"
    }
  }
}
```

**Expected:**
1. Diff phase detects new unit (`IsNew=true`)
2. Execute phase:
   - Writes `/etc/containers/systemd/webapp.container`
   - Calls `daemon-reload` once
   - Calls `StartUnit("webapp.service")`
3. Result shows "created, started"

**Assertions:**
- `AssertUnitFileExists("webapp.container")`
- `AssertDaemonReloaded(1)`
- `AssertUnitStarted("webapp.service")`

---

#### A2. Update running container (unit file change)

**Scenario:** Modify the image tag of a running container.

**Setup:**
- Existing unit file with `Image=nginx:1.24`
- DBus returns `ActiveState=active` for `webapp.service`

**Input:**
```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "nginx:1.25"
    }
  }
}
```

**Expected:**
1. Diff detects unit file changed (`UnitChanged=true`)
2. Execute phase (strict order):
   1. Stop `webapp.service` (uses old config for clean shutdown)
   2. Write new unit file
   3. Call `daemon-reload`
   4. Start `webapp.service`

**Assertions:**
- `AssertUnitStopped("webapp.service")` before unit file write
- `AssertUnitFileContent("webapp.container", contains "Image=nginx:1.25")`
- `AssertDaemonReloaded(1)`
- `AssertUnitStarted("webapp.service")`

---

#### A3. Stop running container (DesiredState change)

**Scenario:** Change `desiredState` from "running" to "stopped".

**Setup:**
- Existing unit file
- DBus returns `ActiveState=active`

**Input:**
```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "stopped",
  "unit": { "Container": { "Image": "nginx:latest" } }
}
```

**Expected:**
1. Diff detects state mismatch (`NeedsStop=true`)
2. Execute stops the unit
3. Unit file is updated with new `[X-Syslet]` section

**Assertions:**
- `AssertUnitStopped("webapp.service")`
- Unit file contains `DesiredState=stopped`

---

#### A4. Start stopped container

**Scenario:** Change `desiredState` from "stopped" to "running".

**Setup:**
- Existing unit file with `DesiredState=stopped`
- DBus returns `ActiveState=inactive`

**Input:**
```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": { "Container": { "Image": "nginx:latest" } }
}
```

**Expected:**
1. Diff detects `NeedsStart=true`
2. Execute starts the unit

**Assertions:**
- `AssertUnitStarted("webapp.service")`

---

#### A5. No-op when container is in desired state

**Scenario:** Apply identical spec to running container.

**Setup:**
- Existing unit file (matching spec)
- DBus returns `ActiveState=active`

**Expected:**
1. Diff detects no changes
2. Execute does nothing

**Assertions:**
- No `StopUnit` calls
- No `StartUnit` calls
- No `daemon-reload`
- Result shows "unchanged" or no change indicator

---

#### A6. Delete container

**Scenario:** Delete a running container via API.

**Setup:**
- Existing unit file and config files
- DBus returns `ActiveState=active`

**Expected:**
1. Stop the container
2. Remove unit file
3. Remove config directory
4. Call `daemon-reload`

**Assertions:**
- `AssertUnitStopped("webapp.service")`
- Unit file does not exist
- Config directory removed
- `AssertDaemonReloaded(1)`

---

### Category B: Config File Management

#### B1. Create container with config files

**Scenario:** Apply container spec with embedded configs.

**Input:**
```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "nginx:latest"
    }
  },
  "configs": [
    {
      "content": "server { listen 80; }",
      "targetVolumePath": "/etc/nginx/nginx.conf"
    },
    {
      "content": "APP_ENV=production",
      "targetVolumePath": "/run/env"
    }
  ]
}
```

**Expected:**
1. Config files written to `/var/syslet/config/webapp/`
2. Unit file includes auto-injected `Volume=` entries for configs
3. Container started

**Assertions:**
- `AssertConfigFileExists("webapp", "nginx.conf")`
- `AssertConfigFileExists("webapp", "env")`
- Unit file contains volume mount for config directory

---

#### B2. Update config file only (no unit change)

**Scenario:** Change config content without changing the container spec.

**Setup:**
- Existing container with config file containing "v1"

**Input:**
- Same spec but config content is "v2"

**Expected:**
1. Diff detects config changed (`ConfigChanged=true`)
2. Execute:
   1. Stop container (for clean config reload)
   2. Write new config file
   3. Start container
3. No `daemon-reload` (unit file unchanged)

**Assertions:**
- `AssertUnitStopped("webapp.service")`
- Config file contains "v2"
- `AssertDaemonReloaded(0)` (no reload needed)
- `AssertUnitStarted("webapp.service")`

---

#### B3. Add new config file to existing container

**Scenario:** Add a second config file to existing container.

**Setup:**
- Container with one config file

**Input:**
- Spec with two config files

**Expected:**
1. Diff detects config changed
2. New config file written
3. Unit file updated with new volume mount
4. Container restarted

---

#### B4. Remove config file from container

**Scenario:** Remove a config file from container spec.

**Setup:**
- Container with two config files

**Input:**
- Spec with one config file

**Expected:**
1. Removed config file is deleted
2. Unit file updated (volume mount removed)
3. Container restarted

---

### Category C: Volume and Network Immutability

#### C1. Create new volume

**Scenario:** Apply a volume spec.

**Input:**
```json
{
  "name": "data",
  "type": "volume",
  "unit": {
    "Volume": {
      "Label": "app=webapp"
    }
  }
}
```

**Expected:**
1. Volume unit file written
2. `daemon-reload` called
3. No start/stop (volumes are not startable)

**Assertions:**
- `AssertUnitFileExists("data.volume")`
- `AssertDaemonReloaded(1)`
- No `StartUnit` or `StopUnit` calls

---

#### C2. Reject volume modification (immutable)

**Scenario:** Attempt to modify an existing volume.

**Setup:**
- Existing volume with `Label=app=webapp`

**Input:**
```json
{
  "name": "data",
  "type": "volume",
  "unit": {
    "Volume": {
      "Label": "app=different"
    }
  }
}
```

**Expected:**
1. Diff detects change and marks as rejected (`Rejected=true`)
2. Execute skips this unit
3. Error logged/returned

**Assertions:**
- Unit file unchanged (still has original label)
- Result includes rejection error
- No `daemon-reload` for this unit

---

#### C3. Create new network

**Scenario:** Apply a network spec.

**Input:**
```json
{
  "name": "internal",
  "type": "network",
  "unit": {
    "Network": {
      "Subnet": "10.89.0.0/24",
      "Gateway": "10.89.0.1"
    }
  }
}
```

**Expected:**
- Network unit file written
- `daemon-reload` called

---

#### C4. Reject network modification (immutable)

Same pattern as C2 but for networks.

---

#### C5. Volume unchanged (no-op)

**Scenario:** Apply identical volume spec.

**Expected:**
- No changes made
- No `daemon-reload`

---

### Category D: Multi-Unit Reconciliation

#### D1. Create multiple units in single apply

**Scenario:** Apply container + volume + network together.

**Input:**
- `webapp.json` (container)
- `webapp-data.json` (volume)
- `webapp-net.json` (network)

**Expected:**
1. Diff phase processes all three
2. Execute phase:
   - Write all unit files
   - Single `daemon-reload` (not three)
   - Start only the container

**Assertions:**
- `AssertDaemonReloaded(1)` (single reload for all)
- `AssertUnitStarted("webapp.service")`
- No start calls for volume/network

---

#### D2. Update multiple containers

**Scenario:** Update two containers simultaneously.

**Setup:**
- Two running containers: `app1`, `app2`

**Input:**
- Modified specs for both

**Expected:**
1. Both stopped (in sequence)
2. Both unit files written
3. Single `daemon-reload`
4. Both started (in sequence)

**Assertions:**
- Both containers stopped before any unit file written
- `AssertDaemonReloaded(1)`
- Both containers started

---

#### D3. Mixed changes (some units changed, some unchanged)

**Scenario:** Apply specs where only some units changed.

**Setup:**
- Three units: `app1` (changed), `app2` (unchanged), `app3` (new)

**Expected:**
- `app1`: stopped, updated, restarted
- `app2`: no action
- `app3`: created, started
- Single `daemon-reload`

---

#### D4. Partial failure handling

**Scenario:** One unit fails to start, others succeed.

**Setup:**
- Mock `StartUnit` to fail for `app2`

**Expected:**
- `app1` starts successfully
- `app2` fails with error
- `app3` starts successfully
- Results include error for `app2`

---

### Category E: Reconciliation Loop

#### E1. Periodic reconciliation

**Scenario:** Verify reconciliation runs on timer.

**Setup:**
- Use `quartz.Mock` clock
- Configure 10-second interval

**Test:**
1. Start daemon
2. Advance clock by 10 seconds
3. Verify reconciliation ran

**Assertions:**
- Reconciliation count increases after each interval

---

#### E2. Reconciliation detects drift

**Scenario:** Unit manually stopped outside syslet.

**Setup:**
- Container with `desiredState=running`
- DBus returns `ActiveState=inactive` (someone stopped it)

**Expected:**
- Reconciliation detects drift
- Container restarted

---

#### E3. Reconciliation handles failures gracefully

**Scenario:** D-Bus connection temporarily fails.

**Setup:**
- Mock `ReloadContext` to return error

**Expected:**
- Reconciliation logs error
- System continues operating
- Next reconciliation retries

---

### Category F: gRPC API

#### F1. Apply endpoint

**Test:** `rpc Apply(ApplyRequest) returns (ApplyResponse)`

- Apply single spec
- Apply multiple specs
- Apply invalid JSON (error)
- Apply unsupported unit type (error)

---

#### F2. Status endpoint

**Test:** `rpc Status(StatusRequest) returns (StatusResponse)`

- Status of existing unit
- Status of non-existent unit (error)
- Status includes active state, desired state, last reconciled time

---

#### F3. List endpoint

**Test:** `rpc List(ListRequest) returns (ListResponse)`

- List all units
- List with type filter (container/volume/network)
- List when empty

---

#### F4. Delete endpoint

**Test:** `rpc Delete(DeleteRequest) returns (DeleteResponse)`

- Delete existing container (stops and removes)
- Delete existing volume
- Delete non-existent unit (error or no-op?)

---

#### F5. Logs endpoint

**Test:** `rpc Logs(LogsRequest) returns (stream LogEntry)`

- Stream logs for existing unit
- Follow mode (`-f`)
- Non-existent unit (error)

---

### Category G: Edge Cases

#### G1. Empty spec list

**Scenario:** Reconcile with no specs in store.

**Expected:**
- No actions taken
- No errors

---

#### G2. Invalid unit file on disk

**Scenario:** Manually corrupted unit file exists.

**Setup:**
- Write garbage to `/etc/containers/systemd/broken.container`

**Expected:**
- Reconciler handles gracefully
- Logs warning
- Continues processing other units

---

#### G3. Concurrent API requests

**Scenario:** Multiple Apply requests simultaneously.

**Expected:**
- Requests serialized safely
- No race conditions
- All changes applied

---

#### G4. Very long config file

**Scenario:** Config file > 1MB.

**Expected:**
- Written correctly
- Checksum calculated correctly

---

#### G5. Special characters in names

**Scenario:** Unit name with special characters.

**Input:**
```json
{ "name": "my-app_v2.0", "type": "container", ... }
```

**Expected:**
- Handled correctly
- File named `my-app_v2.0.container`

---

#### G6. Unicode in config content

**Scenario:** Config file with UTF-8 content.

**Expected:**
- Content preserved exactly
- No encoding issues

---

### Category H: Execution Order Verification

These tests specifically verify the strict ordering requirements from PLAN.md.

#### H1. Stop executes before unit file write

**Scenario:** Update running container.

**Verification:**
- Record timestamp of `StopUnit` call
- Record timestamp of unit file write
- Assert stop timestamp < write timestamp

---

#### H2. Config files written before unit files

**Scenario:** Update container with config changes and unit changes.

**Verification:**
- Record order of file writes
- Assert config files written before unit file

---

#### H3. Daemon-reload after all unit writes

**Scenario:** Update multiple units.

**Verification:**
- Record timestamp of each unit file write
- Record timestamp of `daemon-reload`
- Assert all writes complete before reload

---

#### H4. Start executes after daemon-reload

**Scenario:** Create new container.

**Verification:**
- Record timestamp of reload
- Record timestamp of `StartUnit`
- Assert reload < start

---

## Test Utilities

### Spec Builders

```go
func ContainerSpec(name string) *SpecBuilder
func VolumeSpec(name string) *SpecBuilder
func NetworkSpec(name string) *SpecBuilder

// Example usage:
spec := ContainerSpec("webapp").
    Image("nginx:latest").
    PublishPort("8080:80").
    DesiredState("running").
    WithConfig("/etc/nginx/nginx.conf", "server {}").
    Build()
```

### Assertion Helpers

```go
func (h *TestHarness) AssertUnitFileExists(name string)
func (h *TestHarness) AssertUnitFileNotExists(name string)
func (h *TestHarness) AssertUnitFileContent(name string, contains ...string)
func (h *TestHarness) AssertConfigFileExists(unit, filename string)
func (h *TestHarness) AssertConfigFileContent(unit, filename, expected string)
func (h *TestHarness) AssertUnitStarted(name string)
func (h *TestHarness) AssertUnitStopped(name string)
func (h *TestHarness) AssertUnitNotStarted(name string)
func (h *TestHarness) AssertDaemonReloaded(times int)
func (h *TestHarness) AssertNoErrors(results []daemon.UnitResult)
func (h *TestHarness) AssertError(results []daemon.UnitResult, unitName, errorContains string)
```

### Operation Recording

```go
type Operation struct {
    Type      string    // "stop", "start", "reload", "write_unit", "write_config"
    Unit      string
    Timestamp time.Time
}

func (h *TestHarness) Operations() []Operation
func (h *TestHarness) AssertOperationOrder(ops ...string)
```

---

## Implementation Plan

### Phase 1: Test Infrastructure

1. Create `internal/server/testutil/` package
2. Implement `MockDBusConn`
3. Implement `TestHarness`
4. Implement spec builders
5. Implement assertion helpers

### Phase 2: Component Tests

1. `store/store_test.go` - SQLite operations
2. `containerconfig/manager_test.go` - Config file operations
3. `systemd/client_test.go` - Unit operations with mock D-Bus

### Phase 3: Reconciler Tests

1. `daemon/reconciler_test.go` - Categories A, B, C, D
2. `daemon/daemon_test.go` - Category E (loop behavior)

### Phase 4: API Tests

1. `api/server_test.go` - Category F

### Phase 5: Edge Cases and Order Verification

1. Add Category G tests to relevant files
2. Add Category H tests to reconciler tests

---

## Running Tests

```sh
# Run all tests
make test

# Run integration tests only
go test -v ./internal/server/daemon/... -run Integration

# Run specific category
go test -v ./internal/server/daemon/... -run TestReconciler_Container

# Run with race detector
go test -race ./...

# Run with coverage
go test -cover ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## Success Criteria

1. All test scenarios pass
2. Code coverage > 80% for reconciler and API packages
3. No race conditions detected
4. Tests run in < 30 seconds (no real I/O or sleep)
5. Tests are deterministic (no flakiness)
