# Add ingress with Caddy

In Kubernetes, you declare Ingress objects and an ingress controller watches them and configures a reverse proxy.
syslet has no control plane to run such a controller, but CUE can do the same work when it renders the specs:

- The ingress addon adds an `ingress` map to a CUE repository's sysdef, with one entry per hostname, like Ingress objects.
- CUE renders a reverse proxy config from these entries, and syslet reloads the proxy when that config changes.

This guide sets up Caddy as that reverse proxy, routing each hostname to its container.
The examples route `web.example.com` to the container `site`.

## 1. Enable the ingress addon

Add `#SysdefIngress` and its defaults to `#Sysdef`:

```cue title="syslet.cue" hl_lines="4 14-15"
package syslet

import (
	sysletaddons "github.com/xchangeee/syslet/schema/addons@v0"
	sysletcore "github.com/xchangeee/syslet/schema/core@v0"
	syslettools "github.com/xchangeee/syslet/schema/tools@v0"
)

fqdn: "web01.example.com"

#Sysdef: {
	sysletcore.#Sysdef
	sysletcore.#SysdefDefaults
	sysletaddons.#SysdefIngress
	sysletaddons.#SysdefIngressDefaults
}

sysdef: #Sysdef

syslet: specRendered: (syslettools.#SysletJsonFromSysdef & {in: sysdef}).specRendered
```

## 2. Deploy Caddy

```cue title="caddy.cue"
package syslet

import (
	"text/template"

	syslettools "github.com/xchangeee/syslet/schema/tools@v0"
)

_caddyVersion: "2.11.2"

_caddyfile: """
	{
		email admin@example.com
	}

	localhost:80 {
		respond /healthz 200
	}

	{{ range $name, $ingress := .ingresses -}}
	{{ if $ingress.enabled -}}
	{{ $ingress.host }}{{ if not $ingress.tls }}:80{{ end }} {
	{{- if $ingress.backends }}
		{{ $ingress.backends }}
	{{- else }}
		reverse_proxy http://{{ $name }}:{{ $ingress.containerPort }}{{ if $ingress.reverseProxyConfig }} {
			{{ $ingress.reverseProxyConfig }}
		}{{ end }}
	{{- end }}
	{{- if $ingress.extraConfig }}
		{{ $ingress.extraConfig }}
	{{- end }}
	}

	{{ end -}}
	{{ end -}}
	"""

sysdef: {
	networks: caddy: spec: {
		unit: Network: {}
	}

	volumes: "caddy-data": spec: {
		unit: Volume: {}
	}

	volumes: "caddy-config": spec: {
		unit: Volume: {}
	}

	containers: caddy: spec: {
		unit: {
			Container: {
				Image: "docker.io/library/caddy:\(_caddyVersion)"
				Network: ["caddy.network"]
				PublishPort: ["80:80", "443:443", "443:443/udp"]
				Volume: {
					"caddy-data.volume":   "/data"
					"caddy-config.volume": "/config"
				}
				HealthCmd:         "wget -qO- http://localhost/healthz || exit 1"
				HealthStartPeriod: "5s"
				HealthInterval:    "30s"
				HealthTimeout:     "5s"
				HealthRetries:     "3"
			}
			Service: ExecReload: "podman exec caddy /usr/bin/caddy reload --config /etc/caddy/Caddyfile --force"
		}
		configDirs: [{
			mountPath: "/etc/caddy"
			files: [{
				name: "Caddyfile"
				content: template.Execute(_caddyfile, {ingresses: sysdef.ingress})
			}]
		}]
	}

	(syslettools.#SysdefLock & {in: {
		containers: ["caddy"]
		volumes: ["caddy-data", "caddy-config"]
	}}).out
}
```

- `template.Execute` renders `_caddyfile`, a Go [text/template](https://pkg.go.dev/text/template), with the ingress entries into a site block per enabled entry. The leading `_` makes `_caddyfile` and `_caddyVersion` hidden fields, so `cue export` leaves them out. A host with `tls: true` gets a certificate from Let's Encrypt; `tls: false` serves plain HTTP on port 80.
- The Caddyfile is mounted with `configDirs`, so a changed entry reloads Caddy through `ExecReload=` instead of restarting it (see [Mount config files and dirs](mount-config-files-and-dirs.md#reload-config-without-a-restart)).
- The `localhost:80` site answers `/healthz`, which the container's health check requests.
- The lock keeps the container and both volumes when the Caddy entries are removed, so the hostnames stay reachable and Caddy doesn't request all certificates again; `caddy-data` holds the certificates and their keys.

## 3. Route a hostname to a container

Put the container on the `caddy` network and add an ingress entry with the same name:

```cue title="site.cue"
package syslet

sysdef: {
	containers: site: spec: {
		unit: Container: {
			Image: "docker.io/library/nginx:1.27"
			Network: ["caddy.network"]
		}
	}

	ingress: site: {
		host:          "web.example.com"
		containerPort: 80
	}
}
```

Caddy reaches the container by its name, so the entry's name must be the container's name.
The entry's fields:

- `host`: the hostname Caddy serves. Its DNS record must point to the host.
- `containerPort`: the port the container listens on.
- `tls`: defaults to `true`.
- `enabled`: defaults to `true`; `false` drops the site block.
- `reverseProxyConfig`: lines added to the `reverse_proxy` block, e.g. `header_up X-Real-IP {remote_host}`.
- `extraConfig`: lines added to the site block, e.g. `encode gzip`.
- `backends`: replaces the `reverse_proxy` line, e.g. to proxy to several containers.

Preview and apply:

```sh
cue cmd plan
cue cmd apply
```

A new or changed entry only changes the Caddyfile, so Caddy reloads and the containers keep running.

## 4. Get certificates with a DNS challenge

By default Caddy proves it owns a hostname over port 80, which fails when the host isn't reachable from the internet.
With a DNS challenge Caddy creates a DNS record instead.
This example uses [deSEC](https://desec.io).

Prerequisites:

- A CUE repository with SOPS encryption and secret files loaded, see [Set up SOPS in a CUE repository](set-up-sops-in-a-cue-repository.md).
- A deSEC token with permission to manage the domain's records.

### Store the token

Create `creds-caddy.enc.yaml` next to your CUE files:

```yaml title="creds-caddy.enc.yaml"
desectoken: <deSEC token>
```

Encrypt it in place:

```sh
sops -e -i creds-caddy.enc.yaml
```

The file becomes the secret spec `caddy`, and its key the podman secret `caddy-desectoken` (see [Load every file by name](set-up-sops-in-a-cue-repository.md#load-every-file-by-name)).

### Build Caddy with the deSEC module

The official Caddy image contains no DNS providers, so build one with the deSEC module and pass it the token:

```cue title="caddy.cue" hl_lines="23-31 61-74 78 90-92"
package syslet

import (
	"text/template"

	syslettools "github.com/xchangeee/syslet/schema/tools@v0"
)

_caddyVersion: "2.11.2"

_caddyfile: """
	{
		email admin@example.com
	}

	localhost:80 {
		respond /healthz 200
	}

	{{ range $name, $ingress := .ingresses -}}
	{{ if $ingress.enabled -}}
	{{ $ingress.host }}{{ if not $ingress.tls }}:80{{ end }} {
	{{- if $ingress.tls }}
		tls {
			dns desec {
				token "{$DESEC_TOKEN}"
			}
			resolvers 1.1.1.1 8.8.8.8
			propagation_delay 120s
		}
	{{- end }}
	{{- if $ingress.backends }}
		{{ $ingress.backends }}
	{{- else }}
		reverse_proxy http://{{ $name }}:{{ $ingress.containerPort }}{{ if $ingress.reverseProxyConfig }} {
			{{ $ingress.reverseProxyConfig }}
		}{{ end }}
	{{- end }}
	{{- if $ingress.extraConfig }}
		{{ $ingress.extraConfig }}
	{{- end }}
	}

	{{ end -}}
	{{ end -}}
	"""

sysdef: {
	networks: caddy: spec: {
		unit: Network: {}
	}

	volumes: "caddy-data": spec: {
		unit: Volume: {}
	}

	volumes: "caddy-config": spec: {
		unit: Volume: {}
	}

	builds: "caddy-desec": spec: {
		unit: Build: {
			ImageTag: "localhost/caddy-desec:\(_caddyVersion)"
			BuildArg: "CADDY_VERSION=\(_caddyVersion)"
		}
		containerfile: """
			ARG CADDY_VERSION
			FROM docker.io/library/caddy:${CADDY_VERSION}-builder AS builder
			RUN xcaddy build --with github.com/caddy-dns/desec
			FROM docker.io/library/caddy:${CADDY_VERSION}
			COPY --from=builder /usr/bin/caddy /usr/bin/caddy
			"""
	}

	containers: caddy: spec: {
		unit: {
			Container: {
				Image: "caddy-desec.build"
				Network: ["caddy.network"]
				PublishPort: ["80:80", "443:443", "443:443/udp"]
				Volume: {
					"caddy-data.volume":   "/data"
					"caddy-config.volume": "/config"
				}
				HealthCmd:         "wget -qO- http://localhost/healthz || exit 1"
				HealthStartPeriod: "5s"
				HealthInterval:    "30s"
				HealthTimeout:     "5s"
				HealthRetries:     "3"
				Secret: {
					"caddy-desectoken": "type=env,target=DESEC_TOKEN"
				}
			}
			Service: ExecReload: "podman exec caddy /usr/bin/caddy reload --config /etc/caddy/Caddyfile --force"
		}
		configDirs: [{
			mountPath: "/etc/caddy"
			files: [{
				name: "Caddyfile"
				content: template.Execute(_caddyfile, {ingresses: sysdef.ingress})
			}]
		}]
	}

	(syslettools.#SysdefLock & {in: {
		containers: ["caddy"]
		volumes: ["caddy-data", "caddy-config"]
	}}).out
}
```

- The build adds the `caddy-dns/desec` module with `xcaddy`, and the container runs the built image (see [Build a container image](build-a-container-image.md)).
- The secret `caddy-desectoken` is passed as the environment variable `DESEC_TOKEN`, which the Caddyfile reads with `{$DESEC_TOKEN}`.
- deSEC takes a while to publish new records, so `propagation_delay` waits 2 minutes before Caddy checks the record against the public `resolvers`.
