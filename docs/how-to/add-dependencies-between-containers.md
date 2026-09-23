# Add dependencies between containers

A container that needs another one, such as an app and its database, declares that in its `[Unit]` section.
systemd then starts the other container too, and in the right order.
The examples below make the container `app` depend on `db`.

## 1. Declare the dependency

Name the other container's quadlet file in `Wants=` and `After=`:

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Unit: {
    		Wants: "db.container"
    		After: "db.container"
    	}
    }
    ```

=== "JSON"

    ```json
    "unit": {
      "Unit": {
        "Wants": "db.container",
        "After": "db.container"
      }
    }
    ```

Quadlet translates `db.container` into the service name `db.service`.

- `Wants=` starts `db` whenever `app` starts, for example when you start `app` by hand.
- `After=` makes `app` wait until `db`'s service has started, and stops `app` before `db` on shutdown.

`After=` waits for the container to start, not for the database to accept connections, so `app` should retry its first connection.

syslet doesn't check the name.
With a misspelled one, systemd ignores the dependency and starts `app` on its own.

## 2. Preview and apply

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

The new `[Unit]` options change `app`'s unit, so syslet restarts it.

## Avoid Requires=

`Requires=`, `BindsTo=` and `PartOf=` make systemd stop `app` whenever `db` stops.
When a change restarts `db`, syslet stops it and starts it again, but it doesn't start `app`, since `app` was running when the plan was made.
`app` stays down until the next apply.

`Wants=` doesn't pass the stop on, so `app` keeps running while `db` restarts.
A oneshot job is the exception, see [Run a oneshot job](run-a-oneshot-job.md).
