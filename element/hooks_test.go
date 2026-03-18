package element_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/element"
	"github.com/amrshadid/go-dicom/filebase"
)

// TestExecuteEncodingHook tests the encoding hook.
func TestExecuteEncodingHook(t *testing.T) {
	result := element.ExecuteEncodingHook(dataelem.LO, "TestValue", filebase.LittleEndian)

	if result["vr"] != "LO" {
		t.Errorf("vr = %v, want LO", result["vr"])
	}
	if result["byte_order"] != "LittleEndian" {
		t.Errorf("byte_order = %v, want LittleEndian", result["byte_order"])
	}
	if result["value_type"] == "" {
		t.Error("value_type is empty")
	}
}

// TestExecuteDecodingHook tests the decoding hook.
func TestExecuteDecodingHook(t *testing.T) {
	result := element.ExecuteDecodingHook(dataelem.US, 2, filebase.BigEndian)

	if result["vr"] != "US" {
		t.Errorf("vr = %v, want US", result["vr"])
	}
	if result["bytes_length"] != 2 {
		t.Errorf("bytes_length = %v, want 2", result["bytes_length"])
	}
	if result["byte_order"] != "BigEndian" {
		t.Errorf("byte_order = %v, want BigEndian", result["byte_order"])
	}
}

// TestExecutePaddingHook tests the padding hook.
func TestExecutePaddingHook(t *testing.T) {
	result := element.ExecutePaddingHook(dataelem.LO, 5, 6)

	if result["vr"] != "LO" {
		t.Errorf("vr = %v, want LO", result["vr"])
	}
	if result["original_length"] != 5 {
		t.Errorf("original_length = %v, want 5", result["original_length"])
	}
	if result["padded_length"] != 6 {
		t.Errorf("padded_length = %v, want 6", result["padded_length"])
	}
	if result["padding_bytes"] != 1 {
		t.Errorf("padding_bytes = %v, want 1", result["padding_bytes"])
	}
}

// TestExecuteValidationHook tests the validation hook with success.
func TestExecuteValidationHookSuccess(t *testing.T) {
	result := element.ExecuteValidationHook(dataelem.FD, []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, true, "")

	if result["vr"] != "FD" {
		t.Errorf("vr = %v, want FD", result["vr"])
	}
	if result["valid"] != true {
		t.Errorf("valid = %v, want true", result["valid"])
	}
	if result["value_length"] != 8 {
		t.Errorf("value_length = %v, want 8", result["value_length"])
	}
	if _, ok := result["error"]; ok {
		t.Error("error should not be present on successful validation")
	}
}

// TestExecuteValidationHookFailure tests the validation hook with failure.
func TestExecuteValidationHookFailure(t *testing.T) {
	errorMsg := "odd length not allowed"
	result := element.ExecuteValidationHook(dataelem.FD, []byte{0x00, 0x00, 0x00}, false, errorMsg)

	if result["valid"] != false {
		t.Errorf("valid = %v, want false", result["valid"])
	}
	if result["error"] != errorMsg {
		t.Errorf("error = %v, want %v", result["error"], errorMsg)
	}
}

// TestHookIntegrationWithEncoder tests hooks during encoding operations.
func TestHookIntegrationWithEncoder(t *testing.T) {
	encoder := element.NewValueEncoder(filebase.LittleEndian)

	// Simulate encoding hook execution
	hookResult := element.ExecuteEncodingHook(dataelem.IS, int64(12345), filebase.LittleEndian)

	if hookResult["vr"] != "IS" {
		t.Error("Encoding hook did not record correct VR")
	}

	// Actually encode the value
	encodedBytes := encoder.EncodeInt32(12345)
	if len(encodedBytes) == 0 {
		t.Error("Encoding produced empty bytes")
	}
}

// TestHookIntegrationWithPadder tests hooks during padding operations.
func TestHookIntegrationWithPadder(t *testing.T) {
	padder := element.NewValuePadder()

	originalValue := []byte("Test")
	paddedValue := padder.Pad(originalValue, dataelem.LO)

	// Simulate padding hook
	hookResult := element.ExecutePaddingHook(dataelem.LO, len(originalValue), len(paddedValue))

	if hookResult["padding_bytes"] != 0 && hookResult["padding_bytes"] != 1 {
		t.Errorf("padding_bytes = %v, expected 0 or 1", hookResult["padding_bytes"])
	}
}

// TestHookIntegrationWithValidation tests hooks during validation.
func TestHookIntegrationWithValidation(t *testing.T) {
	testValue := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	// Valid FD (float64) value
	validErr := element.ValidateLength(testValue, dataelem.FD)
	hookResult := element.ExecuteValidationHook(dataelem.FD, testValue, validErr == nil, "")

	if hookResult["valid"] != true {
		t.Error("Validation hook did not record valid result")
	}

	// Invalid odd-length value for LO
	invalidValue := []byte{0x01, 0x02, 0x03}
	invalidErr := element.ValidateLength(invalidValue, dataelem.LO)
	hookResult2 := element.ExecuteValidationHook(dataelem.LO, invalidValue, invalidErr == nil, "")

	if hookResult2["valid"] != false {
		t.Error("Validation hook did not record invalid result")
	}
}

// TestHookDataStructures tests that all hooks return properly structured maps.
func TestHookDataStructures(t *testing.T) {
	// Test encoding hook structure
	encodingHook := element.ExecuteEncodingHook(dataelem.CS, "VALUE", filebase.LittleEndian)
	requiredFields := []string{"vr", "value_type", "byte_order"}
	for _, field := range requiredFields {
		if _, ok := encodingHook[field]; !ok {
			t.Errorf("Encoding hook missing field: %s", field)
		}
	}

	// Test decoding hook structure
	decodingHook := element.ExecuteDecodingHook(dataelem.FL, 4, filebase.BigEndian)
	requiredFields = []string{"vr", "bytes_length", "byte_order"}
	for _, field := range requiredFields {
		if _, ok := decodingHook[field]; !ok {
			t.Errorf("Decoding hook missing field: %s", field)
		}
	}

	// Test padding hook structure
	paddingHook := element.ExecutePaddingHook(dataelem.UI, 10, 11)
	requiredFields = []string{"vr", "original_length", "padded_length", "padding_bytes"}
	for _, field := range requiredFields {
		if _, ok := paddingHook[field]; !ok {
			t.Errorf("Padding hook missing field: %s", field)
		}
	}

	// Test validation hook structure
	validationHook := element.ExecuteValidationHook(dataelem.DS, []byte("1.5"), true, "")
	requiredFields = []string{"vr", "value_length", "valid"}
	for _, field := range requiredFields {
		if _, ok := validationHook[field]; !ok {
			t.Errorf("Validation hook missing field: %s", field)
		}
	}
}
