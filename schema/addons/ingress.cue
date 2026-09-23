package addons

#SysdefIngress: {
	ingress!: [string]: #SysdefIngressEntry
}

// Sysdef collects containers, networks, volumes, builds, secrets by name and
// provides an 'enabled' flag to quickly remove items without deleting them from
// code
#SysdefIngressEntry: {
	enabled!:       bool
	host!:          string
	tls!:           bool
	containerPort?: int
	backends?:      string

	reverseProxyConfig: string
	extraConfig:        string
}

#SysdefIngressDefaults: {
	ingress: {
		[string]: enabled: bool | *true
		[string]: tls:     bool | *true

		[string]: reverseProxyConfig: string | *""
		[string]: extraConfig:        string | *""
	}
}
