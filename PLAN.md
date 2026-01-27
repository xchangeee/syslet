# rsystemd - Declarative container reconciliation daemon

## Overview

A Go-based single-node daemon (`rsystemd`) + CLI (`rsctl`) that provides Kubernetes-style declarative state management for Podman quadlet containers, volumes, and networks, with a reconciliation loop and remote API. Designed for gitops pipelines.

## Design Decisions

- **Language**: Go
- **Managed resource types**:
  - `.container`, `.volume`, `.network` - Podman quadlet units
- **Config format**: Native unit files with `[X-Rsystemd]` custom section
- **Config files**: Fixed unit-specific directories under `/etc/containers/config/`
- **API**: gRPC over TCP (remote), typed data model
- **Logging**: Integrated journal log streaming
- **GitOps**: `rsctl apply -f <dir>` pushes an entire directory
- **Reconciliation**: Two-phase (diff all, then execute globally with single daemon-reload)

## Architecture

```text
rsctl (CLI) ---gRPC---> rsystemd (daemon) ---D-Bus---> systemd
                              |                          |
                         watches config dirs        quadlet generator
                                                   converts .container/.volume/.network
                                                   into .service units at runtime
```

## Managed Resource Types

### Podman quadlets (.container, .volume, .network)

- Installed to `/etc/containers/systemd/`
- Parsed by `systemd-generator` at daemon-reload, which creates runtime `.service` units
- **Container** (`.container`):
  - Enablement is via `[Install] WantedBy=` in the file
  - Can be started/stopped/restarted (operates on the generated service unit)
  - Config files in `/etc/containers/config/<container-name>/`
- **Volume** (`.volume`):
  - Created once, **immutable after creation** - changes are rejected by reconciler
  - Cannot be started/stopped (declarative resource only)
- **Network** (`.network`):
  - Created once, **immutable after creation** - changes are rejected by reconciler
  - Cannot be started/stopped (declarative resource only)

## Directory Layout (on daemon host)

```text
/etc/rsystemd/
└── units/                          # managed unit files
    ├── webapp.container
    ├── webapp-data.volume
    └── webapp-net.network

/etc/containers/
├── systemd/                        # quadlet unit files (installed by rsystemd)
│   ├── webapp.container
│   ├── webapp-data.volume
│   └── webapp-net.network
└── config/                         # config files for container units
    └── webapp/
        ├── app.conf
        └── env
```

## Unit File Examples

### Container quadlet

```ini
[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80
Volume=webapp-data.volume:/data
Network=webapp-net.network
Environment=APP_ENV=production

[Install]
WantedBy=multi-user.target default.target

[X-Rsystemd]
DesiredState=running
```

Note: Enablement is solely via `[Install] WantedBy=`.

### Volume quadlet (immutable after creation)

```ini
[Volume]
Label=app=webapp

[X-Rsystemd]
```

### Network quadlet (immutable after creation)

```ini
[Network]
Subnet=10.89.0.0/24
Gateway=10.89.0.1

[X-Rsystemd]
```

## Reconciliation Loop (two-phase: plan then execute)

Since `daemon-reload` is a global operation affecting all units, the reconciler must examine ALL managed units first, build a change plan, then execute in a single coordinated pass.

### Phase 1: Diff (read-only, all units)

For every managed unit (containers, volumes, networks):

1. Read unit file from source directory
2. Parse `[X-Rsystemd]` section
3. Compute diff against installed state:
   - Unit file checksum vs installed copy
   - Config file checksums vs deployed copies
   - Active state vs desired state
4. For volumes/networks: if exists and changed, mark as **rejected** (immutable)
5. Produce a `ChangePlan` listing per-unit actions needed

### Phase 2: Execute (single coordinated pass)

Execute the change plan in this **strict global order**:

1. **Stop** all units that need stopping (unit file or config changed, or DesiredState=stopped). Stop sequentially, one at a time. This uses the OLD config for clean shutdown.
2. **Write config files** for all units that have config changes.
3. **Write unit files** for all units that have unit file changes (to `/etc/containers/systemd/`).
4. **Single `daemon-reload`** if ANY unit file changed (one reload covers all changes, also triggers quadlet generator for containers).
5. **Start** all units with DesiredState=running that are not active (or were stopped in step 1 for update). Start sequentially.

### Volume/network special handling

- New: write file (included in the single daemon-reload in step 4)
- Changed: reject, log error, skip
- Deleted from desired state: log warning (manual cleanup required)

## GitOps Source Directory Layout

```text
my-infra-repo/
└── hosts/
    └── webserver01/
        ├── webapp.container
        ├── webapp-data.volume
        ├── webapp-net.network
        └── configs/
            └── webapp/
                ├── app.conf
                └── env
```

`rsctl apply -f hosts/webserver01/` pushes everything to the daemon.

## Project Structure

```text
rsystemd/
├── cmd/
│   ├── rsystemd/              # daemon entrypoint
│   │   └── main.go
│   └── rsctl/                 # CLI entrypoint
│       └── main.go
├── internal/
│   ├── daemon/                # reconciliation loop
│   │   ├── daemon.go
│   │   ├── reconciler.go
│   │   └── fs.go
│   ├── systemd/               # D-Bus client for systemd
│   │   └── client.go
│   ├── parser/                # unit file parser (with X-Rsystemd section)
│   │   └── parser.go
│   ├── config/                # config file management
│   │   └── manager.go
│   ├── api/                   # gRPC server
│   │   └── server.go
│   └── journal/               # journalctl log streaming
│       └── reader.go
├── proto/
│   └── rsystemd.proto
├── go.mod
└── go.sum
```

## gRPC Data Model

```protobuf
syntax = "proto3";
package rsystemd.v1;

service RsystemdService {
  rpc Apply(ApplyRequest) returns (ApplyResponse);
  rpc Status(StatusRequest) returns (StatusResponse);
  rpc List(ListRequest) returns (ListResponse);
  rpc Logs(LogsRequest) returns (stream LogEntry);
  rpc Delete(DeleteRequest) returns (DeleteResponse);
}

enum UnitType {
  UNIT_TYPE_UNSPECIFIED = 0;
  UNIT_TYPE_CONTAINER = 3;
  UNIT_TYPE_VOLUME = 4;
  UNIT_TYPE_NETWORK = 5;
}

enum DesiredState {
  DESIRED_STATE_UNSPECIFIED = 0;
  DESIRED_STATE_RUNNING = 1;
  DESIRED_STATE_STOPPED = 2;
}

enum ActiveState {
  ACTIVE_STATE_UNSPECIFIED = 0;
  ACTIVE_STATE_ACTIVE = 1;
  ACTIVE_STATE_INACTIVE = 2;
  ACTIVE_STATE_FAILED = 3;
  ACTIVE_STATE_ACTIVATING = 4;
  ACTIVE_STATE_DEACTIVATING = 5;
}
```

## CLI Commands

- `rsctl apply -f <file-or-dir>` - push unit files and configs (recursive for dirs)
- `rsctl status [unit]` - show status of managed units
- `rsctl list [--type=container|volume|network]` - list managed units
- `rsctl logs <unit> [-f]` - stream/tail journal logs
- `rsctl delete <unit>` - remove unit and its config files

## Implementation Order

1. Go module + project skeleton
2. Unit file parser with `[X-Rsystemd]` section and unit type detection
3. Config file manager (checksum diffing, write to fixed paths)
4. systemd D-Bus client (query state, daemon-reload, start/stop)
5. Reconciler (two-phase: diff all units, then single coordinated execute pass)
6. Daemon main loop
7. gRPC proto + server
8. Journal log reader + streaming
9. rsctl CLI with cobra (apply -f supporting files and directories)
10. Integration test with container quadlet

## Key Dependencies

- `github.com/coreos/go-systemd/v22` - D-Bus bindings
- `google.golang.org/grpc` + `google.golang.org/protobuf` - gRPC
- `github.com/spf13/cobra` - CLI
- `github.com/coreos/go-systemd/v22/sdjournal` - journal reader

## Verification

- Unit tests for parser (all unit types), reconciler diff logic (stop-before-reload ordering), config manager, quadlet immutability checks
- Place a .container + .volume + .network, verify generator runs and container starts
- Attempt to modify a .volume file, verify change is rejected
- `rsctl apply -f <dir>` with mixed unit types + configs, verify all deployed correctly
- Modify a container config file, verify stop (old config) -> update -> restart
- Set `DesiredState=stopped` on container, verify it stops
- `rsctl logs <unit> -f` streams output
