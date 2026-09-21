# Architecture

This page is for contributors and maintainers working on syslet itself, not for end users writing specs. It maps the packages a spec's data flows through, from raw JSON to a running container.

```
cmd/syslet            single CLI entrypoint, wires everything else together
  │
  ▼
internal/api          deserializes JSON specs (dir, file, or stream) into Raw*Spec structs
  │
  ▼
internal/loader       converts Raw*Spec → internal/model domain objects; decrypts secrets via internal/sops
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

## The plan/apply split

`internal/syslet.BuildPlan` computes a full `ApplyPlan` (every operation that would run: writes, deletes, stops, starts) without touching the live system; this is what `--diff` displays. `internal/syslet.Apply` executes a plan produced this way. The two are separate functions specifically so a diff and an apply share the same planning logic. There is no separate "dry-run" code path that could drift from what happens on apply.

See [Workflow](../explanation/workflow.md) for the apply phase ordering, and [Safety mechanisms](../explanation/safety-mechanisms.md) for what the three validation stages check.
