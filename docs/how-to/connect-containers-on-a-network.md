# Connect containers on a network

Containers on the same network reach each other by container name.
This guide puts an app and a Redis cache on one network, so the app connects to `cache:6379` and the cache isn't published on the host.

## 1. Declare the network and attach both containers

=== "CUE"

    ```cue
    sysdef: networks: "app-net": spec: {}

    sysdef: containers: cache: spec: {
    	unit: Container: {
    		Image: "docker.io/library/redis:7"
    		Network: ["app-net.network"]
    	}
    }

    sysdef: containers: app: spec: {
    	unit: Container: {
    		Image: "registry.example.com/app:1.4"
    		Network: ["app-net.network"]
    		PublishPort: ["8080:8080"]
    		Environment: REDIS_URL: "redis://cache:6379"
    	}
    }
    ```

=== "JSON"

    ```json
    [
      {
        "apiVersion": "v1",
        "type": "network",
        "name": "app-net",
        "removalAllowed": false
      },
      {
        "apiVersion": "v1",
        "type": "container",
        "name": "cache",
        "desiredState": "running",
        "unit": {
          "Container": {
            "Image": "docker.io/library/redis:7",
            "Network": ["app-net.network"]
          }
        }
      },
      {
        "apiVersion": "v1",
        "type": "container",
        "name": "app",
        "desiredState": "running",
        "unit": {
          "Container": {
            "Image": "registry.example.com/app:1.4",
            "Network": ["app-net.network"],
            "PublishPort": ["8080:8080"],
            "Environment": { "REDIS_URL": "redis://cache:6379" }
          }
        }
      }
    ]
    ```

syslet sets each container's name to its spec name, so `cache` resolves to the Redis container.
Podman enables DNS on every network it creates, unless the network sets `DisableDNS=true`; podman's default network has no DNS, which is why the containers need a network of their own.

Only `app` publishes a port.
`cache` is reachable from containers on `app-net`, but not from outside the host.

## 2. Apply and check

=== "CUE"

    ```sh
    cue cmd apply
    ssh web01 sudo podman exec app getent hosts cache
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --stdin
    ssh web01 sudo podman exec app getent hosts cache
    ```

The second command prints the cache's address on `app-net`.
It needs `getent` in the app image; any tool that resolves names works.

## Add more names

`NetworkAlias=` gives a container extra names on its networks, for example to keep an old hostname working:

=== "CUE"

    ```cue
    sysdef: containers: cache: spec: {
    	unit: Container: NetworkAlias: ["redis"]
    }
    ```

=== "JSON"

    ```json
    "NetworkAlias": ["redis"]
    ```

Several containers with the same alias share it, and a lookup returns all of them.

## Assign networks in CUE

With many containers and networks, `#SysdefAssignNetwork` keeps the membership in one place.
Each key is a network, each list the containers on it:

```cue
sysdef: (syslettools.#SysdefAssignNetwork & {in: {
	"app-net": ["app", "cache"]
	"web-net": ["app"]
}}).out
```

It sets `Network` on each listed container, so don't set `Network` on those containers yourself; CUE reports the two lists as a conflict.
The networks still need their own `sysdef: networks:` entries.

To keep a backend network from reaching outside the host, set `Internal: "true"` in its `unit: Network:` section, and attach the containers that need outbound access to a second network.
