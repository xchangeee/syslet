# Spec schema

Every spec file (or stream element) is a JSON object that starts with three required fields: `apiVersion` selecting the [spec format](#api-versioning), `type` selecting one of the unit types below, and `name`, used as the quadlet unit name and the resource name (`ContainerName`, `VolumeName`, `NetworkName`).

- [Container](container.md)
- [Build](build.md)
- [Network](network.md)
- [Volume](volume.md)
- [Secret](secret.md)
- [Unit options](unit-options.md)

`container`, `build`, `network`, `volume` all share a `unit` object, whose keys are unit file section names, quadlet's (`Container`, `Volume`, `Network`, `Build`) and systemd's (`Unit`, `Service`, `Install`), mapping 1:1 to that section's options; see [Unit options](unit-options.md) for the full set. syslet does not validate unknown quadlet options; it passes them through and lets podman's own quadlet generator reject anything invalid at [staging time](../../explanation/safety-mechanisms.md).

`secret` has no `unit` object, and its `name` is a prefix for the podman secrets it produces rather than a unit name.

## API versioning

Every spec must set `apiVersion`, which selects the spec format. The only version is `"v1"`. The CUE schema sets it for you.

Versions are linear (`v1`, `v2`, …). A breaking change introduces a new version and leaves the old one untouched.

A spec without `apiVersion`, or with one syslet doesn't know, such as one newer than the running syslet, is rejected instead of being guessed or partially understood.

An old version prints a warning once it is superseded. It is removed only in a release whose notes say so.
