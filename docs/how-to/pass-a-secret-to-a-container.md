# Pass a secret to a container

A secret spec turns each key of a SOPS-encrypted file into one podman secret named `<spec name>-<key>` (see [Set up SOPS encryption](../tutorials/06-setup-sops-encryption.md)).
A container gets it through its `Secret=` option, either as a file or as an environment variable.
The examples below use a secret spec `webapp` with the key `db-password`.

## Mount it as a file

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Secret: ["webapp-db-password"]
    }
    ```

=== "JSON"

    ```json
    "Secret": ["webapp-db-password"]
    ```

Podman mounts the secret at `/run/secrets/webapp-db-password`.
Options after the name change the path and permissions:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Secret: ["webapp-db-password,target=/etc/app/db-password,uid=1000,gid=1000,mode=0400"]
    }
    ```

=== "JSON"

    ```json
    "Secret": ["webapp-db-password,target=/etc/app/db-password,uid=1000,gid=1000,mode=0400"]
    ```

A relative `target` is placed under `/run/secrets/`.

## Pass it as an environment variable

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Secret: ["webapp-db-password,type=env,target=DB_PASSWORD"]
    }
    ```

=== "JSON"

    ```json
    "Secret": ["webapp-db-password,type=env,target=DB_PASSWORD"]
    ```

Prefer the file when the application can read one, for example through a `*_FILE` variable: environment variables are inherited by child processes and tend to end up in logs and crash reports.

Never put the value into `Environment=` instead.
It would be written to the unit file and printed in every plan in plain text.

## Use the map form

`Secret` also takes a map from the podman secret name to its options:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Secret: {
    		"webapp-db-password": "type=env,target=DB_PASSWORD"
    		"webapp-api-key":     ""
    	}
    }
    ```

=== "JSON"

    ```json
    "Secret": {
      "webapp-db-password": "type=env,target=DB_PASSWORD",
      "webapp-api-key": ""
    }
    ```

An empty string mounts the secret with the defaults.
In CUE, maps from several files merge, so a shared file and a host file can each add secrets.

## What syslet checks

Every `Secret=` must name a key of a secret spec in the same input.
Otherwise the plan fails before anything is decrypted:

```text
container "webapp": Secret=webapp-db-pasword: key "db-pasword" not found in secret "webapp"
```

Podman copies secrets into a container when it's created, so a container picks up a changed value only on a restart.
syslet restarts every container that references a changed secret in the same apply (see [Rotate a secret](rotate-a-secret.md)).
Changing a `Secret=` option restarts the container as well, like any other change to its unit.
