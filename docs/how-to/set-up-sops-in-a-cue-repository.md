# Set up SOPS in a CUE repository

To keep secrets in a CUE repository, encrypt them with [SOPS](https://getsops.io/) to your own key and to each host's, and load the encrypted files into `sysdef: secrets`.
CUE reads the files still encrypted, so `cue vet` and `cue export` work without any age key; syslet decrypts them on the host.

Plain JSON specs can carry secrets too, but each secret spec holds the whole encrypted file as one string that you'd have to regenerate after every edit.
In CUE, the spec is always built from the file as it is in the repository.

For a guided walkthrough of the same setup, see the tutorial [Set up SOPS encryption](../tutorials/06-setup-sops-encryption.md).
For how the keys fit together, see [Secret encryption](../explanation/secret-encryption.md).

## Prerequisites

- A CUE repository as in [Set up CUE repository](../tutorials/03-setup-cue-repository.md).
- [sops](https://getsops.io/docs/#download), [age](https://github.com/FiloSottile/age#installation) and [ssh-to-age](https://github.com/Mic92/ssh-to-age) on your workstation.
- On each host, an ed25519 SSH host key at `/etc/ssh/ssh_host_ed25519_key`, or a [dedicated decryption key](use-a-dedicated-decryption-key.md).

## Get your admin key

Keep your admin key in a password manager, not in a plain-text file on disk: anyone who can read that file can decrypt every secret in the repository.

If you don't have an age key yet, generate one and print it to the terminal instead of a file:

```sh
age-keygen
```

Store the line starting with `AGE-SECRET-KEY-1` in your password manager.
The `# public key:` line above it is the public half you list in `.sops.yaml`.

Then tell sops (3.10 or later) how to fetch the key, by setting `SOPS_AGE_KEY_CMD` in your shell profile to a command that prints it.
For example, with the 1Password CLI:

```sh
export SOPS_AGE_KEY_CMD="op read op://Private/sops-age-key/password"
```

sops runs the command without a shell, so wrap pipes or redirects in `sh -c '...'`.
Any password manager CLI that prints the key on standard output works the same way.

To print the public key again, pipe the private key into `age-keygen -y`:

```sh
op read op://Private/sops-age-key/password | age-keygen -y
```

Your password manager is also the backup: without the key, you can't edit the secrets anymore.
If several people edit the secrets, each uses their own key and sends you the public half.

## Get each host's public key

Convert the host's public SSH host key to an age recipient:

```sh
ssh web01.example.com cat /etc/ssh/ssh_host_ed25519_key.pub | ssh-to-age
```

With a dedicated decryption key, convert `/etc/syslet/age_ed25519.pub` instead.
Repeat this for every host that deploys secrets.

## Configure sops

Create `.sops.yaml` in the repository root and list the public keys from above:

```yaml title=".sops.yaml"
--8<-- "06-setup-sops-encryption/.sops.yaml"
```

sops applies this rule to every file ending in `.enc.yaml` when it encrypts one, so you never pass recipients on the command line.
Keep the suffix: the CUE config below loads secret files by it.

If the repository describes several hosts, generate one rule per host directory instead, so each host only decrypts its own secrets (see [Manage several hosts](manage-several-hosts.md#describe-the-sops-rules)).

## Load the secret files in CUE

Choose how to turn files into secret specs:

- [Load every file by name](#load-every-file-by-name) when the files can sit next to the CUE file and their names can be the spec names.
  This is the common case: adding a secret means adding a file.
- [Load a single file](#load-a-single-file) when a file lives in another directory, or its spec needs a name of its own.

You can combine both in one repository.

### Load every file by name

`#SysdefSecretsFromEmbeddedFiles` turns each `creds-<name>.enc.yaml` next to the CUE file into the secret spec `<name>`.
Add `@extern(embed)` as the first line of `syslet.cue`, and the `secretFiles` and `sysdef: secrets` lines:

```cue title="syslet.cue" hl_lines="1 20-23"
--8<-- "06-setup-sops-encryption/syslet.cue"
```

`@extern(embed)` has to be the first line of every file that uses `@embed`.
`allowEmptyGlob` keeps the config valid while there are no secret files.

`#SysdefSecretsFromEmbeddedFiles` only handles files in the same directory as the CUE file that embeds them, to keep it simple.
For a file elsewhere, [load it as a single file](#load-a-single-file).

### Load a single file

Embed the file into the spec directly:

```cue
sysdef: secrets: db: spec: {
	ciphertext: _ @embed(file=secrets/db.enc.yaml,type=text)
}
```

The path is relative to the CUE file's directory and can't contain `..`.
The file that contains this line needs `@extern(embed)` as its first line, too.

## Add the first secret

Write the values in plain text first, with a name that matches your `.sops.yaml` rule and CUE glob:

```yaml title="creds-webapp.enc.yaml"
db-password: hunter2
```

The file must be a flat mapping of string values, and key names may only contain `a-z`, `0-9` and `-`, since each key becomes the podman secret `<spec name>-<key>`, here `webapp-db-password`.

Encrypt it in place before you commit it:

```sh
sops -e -i creds-webapp.enc.yaml
```

The `sops` block at the end of the file lists one `recipient` per key from `.sops.yaml`.
If a host is missing there, it can't decrypt the file: fix the rule, then run `sops updatekeys creds-webapp.enc.yaml`.

From now on, edit the file with `sops edit creds-webapp.enc.yaml`; to remove the secret, delete the file.

## Check the setup

Check that CUE picks up the file, without any key:

```sh
cue export -e sysdef.secrets
```

The output has an entry `webapp` whose `spec.ciphertext` is the encrypted file.

Then check that the host can decrypt it:

```sh
cue cmd plan
```

The plan decrypts the file on the host and lists its keys under `Secret changes`, with values hidden:

```text
Secret changes:
  webapp:
    + db-password=(secret)
```

If the host isn't a recipient of the file, the plan reports `secret "webapp": decryption failed` and syslet applies nothing.

To use the secret, reference it from a container as in [Pass a secret to a container](pass-a-secret-to-a-container.md).
