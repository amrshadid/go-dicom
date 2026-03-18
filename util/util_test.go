package util_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/util"
)

func TestHex2Bytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []byte
		wantErr bool
	}{
		{
			name:  "simple hex string",
			input: "48656c6c6f",
			want:  []byte("Hello"),
		},
		{
			name:  "hex with spaces",
			input: "48 65 6c 6c 6f",
			want:  []byte("Hello"),
		},
		{
			name:  "hex with newlines",
			input: "48 65\n6c 6c\n6f",
			want:  []byte("Hello"),
		},
		{
			name:  "hex with tabs",
			input: "48\t65\t6c\t6c\t6f",
			want:  []byte("Hello"),
		},
		{
			name:  "mixed whitespace",
			input: "48 65\n6c\t6c 6f",
			want:  []byte("Hello"),
		},
		{
			name:  "DICOM tag bytes",
			input: "08 00 32 10",
			want:  []byte{0x08, 0x00, 0x32, 0x10},
		},
		{
			name:    "invalid hex",
			input:   "ZZ",
			wantErr: true,
		},
		{
			name:  "empty string",
			input: "",
			want:  []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := util.Hex2Bytes(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Hex2Bytes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Hex2Bytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBytes2Hex(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			name:  "simple bytes",
			input: []byte("Hello"),
			want:  "48 65 6c 6c 6f",
		},
		{
			name:  "DICOM tag bytes",
			input: []byte{0x08, 0x00, 0x32, 0x10},
			want:  "08 00 32 10",
		},
		{
			name:  "single byte",
			input: []byte{0xFF},
			want:  "ff",
		},
		{
			name:  "empty bytes",
			input: []byte{},
			want:  "",
		},
		{
			name:  "null bytes",
			input: []byte{0x00, 0x00},
			want:  "00 00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.Bytes2Hex(tt.input)
			if got != tt.want {
				t.Errorf("Bytes2Hex() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPrintCharacter(t *testing.T) {
	tests := []struct {
		name     string
		input    byte
		expected string
	}{
		{
			name:     "printable letter",
			input:    'A',
			expected: "A",
		},
		{
			name:     "printable digit",
			input:    '5',
			expected: "5",
		},
		{
			name:     "printable space",
			input:    ' ',
			expected: " ",
		},
		{
			name:     "backslash special case",
			input:    92, // '\'
			expected: ".",
		},
		{
			name:     "control character",
			input:    0x01,
			expected: ".",
		},
		{
			name:     "high byte",
			input:    0xFF,
			expected: ".",
		},
		{
			name:     "DEL character",
			input:    127,
			expected: ".",
		},
		{
			name:     "printable punctuation",
			input:    '!',
			expected: "!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.PrintCharacter(tt.input)
			if got != tt.expected {
				t.Errorf("PrintCharacter(%d) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHexDump(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		startAddress  int
		stopAddress   int
		expectedLines int
	}{
		{
			name:          "simple data",
			data:          []byte("Hello World"),
			startAddress:  0,
			stopAddress:   0,
			expectedLines: 1,
		},
		{
			name:          "16 bytes",
			data:          []byte("1234567890123456"),
			startAddress:  0,
			stopAddress:   0,
			expectedLines: 1,
		},
		{
			name:          "32 bytes",
			data:          []byte("12345678901234567890123456789012"),
			startAddress:  0,
			stopAddress:   0,
			expectedLines: 2,
		},
		{
			name:          "partial line",
			data:          []byte("ABC"),
			startAddress:  0,
			stopAddress:   0,
			expectedLines: 1,
		},
		{
			name:          "custom start address",
			data:          []byte("Test"),
			startAddress:  0x1000,
			stopAddress:   0,
			expectedLines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := util.HexDump(tt.data, tt.startAddress, tt.stopAddress)
			lines := strings.Split(got, "\n")
			// Filter out empty lines
			nonEmpty := 0
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					nonEmpty++
				}
			}
			if nonEmpty != tt.expectedLines {
				t.Errorf("HexDump produced %d lines, want %d\nOutput:\n%s", nonEmpty, tt.expectedLines, got)
			}
		})
	}
}

func TestHexDumpReader(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		startAddress  int
		stopAddress   int
		showAddress   bool
		expectedLines int
	}{
		{
			name:          "with address",
			data:          []byte("Test Data"),
			startAddress:  0,
			stopAddress:   0,
			showAddress:   true,
			expectedLines: 1,
		},
		{
			name:          "without address",
			data:          []byte("Test Data"),
			startAddress:  0,
			stopAddress:   0,
			showAddress:   false,
			expectedLines: 1,
		},
		{
			name:          "multiple lines with address",
			data:          []byte("12345678901234567890123456789012"),
			startAddress:  0,
			stopAddress:   0,
			showAddress:   true,
			expectedLines: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.data)
			got := util.HexDumpReader(reader, tt.startAddress, tt.stopAddress, tt.showAddress)
			lines := strings.Split(got, "\n")
			nonEmpty := 0
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					nonEmpty++
				}
			}
			if nonEmpty != tt.expectedLines {
				t.Errorf("HexDumpReader produced %d lines, want %d", nonEmpty, tt.expectedLines)
			}
		})
	}
}

func TestGetDatasetInfo(t *testing.T) {
	// Create a test dataset
	ds := dataset.NewDataset()

	// Add some test elements
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe^John")))                  // Patient Name
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345")))                     // Patient ID
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0020), dataelem.DA, []byte("20231015")))                  // Study Date
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0060), dataelem.CS, []byte("CT")))                        // Modality
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0011), dataelem.IS, []byte("1")))                         // Series Number
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.IS, []byte("1")))                         // Instance Number
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0016), dataelem.UI, []byte("1.2.840.10008.5.1.4.1.1.2"))) // SOP Class UID
	ds.Add(dataelem.NewDataElement(tag.New(0x0008, 0x0018), dataelem.UI, []byte("1.2.3.4.5")))                 // SOP Instance UID

	info, err := util.GetDatasetInfo(ds)
	if err != nil {
		t.Fatalf("GetDatasetInfo() error = %v", err)
	}

	tests := []struct {
		name  string
		field string
		got   interface{}
		want  interface{}
	}{
		{"PatientName", "PatientName", info.PatientName, "Doe^John"},
		{"PatientID", "PatientID", info.PatientID, "12345"},
		{"StudyDate", "StudyDate", info.StudyDate, "20231015"},
		{"Modality", "Modality", info.Modality, "CT"},
		{"SeriesNumber", "SeriesNumber", info.SeriesNumber, 1},
		{"InstanceNumber", "InstanceNumber", info.InstanceNumber, 1},
		{"SOPClassUID", "SOPClassUID", info.SOPClassUID, "1.2.840.10008.5.1.4.1.1.2"},
		{"SOPInstanceUID", "SOPInstanceUID", info.SOPInstanceUID, "1.2.3.4.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("GetDatasetInfo() %s = %v, want %v", tt.field, tt.got, tt.want)
			}
		})
	}
}

func TestGetDatasetInfoEmpty(t *testing.T) {
	// Create empty dataset
	ds := dataset.NewDataset()

	info, err := util.GetDatasetInfo(ds)
	if err != nil {
		t.Fatalf("GetDatasetInfo() error = %v", err)
	}

	// All fields should be empty/zero
	if info.PatientName != "" || info.PatientID != "" || info.StudyDate != "" {
		t.Errorf("GetDatasetInfo() should have empty string fields for empty dataset")
	}
	if info.SeriesNumber != 0 || info.InstanceNumber != 0 {
		t.Errorf("GetDatasetInfo() should have zero int fields for empty dataset")
	}
}

func TestPrintDatasetInfo(t *testing.T) {
	// Test that PrintDatasetInfo doesn't panic and produces output
	info := &util.DatasetInfo{
		PatientName:    "Test^Patient",
		PatientID:      "PID123",
		StudyDate:      "20231015",
		Modality:       "CT",
		SeriesNumber:   1,
		InstanceNumber: 1,
		Rows:           512,
		Columns:        512,
	}

	// This function prints to stdout, so we're just checking it doesn't panic
	// In a real test, you'd capture stdout
	util.PrintDatasetInfo(info)
}

func TestDumpDataset(t *testing.T) {
	ds := dataset.NewDataset()

	// Add some test elements
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Test^Patient")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345")))

	dump := util.DumpDataset(ds)

	// Check that dump contains expected content
	if !strings.Contains(dump, "DICOM Dataset Dump") {
		t.Errorf("DumpDataset() should contain header, got: %s", dump)
	}
	if !strings.Contains(dump, "(0010,0010)") {
		t.Errorf("DumpDataset() should contain patient name tag")
	}
	if !strings.Contains(dump, "Test^Patient") {
		t.Errorf("DumpDataset() should contain patient name value")
	}
}

func TestPrettyPrint(t *testing.T) {
	ds := dataset.NewDataset()

	// Add some test elements
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Test^Patient")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0020), dataelem.LO, []byte("12345")))

	// PrettyPrint outputs to stdout, so we just check it doesn't panic
	// For a full test, you'd capture stdout
	util.PrettyPrint(ds, 0)
}

func TestDefaultFixSeparatorConfig(t *testing.T) {
	config := util.DefaultFixSeparatorConfig()

	if config.InvalidSeparator != ' ' {
		t.Errorf("DefaultFixSeparatorConfig() InvalidSeparator = %c, want ' '", config.InvalidSeparator)
	}

	if len(config.ForVRs) != 2 {
		t.Errorf("DefaultFixSeparatorConfig() ForVRs length = %d, want 2", len(config.ForVRs))
	}

	if config.ForVRs[0] != "DS" || config.ForVRs[1] != "IS" {
		t.Errorf("DefaultFixSeparatorConfig() ForVRs = %v, want [DS IS]", config.ForVRs)
	}

	if !config.ProcessUnknownVRs {
		t.Errorf("DefaultFixSeparatorConfig() ProcessUnknownVRs = %v, want true", config.ProcessUnknownVRs)
	}
}

func TestFixSeparatorConfig(t *testing.T) {
	config := &util.FixSeparatorConfig{
		InvalidSeparator:  ' ',
		ForVRs:            []string{"DS", "IS", "LO"},
		ProcessUnknownVRs: false,
	}

	if config.InvalidSeparator != ' ' {
		t.Errorf("FixSeparatorConfig InvalidSeparator = %c, want ' '", config.InvalidSeparator)
	}

	if len(config.ForVRs) != 3 {
		t.Errorf("FixSeparatorConfig ForVRs length = %d, want 3", len(config.ForVRs))
	}

	if config.ProcessUnknownVRs {
		t.Errorf("FixSeparatorConfig ProcessUnknownVRs = %v, want false", config.ProcessUnknownVRs)
	}
}

func TestHex2BytesAndBytes2HexRoundTrip(t *testing.T) {
	original := "48 65 6c 6c 6f"

	bytes, err := util.Hex2Bytes(original)
	if err != nil {
		t.Fatalf("Hex2Bytes() error = %v", err)
	}

	result := util.Bytes2Hex(bytes)

	// Both should represent the same data (spaces may differ)
	originalClean := strings.ReplaceAll(original, " ", "")
	resultClean := strings.ReplaceAll(result, " ", "")

	if originalClean != resultClean {
		t.Errorf("Round trip failed: %s -> %s -> %s", original, bytes, result)
	}
}

func TestDatasetWithValueRep(t *testing.T) {
	ds := dataset.NewDataset()

	// Create elements with proper value representation
	elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Test^Patient"))
	ds.Add(elem)

	// Test GetDatasetInfo with proper element
	info, err := util.GetDatasetInfo(ds)
	if err != nil {
		t.Fatalf("GetDatasetInfo() error = %v", err)
	}

	if info.PatientName != "Test^Patient" {
		t.Errorf("Expected 'Test^Patient', got '%s'", info.PatientName)
	}
}

func TestHexDumpFormat(t *testing.T) {
	// Test that hex dump has proper formatting
	data := []byte("0123456789ABCDEF")
	dump := util.HexDump(data, 0, 0)

	// Should contain hex values
	if !strings.Contains(dump, "30") { // ASCII '0' = 0x30
		t.Errorf("HexDump should contain hex representation")
	}

	// Should contain ASCII representation
	if !strings.Contains(dump, "|0123456789ABCDEF|") {
		t.Errorf("HexDump should contain ASCII representation")
	}
}

func TestDatasetInfoStructFields(t *testing.T) {
	info := &util.DatasetInfo{
		PatientName:    "Test",
		PatientID:      "123",
		StudyDate:      "20231015",
		Modality:       "CT",
		SeriesNumber:   1,
		InstanceNumber: 2,
		Rows:           512,
		Columns:        512,
		BitsAllocated:  16,
		BitsStored:     12,
		SOPClassUID:    "1.2.3.4",
		SOPInstanceUID: "1.2.3.4.5",
	}

	// Verify all fields are accessible and have correct types
	if info.PatientName != "Test" {
		t.Error("PatientName field issue")
	}
	if info.SeriesNumber != 1 {
		t.Error("SeriesNumber field issue")
	}
	if info.Rows != 512 {
		t.Error("Rows field issue")
	}
}

func TestFixSeparatorBasic(t *testing.T) {
	// Create a dataset with space-separated values
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, []byte("100 200 300")))

	// Fix separators
	config := util.DefaultFixSeparatorConfig()
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	// Check the fixed value
	if elem, ok := fixedDS.Get(tag.New(0x0028, 0x1050)); ok {
		value := extractString(elem.GetValue())
		expected := "100\\200\\300"
		if value != expected {
			t.Errorf("FixSeparator() = %q, want %q", value, expected)
		}
	} else {
		t.Error("FixSeparator() did not preserve element")
	}
}

func TestFixSeparatorStringValue(t *testing.T) {
	ds := dataset.NewDataset()
	// Add element with space-separated integer string
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.IS, []byte("1 2 3")))

	config := util.DefaultFixSeparatorConfig()
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	if elem, ok := fixedDS.Get(tag.New(0x0020, 0x0013)); ok {
		value := extractString(elem.GetValue())
		if !strings.Contains(value, "\\") {
			t.Errorf("FixSeparator() should replace spaces with backslashes, got %q", value)
		}
	}
}

func TestFixSeparatorPreserveCorrectFormat(t *testing.T) {
	ds := dataset.NewDataset()
	// Add element that already has correct separator
	ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, []byte("100\\200\\300")))

	config := util.DefaultFixSeparatorConfig()
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	if elem, ok := fixedDS.Get(tag.New(0x0028, 0x1050)); ok {
		value := extractString(elem.GetValue())
		expected := "100\\200\\300"
		if value != expected {
			t.Errorf("FixSeparator() should preserve correct format, got %q", value)
		}
	}
}

func TestFixSeparatorIgnoreUnspecifiedVR(t *testing.T) {
	ds := dataset.NewDataset()
	// Add element with VR not in config
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("Doe John Smith")))

	config := &util.FixSeparatorConfig{
		InvalidSeparator:  ' ',
		ForVRs:            []string{"DS", "IS"}, // PN not included
		ProcessUnknownVRs: false,
	}
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	if elem, ok := fixedDS.Get(tag.New(0x0010, 0x0010)); ok {
		value := extractString(elem.GetValue())
		expected := "Doe John Smith" // Should not be modified
		if value != expected {
			t.Errorf("FixSeparator() should not modify unspecified VR, got %q", value)
		}
	}
}

func TestFixSeparatorMultipleElements(t *testing.T) {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, []byte("100 200")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0020, 0x0013), dataelem.IS, []byte("1 2")))
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("John Doe")))

	config := util.DefaultFixSeparatorConfig()
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	// Check DS element
	if elem, ok := fixedDS.Get(tag.New(0x0028, 0x1050)); ok {
		value := extractString(elem.GetValue())
		if !strings.Contains(value, "\\") {
			t.Error("FixSeparator() should fix DS element")
		}
	}

	// Check IS element
	if elem, ok := fixedDS.Get(tag.New(0x0020, 0x0013)); ok {
		value := extractString(elem.GetValue())
		if !strings.Contains(value, "\\") {
			t.Error("FixSeparator() should fix IS element")
		}
	}

	// Check PN element (should not be fixed by default)
	if elem, ok := fixedDS.Get(tag.New(0x0010, 0x0010)); ok {
		value := extractString(elem.GetValue())
		if strings.Contains(value, "\\") && !strings.Contains("John Doe", "\\") {
			t.Error("FixSeparator() should not fix PN element")
		}
	}
}

func TestFixSeparatorNilInputs(t *testing.T) {
	tests := []struct {
		name string
		ds   *dataset.Dataset
		cfg  *util.FixSeparatorConfig
	}{
		{"nil dataset", nil, util.DefaultFixSeparatorConfig()},
		{"nil config", dataset.NewDataset(), nil},
		{"both nil", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := util.FixSeparator(tt.ds, tt.cfg)
			if err != nil {
				t.Errorf("FixSeparator() with nil inputs should not error: %v", err)
			}
			// Should return the input or nil
			if tt.ds != nil && result == nil {
				t.Error("FixSeparator() should return a dataset")
			}
		})
	}
}

func TestFixSeparatorEmptyDataset(t *testing.T) {
	ds := dataset.NewDataset()
	config := util.DefaultFixSeparatorConfig()

	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	tags := fixedDS.Tags()
	if len(tags) != 0 {
		t.Errorf("FixSeparator() on empty dataset should return empty dataset, got %d tags", len(tags))
	}
}

func TestFixSeparatorCustomSeparator(t *testing.T) {
	ds := dataset.NewDataset()
	// Use comma as the invalid separator
	ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, []byte("100,200,300")))

	config := &util.FixSeparatorConfig{
		InvalidSeparator:  ',',
		ForVRs:            []string{"DS"},
		ProcessUnknownVRs: false,
	}
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	if elem, ok := fixedDS.Get(tag.New(0x0028, 0x1050)); ok {
		value := extractString(elem.GetValue())
		expected := "100\\200\\300"
		if value != expected {
			t.Errorf("FixSeparator() with custom separator = %q, want %q", value, expected)
		}
	}
}

func TestFixSeparatorByteArrayValue(t *testing.T) {
	ds := dataset.NewDataset()
	// Test with byte array value
	ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, []byte("10 20 30")))

	config := util.DefaultFixSeparatorConfig()
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	if elem, ok := fixedDS.Get(tag.New(0x0028, 0x1050)); ok {
		value := elem.GetValue()
		switch v := value.(type) {
		case []byte:
			if !strings.Contains(string(v), "\\") {
				t.Error("FixSeparator() should convert byte array separators")
			}
		default:
			// Value might have been converted to string, which is also acceptable
			strValue := extractString(value)
			if !strings.Contains(strValue, "\\") {
				t.Error("FixSeparator() should convert separators")
			}
		}
	}
}

func TestFixSeparatorDoesNotModifyOriginal(t *testing.T) {
	ds := dataset.NewDataset()
	ds.Add(dataelem.NewDataElement(tag.New(0x0028, 0x1050), dataelem.DS, []byte("100 200")))

	config := util.DefaultFixSeparatorConfig()
	fixedDS, err := util.FixSeparator(ds, config)
	if err != nil {
		t.Fatalf("FixSeparator() error = %v", err)
	}

	// Check original is unchanged
	if elem, ok := ds.Get(tag.New(0x0028, 0x1050)); ok {
		value := extractString(elem.GetValue())
		if strings.Contains(value, "\\") {
			t.Error("FixSeparator() should not modify original dataset")
		}
	}

	// Check fixed is changed
	if elem, ok := fixedDS.Get(tag.New(0x0028, 0x1050)); ok {
		value := extractString(elem.GetValue())
		if !strings.Contains(value, "\\") {
			t.Error("FixSeparator() should create fixed dataset")
		}
	}
}

func TestFixSeparatorProcessUnknownVRs(t *testing.T) {
	ds := dataset.NewDataset()
	// Create a dataset element with known tag but test with UnknownVRs flag
	// The key is to test when ForVRs list is empty and no matching VRs
	ds.Add(dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, []byte("value1 value2")))

	tests := []struct {
		name       string
		processUnk bool
		forVRs     []string
		shouldFix  bool
	}{
		{"process unknown with empty VR list", true, []string{}, true},
		{"skip unknown with empty VR list", false, []string{}, false},
		{"explicit VR DS should always fix", false, []string{"DS"}, false}, // PN not in list
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &util.FixSeparatorConfig{
				InvalidSeparator:  ' ',
				ForVRs:            tt.forVRs,
				ProcessUnknownVRs: tt.processUnk,
			}
			fixedDS, err := util.FixSeparator(ds, config)
			if err != nil {
				t.Fatalf("FixSeparator() error = %v", err)
			}

			if elem, ok := fixedDS.Get(tag.New(0x0010, 0x0010)); ok {
				value := extractString(elem.GetValue())
				hasBackslash := strings.Contains(value, "\\")

				// Only check if it was fixed based on processUnk when ForVRs is empty
				if len(tt.forVRs) == 0 {
					if tt.shouldFix && !hasBackslash {
						t.Errorf("FixSeparator() should process VRs when ProcessUnknownVRs=%v, got %q", tt.processUnk, value)
					}
					if !tt.shouldFix && hasBackslash {
						t.Errorf("FixSeparator() should not process VRs when ProcessUnknownVRs=%v, got %q", tt.processUnk, value)
					}
				}
			}
		})
	}
}

// extractString helper function for tests
func extractString(elem interface{}) string {
	if elem == nil {
		return ""
	}

	switch v := elem.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}
