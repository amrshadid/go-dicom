package dataset_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// TestGetByKeyword tests retrieving elements by keyword
func TestGetByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	// Add patient name using tag
	patientTag := tag.New(0x0010, 0x0010) // PatientName
	elem := dataelem.NewDataElement(patientTag, dataelem.PN, []byte("John Doe"))
	ds.Add(elem)

	// Retrieve using keyword
	retrieved, exists := ds.GetByKeyword("PatientName")
	if !exists {
		t.Fatal("GetByKeyword() returned false for existing element")
	}

	if retrieved == nil {
		t.Fatal("GetByKeyword() returned nil")
	}

	value := retrieved.GetValue()
	if b, ok := value.([]byte); ok {
		if string(b) != "John Doe" {
			t.Errorf("Value = %q, want \"John Doe\"", string(b))
		}
	}
}

// TestGetByKeywordNonExistent tests getting non-existent keyword
func TestGetByKeywordNonExistent(t *testing.T) {
	ds := dataset.NewDataset()

	_, exists := ds.GetByKeyword("PatientName")
	if exists {
		t.Error("GetByKeyword() should return false for non-existent element")
	}
}

// TestGetByKeywordInvalidKeyword tests getting with invalid keyword
func TestGetByKeywordInvalidKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	_, exists := ds.GetByKeyword("InvalidKeywordXYZ")
	if exists {
		t.Error("GetByKeyword() should return false for invalid keyword")
	}
}

// TestGetValueByKeyword tests getting raw value by keyword
func TestGetValueByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	patientTag := tag.New(0x0010, 0x0010)
	elem := dataelem.NewDataElement(patientTag, dataelem.PN, []byte("Jane Doe"))
	ds.Add(elem)

	value := ds.GetValueByKeyword("PatientName")
	if value == nil {
		t.Fatal("GetValueByKeyword() returned nil")
	}

	if string(value) != "Jane Doe" {
		t.Errorf("Value = %q, want \"Jane Doe\"", string(value))
	}
}

// TestSetValueByKeyword tests setting value by keyword
func TestSetValueByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	// Set value using keyword
	err := ds.SetValueByKeyword("PatientName", []byte("Alice Smith"))
	if err != nil {
		t.Fatalf("SetValueByKeyword() error = %v", err)
	}

	// Verify value was set
	value := ds.GetValueByKeyword("PatientName")
	if string(value) != "Alice Smith" {
		t.Errorf("Value = %q, want \"Alice Smith\"", string(value))
	}
}

// TestSetValueByKeywordInvalid tests setting with invalid keyword
func TestSetValueByKeywordInvalid(t *testing.T) {
	ds := dataset.NewDataset()

	err := ds.SetValueByKeyword("InvalidKeywordXYZ", []byte("Test"))
	if err == nil {
		t.Error("SetValueByKeyword() should return error for invalid keyword")
	}
}

// TestContainsByKeyword tests checking keyword existence
func TestContainsByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	// Should not exist initially
	if ds.ContainsByKeyword("PatientName") {
		t.Error("ContainsByKeyword() = true for non-existent keyword")
	}

	// Add element
	ds.SetValueByKeyword("PatientName", []byte("Test"))

	// Should exist now
	if !ds.ContainsByKeyword("PatientName") {
		t.Error("ContainsByKeyword() = false for existing keyword")
	}
}

// TestRemoveByKeyword tests removing by keyword
func TestRemoveByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	// Add element
	ds.SetValueByKeyword("PatientID", []byte("12345"))

	// Verify exists
	if !ds.ContainsByKeyword("PatientID") {
		t.Fatal("Element not added")
	}

	// Remove by keyword
	removed := ds.RemoveByKeyword("PatientID")
	if !removed {
		t.Error("RemoveByKeyword() returned false")
	}

	// Verify removed
	if ds.ContainsByKeyword("PatientID") {
		t.Error("Element still exists after removal")
	}
}

// TestRemoveByKeywordInvalid tests removing with invalid keyword
func TestRemoveByKeywordInvalid(t *testing.T) {
	ds := dataset.NewDataset()

	removed := ds.RemoveByKeyword("InvalidKeywordXYZ")
	if removed {
		t.Error("RemoveByKeyword() should return false for invalid keyword")
	}
}

// TestUpdateElementByKeyword tests updating existing element by keyword
func TestUpdateElementByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	// Add initial value
	ds.SetValueByKeyword("PatientName", []byte("Initial Name"))

	// Update value
	err := ds.UpdateElementByKeyword("PatientName", []byte("Updated Name"))
	if err != nil {
		t.Fatalf("UpdateElementByKeyword() error = %v", err)
	}

	// Verify updated
	value := ds.GetValueByKeyword("PatientName")
	if string(value) != "Updated Name" {
		t.Errorf("Value = %q, want \"Updated Name\"", string(value))
	}
}

// TestUpdateElementByKeywordNonExistent tests error when updating non-existent
func TestUpdateElementByKeywordNonExistent(t *testing.T) {
	ds := dataset.NewDataset()

	err := ds.UpdateElementByKeyword("PatientName", []byte("Test"))
	if err == nil {
		t.Error("UpdateElementByKeyword() should return error for non-existent element")
	}
}

// TestAddByKeyword tests adding element by keyword
func TestAddByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	err := ds.AddByKeyword("PatientName", dataelem.PN, []byte("Test Patient"))
	if err != nil {
		t.Fatalf("AddByKeyword() error = %v", err)
	}

	// Verify added
	if !ds.ContainsByKeyword("PatientName") {
		t.Error("Element not added")
	}

	value := ds.GetValueByKeyword("PatientName")
	if string(value) != "Test Patient" {
		t.Errorf("Value = %q, want \"Test Patient\"", string(value))
	}
}

// TestAddByKeywordInvalid tests adding with invalid keyword
func TestAddByKeywordInvalid(t *testing.T) {
	ds := dataset.NewDataset()

	err := ds.AddByKeyword("InvalidKeywordXYZ", dataelem.LO, []byte("Test"))
	if err == nil {
		t.Error("AddByKeyword() should return error for invalid keyword")
	}
}

// TestGetKeywordInfo tests getting keyword information
func TestGetKeywordInfo(t *testing.T) {
	ds := dataset.NewDataset()

	info := ds.GetKeywordInfo("PatientName")
	if info == nil {
		t.Fatal("GetKeywordInfo() returned nil for valid keyword")
	}

	if info.VR != "PN" {
		t.Errorf("VR = %q, want \"PN\"", info.VR)
	}

	if info.Name != "Patient's Name" {
		t.Errorf("Name = %q, want \"Patient's Name\"", info.Name)
	}
}

// TestGetKeywordInfoInvalid tests getting info for invalid keyword
func TestGetKeywordInfoInvalid(t *testing.T) {
	ds := dataset.NewDataset()

	info := ds.GetKeywordInfo("InvalidKeywordXYZ")
	if info != nil {
		t.Error("GetKeywordInfo() should return nil for invalid keyword")
	}
}

// TestGetElementsByKeywords tests getting multiple elements by keywords
func TestGetElementsByKeywords(t *testing.T) {
	ds := dataset.NewDataset()

	// Add multiple elements
	ds.SetValueByKeyword("PatientName", []byte("John Doe"))
	ds.SetValueByKeyword("PatientID", []byte("12345"))
	ds.SetValueByKeyword("StudyDate", []byte("20231225"))

	// Get by keywords
	elems := ds.GetElementsByKeywords("PatientName", "PatientID", "StudyDate")

	if len(elems) != 3 {
		t.Errorf("GetElementsByKeywords() returned %d elements, want 3", len(elems))
	}
}

// TestGetElementsByKeywordsPartial tests with some invalid keywords
func TestGetElementsByKeywordsPartial(t *testing.T) {
	ds := dataset.NewDataset()

	ds.SetValueByKeyword("PatientName", []byte("John Doe"))

	// Request valid and invalid keywords
	elems := ds.GetElementsByKeywords("PatientName", "InvalidKeyword", "PatientID")

	// Should only return valid, existing elements
	if len(elems) != 1 {
		t.Errorf("GetElementsByKeywords() returned %d elements, want 1", len(elems))
	}
}

// TestRemoveElementsByKeywords tests removing multiple elements
func TestRemoveElementsByKeywords(t *testing.T) {
	ds := dataset.NewDataset()

	// Add elements
	ds.SetValueByKeyword("PatientName", []byte("John"))
	ds.SetValueByKeyword("PatientID", []byte("123"))
	ds.SetValueByKeyword("StudyDate", []byte("20231225"))

	// Remove two of them
	count := ds.RemoveElementsByKeywords("PatientName", "PatientID")

	if count != 2 {
		t.Errorf("RemoveElementsByKeywords() removed %d elements, want 2", count)
	}

	// Verify removed
	if ds.ContainsByKeyword("PatientName") || ds.ContainsByKeyword("PatientID") {
		t.Error("Elements should be removed")
	}

	// Verify third still exists
	if !ds.ContainsByKeyword("StudyDate") {
		t.Error("StudyDate should still exist")
	}
}

// TestGetStringByKeyword tests getting string values
func TestGetStringByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	// Add value with trailing nulls and spaces
	ds.SetValueByKeyword("PatientName", []byte("John Doe\x00\x00  "))

	str := ds.GetStringByKeyword("PatientName")
	if str != "John Doe" {
		t.Errorf("GetStringByKeyword() = %q, want \"John Doe\"", str)
	}
}

// TestGetStringByKeywordEmpty tests getting non-existent string
func TestGetStringByKeywordEmpty(t *testing.T) {
	ds := dataset.NewDataset()

	str := ds.GetStringByKeyword("PatientName")
	if str != "" {
		t.Errorf("GetStringByKeyword() = %q, want empty string", str)
	}
}

// TestSetStringByKeyword tests setting string values
func TestSetStringByKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	err := ds.SetStringByKeyword("PatientName", "Jane Smith")
	if err != nil {
		t.Fatalf("SetStringByKeyword() error = %v", err)
	}

	str := ds.GetStringByKeyword("PatientName")
	if str != "Jane Smith" {
		t.Errorf("GetStringByKeyword() = %q, want \"Jane Smith\"", str)
	}
}

// TestHasKeyword tests alias for ContainsByKeyword
func TestHasKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	if ds.HasKeyword("PatientName") {
		t.Error("HasKeyword() = true for non-existent element")
	}

	ds.SetValueByKeyword("PatientName", []byte("Test"))

	if !ds.HasKeyword("PatientName") {
		t.Error("HasKeyword() = false for existing element")
	}
}

// TestKeywordToTag tests keyword to tag conversion
func TestKeywordToTag(t *testing.T) {
	ds := dataset.NewDataset()

	patientNameTag := ds.KeywordToTag("PatientName")
	expectedTag := tag.New(0x0010, 0x0010)

	if patientNameTag != expectedTag {
		t.Errorf("KeywordToTag() = %s, want %s", patientNameTag.String(), expectedTag.String())
	}
}

// TestKeywordToTagInvalid tests invalid keyword conversion
func TestKeywordToTagInvalid(t *testing.T) {
	ds := dataset.NewDataset()

	invalidTag := ds.KeywordToTag("InvalidKeywordXYZ")
	if invalidTag != 0 {
		t.Errorf("KeywordToTag() = %d, want 0", invalidTag)
	}
}

// TestTagToKeyword tests tag to keyword conversion
func TestTagToKeyword(t *testing.T) {
	ds := dataset.NewDataset()

	patientNameTag := tag.New(0x0010, 0x0010)
	keyword := ds.TagToKeyword(patientNameTag)

	if keyword != "PatientName" {
		t.Errorf("TagToKeyword() = %q, want \"PatientName\"", keyword)
	}
}

// TestTagToKeywordInvalid tests invalid tag conversion
func TestTagToKeywordInvalid(t *testing.T) {
	ds := dataset.NewDataset()

	// Use a private tag (no keyword)
	privateTag := tag.New(0x0009, 0x1001)
	keyword := ds.TagToKeyword(privateTag)

	if keyword != "" {
		t.Errorf("TagToKeyword() = %q, want empty string for private tag", keyword)
	}
}

// TestGetByKeywordWithDefault tests getting value with default
func TestGetByKeywordWithDefault(t *testing.T) {
	ds := dataset.NewDataset()

	// Non-existent keyword should return default
	value := ds.GetByKeywordWithDefault("PatientName", []byte("Default"))
	if string(value) != "Default" {
		t.Errorf("Value = %q, want \"Default\"", string(value))
	}

	// Existing keyword should return actual value
	ds.SetValueByKeyword("PatientName", []byte("Actual"))
	value = ds.GetByKeywordWithDefault("PatientName", []byte("Default"))
	if string(value) != "Actual" {
		t.Errorf("Value = %q, want \"Actual\"", string(value))
	}
}

// TestGetStringByKeywordWithDefault tests getting string with default
func TestGetStringByKeywordWithDefault(t *testing.T) {
	ds := dataset.NewDataset()

	// Non-existent should return default
	str := ds.GetStringByKeywordWithDefault("PatientName", "Unknown")
	if str != "Unknown" {
		t.Errorf("Value = %q, want \"Unknown\"", str)
	}

	// Existing should return actual
	ds.SetStringByKeyword("PatientName", "Known")
	str = ds.GetStringByKeywordWithDefault("PatientName", "Unknown")
	if str != "Known" {
		t.Errorf("Value = %q, want \"Known\"", str)
	}
}

// TestKeywordAccessRealistic tests realistic keyword usage scenario
func TestKeywordAccessRealistic(t *testing.T) {
	ds := dataset.NewDataset()

	// Simulate creating a DICOM dataset using keywords
	ds.SetStringByKeyword("PatientName", "John^Doe")
	ds.SetStringByKeyword("PatientID", "123456")
	ds.SetStringByKeyword("PatientBirthDate", "19800115")
	ds.SetStringByKeyword("PatientSex", "M")
	ds.SetStringByKeyword("StudyDate", "20231225")
	ds.SetStringByKeyword("StudyTime", "143022")
	ds.SetStringByKeyword("Modality", "CT")

	// Verify all values
	tests := []struct {
		keyword string
		want    string
	}{
		{"PatientName", "John^Doe"},
		{"PatientID", "123456"},
		{"PatientBirthDate", "19800115"},
		{"PatientSex", "M"},
		{"StudyDate", "20231225"},
		{"StudyTime", "143022"},
		{"Modality", "CT"},
	}

	for _, tt := range tests {
		got := ds.GetStringByKeyword(tt.keyword)
		if got != tt.want {
			t.Errorf("GetStringByKeyword(%q) = %q, want %q", tt.keyword, got, tt.want)
		}
	}

	// Check keyword info
	info := ds.GetKeywordInfo("Modality")
	if info == nil {
		t.Fatal("GetKeywordInfo() returned nil for Modality")
	}
	if info.VR != "CS" {
		t.Errorf("Modality VR = %q, want \"CS\"", info.VR)
	}

	// Remove some elements
	count := ds.RemoveElementsByKeywords("PatientBirthDate", "PatientSex")
	if count != 2 {
		t.Errorf("Removed %d elements, want 2", count)
	}

	// Verify remaining
	if ds.Length() != 5 {
		t.Errorf("Dataset length = %d, want 5", ds.Length())
	}
}
