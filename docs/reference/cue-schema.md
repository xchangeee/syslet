# CUE schema

The CUE module `github.com/xchangeee/syslet@v0` describes the JSON specs and renders them from a `sysdef` struct.
Its schema is in two packages:

```cue
import (
	sysletcore "github.com/xchangeee/syslet/schema/core@v0"
	syslettools "github.com/xchangeee/syslet/schema/tools@v0"
)
```

## core

### #Sysdef

Collects a host's entries by type, each keyed by name.
The key becomes the spec's `name`.

| Field | Entry type | Spec |
| --- | --- | --- |
| `containers` | `#SysdefContainerUnit` | `#ContainerSpec` |
| `networks` | `#SysdefNetworkUnit` | `#NetworkSpec` |
| `volumes` | `#SysdefVolumeUnit` | `#VolumeSpec` |
| `builds` | `#SysdefBuildUnit` | `#BuildSpec` |
| `secrets` | `#SysdefSecret` | `#SecretSpec` |

Every entry has two fields:

| Field | Type | Notes |
| --- | --- | --- |
| `enabled` | bool | Required. `false` leaves the entry out of the rendered specs. |
| `spec` | the spec definition | Required. `name` and `type` are set for you. |

### #SysdefDefaults

Unify with `#Sysdef` to default these fields:

| Type | `enabled` | `removalAllowed` | `desiredState` | `reclaimPolicy` |
| --- | --- | --- | --- | --- |
| containers | `true` | `true` | `"running"` | — |
| networks | `true` | `true` | — | — |
| volumes | `true` | `true` | — | `"Delete"` |
| builds | `true` | `true` | — | `"Delete"` |
| secrets | `true` | — | — | — |

### Spec definitions

`#ContainerSpec`, `#NetworkSpec`, `#VolumeSpec`, `#BuildSpec` and `#SecretSpec` match the [JSON spec schema](spec-schema/index.md), with these differences:

| Field | CUE | JSON |
| --- | --- | --- |
| `apiVersion` | Set to `"v1"` | Required |
| `removalAllowed` | Required on all four unit types, including builds, where syslet ignores it | Optional, defaults to `false`, no build field |
| `desiredState` | Required, `"running"` or `"stopped"` | Optional, defaults to `"stopped"` |
| `reclaimPolicy` | Required on volumes and builds; not accepted on networks | Optional, defaults to `"Retain"` |
| `configFiles[].mode`, `configDirs[].files[].mode` | Required | Optional, defaults to `"0644"` |
| `contextFiles[].mode` | Not accepted | Optional |

`#SysdefDefaults` provides the required values that have a default.

<!-- TODO: drop the build removalAllowed and contextFiles mode rows once the schema is fixed, see docs/TODO.md -->
<!-- TODO: note that unit is required once the loader and schema require it, see docs/TODO.md -->

## tools

### #SysdefLock

| Field | Type | Notes |
| --- | --- | --- |
| `in.containers` | list of names | Sets `removalAllowed: false`. |
| `in.networks` | list of names | Sets `removalAllowed: false`. |
| `in.volumes` | list of names | Sets `removalAllowed: false` and `reclaimPolicy: "Retain"`. |
| `out` | partial `#Sysdef` | Unify with `sysdef`. |

```cue
sysdef: (syslettools.#SysdefLock & {in: volumes: ["site-data"]}).out
```

### #SysdefAssignNetwork

| Field | Type | Notes |
| --- | --- | --- |
| `in` | map of network name to list of container names | |
| `out` | partial `#Sysdef` | Sets `unit: Container: Network` on each listed container to all networks it's listed under, as `<network>.network`. |

```cue
sysdef: (syslettools.#SysdefAssignNetwork & {in: "app-net": ["app", "cache"]}).out
```

### #SysdefSecretsFromEmbeddedFiles

| Field | Type | Notes |
| --- | --- | --- |
| `in` | map of file name to file content | The result of `@embed(glob=creds-*.enc.yaml,type=text,allowEmptyGlob)`. |
| `secrets` | map of `#SysdefSecret` | One entry per file, named after the file without the `creds-` prefix and `.enc.yaml` suffix. |

```cue
sysdef: secrets: (syslettools.#SysdefSecretsFromEmbeddedFiles & {in: secretFiles}).secrets
```

### #SysletJsonFromSysdef

| Field | Type | Notes |
| --- | --- | --- |
| `in` | `#Sysdef` | |
| `specRendered` | string | JSON array of every enabled entry's spec, the input for `syslet --stdin`. |

```cue
syslet: specRendered: (syslettools.#SysletJsonFromSysdef & {in: sysdef}).specRendered
```
