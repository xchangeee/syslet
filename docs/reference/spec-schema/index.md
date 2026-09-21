# Spec schema

Every spec file (or stream element) is a JSON object with a `type` field selecting one of the unit types below, plus a `name` used to derive the quadlet unit name and, unless overridden, the resource name (`ContainerName`, `VolumeName`, `NetworkName`).

- [Container](container.md)
- [Build](build.md)
- [Network](network.md)
- [Volume](volume.md)

All four share a `unit` object, whose keys are quadlet unit file section names (`Container`, `Volume`, `Network`, `Build`, `Service`, ...) mapping 1:1 to that section's options; see [Quadlet options](../quadlet-options.md) for the full set podman supports. syslet does not validate unknown quadlet options; it passes them through and lets podman's own quadlet generator reject anything invalid at [staging time](../../explanation/safety-mechanisms.md).
