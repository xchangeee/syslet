# Network spec

```json
{
  "name": "webapp-net",
  "type": "network",
  "unit": {
    "Network": {
      "Subnet": "10.89.0.0/24",
      "Gateway": "10.89.0.1"
    }
  },
  "reclaimPolicy": "Retain",
  "removalAllowed": false
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `type` | string | yes | `"network"` |
| `name` | string | yes | Also used as `NetworkName` unless overridden in `unit.Network`. Referenced from a container as `Network: ["<name>.network"]`. |
| `unit` | object | yes | Quadlet `Network` section options. |
| `reclaimPolicy` | string | no | `"Retain"` (default) or `"Delete"`. Controls whether the podman network is also deleted when the unit is pruned. |
| `removalAllowed` | bool | no | Default `false`. Must be `true` before syslet will prune this unit. |

`NetworkName` is set automatically to the spec's `name` if not explicitly given in `unit.Network`.
