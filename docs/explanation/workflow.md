# Workflow

Every syslet invocation runs the same five-step pipeline, whether it's a local dry-run, an SSH push, or a webhookd-triggered git pull:

1. **Read** all JSON specs from the input (stdin stream, directory, or file).
2. **Validate** consistency — no duplicate unit names, all volumes/networks/builds referenced by containers exist as specs, no duplicate config paths within a container, `configDirs` require `ExecReload=`, and so on.
3. **Diff** the desired specs against the installed state already on disk.
4. **Execute** the resulting plan, in strict order (see below).
5. **Exit** with a status summary — non-zero if anything failed.

`--diff` runs steps 1–3 and prints the plan; the normal (apply) mode runs all five.

## Apply phase ordering

Step 4 always executes in this order, regardless of how many units are affected:

1. Stop containers that changed or are being removed.
2. Write config files to `/etc/containers/config/<name>/`.
3. Write quadlet unit files to `/etc/containers/systemd/`.
4. Remove stale unit files and config directories (pruning).
5. Delete podman volumes/networks/images whose `reclaimPolicy` is `Delete`.
6. A single `systemctl daemon-reload`.
7. Reload containers that only need an in-place config reload (see below).
8. Start containers with `desiredState: "running"`.

This ordering is deliberate and invariant: stopping before rewriting avoids a running container referencing a unit file mid-change, files exist before the unit files that reference them, pruning happens before the reload so systemd never sees stale units, and starting happens last so a container only comes up once everything it depends on is in its final state.

## Restart, reload, or no-op

Not every change to a container causes a restart. syslet distinguishes:

- **No-op** — nothing about the unit or its files changed; no daemon-reload, no service transition.
- **Metadata-only rewrite** (`NoRecreation`) — the unit file is rewritten and daemon-reloaded, but the backing podman resource and running container are left alone (no stop/start).
- **Reload** (`ReloadsService`) — only a `configDir`'s content changed; the version symlink is swapped and the unit is reloaded (`ExecReload=`) in place, without stopping the container. See [Reload config without restart](../how-to/reload-config-without-restart.md).
- **Restart** (`RestartsService`) — the container's own unit options changed (image, ports, a plain `configs` file, etc.); the container is stopped, its unit/config files rewritten, and it's started again.

This matters when reasoning about a deploy's blast radius: changing a `configDir`'s file content is safe to do frequently (no downtime, if the service supports reload), while changing image tags, volumes, or `configs` files always costs a restart.
