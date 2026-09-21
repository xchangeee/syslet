# Daemon config

An optional JSON file at `/etc/syslet/syslet.json` configures host-level settings. syslet works without it, using the defaults below.

```json
{
  "sshKeyPath": "/etc/ssh/ssh_host_ed25519_key"
}
```

| Field | Default | Description |
|---|---|---|
| `sshKeyPath` | `/etc/ssh/ssh_host_ed25519_key` | Path to the host's SSH private key, used to derive an age identity for decrypting SOPS-encrypted secret specs. |

If the file at `sshKeyPath` doesn't exist, syslet skips secret decryption setup silently. This only becomes an error if the applied specs contain a `secret` unit that needs decrypting.

## Age key cache

Derived age private keys are cached, one per line, in an append-only file at `/var/lib/syslet/key.txt`. Old keys are kept across SSH host-key rotations so secrets encrypted against a previous key remain decryptable.
