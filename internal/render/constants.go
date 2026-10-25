package render

import "github.com/xchangeee/syslet/internal/model"

// Unit file sections.
const (
	SectionUnit      model.SectionName = "Unit"
	SectionXSyslet   model.SectionName = "X-Syslet"
	SectionInstall   model.SectionName = "Install"
	SectionService   model.SectionName = "Service"
	SectionContainer model.SectionName = "Container"
	SectionVolume    model.SectionName = "Volume"
	SectionNetwork   model.SectionName = "Network"
	SectionBuild     model.SectionName = "Build"
)

// Unit section keys.
const (
	KeyUnitDescription           model.SectionKey = "Description"
	KeyXSysletRemovalAllowed     model.SectionKey = "RemovalAllowed"
	KeyXSysletReclaimPolicy      model.SectionKey = "ReclaimPolicy"
	KeyInstallWantedBy           model.SectionKey = "WantedBy"
	KeyServiceType               model.SectionKey = "Type"
	KeyServiceRestart            model.SectionKey = "Restart"
	KeyServiceRestartSec         model.SectionKey = "RestartSec"
	KeyServiceRestartSteps       model.SectionKey = "RestartSteps"
	KeyServiceRestartMaxDelaySec model.SectionKey = "RestartMaxDelaySec"
	KeyServiceExecReload         model.SectionKey = "ExecReload"
	KeyContainerReloadCmd        model.SectionKey = "ReloadCmd"
	KeyContainerReloadSignal     model.SectionKey = "ReloadSignal"
	KeyUnitStartLimitIntervalSec model.SectionKey = "StartLimitIntervalSec"
	KeyUnitStartLimitBurst       model.SectionKey = "StartLimitBurst"
	KeyContainerName             model.SectionKey = "ContainerName"
	KeyContainerImage            model.SectionKey = "Image"
	KeyContainerNetwork          model.SectionKey = "Network"
	KeyContainerVolume           model.SectionKey = "Volume"
	KeyContainerSecret           model.SectionKey = "Secret"
	KeyEnvironment               model.SectionKey = "Environment"
	KeyVolumeName                model.SectionKey = "VolumeName"
	KeyNetworkName               model.SectionKey = "NetworkName"
	KeyBuildFile                 model.SectionKey = "File"
	KeyBuildImageTag             model.SectionKey = "ImageTag"
)

// Systemd option values.
const (
	SystemdTrue  = "true"
	SystemdFalse = "false"

	InstallMultiUserTarget = "multi-user.target default.target"
	ServiceTypeOneshot     = "oneshot"
	ServiceRestartAlways   = "always"

	// Restart backoff defaults for running containers: the first restart waits
	// 1s, the delay grows exponentially over 10 restarts up to 5min and stays
	// there. StartLimitIntervalSec=0 disables systemd's start rate limit, which
	// would otherwise give up after 5 starts in 10s and leave the unit failed
	// until someone starts it by hand; the backoff already throttles restarts.
	ServiceRestartSecDefault         = "1s"
	ServiceRestartStepsDefault       = "10"
	ServiceRestartMaxDelaySecDefault = "5min"
	UnitStartLimitIntervalSecDefault = "0"

	// ServiceRestartSecFixed is the restart delay on hosts whose systemd lacks
	// RestartSteps= (see HostCapabilities): without a growing delay, a fixed
	// one keeps a crash-looping container from restarting every second.
	ServiceRestartSecFixed = "10s"
)
