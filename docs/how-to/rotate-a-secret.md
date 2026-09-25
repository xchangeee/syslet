# Rotate a secret

A secret spec carries a whole SOPS file, and syslet detects changes to it by a hash of that file's ciphertext.
Any change to the file updates all of its podman secrets and restarts every container that references one of them.

## Change a value

Edit the file; sops decrypts it into your editor and re-encrypts it on save:

```sh
sops edit creds-webapp.enc.yaml
```

Preview the change:

```sh
cue cmd plan
```

The plan lists every key of the file, with values hidden; set `SYSLET_SHOW_SECRETS=1` to see them.
Every container that references one of the keys restarts:

```text
Secret changes:
  webapp:
    + api-key=...
    + db-password=...

...

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    secret updated, restarted (desired: running)
```

Then apply it.

Because the whole file counts as one change, give each container its own secret file when you don't want a rotation for one to restart the others.

## Add, rename or remove a key

A new key creates a new podman secret, and a removed key deletes one.
Renaming a key does both.

Update the `Secret=` references in the same change: a reference to a key that no longer exists fails the plan (see [Pass a secret to a container](pass-a-secret-to-a-container.md#what-syslet-checks)).

## Remove a secret file

Delete the file together with every `Secret=` that references its keys.
syslet deletes the podman secrets on the next apply.
It only deletes podman secrets it created itself, recognized by their `syslet/hash` label.
