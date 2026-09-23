# Build a container image

To run an upstream image with local changes, such as a baked-in config file or a patched package, build it on the host instead of pushing it through a registry.
The examples build `myapp` from nginx and run it as the container `webapp`.

## 1. Write a build spec

=== "CUE"

    ```cue
    sysdef: builds: myapp: spec: {
    	unit: Build: ImageTag: "localhost/myapp:latest"
    	containerfile: """
    		FROM docker.io/library/nginx:latest
    		COPY app.conf /etc/nginx/conf.d/app.conf
    		"""
    	contextFiles: [{
    		filename: "app.conf"
    		content:  "server_name example.internal;"
    	}]
    }
    ```

=== "JSON"

    ```json
    {
      "apiVersion": "v1",
      "type": "build",
      "name": "myapp",
      "unit": {
        "Build": {
          "ImageTag": "localhost/myapp:latest"
        }
      },
      "containerfile": "FROM docker.io/library/nginx:latest\nCOPY app.conf /etc/nginx/conf.d/app.conf\n",
      "contextFiles": [
        {
          "filename": "app.conf",
          "content": "server_name example.internal;"
        }
      ]
    }
    ```

- `ImageTag` must start with `localhost/<build name>:`, here `localhost/myapp:`.
- `containerfile` holds the Containerfile's content, not a path.
- `contextFiles` are the files the Containerfile can `COPY` or `ADD`. To mount files into the running container instead, use `configFiles` (see [Mount config files and dirs](mount-config-files-and-dirs.md)).

## 2. Reference the build from a container

Set the container's `Image` to `<build name>.build`:

=== "CUE"

    ```cue
    sysdef: containers: webapp: spec: {
    	unit: Container: Image: "myapp.build"
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
          "Image": "myapp.build"
        }
      }
    }
    ```

## 3. Preview and apply

Deploy both specs in the same input; a container that references a missing build fails validation.

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

podman builds the image before it starts the container.
From then on, a change to `containerfile` or `contextFiles` rebuilds the image and restarts the container.
A newer base image behind the same `FROM` tag doesn't trigger a rebuild; syslet leaves image updates to other tools.

## Remove a build

Drop the build spec and every `Image` that references it, then preview and apply.
Builds can't be locked, so the build unit and its context are always removed.
The built image is deleted too, unless the build sets `reclaimPolicy: "Retain"`.
