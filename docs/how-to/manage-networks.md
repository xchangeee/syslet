# Manage networks

This guide covers adding, changing, locking and removing a network spec.
A network holds no data, so syslet recreates a changed network on its own; only removal is guarded by `removalAllowed`.

## Know your defaults

The setup only decides what happens when you drop a network's spec:

| Setup | Spec dropped | Spec changed |
| --- | --- | --- |
| CUE | Unit removed, podman network left behind | Recreated |
| CUE, in `#SysdefLock` | Unit and network kept | Recreated |
| JSON | Unit and network kept | Recreated |

<!-- TODO: mention reclaimPolicy once #NetworkSpec accepts it, see docs/TODO.md -->

## Add a network

=== "CUE"

    ```cue
    sysdef: networks: "webapp-net": spec: {
    	unit: Network: Subnet: "10.89.0.0/24"
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "network",
      "name": "webapp-net",
      "unit": {
        "Network": {
          "Subnet": "10.89.0.0/24"
        }
      },
      "removalAllowed": false,
      "reclaimPolicy": "Retain"
    }
    ```

Attach a container to it in the same input:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Network: ["webapp-net.network"]
    }
    ```

=== "JSON"

    ```json
    "Network": ["webapp-net.network"]
    ```

syslet names the podman network after the spec, here `webapp-net`.
The network's service, `webapp-net-network.service`, creates it when the first container on it starts; a network no container references is never created.
See [Connect containers on a network](connect-containers-on-a-network.md) for name resolution between containers.

## Change a network

Podman can't reconfigure a network in place.
Changing `[Unit] Description`, `removalAllowed` or `reclaimPolicy` only rewrites the unit file; any other change recreates the network.

Unlike a volume, a network needs no markers for that, not even when it's locked.
Preview the change:

=== "CUE"

    ```sh
    cue cmd plan
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    ```

The plan stops every running container attached to the network, deletes the podman network and starts the containers again, which creates the new network:

```text
Services to stop:
  - webapp-net.network
  - webapp.container

...

Podman networks to delete:
  - webapp-net

Summary:
UNIT                                     STATUS     CHANGES
webapp-net.network                       updated    unit updated
webapp.container                         updated    network recreated, restarted (desired: running)
```

<!-- TODO: document a failed network delete once it fails the apply, see docs/TODO.md -->

## Lock a network

Lock the network and apply:

=== "CUE"

    ```cue
    sysdef: (syslettools.#SysdefLock & {in: networks: ["webapp-net"]}).out
    ```

=== "JSON"

    ```json
    "removalAllowed": false
    ```

A locked network is skipped when its spec disappears from the input, but changes still recreate it.

## Remove a network

1. Drop the network from every container's `Network=`. A network that is still referenced fails validation.
2. Make sure the installed unit allows removal. If it's locked, drop it from `#SysdefLock` (in JSON, set `"removalAllowed": true`), keep the spec, and apply once.
3. Drop the network spec, preview and apply.

Steps 1 and 3 can go into the same change.
The plan shows the network as `removed`, and under `reclaimPolicy: "Delete"` also lists it in `Podman networks to delete:`.

Under `Retain`, the podman network stays behind; delete it by hand:

```sh
podman network rm webapp-net
```
