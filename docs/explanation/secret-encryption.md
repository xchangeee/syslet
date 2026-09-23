# Secret encryption

Containers need passwords and tokens, and a GitOps repository is the wrong place for them in plain text.
syslet keeps them [SOPS](https://getsops.io/)-encrypted in the repository and decrypts them only on the host that uses them.

## Who can decrypt

Each secret file is encrypted to several [age](https://age-encryption.org/) recipients.
Typically that is an admin key, so people can edit the file, and one key per host that deploys it, so syslet can decrypt it there.

A host's key isn't a separate secret to distribute: syslet derives it from the host's SSH host key with [ssh-to-age](https://github.com/Mic92/ssh-to-age), and you derive the matching public key from the host's public SSH key.
A host can only decrypt what was encrypted for it, and a host key that is already backed up and managed covers secrets too.
The tradeoff is that rotating the host key means re-encrypting; a [dedicated decryption key](../how-to/use-a-dedicated-decryption-key.md) decouples the two.

## Where decryption happens

The spec carries the ciphertext, not the plaintext.
In a CUE repository, `@embed` reads the encrypted files as text, so `cue vet` and `cue export` need no key and can run in CI or for anyone without access to the secrets.
syslet decrypts during the plan, on the host, and writes the values to podman's secret store, where containers reference them by name.

Because SOPS leaves key names readable, syslet checks every `Secret=` reference before decrypting anything.
See [Spec types](spec-types.md#secret) for how syslet detects changes without storing plaintext.

## Where the protection ends

Podman's secret store is root-owned but unencrypted on disk.
That's a [limitation of podman](https://github.com/podman-container-tools/podman/discussions/26762) that syslet can't work around.

The plan hides secret values as `(secret)`, so its output can go to shared logs. Set `SYSLET_SHOW_SECRETS=1` to print them when you inspect a plan locally.
