# First deployment

This tutorial walks through deploying a single container with syslet, from building the binary to seeing it running under systemd.

## Prerequisites

- A Linux server with [Podman](https://podman.io/) installed (e.g. `dnf install podman`) and systemd.
- Root access on that server (syslet needs it for systemd D-Bus operations).
- Go 1.27+ on your local machine to build syslet, and SSH access to the server.

## 1. Build syslet

```sh
make build-bin
```

This cross-compiles the `syslet` binary for macOS and Linux into `build/`. Copy the binary matching your server's architecture, e.g. `build/syslet-linux-amd64`, onto the server and make sure it's on `PATH`.

## 2. Write a container spec

Create a file named `webapp.json`:

```json
{
  "name": "webapp",
  "type": "container",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80"]
    }
  }
}
```

`unit.Container` maps directly onto the `[Container]` section of a [quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) `.container` file. `ContainerName` is set automatically to `webapp` (the spec's `name`) since it isn't given explicitly.

## 3. Deploy it

Copy the spec to the server and apply it:

```sh
scp webapp.json web01:/etc/syslet/config.json
ssh web01 sudo syslet
```

syslet reads the spec, validates it, generates `/etc/containers/systemd/webapp.container`, runs `systemctl daemon-reload`, and starts the container.

## 4. Verify

```sh
ssh web01 sudo systemctl status webapp.service
ssh web01 sudo podman ps
```

You should see `webapp.service` active and a running `nginx` container. Because `desiredState: "running"` was set, syslet also added `Restart=Always` and `WantedBy=multi-user.target default.target`, so the container restarts on crash and starts on boot.

Next: [Updating and diffing](02-updating-and-diffing.md) covers changing this spec safely and re-applying it.
