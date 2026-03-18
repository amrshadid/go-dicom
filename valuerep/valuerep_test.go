package valuerep_test

import (
	"strings"
	"testing"
	"time"

	"github.com/amrshadid/go-dicom/valuerep"
)

func TestVRMetadata(t *testing.T) {
	tests := []struct {
		vrCode      string
		shouldExist bool
		isString    bool
		isBinary    bool
		maxLength   int
	}{
		{"AE", true, true, false, 16},
		{"PN", true, true, false, 64},
		{"OB", true, false, true, 0},
		{"FD", true, false, true, 8},
		{"XX", false, false, false, 0},
	}

	for _, tt := range tests {
		metadata, exists := valuerep.VRMetadataMap[tt.vrCode]
		if exists != tt.shouldExist {
			t.Errorf("VR %s: expected exists=%v, got %v", tt.vrCode, tt.shouldExist, exists)
		}

		if exists {
			if metadata.IsString != tt.isString {
				t.Errorf("VR %s: expected IsString=%v, got %v", tt.vrCode, tt.isString, metadata.IsString)
			}
			if metadata.IsBinary != tt.isBinary {
				t.Errorf("VR %s: expected IsBinary=%v, got %v", tt.vrCode, tt.isBinary, metadata.IsBinary)
			}
			if metadata.MaxLength != tt.maxLength {
				t.Errorf("VR %s: expected MaxLength=%d, got %d", tt.vrCode, tt.maxLength, metadata.MaxLength)
			}
		}
	}
}

func TestValidateType(t *testing.T) {
	tests := []struct {
		vrCode      string
		value       interface{}
		shouldValid bool
	}{
		{"AE", "ApplicationEntity", true},
		{"AE", []byte("ApplicationEntity"), true},
		{"AE", 123, false},
		{"OB", []byte("binarydata"), true},
		{"OB", "string", false},
		{"FD", []byte{0, 0, 0, 0, 0, 0, 0, 0}, true},
		{"XX", "value", false},
		{"PN", nil, true}, // Nil is acceptable
	}

	for _, tt := range tests {
		valid, _ := valuerep.ValidateType(tt.vrCode, tt.value)
		if valid != tt.shouldValid {
			t.Errorf("ValidateType(%s, %T): expected %v, got %v", tt.vrCode, tt.value, tt.shouldValid, valid)
		}
	}
}

func TestValidateVRLength(t *testing.T) {
	tests := []struct {
		vrCode      string
		value       interface{}
		shouldValid bool
	}{
		{"AE", "Valid", true},
		{"AE", "1234567890123456", true},   // 16 chars = max
		{"AE", "12345678901234567", false}, // 17 chars = over
		{"UC", "VeryLongString....................................................", true}, // UC has no limit
		{"PN", "Smith^John", true},
		{"OB", []byte{0, 1, 2, 3}, true},
	}

	for _, tt := range tests {
		valid, _ := valuerep.ValidateVRLength(tt.vrCode, tt.value)
		if valid != tt.shouldValid {
			t.Errorf("ValidateVRLength(%s, %v): expected %v, got %v", tt.vrCode, tt.value, tt.shouldValid, valid)
		}
	}
}

func TestValidateRegex(t *testing.T) {
	tests := []struct {
		vrCode      string
		value       interface{}
		shouldValid bool
	}{
		{"AE", "ValidAE", true},
		{"CS", "CODE", true},
		{"CS", "code", false}, // Must be uppercase
		{"IS", "12345", true},
		{"IS", "  -123  ", true},
		{"IS", "12.34", false},
		{"DS", "123.45", true},
		{"DS", "  +1.23e-4  ", true},
		{"DA", "20230101", true},
		{"DA", "2023-01-01", false},
		{"UI", "1.2.3.4.5", true},
		{"UI", "1.2.3.4.5.", false},
	}

	for _, tt := range tests {
		valid, _ := valuerep.ValidateRegex(tt.vrCode, tt.value)
		if valid != tt.shouldValid {
			t.Errorf("ValidateRegex(%s, %v): expected %v, got %v", tt.vrCode, tt.value, tt.shouldValid, valid)
		}
	}
}

func TestValidateValue(t *testing.T) {
	tests := []struct {
		vrCode      string
		value       interface{}
		shouldError bool
	}{
		{"AE", "Test", false},
		{"AE", 123, true},
		{"PN", "Smith^John", false},
		{"DS", "  123.45  ", false},
		{"IS", "invalid", true},
		{"DA", "20230101", false},
		{"OB", []byte{0, 1, 2}, false},
		{"XX", "anything", true},
	}

	for _, tt := range tests {
		err := valuerep.ValidateValue(tt.vrCode, tt.value)
		hasError := err != nil
		if hasError != tt.shouldError {
			t.Errorf("ValidateValue(%s, %v): expected error=%v, got %v", tt.vrCode, tt.value, tt.shouldError, err)
		}
	}
}

func TestParsePersonName(t *testing.T) {
	tests := []struct {
		input       string
		expectAlpha string
		expectIdeo  string
		expectPhon  string
	}{
		{"Smith^John", "Smith", "John", ""},
		{"Yamada^Taro^Hanako^Dr.^Jr.", "Yamada", "Taro", "Hanako"},
		{"OnlyFamily", "OnlyFamily", "", ""},
		{"", "", "", ""},
	}

	for _, tt := range tests {
		pn := valuerep.ParsePersonName(tt.input)
		if pn.Alphabetic != tt.expectAlpha {
			t.Errorf("ParsePersonName(%s): expected Alphabetic=%s, got %s", tt.input, tt.expectAlpha, pn.Alphabetic)
		}
		if pn.Ideographic != tt.expectIdeo {
			t.Errorf("ParsePersonName(%s): expected Ideographic=%s, got %s", tt.input, tt.expectIdeo, pn.Ideographic)
		}
		if pn.Phonetic != tt.expectPhon {
			t.Errorf("ParsePersonName(%s): expected Phonetic=%s, got %s", tt.input, tt.expectPhon, pn.Phonetic)
		}
	}
}

func TestParseDecimalString(t *testing.T) {
	tests := []struct {
		input       string
		expectValue float64
		shouldError bool
	}{
		{"123.45", 123.45, false},
		{"  -123.45  ", -123.45, false},
		{"+1.23e-4", 0.000123, false},
		{"0", 0, false},
		{"not_a_number", 0, true},
	}

	for _, tt := range tests {
		ds, err := valuerep.ParseDecimalString(tt.input)
		if (err != nil) != tt.shouldError {
			t.Errorf("ParseDecimalString(%s): expected error=%v, got %v", tt.input, tt.shouldError, err)
		}
		if err == nil && ds.Value != tt.expectValue {
			t.Errorf("ParseDecimalString(%s): expected %f, got %f", tt.input, tt.expectValue, ds.Value)
		}
	}
}

func TestParseIntegerString(t *testing.T) {
	tests := []struct {
		input       string
		expectValue int64
		shouldError bool
	}{
		{"123", 123, false},
		{"  -456  ", -456, false},
		{"+789", 789, false},
		{"0", 0, false},
		{"not_a_number", 0, true},
		{"123.45", 0, true},
	}

	for _, tt := range tests {
		is, err := valuerep.ParseIntegerString(tt.input)
		if (err != nil) != tt.shouldError {
			t.Errorf("ParseIntegerString(%s): expected error=%v, got %v", tt.input, tt.shouldError, err)
		}
		if err == nil && is.Value != tt.expectValue {
			t.Errorf("ParseIntegerString(%s): expected %d, got %d", tt.input, tt.expectValue, is.Value)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input       string
		expectYear  int
		expectMonth time.Month
		expectDay   int
		shouldError bool
	}{
		{"20230101", 2023, time.January, 1, false},
		{"20231225", 2023, time.December, 25, false},
		{"  20230615  ", 2023, time.June, 15, false},
		{"2023-01-01", 0, 0, 0, true},
		{"202301", 0, 0, 0, true},
	}

	for _, tt := range tests {
		d, err := valuerep.ParseDate(tt.input)
		if (err != nil) != tt.shouldError {
			t.Errorf("ParseDate(%s): expected error=%v, got %v", tt.input, tt.shouldError, err)
		}
		if err == nil {
			if d.Value.Year() != tt.expectYear || d.Value.Month() != tt.expectMonth || d.Value.Day() != tt.expectDay {
				t.Errorf("ParseDate(%s): expected %04d-%02d-%02d, got %04d-%02d-%02d",
					tt.input, tt.expectYear, tt.expectMonth, tt.expectDay,
					d.Value.Year(), d.Value.Month(), d.Value.Day())
			}
		}
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		input       string
		shouldError bool
	}{
		{"12", false},
		{"1234", false},
		{"123456", false},
		{"123456.000000", false},
		{"  153045  ", false},
		{"25", true},
		{"1260", true},
	}

	for _, tt := range tests {
		tm, err := valuerep.ParseTime(tt.input)
		if (err != nil) != tt.shouldError {
			t.Errorf("ParseTime(%s): expected error=%v, got %v", tt.input, tt.shouldError, err)
		}
		if err == nil && tm.Value.IsZero() && tt.input != "00" {
			t.Errorf("ParseTime(%s): got zero time", tt.input)
		}
	}
}

func TestValidateUID(t *testing.T) {
	tests := []struct {
		uid         string
		shouldError bool
	}{
		{"1.2.3.4.5", false},
		{"1.2.840.10008.5.1.4.1.1.2", false},
		{"0.0.0", false},
		{"1.2.3.", true},
		{".1.2.3", true},
		{"1..3", true},
		{"1.2.a", true},
	}

	for _, tt := range tests {
		err := valuerep.ValidateUID(tt.uid)
		if (err != nil) != tt.shouldError {
			t.Errorf("ValidateUID(%s): expected error=%v, got %v", tt.uid, tt.shouldError, err)
		}
	}
}

func TestGetVRMetadata(t *testing.T) {
	metadata, err := valuerep.GetVRMetadata("AE")
	if err != nil {
		t.Fatalf("GetVRMetadata(AE): unexpected error: %v", err)
	}
	if metadata.Code != "AE" || metadata.MaxLength != 16 {
		t.Errorf("GetVRMetadata(AE): unexpected metadata: %+v", metadata)
	}

	_, err = valuerep.GetVRMetadata("XX")
	if err == nil {
		t.Error("GetVRMetadata(XX): expected error for invalid VR")
	}
}

func TestIsValidVR(t *testing.T) {
	tests := []struct {
		vrCode  string
		isValid bool
	}{
		{"AE", true},
		{"PN", true},
		{"OB", true},
		{"SQ", true},
		{"XX", false},
		{"", false},
	}

	for _, tt := range tests {
		valid := valuerep.IsValidVR(tt.vrCode)
		if valid != tt.isValid {
			t.Errorf("IsValidVR(%s): expected %v, got %v", tt.vrCode, tt.isValid, valid)
		}
	}
}

func TestGetAllVRCodes(t *testing.T) {
	codes := valuerep.GetAllVRCodes()
	if len(codes) != len(valuerep.VRMetadataMap) {
		t.Errorf("GetAllVRCodes: expected %d codes, got %d", len(valuerep.VRMetadataMap), len(codes))
	}

	// Check that all codes are unique
	seen := make(map[string]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("GetAllVRCodes: duplicate code %s", code)
		}
		seen[code] = true
	}
}

func TestPersonNameGetComponent(t *testing.T) {
	pn := &valuerep.PersonName{
		Alphabetic:  "Smith",
		Ideographic: "スミス",
		Phonetic:    "Smith",
	}

	tests := []struct {
		which    string
		expected string
	}{
		{"alphabetic", "Smith"},
		{"ideographic", "スミス"},
		{"phonetic", "Smith"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		result := pn.GetComponent(tt.which)
		if result != tt.expected {
			t.Errorf("GetComponent(%s): expected %s, got %s", tt.which, tt.expected, result)
		}
	}
}

func TestMultiValuedElements(t *testing.T) {
	// Test parsing multi-valued string
	value := "Value1\\Value2\\Value3"
	values := ParseMultiValue(value)

	if len(values) != 3 {
		t.Errorf("ParseMultiValue: expected 3 values, got %d", len(values))
	}

	if values[0] != "Value1" || values[1] != "Value2" || values[2] != "Value3" {
		t.Errorf("ParseMultiValue: incorrect values: %v", values)
	}
}

func TestVRTypeClassification(t *testing.T) {
	tests := []struct {
		vrCode    string
		isString  bool
		isBinary  bool
		isNumeric bool
	}{
		{"AE", true, false, false},
		{"PN", true, false, false},
		{"DS", true, false, true},
		{"IS", true, false, true},
		{"OB", false, true, false},
		{"FD", false, true, true},
		{"SQ", false, true, false},
	}

	for _, tt := range tests {
		metadata, _ := valuerep.GetVRMetadata(tt.vrCode)
		if metadata.IsString != tt.isString {
			t.Errorf("VR %s: IsString mismatch", tt.vrCode)
		}
		if metadata.IsBinary != tt.isBinary {
			t.Errorf("VR %s: IsBinary mismatch", tt.vrCode)
		}
		if metadata.IsNumeric != tt.isNumeric {
			t.Errorf("VR %s: IsNumeric mismatch", tt.vrCode)
		}
	}
}

// ParseMultiValue splits a backslash-separated string
func ParseMultiValue(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, "\\")
}

// TestParseMultiValue helper function for testing
func TestParseMultiValue(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"Single", 1},
		{"First\\Second", 2},
		{"A\\B\\C\\D", 4},
	}

	for _, tt := range tests {
		result := ParseMultiValue(tt.input)
		if len(result) != tt.expected {
			t.Errorf("ParseMultiValue(%s): expected %d values, got %d", tt.input, tt.expected, len(result))
		}
	}
}
