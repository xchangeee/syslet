# Upgrade a container image

syslet only restarts a container when its spec changes.
Pin images to a version tag and bump it to upgrade; a moving tag like `latest` is never re-pulled on its own.

## Bump the tag

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Image: "docker.io/library/nginx:1.28"
    }
    ```

=== "JSON"

    ```json
    "Image": "docker.io/library/nginx:1.28"
    ```

Preview it:

=== "CUE"

    ```sh
    cue cmd plan
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --diff --stdin
    ```

The plan shows the image to pull, the changed `Image` and a restart:

```text
Images to pull:
  - docker.io/library/nginx:1.28

Unit file changes:

--- webapp.container
changed:
  [Container] Image
    old: docker.io/library/nginx:1.27
    new: docker.io/library/nginx:1.28

...

webapp.container                         updated    unit updated, image pulled, restarted (desired: running)
```

syslet pulls an image that isn't on the host yet before it stops anything, so the download doesn't count as downtime.
An image that is already on the host is not pulled again.
If the pull fails, the apply stops before changing anything, and the old container keeps running.

## Apply

=== "CUE"

    ```sh
    cue cmd apply
    ```

=== "JSON"

    ```sh
    cat hosts/web01/*.json | ssh web01 sudo syslet --stdin
    ```

To roll back, set the old tag again and apply; the old image is still on the host.
Remove images you no longer need with `podman image prune`.

## Related tasks

### Upgrade a container on a moving tag

To upgrade a container that uses a moving tag, pull the tag again and restart the service by hand:

```sh
ssh web01 'sudo podman pull docker.io/library/nginx:latest && sudo systemctl restart webapp.service'
```

The spec doesn't change, so the plan shows nothing, and you can't tell from the repository which image runs.
Pinning a digest, `nginx:1.28@sha256:...`, makes every upgrade a visible change instead.
