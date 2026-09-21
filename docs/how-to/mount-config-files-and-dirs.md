# Mount config files and config directories

Containers have two ways to get files bind-mounted into them: `configs` (single files) and `configDirs` (directories, versioned via symlinks; see [Reload config without restart](reload-config-without-restart.md) for why you'd pick that instead).

## configs: single files

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest"
    }
  },
  "configs": [
    {
      "content": "server { listen 80; root /usr/share/nginx/html; }",
      "mountPath": "/etc/nginx/nginx.conf"
    },
    {
      "content": "APP_ENV=production",
      "mountPath": "/run/env",
      "mode": "0640"
    }
  ]
}
```

Each entry is written to `/etc/containers/config/webapp/` on the host and injected into the quadlet as a read-only bind mount at `mountPath`. `mode` is optional (defaults applied by syslet if omitted). Changing a config's `content` changes the file on disk and, because it's a static bind mount, requires the container to be recreated/restarted to pick it up. syslet does this automatically as part of applying the change.

## configDirs: directories

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
      "mountPath": "/etc/app/",
      "files": [
        { "name": "app.conf", "content": "key=value" }
      ]
    }
  ]
}
```

A `configDir` mount path must end in a real, bind-mountable directory; its files are written and swapped in atomically via a versioned symlink (see [Reload config without restart](reload-config-without-restart.md)). Unlike `configs`, a `configDir`-using container **must** set `[Service] ExecReload=`. syslet's pre-render validation rejects a spec that declares `configDirs` without it, because `configDirs` exist to reload config in place rather than restart the container.

Both mechanisms support multiple entries per container, and both are pruned (files/directories removed) when dropped from the spec.
