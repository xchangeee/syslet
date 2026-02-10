package api

import (
	"encoding/json"
	"testing"
)

func TestUnitValue_UnmarshalJSON_String(t *testing.T) {
	var uv UnitValue
	err := json.Unmarshal([]byte(`"nginx:latest"`), &uv)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	values := uv.Values()
	if len(values) != 1 || values[0] != "nginx:latest" {
		t.Errorf("expected [\"nginx:latest\"], got %v", values)
	}
}

func TestUnitValue_UnmarshalJSON_Array(t *testing.T) {
	var uv UnitValue
	err := json.Unmarshal([]byte(`["8080:80", "443:443"]`), &uv)
	if err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	values := uv.Values()
	if len(values) != 2 || values[0] != "8080:80" || values[1] != "443:443" {
		t.Errorf("expected [\"8080:80\", \"443:443\"], got %v", values)
	}
}

func TestUnitValue_UnmarshalJSON_RejectsNumber(t *testing.T) {
	var uv UnitValue
	err := json.Unmarshal([]byte(`8080`), &uv)
	if err == nil {
		t.Error("expected UnmarshalJSON to fail with number, but it succeeded")
	}
}

func TestUnitValue_UnmarshalJSON_RejectsBoolean(t *testing.T) {
	var uv UnitValue
	err := json.Unmarshal([]byte(`true`), &uv)
	if err == nil {
		t.Error("expected UnmarshalJSON to fail with boolean, but it succeeded")
	}
}

func TestUnitValue_UnmarshalJSON_RejectsArrayWithNumber(t *testing.T) {
	var uv UnitValue
	err := json.Unmarshal([]byte(`[8080, 9090]`), &uv)
	if err == nil {
		t.Error("expected UnmarshalJSON to fail with array of numbers, but it succeeded")
	}
}

func TestUnitValue_MarshalJSON_String(t *testing.T) {
	var uv UnitValue
	_ = json.Unmarshal([]byte(`"nginx:latest"`), &uv)

	data, err := json.Marshal(uv)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	expected := `"nginx:latest"`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestUnitValue_MarshalJSON_Array(t *testing.T) {
	var uv UnitValue
	_ = json.Unmarshal([]byte(`["8080:80", "443:443"]`), &uv)

	data, err := json.Marshal(uv)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	expected := `["8080:80","443:443"]`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestRenderedUnit_SerializeUnitOptions_Empty(t *testing.T) {
	ru := &RenderedUnit{
		UnitOptions: []UnitOption{},
	}

	content, err := ru.SerializeUnitOptions()
	if err != nil {
		t.Fatalf("SerializeUnitOptions failed: %v", err)
	}

	// Empty options should produce empty or minimal content
	if content != "" && content != "\n" {
		t.Errorf("expected empty or minimal content, got %q", content)
	}
}

func TestRenderedUnit_SerializeUnitOptions(t *testing.T) {
	ru := &RenderedUnit{
		UnitOptions: []UnitOption{
			{Section: "Container", Name: "Image", Value: "nginx:latest"},
			{Section: "Container", Name: "ContainerName", Value: "webapp"},
			{Section: "Install", Name: "WantedBy", Value: "multi-user.target"},
		},
	}

	content, err := ru.SerializeUnitOptions()
	if err != nil {
		t.Fatalf("SerializeUnitOptions failed: %v", err)
	}

	expected := `[Container]
Image=nginx:latest
ContainerName=webapp

[Install]
WantedBy=multi-user.target
`

	if content != expected {
		t.Errorf("SerializeUnitOptions() mismatch:\ngot:\n%s\nwant:\n%s", content, expected)
	}
}

func TestRenderedUnit_SerializeUnitOptions_MultipleValues(t *testing.T) {
	ru := &RenderedUnit{
		UnitOptions: []UnitOption{
			{Section: "Container", Name: "Volume", Value: "/host:/container"},
			{Section: "Container", Name: "Volume", Value: "/another:/path"},
		},
	}

	content, err := ru.SerializeUnitOptions()
	if err != nil {
		t.Fatalf("SerializeUnitOptions failed: %v", err)
	}

	expected := `[Container]
Volume=/host:/container
Volume=/another:/path
`

	if content != expected {
		t.Errorf("SerializeUnitOptions() mismatch:\ngot:\n%s\nwant:\n%s", content, expected)
	}
}
