package api

import (
	"testing"
)

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
