# Secret spec

```json
{
  "name": "webapp",
  "type": "secret",
  "ciphertext": "db-password: ENC[AES256_GCM,data:...,type:str]\napi-key: ENC[AES256_GCM,data:...,type:str]\nsops:\n    age:\n        - recipient: age1...\n          enc: |\n            -----BEGIN AGE ENCRYPTED FILE-----\n            ...\n            -----END AGE ENCRYPTED FILE-----\n    version: 3.9.0\n"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `type` | string | yes | `"secret"` |
| `name` | string | yes | Prefix for the podman secret names this spec produces: each key becomes `<name>-<key>`. |
| `ciphertext` | string | yes | The SOPS-encrypted YAML file content itself (the text, not a path and not base64). A flat mapping of key → encrypted value, plus SOPS' own `sops` metadata block. Key names must match `[a-z0-9-]+`. |

A secret has no `unit`, `removalAllowed`, or `reclaimPolicy` field. Referenced from a container as `Secret: ["<name>-<key>,type=env,target=ENV_VAR"]`. See [Spec types](../../explanation/spec-types.md#secret).
