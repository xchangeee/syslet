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
	KeyUnitDescription       model.SectionKey = "Description"
	KeyXSysletRemovalAllowed model.SectionKey = "RemovalAllowed"
	KeyXSysletReclaimPolicy  model.SectionKey = "ReclaimPolicy"
	KeyInstallWantedBy       model.SectionKey = "WantedBy"
	KeyServiceType           model.SectionKey = "Type"
	KeyServiceRestart        model.SectionKey = "Restart"
	KeyContainerName         model.SectionKey = "ContainerName"
	KeyContainerImage        model.SectionKey = "Image"
	KeyContainerNetwork      model.SectionKey = "Network"
	KeyContainerVolume       model.SectionKey = "Volume"
	KeyContainerSecret       model.SectionKey = "Secret"
	KeyEnvironment           model.SectionKey = "Environment"
	KeyVolumeName            model.SectionKey = "VolumeName"
	KeyNetworkName           model.SectionKey = "NetworkName"
	KeyBuildFile             model.SectionKey = "File"
	KeyBuildImageTag         model.SectionKey = "ImageTag"
)

// Systemd option values.
const (
	SystemdTrue  = "true"
	SystemdFalse = "false"

	InstallMultiUserTarget = "multi-user.target default.target"
	ServiceTypeOneshot     = "oneshot"
	ServiceRestartAlways   = "always"
)
