package model

import (
	"os"
	"path"
)

// DesiredState represents the intended runtime state of a container.
type DesiredState string

const (
	DesiredStateRunning DesiredState = "running"
	DesiredStateStopped DesiredState = "stopped"
	DesiredStateOneshot DesiredState = "oneshot"
)

// ContainerUnitRef is the typed base name for a container unit (e.g. "webapp").
// It implements Unit and derives the full filename "webapp.container".
type ContainerUnitRef string

func (n ContainerUnitRef) Name() string           { return string(n) }
func (n ContainerUnitRef) FullName() FullUnitName { return FullUnitName(string(n) + ".container") }
func (n ContainerUnitRef) UnitType() UnitType     { return UnitTypeContainer }

// ServiceUnitName returns the systemd service unit name for this container.
func (n ContainerUnitRef) ServiceUnitName() ServiceUnitName {
	return ServiceUnitName(string(n) + ".service")
}

// ContainerMountPath is an absolute path inside a container (file or directory).
type ContainerMountPath string

// ContainerConfigFile is a single config file with a plain filename, mode, and content.
// It is used both as a standalone file within a ContainerFileMount and as a member of a ContainerDirMount.
type ContainerConfigFile struct {
	Name    string
	Mode    os.FileMode
	Content string
}

// ContainerFileMount defines a single config file bind-mounted into a container at Directory/File.Name.
type ContainerFileMount struct {
	Directory ContainerMountPath
	File      ContainerConfigFile
}

// FullPath returns the absolute path inside the container where the file is mounted.
func (f ContainerFileMount) FullPath() ContainerMountPath {
	return ContainerMountPath(path.Join(string(f.Directory), f.File.Name))
}

// NewContainerFileMount creates a ContainerFileMount from a full container path, splitting it
// into Directory and File.Name automatically.
func NewContainerFileMount(mountPath, content string, mode os.FileMode) ContainerFileMount {
	return ContainerFileMount{
		Directory: ContainerMountPath(path.Dir(mountPath)),
		File: ContainerConfigFile{
			Name:    path.Base(mountPath),
			Mode:    mode,
			Content: content,
		},
	}
}

// ContainerDirMount defines a directory bind-mounted into a container, whose files
// are updated atomically via versioned symlinks to allow in-place reload.
type ContainerDirMount struct {
	Directory ContainerMountPath
	Files     []ContainerConfigFile
}

// NewContainerDirMount creates a ContainerDirMount with the given mount path and files.
func NewContainerDirMount(mountPath string, files ...ContainerConfigFile) ContainerDirMount {
	return ContainerDirMount{
		Directory: ContainerMountPath(mountPath),
		Files:     files,
	}
}

// NewContainerConfigFile creates a ContainerConfigFile (used as a member of a ContainerDirMount).
func NewContainerConfigFile(name, content string, mode os.FileMode) ContainerConfigFile {
	return ContainerConfigFile{Name: name, Content: content, Mode: mode}
}

// AddFile appends a file to the ContainerDirMount and returns the updated mount.
func (d ContainerDirMount) AddFile(name, content string, mode os.FileMode) ContainerDirMount {
	d.Files = append(d.Files, ContainerConfigFile{Name: name, Content: content, Mode: mode})
	return d
}

// ContainerUnit describes a container in domain terms.
type ContainerUnit struct {
	ref            ContainerUnitRef
	options        UnitOptions
	DesiredState   DesiredState
	FileMounts     []ContainerFileMount
	DirMounts      []ContainerDirMount
	RemovalAllowed bool
}

// NewContainerUnit constructs a ContainerUnit.
func NewContainerUnit(ref ContainerUnitRef, options UnitOptions, desiredState DesiredState, fileMounts []ContainerFileMount, removalAllowed bool) *ContainerUnit {
	return &ContainerUnit{
		ref:            ref,
		options:        options,
		DesiredState:   desiredState,
		FileMounts:     fileMounts,
		RemovalAllowed: removalAllowed,
	}
}

// NewContainerUnitWithDirs constructs a ContainerUnit including configDirs.
func NewContainerUnitWithDirs(ref ContainerUnitRef, options UnitOptions, desiredState DesiredState, fileMounts []ContainerFileMount, configDirs []ContainerDirMount, removalAllowed bool) *ContainerUnit {
	return &ContainerUnit{
		ref:            ref,
		options:        options,
		DesiredState:   desiredState,
		FileMounts:     fileMounts,
		DirMounts:      configDirs,
		RemovalAllowed: removalAllowed,
	}
}

func (s *ContainerUnit) Ref() UnitRef                   { return s.ref }
func (s *ContainerUnit) Options() UnitOptions           { return s.options }
func (s *ContainerUnit) TypedUnitRef() ContainerUnitRef { return s.ref }
