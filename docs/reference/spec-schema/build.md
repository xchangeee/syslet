# Build spec

```json
{
  "name": "myapp",
  "type": "build",
  "unit": {
    "Build": {
      "ImageTag": "localhost/myapp:latest"
    }
  },
  "containerfile": "FROM docker.io/library/nginx:latest\n",
  "configs": [
    { "filename": "app.conf", "content": "file contents here", "mode": "0644" }
  ],
  "reclaimPolicy": "Retain"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `type` | string | yes | `"build"` |
| `name` | string | yes | Referenced from a container as `Image: "<name>.build"`. |
| `unit` | object | yes | Quadlet sections, most importantly `Build.ImageTag`, which **must** be prefixed `localhost/`. syslet rejects a build whose tag isn't local-only. |
| `containerfile` | string | yes | The Containerfile content itself (not a path), written to the build's context directory. |
| `configs` | array | no | Build **context** files copied alongside the Containerfile (for `COPY`/`ADD`). Each entry: `filename` (relative, no path separators), `content`, optional `mode`. Not to be confused with a container's `configs`/`configDirs`, which bind-mount into a running container. |
| `reclaimPolicy` | string | no | `"Retain"` (default) or `"Delete"`. Controls whether the built podman image is deleted along with the unit when the build is pruned. |

Build units have no `removalAllowed` field, and unlike containers, volumes, and networks they are pruned unconditionally once the spec disappears from the input. Only the built image is protected, by `reclaimPolicy`. See [Removing specs](../../explanation/removing-specs.md).
