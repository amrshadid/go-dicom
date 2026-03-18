package tag_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// TestDictionaryPatientName verifies Patient Name tag metadata
func TestDictionaryPatientName(t *testing.T) {
	patientNameTag := tag.New(0x0010, 0x0010)

	// Test GetInfo
	info := patientNameTag.GetInfo()
	if info == nil {
		t.Fatal("Patient Name tag not in dictionary")
	}

	// Test VR
	if info.VR != "PN" {
		t.Errorf("Expected VR 'PN', got '%s'", info.VR)
	}

	// Test VM
	if info.VM != "1" {
		t.Errorf("Expected VM '1', got '%s'", info.VM)
	}

	// Test Name
	if info.Name != "Patient's Name" {
		t.Errorf("Expected name \"Patient's Name\", got \"%s\"", info.Name)
	}

	// Test Retired status
	if info.Retired {
		t.Error("Patient Name should not be retired")
	}

	// Test Keyword
	if info.Keyword != "PatientName" {
		t.Errorf("Expected keyword 'PatientName', got '%s'", info.Keyword)
	}
}

// TestDictionaryMethods verifies Tag methods
func TestDictionaryMethods(t *testing.T) {
	patientNameTag := tag.New(0x0010, 0x0010)

	// Test GetName
	name := patientNameTag.GetName()
	if name != "Patient's Name" {
		t.Errorf("GetName failed: expected \"Patient's Name\", got \"%s\"", name)
	}

	// Test GetVR
	vr := patientNameTag.GetVR()
	if vr != "PN" {
		t.Errorf("GetVR failed: expected 'PN', got '%s'", vr)
	}

	// Test GetVM
	vm := patientNameTag.GetVM()
	if vm != "1" {
		t.Errorf("GetVM failed: expected '1', got '%s'", vm)
	}

	// Test GetKeyword
	keyword := patientNameTag.GetKeyword()
	if keyword != "PatientName" {
		t.Errorf("GetKeyword failed: expected 'PatientName', got '%s'", keyword)
	}

	// Test IsRetired
	if patientNameTag.IsRetired() {
		t.Error("IsRetired failed: Patient Name should not be retired")
	}

	// Test Exists
	if !patientNameTag.Exists() {
		t.Error("Exists failed: Patient Name should exist in dictionary")
	}
}

// TestDictionaryImageType verifies Image Type tag (multi-valued)
func TestDictionaryImageType(t *testing.T) {
	imageTypeTag := tag.New(0x0008, 0x0008)

	info := imageTypeTag.GetInfo()
	if info == nil {
		t.Fatal("Image Type tag not in dictionary")
	}

	// Test multi-valued VM
	if info.VM != "2-n" {
		t.Errorf("Expected VM '2-n', got '%s'", info.VM)
	}

	// Test VR
	if info.VR != "CS" {
		t.Errorf("Expected VR 'CS', got '%s'", info.VR)
	}
}

// TestDictionaryRepeaterTag verifies repeater tag matching (e.g., overlay)
func TestDictionaryRepeaterTag(t *testing.T) {
	// Overlay group 0x6000, element 0x3000
	overlayTag := tag.New(0x6000, 0x3000)

	info := overlayTag.GetInfo()
	if info == nil {
		t.Fatal("Overlay tag (60xx3000) not in dictionary")
	}

	// Check it matched the repeater pattern
	if info.Name != "Overlay Data" {
		t.Errorf("Expected name 'Overlay Data', got '%s'", info.Name)
	}

	// Test another overlay group
	overlayTag2 := tag.New(0x6010, 0x3000)
	info2 := overlayTag2.GetInfo()
	if info2 == nil {
		t.Fatal("Overlay tag (60xx3000) group 0x6010 not matched")
	}

	if info2.Name != "Overlay Data" {
		t.Errorf("Expected name 'Overlay Data', got '%s'", info2.Name)
	}
}

// TestDictionaryRetiredTag verifies retired tag handling
func TestDictionaryRetiredTag(t *testing.T) {
	// Command Length to End (0x0000,0x0001) - retired
	retiredTag := tag.New(0x0000, 0x0001)

	info := retiredTag.GetInfo()
	if info == nil {
		t.Fatal("Retired tag not in dictionary")
	}

	// Check retirement status
	if !info.Retired {
		t.Error("Command Length to End should be marked as retired")
	}

	if !retiredTag.IsRetired() {
		t.Error("IsRetired should return true for retired tags")
	}
}

// TestDictionaryGetByKeyword verifies reverse lookup
func TestDictionaryGetByKeyword(t *testing.T) {
	dict := tag.GlobalDictionary()

	// Look up by keyword
	patientNameTag := dict.GetByKeyword("PatientName")
	expected := tag.New(0x0010, 0x0010)

	if patientNameTag != expected {
		t.Errorf("GetByKeyword failed: expected %s, got %s", expected, patientNameTag)
	}

	// Get info using retrieved tag
	info := patientNameTag.GetInfo()
	if info == nil {
		t.Fatal("Retrieved tag not in dictionary")
	}

	if info.Keyword != "PatientName" {
		t.Errorf("Expected keyword 'PatientName', got '%s'", info.Keyword)
	}
}

// TestDictionarySize verifies we have expected number of tags
func TestDictionarySize(t *testing.T) {
	dict := tag.GlobalDictionary()

	// Count standard tags
	count := 0
	for _, info := range tag.StandardDicomDictionary {
		if info != nil {
			count++
		}
	}

	// Should have ~5,182 tags
	if count < 5000 {
		t.Errorf("Expected at least 5000 standard tags, got %d", count)
	}

	// Check repeaters exist
	if len(tag.RepeaterDictionary) == 0 {
		t.Error("Repeater dictionary is empty")
	}

	// Should have ~88 repeater patterns
	if len(tag.RepeaterDictionary) < 80 {
		t.Errorf("Expected at least 80 repeater patterns, got %d", len(tag.RepeaterDictionary))
	}

	_ = dict // Use dict to avoid unused variable
}

// TestDictionaryUnknownTag verifies behavior for unknown tags
func TestDictionaryUnknownTag(t *testing.T) {
	// Create a potentially unknown tag
	unknownTag := tag.New(0x9999, 0x9999)

	// GetInfo should return nil for unknown tags
	if unknownTag.GetInfo() != nil && !unknownTag.IsPrivate() {
		// Private tags might exist, so only check non-private
		t.Error("GetInfo should return nil for unknown non-private tags")
	}

	// GetName should return empty string
	name := unknownTag.GetName()
	if name != "" && !unknownTag.IsPrivate() {
		t.Errorf("GetName should return empty string for unknown tags, got '%s'", name)
	}

	// Exists should return false
	if unknownTag.Exists() && !unknownTag.IsPrivate() {
		t.Error("Exists should return false for unknown non-private tags")
	}
}
