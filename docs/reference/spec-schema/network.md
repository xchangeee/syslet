# Network spec

```json
{
  "apiVersion": "v1",
  "type": "network",
  "name": "webapp-net",
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
| `apiVersion` | string | yes | `"v1"`. See [API versioning](index.md#api-versioning). |
| `type` | string | yes | `"network"` |
| `name` | string | yes | Also used as `NetworkName`. Referenced from a container as `Network: ["<name>.network"]`. |
| `unit` | object | no | Quadlet `Network` section options. |
| `reclaimPolicy` | string | no | `"Delete"` (default) or `"Retain"`. Controls whether the podman network is also deleted when the unit is pruned. |
| `removalAllowed` | bool | no | Default `false`. Must be `true` before syslet will prune this unit. |

<!-- TODO: mark unit as required once the loader enforces it, see docs/TODO.md -->

`NetworkName` is always set to the spec's `name`; a `NetworkName` in `unit.Network` is overwritten.
