package model

// ReclaimPolicy controls whether underlying resources (images, volumes, networks) are
// deleted when a unit is removed.
type ReclaimPolicy string

const (
	ReclaimPolicyDelete ReclaimPolicy = "Delete"
	ReclaimPolicyRetain ReclaimPolicy = "Retain"
)

// VolumeUnitRef is the typed base name for a volume unit (e.g. "webapp-data").
type VolumeUnitRef string

func (n VolumeUnitRef) Name() string           { return string(n) }
func (n VolumeUnitRef) FullName() FullUnitName { return FullUnitName(string(n) + ".volume") }
func (n VolumeUnitRef) UnitType() UnitType     { return UnitTypeVolume }
func (n VolumeUnitRef) ServiceUnitName() ServiceUnitName {
	return ServiceUnitName(string(n) + "-volume.service")
}

// VolumeUnit describes a volume in domain terms.
type VolumeUnit struct {
	ref            VolumeUnitRef
	options        UnitOptions
	RemovalAllowed bool
	ReclaimPolicy  ReclaimPolicy
}

// NewVolumeUnit constructs a VolumeUnit.
func NewVolumeUnit(ref VolumeUnitRef, options UnitOptions, removalAllowed bool, reclaimPolicy ReclaimPolicy) *VolumeUnit {
	return &VolumeUnit{
		ref:            ref,
		options:        options,
		RemovalAllowed: removalAllowed,
		ReclaimPolicy:  reclaimPolicy,
	}
}

func (s *VolumeUnit) Ref() UnitRef                { return s.ref }
func (s *VolumeUnit) Options() UnitOptions        { return s.options }
func (s *VolumeUnit) TypedUnitRef() VolumeUnitRef { return s.ref }
