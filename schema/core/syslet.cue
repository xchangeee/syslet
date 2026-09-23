package core

// Core schema for the syslet tool

// Values of the 'type' field, selecting which spec a document describes
#SpecTypeContainer: "container"
#SpecTypeNetwork:   "network"
#SpecTypeVolume:    "volume"
#SpecTypeBuild:     "build"
#SpecTypeSecret:    "secret"

// Retain keeps the podman resource when its unit is removed
#ReclaimPolicyRetain: "Retain"

// Retain removes the podman resource when its unit is removed
#ReclaimPolicyDelete: "Delete"

// All valid reclaim policy values
#ReclaimPolicy: #ReclaimPolicyRetain | #ReclaimPolicyDelete

// Spec apiVersion this schema describes, syslet routes each spec to its decoder by it
#APIVersionV1: "v1"

// Fields shared by every spec type
#Spec: {
	// Spec apiVersion, fixed by the schema so exported specs always carry it
	apiVersion: #APIVersionV1

	// Spec type
	type!: string

	// Spec name, will be also set as the unit name and podman name
	name!: string
}

// Map form of a unit option value (e.g. Environment), rendered as one entry per key
#Env: {
	[string]: string
}

// Section name of a unit option
#UnitOptionSectionName: string

// Section key of a unit option
#UnitOptionSectionKey: string

// Value of a unit option
#UnitOptionValue: string | [...string] | #Env

// Fields shared by specs that render to a quadlet unit (all except secret)
#UnitSpec: {
	#Spec

	// If this is true, and the container is not in the spec, syslet will remove the container
	removalAllowed!: bool

	// Quadlet unit options, keyed by section name and then option name
	unit?: [#UnitOptionSectionName]: [#UnitOptionSectionKey]: #UnitOptionValue
}

// Whether a container should be running or stopped after reconciliation
#ContainerDesiredStateRunning: "running"

// Whether a container should be stopped after reconciliation
#ContainerDesiredStateStopped: "stopped"

// All valid container desired states
#ContainerDesiredState: #ContainerDesiredStateRunning | #ContainerDesiredStateStopped

// Container rendered to a .container quadlet unit
#ContainerSpec: {
	#UnitSpec

	// Spec type
	type: #SpecTypeContainer

	// Desired state controls if container will be started or stopped
	desiredState!: #ContainerDesiredState

	// Bind-mounted config files
	configFiles?: [...#ContainerConfigFile]

	// Bind-mounted config directories
	configDirs?: [...#ContainerConfigDir]
}

// Single file bind-mounted into a container
#ContainerConfigFile: {
	// Mount path within the container
	mountPath!: string

	// File mode, e.g. 0600
	mode!: string

	// File content
	content!: string
}

// Directory of files bind-mounted into a container
#ContainerConfigDir: {
	// Mount path within the container
	mountPath!: string

	// Files inside the directory
	files!: [...#ContainerConfigDirFile]
}

// File within a #ContainerConfigDir
#ContainerConfigDirFile: {
	// Filename within the directory
	name!: string

	// File mode, e.g. 0600
	mode!: string

	// File content
	content!: string
}

// Network rendered to a .network quadlet unit
#NetworkSpec: {
	#UnitSpec

	// Spec type
	type: #SpecTypeNetwork
}

// Volume rendered to a .volume quadlet unit
#VolumeSpec: {
	#UnitSpec

	// Spec type
	type: #SpecTypeVolume

	// Reclaim policy controls if the volume will be removed when the unit is removed
	reclaimPolicy!: #ReclaimPolicy
}

// Image build rendered to a .build quadlet unit
#BuildSpec: {
	#UnitSpec

	// Spec type
	type: #SpecTypeBuild

	// Reclaim policy controls if the built image will be removed when the unit is removed
	reclaimPolicy!: #ReclaimPolicy

	// Containerfile content
	containerfile!: string

	// Files that will be added to the build context
	contextFiles?: [...#BuildContextFile]
}

// File added to the build context
#BuildContextFile: {
	// Filename within the build context
	filename!: string

	// File content
	content!: string
}

// SOPS-encrypted key/value file, each key becomes a podman secret named <name>-<key>
#SecretSpec: {
	#Spec

	// Spec type
	type: #SpecTypeSecret

	// SOPS-encrypted YAML file content, a flat mapping of key to encrypted value
	ciphertext: string
}
