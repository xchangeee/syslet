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

// Delete removes the podman resource when its unit is removed
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

	// Quadlet unit options, keyed by section name and then option name
	unit?: [#UnitOptionSectionName]: [#UnitOptionSectionKey]: #UnitOptionValue
}

// Fields shared by specs whose unit syslet only prunes when allowed (container, volume, network).
// Builds are pruned unconditionally, so #BuildSpec doesn't include it.
#RemovableSpec: {
	// If this is true, and the unit is no longer in the input, syslet will remove the unit
	removalAllowed!: bool
}

// Container should be running after reconciliation
#ContainerDesiredStateRunning: "running"

// Container should be stopped after reconciliation
#ContainerDesiredStateStopped: "stopped"

// Container runs as a oneshot service (Type=oneshot). syslet never starts or
// stops it, other units trigger it through their [Unit] dependencies.
#ContainerDesiredStateOneshot: "oneshot"

// All valid container desired states
#ContainerDesiredState: #ContainerDesiredStateRunning | #ContainerDesiredStateStopped | #ContainerDesiredStateOneshot

// Container rendered to a .container quadlet unit
#ContainerSpec: {
	#UnitSpec
	#RemovableSpec

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

	// File mode, e.g. 0600, defaults to 0644
	mode?: string

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

	// File mode, e.g. 0600, defaults to 0644
	mode?: string

	// File content
	content!: string
}

// Network rendered to a .network quadlet unit
#NetworkSpec: {
	#UnitSpec
	#RemovableSpec

	// Spec type
	type: #SpecTypeNetwork

	// Reclaim policy controls if the podman network will be removed when the unit is removed
	reclaimPolicy!: #ReclaimPolicy
}

// Volume rendered to a .volume quadlet unit
#VolumeSpec: {
	#UnitSpec
	#RemovableSpec

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

	// File mode, e.g. 0600, defaults to 0644
	mode?: string

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
