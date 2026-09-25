# Manage several hosts

One CUE repository can describe several hosts: shared files at the root, and one directory per host.
CUE merges a directory's files with the files of the same package in its parent directories, so each host directory sees the shared definitions.

This guide starts from a repository with SOPS set up as in [Set up SOPS in a CUE repository](set-up-sops-in-a-cue-repository.md).

## 1. Lay out the repository

```text
infra/
├── cue.mod/
├── .sops.yaml
├── syslet.cue          # schema, defaults and output, shared
├── syslet_tool.cue     # cue cmd plan / apply, shared
├── site.cue            # templates shared between hosts
└── hosts/
    ├── web01/
    │   ├── web01.cue
    │   └── creds-site.enc.yaml
    └── web02/
        └── web02.cue
```

Every `.cue` file uses `package syslet`.

## 2. Move the host settings out of `syslet.cue`

Declare `fqdn` without a value, and move the secret loading into the host directories, since `@embed` reads files relative to the file it's in:

```cue title="syslet.cue"
package syslet

import (
	sysletcore "github.com/xchangeee/syslet/schema/core@v0"
	syslettools "github.com/xchangeee/syslet/schema/tools@v0"
)

// Host that cue cmd plan/apply deploy to, set per host
fqdn: string

#Sysdef: {
	sysletcore.#Sysdef
	sysletcore.#SysdefDefaults
}

sysdef: #Sysdef

syslet: specRendered: (syslettools.#SysletJsonFromSysdef & {in: sysdef}).specRendered
```

## 3. Share templates

Put what several hosts deploy into a hidden field at the root.
Defaults (`*`) let a host override single values:

```cue title="site.cue"
package syslet

// nginx site shared by all web hosts
_site: spec: {
	unit: Container: {
		Image: string | *"docker.io/library/nginx:1.27"
		PublishPort: [...string] | *["8080:80"]
	}
}
```

## 4. Describe each host

```cue title="hosts/web01/web01.cue"
@extern(embed)

package syslet

import syslettools "github.com/xchangeee/syslet/schema/tools@v0"

fqdn: "web01.example.com"

sysdef: containers: site: _site

secretFiles: _ @embed(glob=creds-*.enc.yaml,type=text,allowEmptyGlob)

sysdef: secrets: (syslettools.#SysdefSecretsFromEmbeddedFiles & {in: secretFiles}).secrets
```

```cue title="hosts/web02/web02.cue"
package syslet

fqdn: "web02.example.com"

sysdef: containers: site: _site
sysdef: containers: site: spec: {
	unit: Container: Image: "docker.io/library/nginx:1.28"
}
```

## 5. Plan and apply per host

Pass the host directory to every command:

```sh
cue cmd plan ./hosts/web01
cue cmd apply ./hosts/web01
cue vet -c ./hosts/...
```

Without a host directory, `cue cmd plan` fails because `fqdn` has no value, so it can't deploy to the wrong host by accident.

With GitOps, export the host's directory in the deploy script:

```sh
cue export ./hosts/web01 -e syslet.specRendered --out text > "$SPEC_FILE"
```

## Encrypt secrets per host

Give each host directory its own creation rule, so a host only decrypts its own secrets:

```yaml title=".sops.yaml"
keys:
  - &admin age1...   # your admin key
  - &web01 age1...   # web01's host key
  - &web02 age1...   # web02's host key
creation_rules:
  - path_regex: ^hosts/web01/.*\.enc\.yaml$
    key_groups:
      - age: [*admin, *web01]
  - path_regex: ^hosts/web02/.*\.enc\.yaml$
    key_groups:
      - age: [*admin, *web02]
```

After changing the rules, re-encrypt existing files with `sops updatekeys` (see [Add a SOPS recipient](add-a-sops-recipient.md)).
