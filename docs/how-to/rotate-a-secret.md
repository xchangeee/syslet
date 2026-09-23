# Rotate a secret

A secret spec carries a whole SOPS file, and syslet detects changes to it by a hash of that file's ciphertext.
Any change to the file updates all of its podman secrets and restarts every container that references one of them.

## Change a value

Edit the file; sops decrypts it into your editor and re-encrypts it on save:

```sh
sops edit creds-webapp.enc.yaml
```

Preview the change:

=== "CUE"

    ```sh
    cue cmd plan
    ```

    CUE loads the file from the repository as it is.

=== "JSON"

    Regenerate the secret spec from the file first:

    ```sh
    jq -Rs '{apiVersion: "v1", type: "secret", name: "webapp", ciphertext: .}' creds-webapp.enc.yaml > hosts/web01/webapp-secret.json
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    ```

The plan lists every key of the file with its new value in plain text, so keep its output out of shared logs.
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

Delete the file, or with plain JSON the secret spec, together with every `Secret=` that references its keys.
syslet deletes the podman secrets on the next apply.
It only deletes podman secrets it created itself, recognized by their `syslet/hash` label.
