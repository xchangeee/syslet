# Wait until a container is healthy

By default, podman reports a container as started as soon as its process runs, long before the app inside may accept requests.
With a health check and `Notify=healthy`, the container's service counts as started only once the check passes.
Containers that depend on it through `After=` then start when it's actually ready (see [Add dependencies between containers](add-dependencies-between-containers.md)).
The examples below use a container `app` that answers on `http://localhost:8080/healthz`.

`Notify=healthy` needs podman 5.1 or later.

## Add a health check

Set the command that checks the app in `[Container]`:

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Container: {
    		HealthCmd:         "curl -fs http://localhost:8080/healthz"
    		HealthStartPeriod: "60s"
    	}
    }
    ```

=== "JSON"

    ```json
    "Container": {
      "HealthCmd": "curl -fs http://localhost:8080/healthz",
      "HealthStartPeriod": "60s"
    }
    ```

`HealthCmd` runs inside the container, so the tool it calls, here `curl`, must be in the image.
Failed checks during `HealthStartPeriod` don't count, which gives the app time to warm up.

## Hold the start until the check passes

Add `Notify=healthy`, and raise `TimeoutStartSec` so the warm-up fits into the start:

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: {
    		Container: Notify:         "healthy"
    		Service: TimeoutStartSec: "300"
    	}
    }
    ```

=== "JSON"

    ```json
    "Container": {
      "Notify": "healthy"
    },
    "Service": {
      "TimeoutStartSec": "300"
    }
    ```

systemd gives up on a start after 90 seconds by default.
If the check hasn't passed by `TimeoutStartSec`, systemd fails the start and syslet reports the container as failed (see [Troubleshoot a failed apply](troubleshoot-a-failed-apply.md#a-unit-failed-during-the-apply)).

## Preview and apply

=== "CUE"

    ```sh
    cue cmd plan
    cue cmd apply
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    cat hosts/web01/*.json | ssh web01 sudo syslet --stdin
    ```

The new options change `app`'s unit, so syslet restarts it, and the apply now waits until `app` is healthy.

## Related tasks

### Restart an unhealthy container

After the start, a failing check only marks the container unhealthy.
To restart it instead, set `HealthOnFailure=kill`: podman stops the container, and systemd starts it again because syslet sets `Restart=always` for running containers.

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Container: HealthOnFailure: "kill"
    }
    ```

=== "JSON"

    ```json
    "Container": {
      "HealthOnFailure": "kill"
    }
    ```
