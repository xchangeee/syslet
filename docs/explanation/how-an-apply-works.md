# How an apply works

Every syslet invocation runs the same pipeline, whether it's a local dry-run, an SSH push, or a webhookd-triggered git pull:

1. **Read** all JSON specs from the input (stdin stream, directory, or file).
2. **Pre-render validation** checks the specs on their own, before anything is derived from them.
3. **Render** turns each spec into a quadlet unit file.
   syslet adds what it manages itself: the resource name, its `X-Syslet` markers for `removalAllowed` and `reclaimPolicy`, `Restart=always` and `WantedBy=` for `desiredState: "running"` or `Type=oneshot` for `"oneshot"`, and a read-only `Volume=` mount for every config file and directory.
4. **Post-render validation** checks the rendered units, such as references between containers and the volumes, networks and builds they use.
5. **Diff** the rendered units and config files against the installed state on disk, and find stale units.
   Two parts come from the live system instead: systemd reports whether each container is running, which decides between start, stop and restart, and podman's secret store is listed and compared by each secret's `syslet/hash` label.
6. **Stage** the rendered units through podman's quadlet generator and `systemd-analyze verify`, when the plan writes unit files.
7. **Execute** the resulting plan, in strict order (see below).
8. **Exit** with a status summary, non-zero if anything failed.

`--diff` runs steps 1–6 and prints the plan; the normal (apply) mode runs all eight.
A failed check in steps 2 to 6 stops before step 7, so nothing on the host changes; see [Validation rules](../reference/validation-rules.md) for every check.

Rendering sits between the two validation stages because some mistakes only show in the rendered unit.
A container references a volume through `Volume=<name>.volume` in its unit options, and syslet's own config mounts are added as `Volume=` lines too, so a conflicting mount destination can only be seen once both are in the same unit.

What `--diff` prints is what an apply would do: both build the plan the same way, and an apply only adds step 7.
An apply builds its own plan when it runs, so if the input or the host changes after a diff, the apply follows those changes.

## Apply phase ordering

Step 7 always executes in this order, regardless of how many units are affected:

1. Stop containers that changed or are being removed.
2. Write config files to `/etc/containers/config/<name>/`.
3. Write quadlet unit files to `/etc/containers/systemd/`.
4. Remove stale unit files and config directories (pruning).
5. Delete podman volumes/networks/images whose `reclaimPolicy` is `Delete`.
6. A single `systemctl daemon-reload`.
7. Reload containers that only need an in-place config reload (see below).
8. Start containers with `desiredState: "running"`.

The order never changes: stopping before rewriting avoids a running container referencing a unit file mid-change, files exist before the unit files that reference them, pruning happens before the reload so systemd never sees stale units, and starting happens last so a container only comes up once everything it depends on is in its final state.

## Restart, reload, or no-op

Not every change to a container causes a restart. syslet distinguishes:

- **No-op** — nothing about the unit or its files changed; no daemon-reload, no service transition.
- **Metadata-only rewrite** — the unit file is rewritten and daemon-reloaded, but the backing podman resource and running container are left alone (no stop/start).
- **Reload** — only a `configDir`'s content changed; the version symlink is swapped and the unit is reloaded (`ExecReload=`) in place, without stopping the container. See [Mount config files and dirs](../how-to/mount-config-files-and-dirs.md#reload-config-without-a-restart).
- **Restart** — the container's own unit options changed (image, ports, a plain `configFiles` file, etc.); the container is stopped, its unit/config files rewritten, and it's started again.

This matters when reasoning about a deploy's blast radius: changing a `configDir`'s file content is safe to do frequently (no downtime, if the service supports reload), while changing image tags, volumes, or `configFiles` files always costs a restart.
