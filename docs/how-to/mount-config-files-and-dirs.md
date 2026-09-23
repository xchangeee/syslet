# Mount config files and dirs

Containers have two ways to get files bind-mounted into them from the spec:

- `configFiles` mount single files. Changing one restarts the container.
- `configDirs` mount a directory whose content is swapped atomically. Changing it reloads the container in place, without a restart.

Use `configDirs` when the service can reload its config on a signal (nginx, most reverse proxies, many daemons via `SIGHUP`), and `configFiles` otherwise.

## Mount single files

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Image: "docker.io/library/nginx:latest"
    	configFiles: [{
    		content:   "server { listen 80; root /usr/share/nginx/html; }"
    		mountPath: "/etc/nginx/nginx.conf"
    	}, {
    		content:   "APP_ENV=production"
    		mountPath: "/run/env"
    		mode:      "0640"
    	}]
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "container",
      "name": "webapp",
      "desiredState": "running",
      "unit": {
        "Container": {
          "Image": "docker.io/library/nginx:latest"
        }
      },
      "configFiles": [
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

Each entry is written to `/etc/containers/config/webapp/` on the host and injected into the quadlet as a read-only bind mount at `mountPath`.
`mode` is optional and defaults to `0644`.

A changed `content` is a new file on disk, and a static bind mount only picks it up when the container is recreated, so syslet restarts the container as part of the apply.

## Reload config without a restart

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: {
    		Container: Image:    "docker.io/library/nginx:latest"
    		Service: ExecReload: "podman kill --signal=HUP webapp"
    	}
    	configDirs: [{
    		mountPath: "/etc/nginx/conf.d/"
    		files: [{
    			name:    "app.conf"
    			content: "server_name example.internal;"
    		}]
    	}]
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "container",
      "name": "webapp",
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

A `configDir`'s files are written into a new versioned subdirectory, and a stable `current` symlink is atomically repointed at it.
The container bind-mounts the symlink path, so it sees the new files at once and never a partially-written directory.
The mount path must end in a real, bind-mountable directory.

`[Service] ExecReload=` is required: it tells the running container to reload its config, here by sending nginx a `SIGHUP`.
syslet's validation rejects a spec with `configDirs` and no `ExecReload=`, since without it there is no way to trigger a reload.

What each change does:

- Only a `configDir`'s file content changed: syslet swaps the symlink and runs `ExecReload=` through `systemctl reload webapp.service`. The container keeps running.
- A `configDirs` entry added or removed, or any change to the container's own unit options: the usual stop and start, as for any other container change (see [Service actions](../reference/spec-schema/container.md#service-actions)).
