# Unit options

A spec's `unit` object maps 1:1 onto the sections and options of the unit file syslet writes.

| Section | Source | Spec types |
| --- | --- | --- |
| `Container` | quadlet | `container` |
| `Volume` | quadlet | `volume` |
| `Network` | quadlet | `network` |
| `Build` | quadlet | `build` |
| `Unit`, `Service`, `Install` | systemd, passed on by quadlet to the generated service | all four |

Every option of these sections is listed in the upstream man page, vendored at [`hack/reference/podman-systemd.unit.5.md`](https://codeberg.org/xchangeee/syslet/src/branch/main/hack/reference/podman-systemd.unit.5.md), and in `man podman-systemd.unit` on a host with podman installed.

## Value forms

| Form | Example | Rendered as |
| --- | --- | --- |
| string | `"Image": "nginx:1.27"` | `Image=nginx:1.27` |
| list of strings | `"PublishPort": ["80:80", "443:443"]` | one line per entry |
| map of strings | `"Environment": {"TZ": "UTC"}` | one `KEY=value` line per entry, quoted if the value contains whitespace |

## Options syslet sets or checks

| Option | Behavior |
| --- | --- |
| `ContainerName`, `VolumeName`, `NetworkName` | Always set to the spec's `name`, overwriting any value in `unit`. |
| `[Service] Restart=always` | Added to a container with `desiredState: "running"`, overwriting any value in `unit`. |
| `[Install] WantedBy=multi-user.target default.target` | Added to a container with `desiredState: "running"`, overwriting any value in `unit`. |
| `[Unit] Description` | Defaults to `<name> container` on containers. A change only rewrites the unit file. |
| `[Service] ExecReload=` | Not set by syslet; required when the container declares `configDirs`. See [Mount config files and dirs](../../how-to/mount-config-files-and-dirs.md#reload-config-without-a-restart). |
| `[Build] ImageTag=` | Must start with `localhost/<build name>:`. |
| `[X-Syslet]` | Reserved for syslet's markers; a spec that sets it is rejected. |

All other option names and values are passed through unchecked, and podman's quadlet generator rejects invalid ones at the staging stage of validation (see [Safety mechanisms](../../explanation/safety-mechanisms.md#multi-stage-validation)).
