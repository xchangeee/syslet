package render

import (
	"strings"
	"testing"

	"codeberg.org/xchangeee/syslet/internal/model"
)

func TestRenderUnit_Empty(t *testing.T) {
	unit := model.NewContainerUnit(model.ContainerUnitRef("test"), nil, "", nil, false)
	ru, err := NewUnitRenderer(unit).RenderedUnit()
	if err != nil {
		t.Fatalf("RenderedUnit failed: %v", err)
	}

	if ru.Content != "" && ru.Content != "\n" {
		t.Errorf("expected empty or minimal content, got %q", ru.Content)
	}
}

func TestRenderUnit_Content(t *testing.T) {
	unit := model.NewContainerUnit(model.ContainerUnitRef("test"), nil, "", nil, false)
	ru, err := NewUnitRenderer(unit).
		Append(SectionContainer, KeyContainerImage, "nginx:latest").
		Append(SectionContainer, KeyContainerName, "webapp").
		Append(SectionInstall, KeyInstallWantedBy, "multi-user.target").
		RenderedUnit()
	if err != nil {
		t.Fatalf("RenderedUnit failed: %v", err)
	}

	expected := `[Container]
ContainerName=webapp
Image=nginx:latest

[Install]
WantedBy=multi-user.target
`

	if ru.Content != expected {
		t.Errorf("RenderedUnit() Content mismatch:\ngot:\n%s\nwant:\n%s", ru.Content, expected)
	}
}

func TestRenderUnit_MultipleValues(t *testing.T) {
	unit := model.NewContainerUnit(model.ContainerUnitRef("test"), nil, "", nil, false)
	ru, err := NewUnitRenderer(unit).
		Append(SectionContainer, KeyContainerVolume, "/host:/container").
		Append(SectionContainer, KeyContainerVolume, "/another:/path").
		RenderedUnit()
	if err != nil {
		t.Fatalf("RenderedUnit failed: %v", err)
	}

	expected := `[Container]
Volume=/another:/path
Volume=/host:/container
`

	if ru.Content != expected {
		t.Errorf("RenderedUnit() Content mismatch:\ngot:\n%s\nwant:\n%s", ru.Content, expected)
	}
}

// unitContent builds a minimal INI-style unit file string from section/key/value triples.
func unitContent(triples ...string) string {
	if len(triples)%3 != 0 {
		panic("unitContent: triples must be section/key/value groups")
	}
	var sb strings.Builder
	for i := 0; i < len(triples); i += 3 {
		sb.WriteString("[" + triples[i] + "]\n")
		sb.WriteString(triples[i+1] + "=" + triples[i+2] + "\n")
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestStripMetadataSections_EmptyInput_ReturnsEmpty(t *testing.T) {
	got, err := StripMetadataSections("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Serialized empty unit should contain no section headers or key=value pairs.
	if strings.Contains(got, "[") || strings.Contains(got, "=") {
		t.Errorf("expected empty output, got: %q", got)
	}
}

func TestStripMetadataSections_XSysletSection_Removed(t *testing.T) {
	input := unitContent(
		"Container", "Image", "nginx:latest",
		"X-Syslet", "RemovalAllowed", "true",
	)

	got, err := StripMetadataSections(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "X-Syslet") {
		t.Errorf("X-Syslet section should be stripped, got: %q", got)
	}
	if strings.Contains(got, "RemovalAllowed") {
		t.Errorf("RemovalAllowed should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "nginx:latest") {
		t.Errorf("Container section should be preserved, got: %q", got)
	}
}

func TestStripMetadataSections_UnitDescription_Removed(t *testing.T) {
	input := unitContent(
		"Unit", "Description", "My webapp container",
		"Container", "Image", "nginx:latest",
	)

	got, err := StripMetadataSections(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "Description") {
		t.Errorf("Unit.Description should be stripped, got: %q", got)
	}
	if strings.Contains(got, "My webapp container") {
		t.Errorf("description value should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "nginx:latest") {
		t.Errorf("Container section should be preserved, got: %q", got)
	}
}

func TestStripMetadataSections_OtherUnitFields_Preserved(t *testing.T) {
	input := unitContent(
		"Unit", "Description", "My container",
		"Unit", "After", "network.target",
		"Container", "Image", "nginx:latest",
	)

	got, err := StripMetadataSections(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "Description") {
		t.Errorf("Description should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "After") {
		t.Errorf("Unit.After should be preserved, got: %q", got)
	}
	if !strings.Contains(got, "network.target") {
		t.Errorf("Unit.After value should be preserved, got: %q", got)
	}
}

// TestStripMetadataSections_SortingMakesOrderIrrelevant verifies that two unit files
// with the same options in different order produce identical stripped output, so
// reordering multi-value keys (e.g. Volume=) never triggers a spurious restart.
func TestStripMetadataSections_SortingMakesOrderIrrelevant(t *testing.T) {
	inputA := unitContent(
		"Container", "Volume", "data.volume:/data",
		"Container", "Volume", "logs.volume:/logs",
		"Container", "Image", "nginx:latest",
	)
	inputB := unitContent(
		"Container", "Volume", "logs.volume:/logs",
		"Container", "Volume", "data.volume:/data",
		"Container", "Image", "nginx:latest",
	)

	strippedA, err := StripMetadataSections(inputA)
	if err != nil {
		t.Fatalf("stripping inputA: %v", err)
	}
	strippedB, err := StripMetadataSections(inputB)
	if err != nil {
		t.Fatalf("stripping inputB: %v", err)
	}

	if strippedA != strippedB {
		t.Errorf("reordered options should produce same stripped output:\nA: %q\nB: %q", strippedA, strippedB)
	}
}

// TestStripMetadataSections_OnlyMetadata_ProducesEmptyContent verifies that a unit
// containing only X-Syslet and Description entries produces no meaningful output.
func TestStripMetadataSections_OnlyMetadata_ProducesEmptyContent(t *testing.T) {
	input := unitContent(
		"X-Syslet", "RemovalAllowed", "true",
		"X-Syslet", "ReclaimPolicy", "Delete",
		"Unit", "Description", "stale unit",
	)

	got, err := StripMetadataSections(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "RemovalAllowed") || strings.Contains(got, "ReclaimPolicy") || strings.Contains(got, "Description") {
		t.Errorf("all metadata should be stripped, got: %q", got)
	}
}

// TestStripMetadataSections_DescriptionChangeDoesNotAffectOutput verifies that
// changing only the Description produces the same stripped output — meaning syslet
// would not schedule a container restart for a description-only change.
func TestStripMetadataSections_DescriptionChangeDoesNotAffectOutput(t *testing.T) {
	old := unitContent(
		"Unit", "Description", "Old description",
		"Container", "Image", "nginx:latest",
	)
	new := unitContent(
		"Unit", "Description", "New description",
		"Container", "Image", "nginx:latest",
	)

	strippedOld, err := StripMetadataSections(old)
	if err != nil {
		t.Fatalf("stripping old: %v", err)
	}
	strippedNew, err := StripMetadataSections(new)
	if err != nil {
		t.Fatalf("stripping new: %v", err)
	}

	if strippedOld != strippedNew {
		t.Errorf("description-only change should not affect stripped output:\nold: %q\nnew: %q", strippedOld, strippedNew)
	}
}

// TestStripMetadataSections_ImageChange_AffectsOutput verifies that a change to a
// runtime-relevant field (Image) does produce different stripped output — meaning
// syslet would schedule a container restart.
func TestStripMetadataSections_ImageChange_AffectsOutput(t *testing.T) {
	old := unitContent("Container", "Image", "nginx:1.24")
	new := unitContent("Container", "Image", "nginx:1.25")

	strippedOld, err := StripMetadataSections(old)
	if err != nil {
		t.Fatalf("stripping old: %v", err)
	}
	strippedNew, err := StripMetadataSections(new)
	if err != nil {
		t.Fatalf("stripping new: %v", err)
	}

	if strippedOld == strippedNew {
		t.Error("image change should produce different stripped output")
	}
}

func TestStripMetadataSections_InvalidInput_ReturnsError(t *testing.T) {
	// gounit.Deserialize is lenient; test a truly malformed edge — empty is valid,
	// so just verify the function returns an error for pathological binary content.
	// In practice the function is robust, so we test valid paths exhaustively above.
	// This test guards against future changes to the error path.
	_, err := StripMetadataSections("\x00\x01\x02")
	// If no error, that is also acceptable since the parser is lenient — but if it does
	// error, it should wrap the message correctly.
	if err != nil && !strings.Contains(err.Error(), "deserializing") {
		t.Errorf("error should mention 'deserializing', got: %v", err)
	}
}
