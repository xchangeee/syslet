# Unit options

A spec's `unit` object maps 1:1 onto the sections and options of the unit file syslet writes.

| Section | Source | Spec types |
| --- | --- | --- |
| `Unit`, `Service`, `Install` | systemd, passed on by quadlet to the generated service | all four |
| `Container` | quadlet | `container` |
| `Volume` | quadlet | `volume` |
| `Network` | quadlet | `network` |
| `Build` | quadlet | `build` |

Every option of these sections is listed in the upstream man page, vendored at [`hack/reference/podman-systemd.unit.5.md`](https://codeberg.org/xchangeee/syslet/src/branch/main/hack/reference/podman-systemd.unit.5.md), and in `man podman-systemd.unit` on a host with podman installed.

## Value forms

| Form | Example | Rendered as |
| --- | --- | --- |
| string | `"Image": "nginx:1.27"` | `Image=nginx:1.27` |
| list of strings | `"PublishPort": ["80:80", "443:443"]` | one line per entry |
| map of strings | `"Environment": {"TZ": "UTC"}` | one line per entry, sorted by key; see below |

A map is only accepted for these options, and a map on any other option is rejected:

| Section | Option | Entry rendered as | Empty value |
| --- | --- | --- | --- |
| any | `Environment` | `KEY=value` | allowed, renders `KEY=` |
| `Container` | `Volume` | `source:destination` | rejected |
| `Container` | `Secret` | `name,options` | allowed, renders `name` |
| `Container` | `Mask` | `path:path` | rejected |
| `Container` | `LogOpt` | `option=value` | rejected |
| `Container` | `Network` | `name`; the value is ignored | ignored |

An `Environment` entry that contains whitespace is wrapped in double quotes, whichever form it was written in, so systemd reads it as one assignment.
Unquoted, the quadlet generator splits it at the whitespace into several `--env` flags ([podman#26259](https://github.com/podman-container-tools/podman/issues/26259)).
Other options are written as given.

## Options syslet sets or checks

| Section | Option | Behavior |
| --- | --- | --- |
| `Unit` | `Description` | Defaults to `<name> container` on containers. A change only rewrites the unit file. |
| `Service` | `Restart=always` | Added to a container with `desiredState: "running"`, overwriting any value in `unit`. |
| `Service` | `Type=oneshot` | Added to a container with `desiredState: "oneshot"`; with `"running"`, setting it in `unit` is rejected. |
| `Service` | `ExecReload` | Not set by syslet; required when the container declares `configDirs`. See [Mount config files and dirs](../../how-to/mount-config-files-and-dirs.md#reload-config-without-a-restart). |
| `Install` | `WantedBy=multi-user.target default.target` | Added to a container with `desiredState: "running"`, overwriting any value in `unit`. |
| `Container` | `ContainerName` | Always set to the spec's `name`, overwriting any value in `unit`. |
| `Container` | `Volume` | A read-only mount is added for every `configFiles` and `configDirs` entry, next to any in `unit`. |
| `Volume` | `VolumeName` | Always set to the spec's `name`, overwriting any value in `unit`. |
| `Network` | `NetworkName` | Always set to the spec's `name`, overwriting any value in `unit`. |
| `Build` | `File` | Always set to the Containerfile syslet writes, overwriting any value in `unit`. |
| `Build` | `ImageTag` | Must start with `localhost/<build name>:`. |
| `X-Syslet` | all | Reserved for syslet's markers; a spec that sets it is rejected. |

All other option names and values are passed through unchecked, and podman's quadlet generator rejects invalid ones at the staging stage of validation (see [Safety mechanisms](../../explanation/safety-mechanisms.md#multi-stage-validation)).
