# syslet

syslet (systemd + kubelet) is a GitOps-friendly deployment tool for [Podman](https://podman.io/) containers using [systemd](https://systemd.io/) and [quadlets](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html).
You describe containers, volumes, networks, image builds and secrets as declarative specs, and syslet turns them into quadlet unit files and drives systemd and podman to match.

It keeps some aspects of the Kubernetes user experience (declarative specs, validation and a diff preview before anything changes, cleanup of what's no longer declared) without a control plane: syslet runs once per invocation and leaves the rest to systemd.

## A first look

`webapp.json`:

```json
{
  "apiVersion": "v1",
  "type": "container",
  "name": "webapp",
  "desiredState": "running",
  "unit": {
    "Container": {
      "Image": "docker.io/library/nginx:latest",
      "PublishPort": ["8080:80"]
    }
  }
}
```

```sh
cat webapp.json | ssh web01 sudo syslet --diff --stdin   # preview
cat webapp.json | ssh web01 sudo syslet --stdin          # apply
```

For anything beyond a few specs, write them in [CUE](https://cuelang.org/) instead; see [Set up a CUE repository](docs/tutorials/03-setup-cue-repository.md).

## Documentation

- [Tutorials](docs/tutorials/index.md): learn syslet hands-on, starting from installation and a first deployment.
- [How-to guides](docs/how-to/index.md): recipes for specific tasks.
- [Explanation](docs/explanation/index.md): background and design rationale.
- [Reference](docs/reference/index.md): CLI, spec schema, file layout.

Serve the docs locally with `make docs-serve`.

## Build

```sh
make build-bin
```

Requires Go 1.27 or newer.
See [Installation](docs/tutorials/01-installation.md) for putting syslet on a host.

## License

[AGPL-3.0](LICENSE.txt)
