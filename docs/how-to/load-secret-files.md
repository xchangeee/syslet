# Load secret files

In a CUE repository, secret specs come from SOPS-encrypted files that CUE reads with `@embed`.
CUE reads them still encrypted, so `cue vet` and `cue export` work without any age key.

## Load every file by name

`#SysdefSecretsFromEmbeddedFiles` turns each `creds-<name>.enc.yaml` next to the CUE file into a secret spec `<name>`:

```cue title="syslet.cue"
@extern(embed)

package syslet

import syslettools "github.com/xchangeee/syslet/schema/tools@v0"

secretFiles: _ @embed(glob=creds-*.enc.yaml,type=text,allowEmptyGlob)

sysdef: secrets: (syslettools.#SysdefSecretsFromEmbeddedFiles & {in: secretFiles}).secrets
```

To add a secret, create `creds-<name>.enc.yaml` with `sops edit`; to remove one, delete the file.
Each key in a file becomes the podman secret `<name>-<key>`, so key names may only contain `a-z`, `0-9` and `-`, and values must be strings.

`@extern(embed)` has to be the first line of every file that uses `@embed`.
`allowEmptyGlob` keeps the config valid while there are no secret files.

The secret files must sit next to the CUE file.
With a glob into a subdirectory, the directory stays part of each file name, so the `creds-` prefix isn't stripped and the spec name contains the path.
To keep a file elsewhere, [load it on its own](#load-a-single-file).

## Load a single file

To name a secret independently of its file, or to keep the file elsewhere, embed it into the spec directly:

```cue
sysdef: secrets: db: spec: {
	ciphertext: _ @embed(file=secrets/db.enc.yaml,type=text)
}
```

The path is relative to the CUE file's directory and can't contain `..`.
