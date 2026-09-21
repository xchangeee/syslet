# Customize an upstream container image without a registry

The `build` unit type's main use case is layering local changes onto an upstream image (baking in a config file, patching a package) **without** pushing/pulling through an image registry. syslet writes the `Containerfile` (and any context files) to the host, generates a `.build` quadlet unit, and podman builds the image locally as part of `daemon-reload`/service startup; the resulting image never leaves the host.

## 1. Write a build spec

```json
{
  "name": "myapp",
  "type": "build",
  "unit": {
    "Build": {
      "ImageTag": "localhost/myapp:latest"
    }
  },
  "containerfile": "FROM docker.io/library/nginx:latest\nCOPY app.conf /etc/nginx/conf.d/app.conf\n",
  "configs": [
    {
      "filename": "app.conf",
      "content": "server_name example.internal;"
    }
  ]
}
```

- `unit.Build.ImageTag` **must** use the `localhost/` prefix. syslet enforces this so built images are unambiguously local and never mistaken for a registry-hosted tag.
- `containerfile` is the Containerfile content itself (not a path); syslet writes it to the build's context directory on the host.
- `configs` here are build **context** files (copied into the image via `COPY`/`ADD`), written alongside the Containerfile. They are a different mechanism from a container's `configs`/`configDirs`, which bind-mount files into a running container at runtime.

## 2. Reference the build from a container

Set the container's `Image` to `<build-name>.build`:

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "myapp.build"
    }
  }
}
```

Deploy both specs together. syslet validates that every `<name>.build` a container references exists as a build spec, builds the image before starting the container, and rebuilds/restarts the container whenever the Containerfile or its context files change.

## 3. Reclaiming stale builds

A build unit removed from the spec set is pruned unconditionally — build units have no `removalAllowed` marker, since the unit and its context are fully regenerable from the spec (see [Removing units](../explanation/removing-units.md)). Set `"reclaimPolicy": "Delete"` on the build spec to also delete the built podman image when the unit is reclaimed; the default, `"Retain"`, leaves the image on disk.
