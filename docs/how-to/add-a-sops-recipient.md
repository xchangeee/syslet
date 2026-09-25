# Add a SOPS recipient

Every secret file is encrypted to a list of age recipients, and only those can decrypt it.
Add a recipient when a second host deploys the same secrets, or when another admin needs to edit them.

## 1. Get the recipient's public key

For a host, convert its public SSH host key:

```sh
ssh web02.example.com cat /etc/ssh/ssh_host_ed25519_key.pub | ssh-to-age
```

For an admin, they print it from their own key file:

```sh
age-keygen -y ~/.config/sops/age/keys.txt
```

## 2. Add it to `.sops.yaml`

Add the key and list it in every creation rule whose files it should decrypt:

```yaml title=".sops.yaml" hl_lines="4 11"
keys:
  - &admin age1...   # your admin key
  - &web01 age1...   # web01's host key
  - &web02 age1...   # web02's host key
creation_rules:
  - path_regex: .*\.enc\.yaml$
    key_groups:
      - age:
          - *admin
          - *web01
          - *web02
```

New files pick up the rule when you create them.
To give each host only its own secrets, use one rule per host directory instead (see [Manage several hosts](manage-several-hosts.md#encrypt-secrets-per-host)).

## 3. Re-encrypt the existing files

```sh
sops updatekeys -y creds-*.enc.yaml
```

This changes the files, although the values stay the same.
syslet sees new ciphertext, so the next apply on each host that uses the files updates their podman secrets and restarts every container that references them.
Commit and apply when a restart suits you.

## Remove a recipient

Delete the key from `.sops.yaml` and run `sops updatekeys` again.
The removed key can still decrypt every older version of the files in git history, so change the values too (see [Rotate a secret](rotate-a-secret.md)).
