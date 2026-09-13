package model

// NetworkUnitRef is the typed base name for a network unit (e.g. "webapp-net").
type NetworkUnitRef string

func (n NetworkUnitRef) Name() string           { return string(n) }
func (n NetworkUnitRef) FullName() FullUnitName { return FullUnitName(string(n) + ".network") }
func (n NetworkUnitRef) UnitType() UnitType     { return UnitTypeNetwork }
func (n NetworkUnitRef) ServiceUnitName() ServiceUnitName {
	return ServiceUnitName(string(n) + "-network.service")
}

// NetworkUnit describes a network in domain terms.
type NetworkUnit struct {
	ref            NetworkUnitRef
	options        UnitOptions
	RemovalAllowed bool
	ReclaimPolicy  ReclaimPolicy
}

// NewNetworkUnit constructs a NetworkUnit.
func NewNetworkUnit(ref NetworkUnitRef, options UnitOptions, removalAllowed bool, reclaimPolicy ReclaimPolicy) *NetworkUnit {
	return &NetworkUnit{
		ref:            ref,
		options:        options,
		RemovalAllowed: removalAllowed,
		ReclaimPolicy:  reclaimPolicy,
	}
}

func (s *NetworkUnit) Ref() UnitRef                 { return s.ref }
func (s *NetworkUnit) Options() UnitOptions         { return s.options }
func (s *NetworkUnit) TypedUnitRef() NetworkUnitRef { return s.ref }
