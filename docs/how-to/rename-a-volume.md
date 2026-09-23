# Rename a volume

syslet always names the podman volume after its spec, so a renamed spec points at a new, empty volume.
To keep the data, create the new volume next to the old one, copy the data over, and switch the container.
The container is down while the data is copied.

The steps rename `webapp-data`, mounted by the container `webapp`, to `webapp-files`.

## 1. Add the new volume

Add a spec for `webapp-files`, with the markers you want (see [Manage volumes](manage-volumes.md#add-a-volume)), and apply.
Nothing references it yet, so create the podman volume through its service:

```sh
ssh web01 sudo systemctl start webapp-files-volume.service
```

## 2. Copy the data

Stop the container, so nothing writes while you copy, and copy the volume's content:

```sh
ssh web01 'sudo systemctl stop webapp.service && sudo podman volume export webapp-data | sudo podman volume import webapp-files -'
```

Don't let a deployment run until step 3, since any apply starts the container again on the old volume.

## 3. Switch the container

Point the container's `Volume=` at the new volume:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Volume: ["webapp-files.volume:/data"]
    }
    ```

=== "JSON"

    ```json
    "Volume": ["webapp-files.volume:/data"]
    ```

Preview and apply; syslet starts the container on the new volume.
Check the data is there before you go on.

## 4. Remove the old volume

Remove the `webapp-data` spec as in [Manage volumes](manage-volumes.md#remove-a-volume).
With `reclaimPolicy: "Retain"`, the old podman volume stays as a backup until you delete it with `podman volume rm webapp-data`.
