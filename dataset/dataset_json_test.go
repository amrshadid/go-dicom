package dataset_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// TestMarshalJSON tests JSON marshaling
func TestMarshalJSON(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "JSON Test")
	ds.SetStringByKeyword("PatientID", "98765")

	jsonBytes, err := ds.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("MarshalJSON() returned empty bytes")
	}

	// Verify it's valid JSON
	var jsonObj map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &jsonObj); err != nil {
		t.Errorf("MarshalJSON() produced invalid JSON: %v", err)
	}
}

// TestUnmarshalJSON tests JSON unmarshaling
func TestUnmarshalJSON(t *testing.T) {
	// Create original dataset
	original := dataset.NewDataset()
	original.SetStringByKeyword("PatientName", "Unmarshal Test")
	original.SetStringByKeyword("PatientID", "11111")

	// Marshal to JSON
	jsonBytes, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Unmarshal to new dataset
	restored := dataset.NewDataset()
	if err := restored.UnmarshalJSON(jsonBytes); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	// Verify data
	if restored.GetStringByKeyword("PatientName") != "Unmarshal Test" {
		t.Error("Unmarshaled dataset doesn't match original")
	}
}

// TestToJSONPretty tests pretty JSON output
func TestToJSONPretty(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Pretty JSON")

	jsonStr, err := ds.ToJSONPretty()
	if err != nil {
		t.Fatalf("ToJSONPretty() error = %v", err)
	}

	// Should have indentation
	if !strings.Contains(jsonStr, "  ") {
		t.Error("ToJSONPretty() should have indentation")
	}
}

// TestJSONWithSequences tests JSON with sequences
func TestJSONWithSequences(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Seq JSON")

	// Add sequence
	seq := sequence.New()
	child := dataset.NewDataset()
	child.SetStringByKeyword("SeriesNumber", "1")
	seq.Append(child)
	ds.AddSequence(tag.New(0x0008, 0x1115), seq)

	// Marshal
	jsonBytes, err := ds.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Unmarshal
	restored := dataset.NewDataset()
	if err := restored.UnmarshalJSON(jsonBytes); err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	// Verify sequence exists
	if !restored.HasSequence(tag.New(0x0008, 0x1115)) {
		t.Error("Restored dataset should have sequence")
	}
}

// TestJSONExportOptions tests JSON export with options
func TestJSONExportOptions(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Options Test")

	// Add private tag
	pb, _ := ds.PrivateBlock(0x0009, "TEST_CREATOR")
	pb.AddNew(0x01, dataelem.LO, []byte("Private Value"))

	// Export without private tags
	opts := dataset.DefaultJSONExportOptions()
	opts.IncludePrivateTags = false

	jsonBytes, err := ds.ToJSONWithOptions(opts)
	if err != nil {
		t.Fatalf("ToJSONWithOptions() error = %v", err)
	}

	jsonStr := string(jsonBytes)

	// Should not contain private tag
	if strings.Contains(jsonStr, "0009") {
		t.Error("JSON should not contain private tags when IncludePrivateTags=false")
	}
}

// TestFromJSONString tests JSON string parsing
func TestFromJSONString(t *testing.T) {
	// Create simple JSON
	jsonStr := `{
		"elements": {
			"(0010,0010)": {
				"tag": "(0010,0010)",
				"vr": "PN",
				"value": "Test Patient",
				"keyword": "PatientName"
			}
		}
	}`

	ds := dataset.NewDataset()
	err := ds.FromJSONString(jsonStr)
	if err != nil {
		t.Fatalf("FromJSONString() error = %v", err)
	}

	if ds.GetStringByKeyword("PatientName") != "Test Patient" {
		t.Error("FromJSONString() didn't parse correctly")
	}
}

// TestJSONRoundTrip tests complete JSON round-trip
func TestJSONRoundTrip(t *testing.T) {
	// Create original dataset with various data types
	original := dataset.NewDataset()
	original.SetStringByKeyword("PatientName", "Round Trip")
	original.SetStringByKeyword("PatientID", "RT001")
	original.SetStringByKeyword("Modality", "CT")
	original.SetStringByKeyword("StudyDate", "20231225")

	// Add a sequence
	seq := sequence.New()
	child := dataset.NewDataset()
	child.SetStringByKeyword("SeriesNumber", "1")
	seq.Append(child)
	original.AddSequence(tag.New(0x0008, 0x1115), seq)

	// Marshal
	jsonBytes, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	// Unmarshal
	restored := dataset.NewDataset()
	if err := restored.UnmarshalJSON(jsonBytes); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	// Verify all fields
	tests := []struct {
		keyword string
		want    string
	}{
		{"PatientName", "Round Trip"},
		{"PatientID", "RT001"},
		{"Modality", "CT"},
		{"StudyDate", "20231225"},
	}

	for _, tt := range tests {
		got := restored.GetStringByKeyword(tt.keyword)
		if got != tt.want {
			t.Errorf("After round-trip: %s = %q, want %q", tt.keyword, got, tt.want)
		}
	}

	// Verify sequence
	if !restored.HasSequence(tag.New(0x0008, 0x1115)) {
		t.Error("After round-trip: sequence missing")
	}
}

// TestToJSONString tests JSON string conversion
func TestToJSONString(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "String Test")

	jsonStr, err := ds.ToJSONString()
	if err != nil {
		t.Fatalf("ToJSONString() error = %v", err)
	}

	if jsonStr == "" {
		t.Error("ToJSONString() returned empty string")
	}

	// Should be valid JSON
	var jsonObj map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonObj); err != nil {
		t.Errorf("ToJSONString() produced invalid JSON: %v", err)
	}
}

// TestCloneFromJSON tests creating dataset from JSON
func TestCloneFromJSON(t *testing.T) {
	// Create original
	original := dataset.NewDataset()
	original.SetStringByKeyword("PatientName", "Clone Test")

	// Convert to JSON
	jsonBytes, err := original.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Clone from JSON
	cloned, err := dataset.CloneFromJSON(jsonBytes)
	if err != nil {
		t.Fatalf("CloneFromJSON() error = %v", err)
	}

	if cloned.GetStringByKeyword("PatientName") != "Clone Test" {
		t.Error("CloneFromJSON() didn't clone correctly")
	}
}

// TestJSONMetadata tests JSON metadata inclusion
func TestJSONMetadata(t *testing.T) {
	ds := dataset.NewDataset()
	ds.SetStringByKeyword("PatientName", "Metadata Test")

	// Add sequence
	seq := sequence.New()
	ds.AddSequence(tag.New(0x0008, 0x1115), seq)

	jsonDS := ds.ToJSON()

	if jsonDS.Metadata.ElementCount == 0 {
		t.Error("Metadata should include element count")
	}

	if !jsonDS.Metadata.HasSequences {
		t.Error("Metadata should indicate HasSequences=true")
	}
}

// TestDefaultJSONExportOptions tests default JSON options
func TestDefaultJSONExportOptions(t *testing.T) {
	opts := dataset.DefaultJSONExportOptions()

	if opts.IncludePrivateTags {
		t.Error("Default should not include private tags")
	}

	if opts.IncludePixelData {
		t.Error("Default should not include pixel data")
	}

	if !opts.IncludeSequences {
		t.Error("Default should include sequences")
	}
}

// TestPhotometricInterpretation tests photometric interpretation type enum
func TestPhotometricInterpretation(t *testing.T) {
	rgb := dataset.PhotometricRGB
	if string(rgb) != "RGB" {
		t.Errorf("PhotometricRGB = %q, want \"RGB\"", rgb)
	}

	mono2 := dataset.PhotometricMONOCHROME2
	if string(mono2) != "MONOCHROME2" {
		t.Errorf("PhotometricMONOCHROME2 = %q, want \"MONOCHROME2\"", mono2)
	}
}

// TestImageFormat tests image format enum
func TestImageFormat(t *testing.T) {
	png := dataset.FormatPNG
	if png.String() != "PNG" {
		t.Errorf("FormatPNG.String() = %q, want \"PNG\"", png.String())
	}

	if png.FileExtension() != ".png" {
		t.Errorf("FormatPNG.FileExtension() = %q, want \".png\"", png.FileExtension())
	}
}

// TestDefaultExportOptions tests default export options
func TestDefaultExportOptions(t *testing.T) {
	opts := dataset.DefaultExportOptions()

	if opts.Format != dataset.FormatPNG {
		t.Errorf("Default format = %v, want FormatPNG", opts.Format)
	}

	if opts.Quality != 90 {
		t.Errorf("Default quality = %d, want 90", opts.Quality)
	}
}
