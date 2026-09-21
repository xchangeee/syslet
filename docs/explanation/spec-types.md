# Spec types

A spec is syslet's high-level object, similar in spirit to a Kubernetes resource: a declarative description of one thing you want to exist on the host. Every spec maps to a podman object — a container, volume, network, image, or secret.

The resemblance stops short of the Kubernetes object model, deliberately. syslet aims for a small set of types, so there is no separate ConfigMap-style object and no standalone mount type; config files belong to the container spec that uses them.

Four of the five types (`container`, `volume`, `network`, `build`) get there by way of a [quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html) unit file, which systemd's quadlet generator turns into a systemd service unit, which in turn creates the podman object. So the chain is spec → quadlet unit → systemd unit → podman object. `secret` is the exception: it has no unit file and is written straight to podman's secret store.

## container

A running (or stopped) podman container, generating a `.container` quadlet unit. It is the only type with a `desiredState` (`running`/`stopped`), since it's the only unit that's started or stopped as a service — volumes, networks, and builds exist to be referenced by containers, not run themselves.

A container spec manages more than the unit file: some containers needs configuration alongside, so syslet writes both from the same spec in one pass. `configs` are single bind-mounted files, the simple case; changing one restarts the container. `configDirs` bind-mount a directory, written as a versioned directory behind a symlink that is swapped atomically — the same trick Kubernetes uses for ConfigMap volume mounts — so the container sees a complete new set of files without a restart — use them when the workload can reload its config in place. See [Mount config files and dirs](../how-to/mount-config-files-and-dirs.md) and [Reload config without restart](../how-to/reload-config-without-restart.md).

## volume

A podman volume, generating a `.volume` quadlet unit, referenced from a container's `Volume=` option. Volumes and networks are the two types that mostly exist to be attached to containers, and they are typically declared in the same change as the container that uses them.

## network

A podman network, generating a `.network` quadlet unit, referenced from a container's `Network=` option. Like volumes, a network is inert on its own; it matters only once a container joins it.

## build

A local image build, generating a `.build` quadlet unit; a container references it by setting `Image` to `<build-name>.build`. Unlike the other types, it always uses a `localhost/`-prefixed image tag, because a build exists to produce a local-only image without a registry round-trip.

Like `container`, a build spec owns files as well as a unit: `containerfile` and `configs` are written into a build context directory on the host. A build unit is meaningless without its context, so the two are versioned and applied together. See [Customize an upstream container image](../how-to/build-a-container-image.md).

## secret

A SOPS-encrypted YAML file, stored in podman's native secret store. Each key in the file becomes one podman secret named `<spec-name>-<key>`, referenced from a container's `Secret=` option. There is no quadlet unit and no systemd involvement — syslet decrypts during the plan phase and writes the values to podman directly.

Change detection uses `sha256` of the ciphertext, stored as a podman label, so plaintext is never hashed or persisted. Containers referencing a changed secret are restarted as part of the same apply.
