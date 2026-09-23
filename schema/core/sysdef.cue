package core

// Sysdef collects containers, networks, volumes, builds, secrets by name and
// provides an 'enabled' flag to quickly remove items without deleting them from
// code
#Sysdef: {
	// Containers by name, the key is set as the spec name
	containers!: [Name=string]: #SysdefContainerUnit & {spec: name: Name}

	// Networks by name, the key is set as the spec name
	networks!: [Name=string]: #SysdefNetworkUnit & {spec: name: Name}

	// Volumes by name, the key is set as the spec name
	volumes!: [Name=string]: #SysdefVolumeUnit & {spec: name: Name}

	// Image builds by name, the key is set as the spec name
	builds!: [Name=string]: #SysdefBuildUnit & {spec: name: Name}

	// Secrets by name, the key is set as the spec name
	secrets!: [Name=string]: #SysdefSecret & {spec: name: Name}
}

// Fields shared by every Sysdef entry
#SysdefUnit: {
	// Disabled entries are left out of the output
	enabled!: bool
}

// Sysdef entry wrapping a container spec
#SysdefContainerUnit: {
	#SysdefUnit

	// Container spec
	spec!: #ContainerSpec
}

// Sysdef entry wrapping a network spec
#SysdefNetworkUnit: {
	#SysdefUnit

	// Network spec
	spec!: #NetworkSpec
}

// Sysdef entry wrapping a volume spec
#SysdefVolumeUnit: {
	#SysdefUnit

	// Volume spec
	spec!: #VolumeSpec
}

// Sysdef entry wrapping a build spec
#SysdefBuildUnit: {
	#SysdefUnit

	// Build spec
	spec!: #BuildSpec
}

// Sysdef entry wrapping a secret spec
#SysdefSecret: {
	#SysdefUnit

	// Secret spec
	spec!: #SecretSpec
}

// Reasonable default values to sysdef
#SysdefDefaults: {
	containers: {
		[string]: enabled: bool | *true
		[string]: spec: removalAllowed: bool | *true
		[string]: spec: desiredState:   string | *#ContainerDesiredStateRunning
	}

	networks: {
		[string]: enabled: bool | *true
		[string]: spec: removalAllowed: bool | *true
		[string]: spec: reclaimPolicy:  string | *#ReclaimPolicyDelete
	}

	volumes: {
		[string]: enabled: bool | *true
		[string]: spec: removalAllowed: bool | *true
		[string]: spec: reclaimPolicy:  string | *#ReclaimPolicyDelete
	}

	builds: {
		[string]: enabled: bool | *true
		[string]: spec: reclaimPolicy: string | *#ReclaimPolicyDelete
	}

	secrets: {
		[string]: enabled: bool | *true
	}
}
