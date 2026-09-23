# Container spec

```json
{
  "apiVersion": "v1",
  "type": "container",
  "name": "webapp",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80"],
      "Volume": ["webapp-data.volume:/data"],
      "Network": ["webapp-net.network"]
    }
  },
  "configFiles": [
    { "mountPath": "/path/in/container", "content": "file contents here", "mode": "0644" }
  ],
  "configDirs": [
    { "mountPath": "/dir/in/container/", "files": [
      { "name": "file.conf", "content": "file contents here", "mode": "0644" }
    ] }
  ],
  "removalAllowed": false
}
```

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `apiVersion` | string | yes | `"v1"`. See [API versioning](index.md#api-versioning). |
| `type` | string | yes | `"container"` |
| `name` | string | yes | Also used as `ContainerName`. |
| `desiredState` | string | no | `"running"` or `"stopped"`. When `"running"`: `[Install] WantedBy=multi-user.target default.target` and `[Service] Restart=Always` are added automatically. |
| `unit` | object | no | Quadlet sections (`Container`, `Service`, `Install`, ...), passed through to the generated `.container` file. |
| `configFiles` | array | no | Single files bind-mounted into the container. Each entry: `mountPath` (absolute path in the container), `content` (file text), `mode` (optional file mode, e.g. `"0644"`). Written to `/etc/containers/config/<name>/`. See [Mount config files and dirs](../../how-to/mount-config-files-and-dirs.md). |
| `configDirs` | array | no | Directories bind-mounted into the container, swapped atomically via a versioned symlink for in-place reload. Each entry: `mountPath` (must end in a real directory path) and `files` (each with `name`, `content`, optional `mode`). Requires `[Service] ExecReload=` to be set. See [Mount config files and dirs](../../how-to/mount-config-files-and-dirs.md#reload-config-without-a-restart). |
| `removalAllowed` | bool | no | Default `false`. Must be `true` before syslet will prune this unit when it disappears from the input, and must have been set on the *previous* apply. See [Removing specs](../../explanation/removing-specs.md). |

<!-- TODO: mark unit as required once the loader enforces it, see docs/TODO.md -->

No `configFiles` or `configDirs` `mountPath` may equal or sit under another one; overlapping mounts are rejected.

`ContainerName` is always set to the spec's `name`; a `ContainerName` in `unit.Container` is overwritten.

## Service actions

Each apply picks one action per container, from what changed and whether the container is running at plan time:

| Situation | Action |
| --- | --- |
| Running, a restart trigger applies, `desiredState: "running"` | Restart (stop in phase 1, start at the end) |
| Running, a restart trigger applies, `desiredState: "stopped"` | Stop |
| Running, only `configDirs` file content changed | Reload via `ExecReload=` |
| Running, `desiredState: "stopped"` | Stop |
| Not running, `desiredState: "running"` | Start |
| Not running, `desiredState: "stopped"` | None |
| Unit sets `[Service] Type=oneshot` | None; syslet never starts or stops oneshot units |

Restart triggers:

- A change to the container's own unit, anything outside `[X-Syslet]` and `[Unit] Description`. This includes adding or removing a `configDirs` entry, which changes the unit's mounts.
- A `configFiles` entry added, changed or removed.
- A referenced volume or network recreated in the same apply.
- A referenced build rebuilt in the same apply.
- A referenced secret changed in the same apply.

"Not running" includes a container that crashed or was stopped by hand.

## Validation

A plan fails, on `--diff` as well as on apply, when:

- a referenced volume, network, build or secret key is not in the input,
- two volume mounts share a destination path, or two `configFiles`/`configDirs` mount paths are equal or nested,
- a config `mountPath` is empty, relative, or contains `..`,
- `configDirs` is set without `[Service] ExecReload=`,
- `unit` contains an `[X-Syslet]` section,
- podman's generator rejects an option during staging.
