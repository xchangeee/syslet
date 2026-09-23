@extern(embed)

package syslet

import (
	sysletcore "github.com/xchangeee/syslet/schema/core@v0"
	syslettools "github.com/xchangeee/syslet/schema/tools@v0"
)

// Host that cue cmd plan/apply deploy to
fqdn: "web01.example.com"

#Sysdef: {
	sysletcore.#Sysdef
	sysletcore.#SysdefDefaults
}

sysdef: #Sysdef

// Encrypted secret files, keyed by file name
secretFiles: _ @embed(glob=creds-*.enc.yaml,type=text,allowEmptyGlob)

sysdef: secrets: (syslettools.#SysdefSecretsFromEmbeddedFiles & {in: secretFiles}).secrets

syslet: specRendered: (syslettools.#SysletJsonFromSysdef & {in: sysdef}).specRendered
