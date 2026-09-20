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
	uv, err := UnitValueFromRaw(ContainerSection, "Image", "nginx:latest")
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 1 || values[0] != "nginx:latest" {
		t.Errorf("expected [\"nginx:latest\"], got %v", values)
	}
}

func TestUnitValueFromRaw_Array(t *testing.T) {
	uv, err := UnitValueFromRaw(ContainerSection, "PublishPort", []any{"8080:80", "443:443"})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 2 || values[0] != "8080:80" || values[1] != "443:443" {
		t.Errorf("expected [\"8080:80\", \"443:443\"], got %v", values)
	}
}

func TestUnitValueFromRaw_RejectsNumber(t *testing.T) {
	_, err := UnitValueFromRaw(ContainerSection, "Image", float64(8080))
	if err == nil {
		t.Error("expected error for number, got nil")
	}
}

func TestUnitValueFromRaw_RejectsBoolean(t *testing.T) {
	_, err := UnitValueFromRaw(ContainerSection, "Image", true)
	if err == nil {
		t.Error("expected error for boolean, got nil")
	}
}

func TestUnitValueFromRaw_RejectsArrayWithNumber(t *testing.T) {
	_, err := UnitValueFromRaw(ContainerSection, "PublishPort", []any{float64(8080), float64(9090)})
	if err == nil {
		t.Error("expected error for array of numbers, got nil")
	}
}

func TestUnitValueFromRaw_EnvironmentKeyWithMap_ReturnsSortedKeyEqualsValuePairs(t *testing.T) {
	uv, err := UnitValueFromRaw(ContainerSection, EnvironmentKey, map[string]any{"faz": "baz", "foo": "bar"})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 2 || values[0] != "faz=baz" || values[1] != "foo=bar" {
		t.Errorf("expected [\"faz=baz\", \"foo=bar\"], got %v", values)
	}
}

func TestUnitValueFromRaw_EnvironmentKeyWithMap_RejectsNonStringValue(t *testing.T) {
	_, err := UnitValueFromRaw(ContainerSection, EnvironmentKey, map[string]any{"foo": float64(8080)})
	if err == nil {
		t.Error("expected error for map with non-string value, got nil")
	}
}

func TestUnitValueFromRaw_EnvironmentKeyWithEmptyValue_KeepsSeparator(t *testing.T) {
	uv, err := UnitValueFromRaw(ContainerSection, EnvironmentKey, map[string]any{"foo": ""})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 1 || values[0] != "foo=" {
		t.Errorf("expected [\"foo=\"], got %v", values)
	}
}

func TestUnitValueFromRaw_NonEnvironmentKeyWithMap_RejectsMap(t *testing.T) {
	_, err := UnitValueFromRaw(ContainerSection, "Label", map[string]any{"foo": "bar"})
	if err == nil {
		t.Error("expected error for map input on a non-Environment key, got nil")
	}
}

func TestUnitValueFromRaw_ContainerVolumeKeyWithMap_ReturnsSortedKeyColonValuePairs(t *testing.T) {
	uv, err := UnitValueFromRaw(ContainerSection, VolumeKey, map[string]any{"data": "/var/lib/data", "cache": "/var/lib/cache"})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 2 || values[0] != "cache:/var/lib/cache" || values[1] != "data:/var/lib/data" {
		t.Errorf("expected [\"cache:/var/lib/cache\", \"data:/var/lib/data\"], got %v", values)
	}
}

func TestUnitValueFromRaw_VolumeKeyOutsideContainerSectionWithMap_RejectsMap(t *testing.T) {
	_, err := UnitValueFromRaw("Network", VolumeKey, map[string]any{"data": "/var/lib/data"})
	if err == nil {
		t.Error("expected error for map input on Volume key outside [Container] section, got nil")
	}
}

func TestUnitValueFromRaw_ContainerSecretKeyWithMap_ReturnsSortedKeyCommaValuePairs(t *testing.T) {
	uv, err := UnitValueFromRaw(ContainerSection, SecretKey, map[string]any{"mysecret": "type=mount", "othersecret": "type=env,target=FOO"})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 2 || values[0] != "mysecret,type=mount" || values[1] != "othersecret,type=env,target=FOO" {
		t.Errorf("expected [\"mysecret,type=mount\", \"othersecret,type=env,target=FOO\"], got %v", values)
	}
}

func TestUnitValueFromRaw_ContainerSecretKeyWithEmptyValue_OmitsSeparator(t *testing.T) {
	uv, err := UnitValueFromRaw(ContainerSection, SecretKey, map[string]any{"mysecret": ""})
	if err != nil {
		t.Fatalf("UnitValueFromRaw failed: %v", err)
	}
	values := uv.Values()
	if len(values) != 1 || values[0] != "mysecret" {
		t.Errorf("expected [\"mysecret\"], got %v", values)
	}
}

func TestUnitValueFromRaw_SecretKeyOutsideContainerSectionWithMap_RejectsMap(t *testing.T) {
	_, err := UnitValueFromRaw("Network", SecretKey, map[string]any{"mysecret": "type=mount"})
	if err == nil {
		t.Error("expected error for map input on Secret key outside [Container] section, got nil")
	}
}

func TestUnitValueFromRaw_MapKeysRequiringValue_RejectsEmptyValue(t *testing.T) {
	cases := []struct {
		section SectionName
		key     SectionKey
	}{
		{ContainerSection, VolumeKey},
		{ContainerSection, MaskKey},
		{ContainerSection, LogOptKey},
	}
	for _, c := range cases {
		_, err := UnitValueFromRaw(c.section, c.key, map[string]any{"foo": ""})
		if err == nil {
			t.Errorf("expected error for empty map value on [%s] %s, got nil", c.section, c.key)
		}
	}
}

func TestUnitValueFromRaw_ContainerMapKeys(t *testing.T) {
	t.Run("MaskWithColonSeparator", func(t *testing.T) {
		uv, err := UnitValueFromRaw(ContainerSection, MaskKey, map[string]any{"/proc/foo": "/proc/bar"})
		if err != nil {
			t.Fatalf("UnitValueFromRaw failed: %v", err)
		}
		values := uv.Values()
		if len(values) != 1 || values[0] != "/proc/foo:/proc/bar" {
			t.Errorf("expected [\"/proc/foo:/proc/bar\"], got %v", values)
		}
	})

	t.Run("LogOptWithEqualsSeparator", func(t *testing.T) {
		uv, err := UnitValueFromRaw(ContainerSection, LogOptKey, map[string]any{"path": "/var/log/foo"})
		if err != nil {
			t.Fatalf("UnitValueFromRaw failed: %v", err)
		}
		values := uv.Values()
		if len(values) != 1 || values[0] != "path=/var/log/foo" {
			t.Errorf("expected [\"path=/var/log/foo\"], got %v", values)
		}
	})

	t.Run("NetworkUsesKeyOnly", func(t *testing.T) {
		uv, err := UnitValueFromRaw(ContainerSection, NetworkKey, map[string]any{"mynet": "ignored", "othernet": "also ignored"})
		if err != nil {
			t.Fatalf("UnitValueFromRaw failed: %v", err)
		}
		values := uv.Values()
		if len(values) != 2 || values[0] != "mynet" || values[1] != "othernet" {
			t.Errorf("expected [\"mynet\", \"othernet\"], got %v", values)
		}
	})

	t.Run("NetworkUsesKeyOnlyEvenWithNonStringValue", func(t *testing.T) {
		uv, err := UnitValueFromRaw(ContainerSection, NetworkKey, map[string]any{"mynet": true})
		if err != nil {
			t.Fatalf("UnitValueFromRaw failed: %v", err)
		}
		values := uv.Values()
		if len(values) != 1 || values[0] != "mynet" {
			t.Errorf("expected [\"mynet\"], got %v", values)
		}
	})
}

func TestUnitValueFromRaw_ContainerMapKeysOutsideContainerSection_RejectsMap(t *testing.T) {
	for _, key := range []SectionKey{MaskKey, LogOptKey, NetworkKey} {
		_, err := UnitValueFromRaw("Service", key, map[string]any{"foo": "bar"})
		if err == nil {
			t.Errorf("expected error for map input on %s key outside [Container] section, got nil", key)
		}
	}
}

func TestUnitValue_Values_NilSlice(t *testing.T) {
	var uv UnitValue
	if values := uv.Values(); len(values) != 0 {
		t.Errorf("expected empty slice for zero UnitValue, got %v", values)
	}
}
