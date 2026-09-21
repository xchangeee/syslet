# Reload config without restarting the container

`configDirs`' primary purpose is letting a container pick up new config **without** being stopped and restarted. This suits services that support a live reload signal (nginx, most reverse proxies, many daemons via `SIGHUP`).

## How it works

A `configDir`'s files are written into a new versioned subdirectory, and a stable `current` symlink is atomically repointed at it. The container always bind-mounts the symlink path, so the swap is instantaneous and the container never sees a partially-written directory. Because the underlying mount path never changes (only what it points to), the container doesn't need to be recreated to see new content.

To take advantage of this, the container must declare `[Service] ExecReload=`, a command that tells the running container to reload its config (e.g. by sending it a signal):

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest"
    },
    "Service": {
      "ExecReload": "podman kill --signal=HUP webapp"
    }
  },
  "configDirs": [
    {
      "mountPath": "/etc/nginx/conf.d/",
      "files": [
        { "name": "app.conf", "content": "server_name example.internal;" }
      ]
    }
  ]
}
```

This is required. syslet's validation rejects a spec with `configDirs` and no `ExecReload=`, since without it there is no way for syslet to trigger a reload.

## What triggers what

- A `configDir` content-only change → syslet swaps the symlink and runs the unit's `ExecReload=` (e.g. `systemctl reload webapp.service`); the container process itself is not restarted.
- A change to the container's own unit options (image, ports, etc.) → the usual stop/recreate/start cycle, same as any other container change.

If your service can't reload on a signal, use a plain [`configs`](mount-config-files-and-dirs.md) mount instead and accept the restart on change.
