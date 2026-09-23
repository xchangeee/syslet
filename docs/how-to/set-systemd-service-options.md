# Set systemd service options

Besides its quadlet section, a spec's `unit` passes `[Unit]`, `[Service]` and `[Install]` straight into the generated systemd service.
Use them for timeouts, restart behavior and dependencies between containers (see [Add dependencies between containers](add-dependencies-between-containers.md)).

## Allow slow starts

Podman pulls a missing image while the service starts, and systemd gives up after 90 seconds by default.
For large images, raise the limit:

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
Other restart settings, such as `[Service] RestartSec=` or `[Unit] StartLimitBurst=`, pass through.

## Apply a change

A change to any of these sections restarts the container, except `[Unit] Description`, which only rewrites the unit file.

syslet doesn't check option names; podman's generator and `systemd-analyze verify` reject invalid ones when you preview (see [Troubleshoot a failed apply](troubleshoot-a-failed-apply.md)).
For every section and option you can set, see [Unit options](../reference/spec-schema/unit-options.md).
