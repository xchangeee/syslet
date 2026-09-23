# Set up SOPS encryption

In this tutorial, you'll set up [SOPS](https://getsops.io/) encryption for the repository from [Protect a container](05-protect-a-container.md), so secrets can live in it encrypted to your key and to `web01`'s.
Then you'll wire secret files into the CUE config.
The next tutorial adds the first secret.

See [Secret encryption](../explanation/secret-encryption.md) for how the keys fit together.

## Prerequisites

- The `infra` repository and `web01.example.com` from [Protect a container](05-protect-a-container.md).
- [sops](https://getsops.io/docs/#download), [age](https://github.com/FiloSottile/age#installation) and [ssh-to-age](https://github.com/Mic92/ssh-to-age) on your workstation.
- An ed25519 SSH host key on the server at `/etc/ssh/ssh_host_ed25519_key`, which most distributions create on install.

## 1. Create your admin key

Generate an age key where sops looks for it by default:

```sh
mkdir -p ~/.config/sops/age
age-keygen -o ~/.config/sops/age/keys.txt
```

On macOS, sops looks in `~/Library/Application Support/sops/age/keys.txt` instead; either use that path or point `SOPS_AGE_KEY_FILE` at the file.
If you already have a key, skip this step.

`age-keygen` prints the public key, which starts with `age1`.
Print it again at any time with:

```sh
age-keygen -y ~/.config/sops/age/keys.txt
```

Keep the private key out of the repository and back it up; without it, you can't edit your secrets anymore.

## 2. Get the server's public key

Convert the server's public SSH host key to an age recipient:

```sh
ssh web01.example.com cat /etc/ssh/ssh_host_ed25519_key.pub | ssh-to-age
```

This also prints a public key starting with `age1`.
syslet performs the same conversion on the private half, so only `web01` can decrypt what you encrypt to this key.

## 3. Configure sops

Create `.sops.yaml` in the repository root, with the two public keys from above:

```yaml title=".sops.yaml"
--8<-- "06-setup-sops-encryption/.sops.yaml"
```

sops applies this rule to every file ending in `.enc.yaml`, so you never pass recipients on the command line.
When you add a server or an admin, add its key here and run `sops updatekeys` on each existing file.

## 4. Load secret files in CUE

To create secrets on the host, syslet needs a secret spec for each encrypted file.
From it, syslet creates one podman secret per key, and containers refer to those podman secrets by name.

CUE can't read files by default.
Allow it by adding the `@extern(embed)` attribute above the `package` line of `syslet.cue`, then add the `secretFiles` and `sysdef: secrets` lines before `syslet: specRendered`:

```cue title="syslet.cue" hl_lines="1 20-23"
--8<-- "06-setup-sops-encryption/syslet.cue"
```

`secretFiles` reads every `creds-<name>.enc.yaml` next to `syslet.cue`, and `sysdef: secrets` turns each file into a secret spec called `<name>`.

`@embed` reads the files as text, still encrypted.
CUE needs no age key for this, so `cue vet` and `cue export` also work in CI or for anyone without access to the secrets.
`allowEmptyGlob` keeps the config valid when there are no secret files.
`#SysdefSecretsFromEmbeddedFiles` strips the `creds-` prefix and `.enc.yaml` suffix to get the spec name, so `creds-site.enc.yaml` becomes the secret spec `site`.

There are no secret files yet, so nothing changes on the host:

```sh
cue cmd plan
```

Commit the setup:

```sh
git add -A && git commit -m "set up sops"
```

Continue with [Add nginx basic auth](07-add-nginx-basic-auth.md).
