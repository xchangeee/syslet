package model

import (
	"encoding/json"
	"testing"
)

func TestUnitValue_UV(t *testing.T) {
	uv := UV("nginx:latest")
	values := uv.Values()
	if len(values) != 1 || values[0] != "nginx:latest" {
		t.Errorf("expected [\"nginx:latest\"], got %v", values)
	}
}

func TestUnitValue_MultiUV(t *testing.T) {
	uv := MultiUV("8080:80", "443:443")
	values := uv.Values()
	if len(values) != 2 || values[0] != "8080:80" || values[1] != "443:443" {
		t.Errorf("expected [\"8080:80\", \"443:443\"], got %v", values)
	}
}

func TestUnitValue_MarshalJSON_String(t *testing.T) {
	uv := UV("nginx:latest")
	data, err := json.Marshal(uv)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if string(data) != `"nginx:latest"` {
		t.Errorf("expected %q, got %q", `"nginx:latest"`, string(data))
	}
}

func TestUnitValue_MarshalJSON_Array(t *testing.T) {
	uv := MultiUV("8080:80", "443:443")
	data, err := json.Marshal(uv)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}
	if string(data) != `["8080:80","443:443"]` {
		t.Errorf("expected %q, got %q", `["8080:80","443:443"]`, string(data))
	}
}

func TestUnitValueFromRaw_String(t *testing.T) {
	uv, err := UnitValueFromRaw("nginx:latest")
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 1 || values[0] != "nginx:latest" {
		t.Errorf("expected [\"nginx:latest\"], got %v", values)
	}
}

func TestUnitValueFromRaw_Array(t *testing.T) {
	uv, err := UnitValueFromRaw([]any{"8080:80", "443:443"})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 2 || values[0] != "8080:80" || values[1] != "443:443" {
		t.Errorf("expected [\"8080:80\", \"443:443\"], got %v", values)
	}
}

func TestUnitValueFromRaw_RejectsNumber(t *testing.T) {
	_, err := UnitValueFromRaw(float64(8080))
	if err == nil {
		t.Error("expected error for number, got nil")
	}
}

func TestUnitValueFromRaw_RejectsBoolean(t *testing.T) {
	_, err := UnitValueFromRaw(true)
	if err == nil {
		t.Error("expected error for boolean, got nil")
	}
}

func TestUnitValueFromRaw_RejectsArrayWithNumber(t *testing.T) {
	_, err := UnitValueFromRaw([]any{float64(8080), float64(9090)})
	if err == nil {
		t.Error("expected error for array of numbers, got nil")
	}
}

func TestUnitValue_Values_NilSlice(t *testing.T) {
	var uv UnitValue
	if values := uv.Values(); len(values) != 0 {
		t.Errorf("expected empty slice for zero UnitValue, got %v", values)
	}
}
