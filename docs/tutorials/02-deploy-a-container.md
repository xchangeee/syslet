# Deploy container

In this tutorial, you'll preview and deploy a single container with syslet and check it runs under systemd.
Then you'll change it, preview the change with `--diff`, apply it, and confirm re-applying an unchanged spec does nothing.

## Prerequisites

- A server with syslet installed, as in [Installation](01-installation.md).

## 1. Write a container spec

Create a file named `webapp.json`:

```json title="webapp.json"
--8<-- "02-deploy-a-container/step-1/webapp.json"
```

`unit.Container` maps directly onto the `[Container]` section of a [quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) `.container` file.
syslet sets `ContainerName` to `webapp`, the spec's `name`.

## 2. Preview the deployment

Pipe the spec to syslet on the server over SSH, with `--diff`:

```sh
cat webapp.json | ssh web01 sudo syslet --stdin --diff
```

`--diff` runs the full plan (validate, diff against installed state) but stops before executing anything.
It shows the unit file syslet would write, including the options it adds on its own, and that the container would be started:

```text
Unit file changes:

--- webapp.container
added:
  [Container] ContainerName=webapp
  [Container] Image=docker.io/library/nginx:latest
  [Container] PublishPort=8080:80
  [Install] WantedBy=multi-user.target default.target
  [Service] Restart=always
  [Unit] Description=webapp container
  [X-Syslet] RemovalAllowed=true

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         created    created, started (desired: running)
```

Nothing on the server is touched yet.

## 3. Apply it

Run the same command without `--diff`:

```sh
cat webapp.json | ssh web01 sudo syslet --stdin
```

syslet reads the spec, validates it, generates `/etc/containers/systemd/webapp.container`, runs `systemctl daemon-reload`, and starts the container.
It logs each action as it runs, then prints the result per unit:

```text
time=2026-09-23T09:39:47.345+02:00 level=INFO msg="writing unit file" unit=webapp.container
time=2026-09-23T09:39:47.345+02:00 level=INFO msg=daemon-reload
time=2026-09-23T09:39:47.345+02:00 level=INFO msg=starting unit=webapp.container
webapp.container                         created    created, started (desired: running)
```

It also saves the applied spec to `/etc/syslet/config.json`.

## 4. Verify

```sh
ssh web01 sudo systemctl status webapp.service
ssh web01 sudo podman ps
```

You should see `webapp.service` active and a running `nginx` container.
Because `desiredState: "running"` was set, syslet also added `Restart=always` and `WantedBy=multi-user.target default.target`, so the container restarts on crash and starts on boot.

## 5. Preview a change

Edit `webapp.json` to publish an extra port:

```json title="webapp.json" hl_lines="9"
--8<-- "02-deploy-a-container/webapp.json"
```

Preview it again:

```sh
cat webapp.json | ssh web01 sudo syslet --stdin --diff
```

This time the plan diffs against the installed unit, so it only shows the new option, and that the container will be stopped and started again:

```text
Unit file changes:

--- webapp.container
added:
  [Container] PublishPort=8443:443

Services to stop:
  - webapp.container

Systemd daemon-reload: required

Containers to start:
  - webapp.container

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    unit updated, restarted (desired: running)
```

Nothing on the server is touched yet.

## 6. Apply it

```sh
cat webapp.json | ssh web01 sudo syslet --stdin
```

syslet carries out the plan it just showed:

```text
time=2026-09-23T09:41:12.508+02:00 level=INFO msg=stopping unit=webapp.container
time=2026-09-23T09:41:12.508+02:00 level=INFO msg="writing unit file" unit=webapp.container
time=2026-09-23T09:41:12.508+02:00 level=INFO msg=daemon-reload
time=2026-09-23T09:41:12.508+02:00 level=INFO msg=starting unit=webapp.container
webapp.container                         updated    unit updated, restarted (desired: running)
```

## 7. Re-apply without changes

Run the exact same command again:

```sh
cat webapp.json | ssh web01 sudo syslet --stdin
```

syslet reports no changes and does not stop or restart `webapp.service`:

```text
webapp.container                         unchanged  up to date (desired: running)
```

Applying an unchanged spec is always a no-op, which makes it safe to re-run syslet on every deploy, or on a schedule, without unnecessary restarts.

See [Deploy over SSH](../how-to/deploy-over-ssh.md) for more on this workflow.
