# Migrate hand-written quadlets

A host that already runs hand-written quadlet files can be moved to syslet unit by unit: each file becomes a spec with the same options.
The containers are down from the moment you stop them until the first apply.

## 1. Translate each file into a spec

The file name without its extension becomes the spec name, and each section becomes a struct under `unit`.
An option that appears several times becomes a list:

```ini title="/etc/containers/systemd/webapp.container"
[Container]
Image=docker.io/library/nginx:1.27
PublishPort=8080:80
PublishPort=8443:443
Volume=webapp-data.volume:/data

[Install]
WantedBy=multi-user.target
```

=== "CUE"

    ```cue title="web01.cue"
    sysdef: containers: webapp: spec: {
    	unit: Container: {
    		Image: "docker.io/library/nginx:1.27"
    		PublishPort: ["8080:80", "8443:443"]
    		Volume: ["systemd-webapp-data.volume:/data"]
    	}
    }
    ```

=== "JSON"

    ```json title="webapp.json"
    {
      "apiVersion": "v1",
      "type": "container",
      "name": "webapp",
      "desiredState": "running",
      "unit": {
        "Container": {
          "Image": "docker.io/library/nginx:1.27",
          "PublishPort": ["8080:80", "8443:443"],
          "Volume": ["systemd-webapp-data.volume:/data"]
        }
      }
    }
    ```

Leave out `[Install] WantedBy=` and `[Service] Restart=`; `desiredState: "running"` sets both.
syslet only manages `.container`, `.volume`, `.network` and `.build` files; leave other types, such as `.pod` or `.kube`, as they are.

## 2. Keep the names that matter

Quadlet names a resource `systemd-<file name>` unless the file sets a name; syslet always names it after the spec, without the prefix.
Check the existing names with `podman ps -a`, `podman volume ls` and `podman network ls`:

- **Volumes** hold data. Name the volume spec after the existing podman volume, here `systemd-webapp-data`, so it's reused (see [Adopt an existing volume](adopt-an-existing-volume.md)), and lock it.
- **Containers** get new names. Update containers that reach each other by name, and scripts that use `podman exec`.
- **Networks** are created again under the new name. Delete the old ones with `podman network rm` once nothing uses them.

Drop `ContainerName=`, `VolumeName=` and `NetworkName=` from the specs; syslet overwrites them.

## 3. Hand over the host

Stop the hand-written containers, move their files out of the quadlet directory and reload systemd:

```sh
sudo systemctl stop webapp.service
sudo mkdir /root/quadlets-old
sudo mv /etc/containers/systemd/*.{container,volume,network,build} /root/quadlets-old/
sudo systemctl daemon-reload
```

<!-- TODO: explain why stopping first is needed, or drop the step once syslet checks the state of new units, see docs/TODO.md -->

## 4. Preview and apply

=== "CUE"

    ```sh
    cue cmd plan
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    ```

Every unit should show as `created`.
Apply, then check that the containers run and the volumes hold their data.

To back out before applying, move the files back and run `systemctl daemon-reload`.
