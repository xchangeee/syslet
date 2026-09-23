package model

import "os"

// ImageTag is the typed image tag for a build unit (e.g. "localhost/myapp:latest").
type ImageTag string

// BuildFilePath is a relative path within a build context directory.
type BuildFilePath string

// BuildUnitRef is the typed base name for a build unit (e.g. "myapp").
type BuildUnitRef string

func (n BuildUnitRef) Name() string           { return string(n) }
func (n BuildUnitRef) FullName() FullUnitName { return FullUnitName(string(n) + ".build") }
func (n BuildUnitRef) UnitType() UnitType     { return UnitTypeBuild }
func (n BuildUnitRef) ServiceUnitName() ServiceUnitName {
	return ServiceUnitName(string(n) + "-build.service")
}

// BuildContextFile defines a file to be written to the build context directory.
type BuildContextFile struct {
	Filename BuildFilePath
	Mode     os.FileMode
	Content  string
}

// NewBuildContextFile creates a BuildContextFile.
func NewBuildContextFile(filename, content string, mode os.FileMode) BuildContextFile {
	return BuildContextFile{Filename: BuildFilePath(filename), Content: content, Mode: mode}
}

// BuildUnit describes a container image build in domain terms.
type BuildUnit struct {
	ref           BuildUnitRef
	options       UnitOptions
	Containerfile string
	ContextFiles  []BuildContextFile
	ReclaimPolicy ReclaimPolicy
}

// NewBuildUnit constructs a BuildUnit.
func NewBuildUnit(ref BuildUnitRef, options UnitOptions, containerfile string, contextFiles []BuildContextFile, reclaimPolicy ReclaimPolicy) *BuildUnit {
	return &BuildUnit{
		ref:           ref,
		options:       options,
		Containerfile: containerfile,
		ContextFiles:  contextFiles,
		ReclaimPolicy: reclaimPolicy,
	}
}

func (s *BuildUnit) Ref() UnitRef               { return s.ref }
func (s *BuildUnit) Options() UnitOptions       { return s.options }
func (s *BuildUnit) TypedUnitRef() BuildUnitRef { return s.ref }
