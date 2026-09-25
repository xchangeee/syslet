# Rotate the SSH host key

syslet decrypts secrets with an age key derived from the host's SSH key.
On every run it appends the derived key to `/var/lib/syslet/key.txt` and never removes one, so secrets encrypted to the old key keep working while you re-encrypt them for the new one.

## Check that the old key is cached

```sh
ssh web01.example.com sudo cat /var/lib/syslet/key.txt
```

The file holds one private key per line; any earlier syslet run, including `--diff`, cached the current one.
If the file is missing, run a plan (`cue cmd plan` or `syslet --diff`) once before you touch the host key.

## Replace the host key

For example, on most distributions:

```sh
ssh web01.example.com 'sudo rm /etc/ssh/ssh_host_ed25519_key* && sudo ssh-keygen -A'
```

Restart the SSH daemon, then remove the old entry from your `known_hosts` with `ssh-keygen -R web01.example.com`.

## Cache the new key

Run a plan again.
syslet derives the new key and appends it, and still decrypts every secret with the old one, so the plan shows no secret changes.

## Re-encrypt for the new key

Convert the new public key:

```sh
ssh web01.example.com cat /etc/ssh/ssh_host_ed25519_key.pub | ssh-to-age
```

Replace the host's old key in `.sops.yaml` with it, then re-encrypt and apply:

```sh
sops updatekeys -y creds-*.enc.yaml
cue cmd apply
```

The new ciphertext updates the podman secrets and restarts every container that references them.

## Drop the old key

Once every file is re-encrypted, keep only the newest key, which is the last line:

```sh
ssh web01.example.com 'sudo sed -i "\$!d" /var/lib/syslet/key.txt'
```
