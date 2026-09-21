# Unit types

syslet specs describe one of four unit types, each mapping to a corresponding [quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) unit file. (A fifth type, `secret`, exists in the code but is out of scope for these docs for now; see `docs/TODO.md`.)

- **`container`** — a running (or stopped) Podman container, generating a `.container` quadlet unit. The only type with a `desiredState` (`running`/`stopped`), since it's the only unit that's started or stopped as a service. Volumes, networks, and builds exist to be referenced by containers, not run themselves. Containers are also the only unit type that can mount `configs`/`configDirs`. See [Reference → Container spec](../reference/spec-schema/container.md).
- **`volume`** — a Podman volume, generating a `.volume` quadlet unit, referenced from a container's `Volume=` option. See [Reference → Volume spec](../reference/spec-schema/volume.md).
- **`network`** — a Podman network, generating a `.network` quadlet unit, referenced from a container's `Network=` option. See [Reference → Network spec](../reference/spec-schema/network.md).
- **`build`** — a local image build, generating a `.build` quadlet unit; a container references it by setting `Image` to `<build-name>.build`. Unlike the other types, it always uses a `localhost/`-prefixed image tag, because a build exists to produce a local-only image without a registry round-trip. See [Customize an upstream container image](../how-to/build-a-container-image.md).

`volume`, `network`, and `build` share a `reclaimPolicy` field controlling whether their backing podman resource is deleted or retained when the unit is removed (see [Removing units](removing-units.md)); `container` does not have one, since removing a container's unit always stops and removes the container itself.
