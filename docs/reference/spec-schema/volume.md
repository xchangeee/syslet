# Volume spec

```json
{
  "name": "webapp-data",
  "type": "volume",
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
| `type` | string | yes | `"volume"` |
| `name` | string | yes | Also used as `VolumeName` unless overridden in `unit.Volume`. Referenced from a container as `Volume: ["<name>.volume:/path"]`. |
| `unit` | object | yes | Quadlet `Volume` section options. |
| `reclaimPolicy` | string | no | `"Retain"` (default) or `"Delete"`. Controls whether the podman volume is also deleted when the unit is pruned. |
| `removalAllowed` | bool | no | Default `false`. Must be `true` before syslet will prune this unit. |

`VolumeName` is set automatically to the spec's `name` if not explicitly given in `unit.Volume`.
