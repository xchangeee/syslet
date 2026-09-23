package tools

import (
	"encoding/json"
	"list"
	"strings"

	"github.com/xchangeee/syslet/schema/core@v0"
)

// This definition provides a shortcut to lock the given network/container/volumes.
//
// Locking means that we disallow removal, and for volumes, we also set reclaim policy
// to retain so data wont be accidentally deleted.
#SysdefLock: {
	X1="in"!: {
		networks: [...string]
		containers: [...string]
		volumes: [...string]
	}
	out: {
		networks: {
			for name in X1.networks {
				"\(name)": spec: {
					removalAllowed: false
				}
			}
		}
		containers: {
			for name in X1.containers {
				"\(name)": spec: {
					removalAllowed: false
				}
			}
		}
		volumes: {
			for name in X1.volumes {
				"\(name)": spec: {
					removalAllowed: false
					reclaimPolicy:  "Retain"
				}
			}
		}
	}
}

// This definition provides a shortcut to assign containers to networks. The network
// name is the first level, the container name the second level.
//
// There is nothing special about this, you can also assign containers to networks
// directly in the container definition, but when there are many containers and complex
// definitions its harder to get an overview of what container is in what network.
#SysdefAssignNetwork: {
	X1="in"!: [string]: [...string]
	out: {
		containers: {
			for network, containerList in X1
			for name in containerList {
				"\(name)": spec: unit: Container: Network: [
					for net, members in X1
					if list.Contains(members, name) {"\(net).network"},
				]
			}
		}
	}
}

// Accepts a struct from @embed(glob=creds-*.enc.yaml,type=text,allowEmptyGlob)
// and returns a struct with matching secret specs. The name is taken from the file
// name, stripping the creds- prefix and the enc.yaml suffix.
#SysdefSecretsFromEmbeddedFiles: {
	X1="in"!: [string]: string
	secrets: {
		for path, content in X1
		let name = strings.TrimSuffix(strings.TrimPrefix(path, "creds-"), ".enc.yaml") {
			(name): core.#SysdefSecret & {
				spec: ciphertext: content
			}
		}
	}
}

// Renders the provided sysdef struct in syslet json format, to be consumed by
// syslet via file or stdin.
#SysletJsonFromSysdef: {
	X1="in": core.#Sysdef
	_specList: [
		for containerName, container in X1.containers if container.enabled {
			core.#ContainerSpec & container.spec & {name: containerName}
		},
		for networkName, network in X1.networks if network.enabled {
			core.#NetworkSpec & network.spec & {name: networkName}
		},
		for volumeName, volume in X1.volumes if volume.enabled {
			core.#VolumeSpec & volume.spec & {name: volumeName}
		},
		for buildName, build in X1.builds if build.enabled {
			core.#BuildSpec & build.spec & {name: buildName}
		},
		for secretName, secret in X1.secrets if secret.enabled {
			core.#SecretSpec & secret.spec & {name: secretName}
		},
	]
	specRendered: json.Marshal(_specList)
}
