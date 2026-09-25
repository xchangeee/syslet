# Adopt an existing volume

A volume created outside syslet, by hand or by another tool, can be brought under a spec without copying its data.
syslet names the podman volume after the spec, and the volume's service only creates it if it's missing, so a spec with the existing volume's name reuses it.

## Find the volume's name

```sh
ssh web01 sudo podman volume ls
```

A volume from a hand-written quadlet is usually called `systemd-<unit name>`; to move a whole quadlet setup over, see [Migrate hand-written quadlets](migrate-hand-written-quadlets.md).

## Add a locked spec with that name

=== "CUE"

    ```cue
    sysdef: volumes: "systemd-webapp-data": spec: {
    	unit: {}
    }

    sysdef: (syslettools.#SysdefLock & {in: volumes: ["systemd-webapp-data"]}).out
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "volume",
      "name": "systemd-webapp-data",
      "unit": {},
      "removalAllowed": false,
      "reclaimPolicy": "Retain"
    }
    ```

Lock it from the start, so a mistake in the input can't delete data you never had in a spec before.

Leave `unit` empty, or match the options the volume was created with.
The existing volume keeps its own options either way, and a later change to `unit` recreates the volume, which syslet refuses while it's locked.

## Mount it and apply

Reference it from the container that uses the data:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Volume: ["systemd-webapp-data.volume:/data"]
    }
    ```

=== "JSON"

    ```json
    "Volume": ["systemd-webapp-data.volume:/data"]
    ```

Preview and apply.
The plan shows the volume as `created`, but that only means the unit file is new: the service finds the volume and leaves its data alone.

A name that doesn't fit your naming scheme can only change by copying the data, see [Rename a volume](rename-a-volume.md).
