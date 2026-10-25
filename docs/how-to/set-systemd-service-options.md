# Set systemd service options

Besides its quadlet section, a spec's `unit` passes `[Unit]`, `[Service]` and `[Install]` straight into the generated systemd service.
Use them for timeouts, restart behavior and dependencies between containers (see [Add dependencies between containers](add-dependencies-between-containers.md)).

## Allow slow starts

systemd gives up on a service that hasn't started after 90 seconds by default.
By default, podman reports a container as started as soon as its process runs, so most containers finish starting well within that limit.
syslet pulls missing images before it stops anything during an apply, so that download doesn't count against the limit either.

A start still takes longer when:

- The container waits until it's healthy: the start then includes the app's whole warm-up (see [Wait until a container is healthy](wait-until-a-container-is-healthy.md)).
- Podman pulls the image during the start: with `[Container] Pull=always` or `Pull=newer`, which check the registry on every start, or when the image is gone at boot, for example after `podman image prune`.
- A large volume is mounted with `:Z` or `:z`, which relabels every file for SELinux on each start.

For these containers, raise the limit:

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Service: TimeoutStartSec: "900"
    }
    ```

=== "JSON"

    ```json
    "Service": {
      "TimeoutStartSec": "900"
    }
    ```

## Tune restarts

With `desiredState: "running"`, syslet sets `[Service] Restart=always` and `[Install] WantedBy=multi-user.target default.target`, overwriting your own values for those two keys.
It also sets a restart backoff, which your own values replace key by key:

| Option | Default | Effect |
| --- | --- | --- |
| `[Service] RestartSec=` | `1s` | Delay before the first restart. |
| `[Service] RestartSteps=` | `10` | Number of restarts over which the delay grows to `RestartMaxDelaySec`. |
| `[Service] RestartMaxDelaySec=` | `5min` | Longest delay between two restarts. |
| `[Unit] StartLimitIntervalSec=` | `0` | Turns off the start limit, so systemd keeps retrying instead of leaving the unit `failed`. |

`RestartSteps=` and `RestartMaxDelaySec=` need systemd 254 or newer.
On an older systemd, or when syslet can't read the host's systemd version, it sets `RestartSec=10s` instead and leaves both out, so systemd retries every 10 seconds.

To give up after a number of failed starts instead, set `[Unit] StartLimitIntervalSec=` and `StartLimitBurst=` yourself:

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Unit: {
    		StartLimitIntervalSec: "1h"
    		StartLimitBurst:       "10"
    	}
    }
    ```

=== "JSON"

    ```json
    "Unit": {
      "StartLimitIntervalSec": "1h",
      "StartLimitBurst": "10"
    }
    ```

## Apply a change

A change to any of these sections restarts the container, except `[Unit] Description` and the restart settings `RestartSec=`, `RestartSteps=`, `RestartMaxDelaySec=`, `StartLimitIntervalSec=` and `StartLimitBurst=`.
systemd picks those up on daemon-reload, so syslet only rewrites the unit file.

syslet doesn't check option names; podman's generator and `systemd-analyze verify` reject invalid ones when you preview (see [Troubleshoot a failed apply](troubleshoot-a-failed-apply.md)).
For every section and option you can set, see [Unit options](../reference/spec-schema/unit-options.md).
