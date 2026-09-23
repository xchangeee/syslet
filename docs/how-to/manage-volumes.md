# Manage volumes

This guide covers adding, changing, locking and removing a volume spec.
Volumes hold data, so two markers decide how far syslet may go: `removalAllowed` allows removing or recreating the unit, and `reclaimPolicy` decides whether the podman volume and its data go with it.

## Know your defaults

What happens to a volume's data depends on how you set it up:

| Setup | Spec dropped | Spec changed |
| --- | --- | --- |
| Default, CUE or JSON | **Volume and data deleted** | **Recreated empty** |
| [Locked](#lock-a-volume) | Volume and data kept | Plan refused |

Lock every volume whose data matters.

## Add a volume

=== "CUE"

    ```cue
    sysdef: volumes: "webapp-data": spec: {
    	unit: Volume: Label: "app=webapp"
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "volume",
      "name": "webapp-data",
      "unit": {
        "Volume": {
          "Label": "app=webapp"
        }
      },
      "removalAllowed": true,
      "reclaimPolicy": "Delete"
    }
    ```

This volume is unlocked: dropping or changing it deletes its data.
To keep the data, [lock it](#lock-a-volume).

Mount it from a container in the same input:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Volume: ["webapp-data.volume:/data"]
    }
    ```

=== "JSON"

    ```json
    "Volume": ["webapp-data.volume:/data"]
    ```

The podman volume is created by the volume's service when the first container using it starts.
A volume spec that no container references is valid, but nothing creates the podman volume.

## Lock a volume

Lock the volume and apply:

=== "CUE"

    ```cue
    sysdef: (syslettools.#SysdefLock & {in: volumes: ["webapp-data"]}).out
    ```

=== "JSON"

    ```json
    "removalAllowed": false,
    "reclaimPolicy": "Retain"
    ```

Both markers live in `[X-Syslet]`, so changing them rewrites the unit file without touching the volume or restarting containers.

A locked volume:

- is skipped, with its data intact, when its spec disappears from the input.
- refuses any meaningful change (see [Change a volume](#change-a-volume)).

## Change a volume

Podman can't reconfigure a volume in place.
Changing `[Unit] Description`, `removalAllowed` or `reclaimPolicy` only rewrites the unit file.
Any other change means deleting the podman volume, **with its data**, and creating it again.

syslet only recreates the volume if the **installed** unit already has `RemovalAllowed=true` and `ReclaimPolicy=Delete`.
Otherwise the whole plan is refused:

```text
Validation errors:
  error [webapp-data.volume]: volume has meaningful changes but recreation is not permitted: set removalAllowed=true and reclaimPolicy=delete to allow
```

To recreate it:

1. Drop the volume from `#SysdefLock` (in JSON, set `"removalAllowed": true` and `"reclaimPolicy": "Delete"`), keep everything else, and apply.
2. Make the change, preview and apply.

On the second apply, syslet stops every running container that mounts the volume, stops the volume service, deletes the podman volume, writes the new unit and starts the containers again, which creates the new, empty volume.
Lock the volume again afterwards if it should stay protected.

Setting the markers and the change in one apply doesn't work, because syslet reads the markers from the installed unit, not from your spec.

To keep the data, don't change the volume. Add a second volume under a new name, copy the data over with podman, point the containers at the new volume, then [remove](#remove-a-volume) the old one.

## Remove a volume

1. Drop the volume from every container's `Volume=` in the input. A volume that is still referenced fails validation.
2. Make sure the installed unit allows removal. If it's locked, drop it from `#SysdefLock` (in JSON, set `"removalAllowed": true` and choose `reclaimPolicy`), keep the spec, and apply once.
3. Drop the volume spec, preview and apply.

Steps 1 and 3 can go into the same change, and so can removing the container that uses the volume. Check the plan before applying:

| Installed `removalAllowed` | Installed `reclaimPolicy` | Plan shows | Result |
| --- | --- | --- | --- |
| `false` | either | `skipped` | Unit file and podman volume stay. |
| `true` | `Retain` | `removed` | Unit file deleted; podman volume and data stay, untracked. |
| `true` | `Delete` | `removed` and `Podman volumes to delete:` | Unit file and podman volume deleted. |

If a container that mounts the volume shows `skipped` in the same plan, don't apply: it would keep running on a volume you're deleting.

A retained volume can be deleted by hand later:

```sh
podman volume rm webapp-data
```

Putting the spec back while a retained volume still exists reattaches the unit to it with the data intact.
See [Removing specs](../explanation/removing-specs.md) for the reasons behind the markers.
