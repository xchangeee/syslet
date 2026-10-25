# Rotate a secret

A secret spec carries a whole SOPS file, and each key of it becomes its own podman secret.
syslet compares each key's decrypted value with the one podman holds, so only keys whose value changed are updated, and only containers that reference one of those keys restart.

## Change a value

Edit the file; sops decrypts it into your editor and re-encrypts it on save:

```sh
sops edit creds-webapp.enc.yaml
```

Preview the change:

```sh
cue cmd plan
```

The plan lists the keys whose value changed, with values hidden; set `SYSLET_SHOW_SECRETS=1` to see them.
Every container that references one of those keys restarts:

```text
Secret changes:
  webapp:
    + db-password=...

...

Summary:
UNIT                                     STATUS     CHANGES
webapp.container                         updated    secret updated, restarted (desired: running)
```

Then apply it.

## Add, rename or remove a key

A new key creates a new podman secret, and a removed key deletes one.
Renaming a key does both.

Update the `Secret=` references in the same change: a reference to a key that no longer exists fails the plan (see [Pass a secret to a container](pass-a-secret-to-a-container.md#what-syslet-checks)).

## Remove a secret file

Delete the file together with every `Secret=` that references its keys.
syslet deletes the podman secrets on the next apply.
It deletes every podman secret that isn't in its input, including ones created by hand.
