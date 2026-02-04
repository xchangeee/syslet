# syslet - Declarative container reconciliation daemon

## Overview

A Go-based single-node daemon (`syslet`) + CLI (`rsctl`) that provides Kubernetes-style declarative state management for Podman quadlet containers, volumes, and networks, with a reconciliation loop and remote API. Designed for gitops pipelines.

## Design Decisions

- **Language**: Go
- **Managed resource types**:
  - `.container`, `.volume`, `.network` - Podman quadlet units
- **Config format**: Native unit files with `[X-Syslet]` custom section
- **Config files**: Fixed unit-specific directories under `/etc/containers/config/`
- **API**: gRPC over TCP (remote), typed data model
- **Logging**: Integrated journal log streaming
- **GitOps**: `rsctl apply -f <dir>` pushes an entire directory
- **Reconciliation**: Two-phase (diff all, then execute globally with single daemon-reload)

## Architecture

```text
rsctl (CLI) ---gRPC---> syslet (daemon) ---D-Bus---> systemd
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

## Reconciliation Loop (two-phase: plan then execute)

Since `daemon-reload` is a global operation affecting all units, the reconciler must examine ALL managed units first, build a change plan, then execute in a single coordinated pass.

### Volume/network special handling

- New: write file (included in the single daemon-reload in step 4)
- Changed: reject, log error, skip
- Deleted from desired state: log warning (manual cleanup required)

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
