# Set environment variables

Set a container's environment in its `[Container]` section with `Environment`.
`[Service] Environment=` would only reach the podman process that starts the container, not the container itself.

## Use the map form

=== "CUE"

    ```cue
    sysdef: containers: app: spec: {
    	unit: Container: {
    		Image: "registry.example.com/app:1.4"
    		Environment: {
    			TZ:       "Europe/Vienna"
    			GREETING: "hello world"
    		}
    	}
    }
    ```

=== "JSON"

    ```json
    "unit": {
      "Container": {
        "Image": "registry.example.com/app:1.4",
        "Environment": {
          "TZ": "Europe/Vienna",
          "GREETING": "hello world"
        }
      }
    }
    ```

syslet writes one `Environment=` line per variable and quotes values that contain whitespace:

```ini
Environment="GREETING=hello world"
Environment=TZ=Europe/Vienna
```

An empty string sets the variable to an empty value.

A list of `NAME=value` strings works as well, `["TZ=Europe/Vienna"]`, with the same quoting.
In CUE, prefer the map: maps from several places merge, so a shared file can set defaults and a host file can add variables; two different values for the same variable are a conflict CUE reports.

## Apply a change

Any change to `Environment` is a change to the unit, so syslet restarts the container.
To change settings without a restart, put them in a config file the service reloads (see [Mount config files and dirs](mount-config-files-and-dirs.md#reload-config-without-a-restart)).

## Keep secrets out

`Environment` values are written to the unit file and shown in every plan.
Pass passwords and tokens as secrets instead (see [Pass a secret to a container](pass-a-secret-to-a-container.md#pass-it-as-an-environment-variable)).
