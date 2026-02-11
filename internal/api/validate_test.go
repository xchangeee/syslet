package api

import (
	"strings"
	"testing"
)

func TestValidateDesiredState_Valid(t *testing.T) {
	tests := []struct {
		name         string
		desiredState string
	}{
		{"running", "running"},
		{"stopped", "stopped"},
		{"empty", ""},
		{"running uppercase", "RUNNING"},
		{"stopped uppercase", "STOPPED"},
		{"running mixed case", "Running"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := []Spec{
				&ContainerSpec{
					Name: "test",
					Unit: map[string]map[string]UnitValue{
						"Container": {"Image": UV("nginx:latest")},
					},
					DesiredState: tt.desiredState,
				},
			}

			if err := validateDesiredState(specs); err != nil {
				t.Errorf("validateDesiredState() with %q should not error, got: %v", tt.desiredState, err)
			}
		})
	}
}

func TestValidateDesiredState_Invalid(t *testing.T) {
	tests := []struct {
		name         string
		desiredState string
	}{
		{"invalid", "invalid"},
		{"paused", "paused"},
		{"active", "active"},
		{"enabled", "enabled"},
		{"random", "random_state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			specs := []Spec{
				&ContainerSpec{
					Name: "test",
					Unit: map[string]map[string]UnitValue{
						"Container": {"Image": UV("nginx:latest")},
					},
					DesiredState: tt.desiredState,
				},
			}

			err := validateDesiredState(specs)
			if err == nil {
				t.Errorf("validateDesiredState() with %q should error, but didn't", tt.desiredState)
			}
			if !strings.Contains(err.Error(), "invalid desiredState") {
				t.Errorf("error should mention 'invalid desiredState', got: %v", err)
			}
			if !strings.Contains(err.Error(), tt.desiredState) {
				t.Errorf("error should include the invalid value %q, got: %v", tt.desiredState, err)
			}
		})
	}
}

func TestValidateDesiredState_MultipleContainers(t *testing.T) {
	// Test with multiple containers, one invalid
	specs := []Spec{
		&ContainerSpec{
			Name: "valid1",
			Unit: map[string]map[string]UnitValue{
				"Container": {"Image": UV("nginx:latest")},
			},
			DesiredState: "running",
		},
		&ContainerSpec{
			Name: "invalid",
			Unit: map[string]map[string]UnitValue{
				"Container": {"Image": UV("redis:latest")},
			},
			DesiredState: "paused", // Invalid
		},
		&ContainerSpec{
			Name: "valid2",
			Unit: map[string]map[string]UnitValue{
				"Container": {"Image": UV("postgres:latest")},
			},
			DesiredState: "stopped",
		},
	}

	err := validateDesiredState(specs)
	if err == nil {
		t.Fatal("validateDesiredState() should error with invalid state in one container")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention container name 'invalid', got: %v", err)
	}
	if !strings.Contains(err.Error(), "paused") {
		t.Errorf("error should mention invalid state 'paused', got: %v", err)
	}
}

func TestValidateDesiredState_SkipsNonContainers(t *testing.T) {
	// Test that validation skips volume and network specs
	specs := []Spec{
		&ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]UnitValue{
				"Container": {"Image": UV("nginx:latest")},
			},
			DesiredState: "running",
		},
		&VolumeSpec{
			Name: "data",
			Unit: map[string]map[string]UnitValue{
				"Volume": {"Device": UV("tmpfs")},
			},
		},
		&NetworkSpec{
			Name: "frontend",
			Unit: map[string]map[string]UnitValue{
				"Network": {"Driver": UV("bridge")},
			},
		},
	}

	if err := validateDesiredState(specs); err != nil {
		t.Errorf("validateDesiredState() should not error with non-container specs, got: %v", err)
	}
}

func TestValidateSpecs_IncludesDesiredStateValidation(t *testing.T) {
	// Test that ValidateSpecs includes desiredState validation
	specs := []Spec{
		&ContainerSpec{
			Name: "test",
			Unit: map[string]map[string]UnitValue{
				"Container": {"Image": UV("nginx:latest")},
			},
			DesiredState: "invalid_state",
		},
	}

	err := ValidateSpecs(specs)
	if err == nil {
		t.Fatal("ValidateSpecs() should error with invalid desiredState")
	}
	if !strings.Contains(err.Error(), "invalid desiredState") {
		t.Errorf("error should mention 'invalid desiredState', got: %v", err)
	}
}

func TestValidateNoXSysletSection_Container(t *testing.T) {
	specs := []Spec{
		&ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]UnitValue{
				"X-Syslet": {
					"RemovalAllowed": UV("true"),
				},
				"Container": {"Image": UV("nginx:latest")},
			},
		},
	}

	err := validateNoXSysletSection(specs)
	if err == nil {
		t.Fatal("validateNoXSysletSection() should error when X-Syslet section is provided")
	}
	if !strings.Contains(err.Error(), "X-Syslet") {
		t.Errorf("error should mention 'X-Syslet', got: %v", err)
	}
	if !strings.Contains(err.Error(), "reserved for internal use") {
		t.Errorf("error should mention 'reserved for internal use', got: %v", err)
	}
}

func TestValidateNoXSysletSection_Volume(t *testing.T) {
	specs := []Spec{
		&VolumeSpec{
			Name: "data",
			Unit: map[string]map[string]UnitValue{
				"X-Syslet": {
					"ReclaimPolicy": UV("Delete"),
				},
				"Volume": {"Device": UV("tmpfs")},
			},
		},
	}

	err := validateNoXSysletSection(specs)
	if err == nil {
		t.Fatal("validateNoXSysletSection() should error when X-Syslet section is provided")
	}
	if !strings.Contains(err.Error(), "volume") {
		t.Errorf("error should mention spec type 'volume', got: %v", err)
	}
}

func TestValidateNoXSysletSection_Network(t *testing.T) {
	specs := []Spec{
		&NetworkSpec{
			Name: "frontend",
			Unit: map[string]map[string]UnitValue{
				"X-Syslet": {
					"ReclaimPolicy": UV("Delete"),
				},
				"Network": {"Driver": UV("bridge")},
			},
		},
	}

	err := validateNoXSysletSection(specs)
	if err == nil {
		t.Fatal("validateNoXSysletSection() should error when X-Syslet section is provided")
	}
	if !strings.Contains(err.Error(), "network") {
		t.Errorf("error should mention spec type 'network', got: %v", err)
	}
}

func TestValidateNoXSysletSection_Valid(t *testing.T) {
	specs := []Spec{
		&ContainerSpec{
			Name: "webapp",
			Unit: map[string]map[string]UnitValue{
				"Container": {"Image": UV("nginx:latest")},
				"Service":   {"Restart": UV("always")},
			},
		},
		&VolumeSpec{
			Name: "data",
			Unit: map[string]map[string]UnitValue{
				"Volume": {"Device": UV("tmpfs")},
			},
		},
	}

	err := validateNoXSysletSection(specs)
	if err != nil {
		t.Errorf("validateNoXSysletSection() should not error with valid specs, got: %v", err)
	}
}

func TestValidateSpecs_IncludesXSysletValidation(t *testing.T) {
	specs := []Spec{
		&ContainerSpec{
			Name: "test",
			Unit: map[string]map[string]UnitValue{
				"X-Syslet":  {"RemovalAllowed": UV("true")},
				"Container": {"Image": UV("nginx:latest")},
			},
		},
	}

	err := ValidateSpecs(specs)
	if err == nil {
		t.Fatal("ValidateSpecs() should error with X-Syslet section")
	}
	if !strings.Contains(err.Error(), "X-Syslet") {
		t.Errorf("error should mention 'X-Syslet', got: %v", err)
	}
}
