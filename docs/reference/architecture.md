# Architecture

This page is for contributors and maintainers working on syslet itself, not for end users writing specs. It maps the packages a spec's data flows through, from raw JSON to a running container.

```text
cmd/syslet            single CLI entrypoint, wires everything else together
  │
  ▼
internal/api          reads JSON specs (dir, file, or stream) and routes each by apiVersion to internal/api/v1, ...
  │
  ▼
internal/loader       converts each apiVersion's structs → internal/model domain objects; decrypts secrets via internal/sops
  │
  ▼
internal/model        domain types: ContainerUnit, VolumeUnit, NetworkUnit, BuildUnit, and shared primitives
  │
  ▼
internal/syslet       orchestration core: BuildPlan (diff desired vs. on-disk state) → Apply (execute the plan)
  │         │      │
  │         │      └─ internal/podman     — volume/network/secret/image operations not owned by systemd
  │         └─ internal/systemd — D-Bus client, quadlet unit files on disk, daemon-reload, journal reads
  └─ internal/render — renders model units into quadlet unit file content
  └─ internal/filestore — on-disk container config files/dirs and build context files
  └─ internal/validate — pre-render, post-render, and staging validation
```

Supporting packages:

- `internal/age` — derives an age identity from the host's SSH key (for secret decryption).
- `internal/sops` — wraps the SOPS decrypt library.
- `internal/util` — shared low-level helpers (hashing, filename generation).
- Test-only packages (fakes and test harnesses, not part of the runtime): `internal/podman/podmantest`, `internal/systemd/systemdtest`, `test/integration/systest`, `test/log`, `test/oplog`.

## Plan and apply

- `internal/syslet.BuildPlan` computes an `ApplyPlan`, every operation an apply would run, without changing the host. `--diff` prints it.
- `internal/syslet.Apply` executes an `ApplyPlan`.

See [How an apply works](../explanation/how-an-apply-works.md) for the phase ordering and why planning and applying are split, and [Safety mechanisms](../explanation/safety-mechanisms.md) for the validation stages.
