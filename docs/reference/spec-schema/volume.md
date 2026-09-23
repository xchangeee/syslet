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
| `unit` | object | yes | Quadlet `Volume` section options. May be empty (`{}`). |
| `reclaimPolicy` | string | no | `"Delete"` (default) or `"Retain"`. Controls whether the podman volume is also deleted when the unit is pruned. |
| `removalAllowed` | bool | no | Default `true`. Must be `true` before syslet will prune this unit. Set `false` to protect it. |

`VolumeName` is always set to the spec's `name`; a `VolumeName` in `unit.Volume` is overwritten.
