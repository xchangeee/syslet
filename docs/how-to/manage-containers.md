# Manage containers

This guide covers adding, changing, stopping, protecting and removing a container spec, and what each change does to the running container.
Preview every change first, with `cue cmd plan` or `syslet --diff` (see [Preview changes with --diff](preview-changes-with-diff.md)); a plan with any error is refused as a whole, so nothing is applied halfway.

## Add a container

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: {
    		Image: "docker.io/library/nginx:1.27"
    		Volume: ["webapp-data.volume:/data"]
    		Network: ["webapp-net.network"]
    	}
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "container",
      "name": "webapp",
      "desiredState": "running",
      "unit": {
        "Container": {
          "Image": "docker.io/library/nginx:1.27",
          "Volume": ["webapp-data.volume:/data"],
          "Network": ["webapp-net.network"]
        }
      }
    }
    ```

- `#SysdefDefaults` sets `desiredState: "running"`. In plain JSON an omitted `desiredState` means `"stopped"`, so set it explicitly.
  <!-- TODO: drop the JSON note once the Go default is running, see docs/TODO.md -->
- Every `<name>.volume`, `<name>.network` and `<name>.build` the container references must be a spec in the same input, and every `Secret=` must name a declared secret key. Otherwise validation fails before anything is touched.
- Deploy the container together with the volumes, networks, builds and secrets it uses. Their services start as dependencies of the container.

## Change a container

Edit the spec, preview, apply:

=== "CUE"

    ```sh
    cue cmd plan
    cue cmd apply
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    cat hosts/web01/*.json | ssh web01 sudo syslet --stdin
    ```

Check `Services to stop:` and the `CHANGES` column of the summary before applying: `restarted` costs downtime, `reloaded` does not.
A change to a volume, network, build or secret the container uses restarts it too; see [Service actions](../reference/spec-schema/container.md#service-actions) for every trigger.
If a change should not restart the service, move the file into a `configDirs` entry (see [Reload config without a restart](mount-config-files-and-dirs.md#reload-config-without-a-restart)).

Any apply also starts a container with `desiredState: "running"` that crashed or was stopped by hand, even with no spec changes.

A container's name is its identity. Renaming one removes the old unit and creates a new one, so the old name needs `removalAllowed` like any other [removal](#remove-a-container).

## Stop a container without removing it

Set `desiredState: "stopped"` and apply.
The unit file stays, syslet stops the container, and without `[Install] WantedBy=` it no longer starts at boot.
Set it back to `"running"` to start it again.

## Protect a container

Lock the container and apply, as in [Protect a container](../tutorials/05-protect-a-container.md):

=== "CUE"

    ```cue
    sysdef: (syslettools.#SysdefLock & {in: containers: ["site"]}).out
    ```

=== "JSON"

    ```json
    "removalAllowed": false
    ```

    This is the JSON default.

    <!-- TODO: drop once the Go default for removalAllowed is true, see docs/TODO.md -->

The marker is written to the installed unit as `[X-Syslet] RemovalAllowed=false`, a metadata-only change, so the container keeps running.
From then on, dropping the spec from the input leaves the container running and reports it as skipped:

```text
site.container                           skipped    not marked for removal (removalAllowed not set)
```

## Remove a container

1. Make sure the installed unit allows removal. If it was protected, drop it from `#SysdefLock` (in JSON, set `"removalAllowed": true`), keep the spec, and apply once.
2. Drop the spec from the input, preview and apply. The summary must read `removed`, not `skipped`.

syslet stops the container, deletes its unit file and deletes `/etc/containers/config/<name>/` with all `configFiles` and `configDirs`.
Step 1 cannot be combined with step 2, because the marker is read from the installed unit (see [Removing specs](../explanation/removing-specs.md#fail-safe-protections)).

Removing a container never touches what it references. Its volumes, networks, builds and secrets stay until you remove their specs too:

- To remove one of them together with the container, drop both specs in the same change and check that the plan shows both as `removed`. If the container shows `skipped`, don't apply; unlock the container first.
- To remove only a dependency, first drop the reference from every container that uses it. A dependency still referenced by a container in the input fails validation:

    ```text
    container "webapp": references undefined volume "webapp-data"
    ```

If the plan fails validation, see [Troubleshoot a failed apply](troubleshoot-a-failed-apply.md), and [Validation](../reference/spec-schema/container.md#validation) for the container checks.
