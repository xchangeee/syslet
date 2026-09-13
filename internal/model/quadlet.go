package model

// AllUnitTypes is the ordered list of all known unit types, used for iterating
// over every quadlet extension without repeating the mapping in multiple places.
var AllUnitTypes = []UnitType{
	UnitTypeContainer,
	UnitTypeVolume,
	UnitTypeNetwork,
	UnitTypeBuild,
}

const (
	UnitTypeContainer UnitType = "container"
	UnitTypeVolume    UnitType = "volume"
	UnitTypeNetwork   UnitType = "network"
	UnitTypeBuild     UnitType = "build"
)
