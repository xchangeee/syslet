# Run a oneshot job

A oneshot container runs a command to completion, such as a database migration or a backup, instead of staying up.
syslet writes its unit but never starts or stops it, so another container or a timer has to start it.

## Define the job

Set `desiredState: "oneshot"`:

=== "CUE"

    ```cue
    sysdef: containers: migrate: spec: {
    	desiredState: "oneshot"
    	unit: Container: {
    		Image: "registry.example.com/app:1.4"
    		Exec: "migrate"
    	}
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "container",
      "name": "migrate",
      "desiredState": "oneshot",
      "unit": {
        "Container": {
          "Image": "registry.example.com/app:1.4",
          "Exec": "migrate"
        }
      }
    }
    ```

syslet sets `[Service] Type=oneshot` and no `[Install]` section, so the job doesn't start at boot on its own.
Setting `Type=oneshot` yourself on a `"running"` container is rejected.

## Run it before another container

Make the container that needs the job, here `app`, require it:

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Unit: {
    		Requires: "migrate.container"
    		After: "migrate.container"
    	}
    }
    ```

=== "JSON"

    ```json
    "unit": {
      "Unit": {
        "Requires": "migrate.container",
        "After": "migrate.container"
      }
    }
    ```

systemd now runs `migrate` to completion every time `app` starts, and doesn't start `app` if `migrate` fails.
Unlike between two long-running containers (see [Avoid Requires=](add-dependencies-between-containers.md#avoid-requires)), `Requires=` is safe here, since syslet never stops the job.

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

The plan shows no service action for the job.
It runs when `app` starts, which the new `[Unit]` options trigger by restarting `app`.

## Related tasks

### Run it once per boot

To skip the job on later starts of `app`, such as a restart by an apply, keep it active after it exits, then apply:

=== "CUE"

    ```cue
    sysdef: containers: migrate: spec: {
    	unit: Service: RemainAfterExit: "yes"
    }
    ```

=== "JSON"

    ```json
    "Service": {
      "RemainAfterExit": "yes"
    }
    ```

A change to the job's spec then doesn't run it again until the next boot; run it by hand with `systemctl restart migrate`, which restarts `app` too.

### Run it on a schedule

syslet doesn't manage timers, so write one by hand, e.g. `/etc/systemd/system/backup.timer` for a job named `backup`:

```ini title="backup.timer"
[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now backup.timer
```

The timer starts `backup.service`, the service of the same name.
Leave `RemainAfterExit=` unset on a scheduled job, since a timer can't start a job that's still active.
