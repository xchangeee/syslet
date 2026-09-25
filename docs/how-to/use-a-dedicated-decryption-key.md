# Use a dedicated decryption key

By default, syslet derives its age key from the host's SSH key at `/etc/ssh/ssh_host_ed25519_key`.
A dedicated key decouples secrets from the host key: you can rotate the host key freely, and restore the dedicated key from a backup after reinstalling the host.

## 1. Create the key on the host

syslet needs an ed25519 key without a passphrase:

```sh
ssh web01.example.com 'sudo mkdir -p /etc/syslet && sudo ssh-keygen -t ed25519 -N "" -C syslet -f /etc/syslet/age_ed25519'
```

Back up `/etc/syslet/age_ed25519`; anyone with it can decrypt the host's secrets.

## 2. Point syslet at it

Create `/etc/syslet/syslet.json` on the host:

```json title="/etc/syslet/syslet.json"
{
  "sshKeyPath": "/etc/syslet/age_ed25519"
}
```

Run a plan once (`cue cmd plan` or `syslet --diff`).
syslet derives the new age key and appends it to `/var/lib/syslet/key.txt`, which still holds the key derived from the host key, so existing secrets keep decrypting.

If the file at `sshKeyPath` doesn't exist, syslet exits before planning:

```text
error: SSH key /etc/syslet/age_ed25519 (sshKeyPath in /etc/syslet/syslet.json) does not exist
```

## 3. Re-encrypt for the new key

Convert the public key:

```sh
ssh web01.example.com cat /etc/syslet/age_ed25519.pub | ssh-to-age
```

Replace the host's key in `.sops.yaml` with it, then re-encrypt and apply:

```sh
sops updatekeys -y creds-*.enc.yaml
cue cmd apply
```

The new ciphertext updates the podman secrets and restarts every container that references them.
Afterwards, drop the host-derived key from the cache as in [Rotate the SSH host key](rotate-the-ssh-host-key.md#5-drop-the-old-key).

## Restore after a reinstall

Copy the backed-up key to `/etc/syslet/age_ed25519` with mode `0600`, and create `syslet.json` again.
The next apply decrypts the secrets without re-encrypting anything.
