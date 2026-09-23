# Volume spec

```json
{
  "apiVersion": "v1",
  "type": "volume",
  "name": "webapp-data",
  "unit": {
    "Volume": {
      "Label": "app=webapp"
    }
  },
  "reclaimPolicy": "Retain",
  "removalAllowed": false
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `apiVersion` | string | yes | `"v1"`. See [API versioning](index.md#api-versioning). |
| `type` | string | yes | `"volume"` |
| `name` | string | yes | Also used as `VolumeName`. Referenced from a container as `Volume: ["<name>.volume:/path"]`. |
| `unit` | object | no | Quadlet `Volume` section options. |
| `reclaimPolicy` | string | no | `"Delete"` (default) or `"Retain"`. Controls whether the podman volume is also deleted when the unit is pruned. |
| `removalAllowed` | bool | no | Default `false`. Must be `true` before syslet will prune this unit. |

<!-- TODO: mark unit as required once the loader enforces it, see docs/TODO.md -->

`VolumeName` is always set to the spec's `name`; a `VolumeName` in `unit.Volume` is overwritten.
