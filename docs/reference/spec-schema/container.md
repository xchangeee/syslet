# Container spec

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80"],
      "Volume": ["webapp-data.volume:/data"],
      "Network": ["webapp-net.network"]
    }
  },
  "configs": [
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
|---|---|---|---|
| `type` | string | yes | `"container"` |
| `name` | string | yes | Also used as `ContainerName` unless overridden in `unit.Container`. |
| `desiredState` | string | no | `"running"` or `"stopped"`. When `"running"`: `[Install] WantedBy=multi-user.target default.target` and `[Service] Restart=Always` are added automatically. |
| `unit` | object | yes | Quadlet sections (`Container`, `Service`, `Install`, ...), passed through to the generated `.container` file. |
| `configs` | array | no | Single files bind-mounted into the container. Each entry: `mountPath` (absolute path in the container), `content` (file text), `mode` (optional file mode, e.g. `"0644"`). Written to `/etc/containers/config/<name>/`. See [Mount config files and dirs](../../how-to/mount-config-files-and-dirs.md). |
| `configDirs` | array | no | Directories bind-mounted into the container, swapped atomically via a versioned symlink for in-place reload. Each entry: `mountPath` (must end in a real directory path) and `files` (each with `name`, `content`, optional `mode`). Requires `[Service] ExecReload=` to be set. See [Reload config without restart](../../how-to/reload-config-without-restart.md). |
| `removalAllowed` | bool | no | Default `false`. Must be `true` before syslet will prune this unit when it disappears from the input, and must have been set on the *previous* apply. See [Removing specs](../../explanation/removing-specs.md). |

`ContainerName` is set automatically to the spec's `name` if not explicitly given in `unit.Container`.
