package errors_test

import (
	"testing"

	"github.com/amrshadid/go-dicom/errors"
)

// TestExecuteErrorHook tests the general error hook.
func TestExecuteErrorHook(t *testing.T) {
	result := errors.ExecuteErrorHook("DicomException", "DICOM_EXCEPTION", "Test error message")

	if result["error_type"] != "DicomException" {
		t.Errorf("error_type = %v, want DicomException", result["error_type"])
	}
	if result["error_code"] != "DICOM_EXCEPTION" {
		t.Errorf("error_code = %v, want DICOM_EXCEPTION", result["error_code"])
	}
	if result["message"] != "Test error message" {
		t.Errorf("message = %v, want Test error message", result["message"])
	}
	if _, ok := result["timestamp"]; !ok {
		t.Error("timestamp is missing from hook result")
	}
}

// TestExecuteValidationErrorHook tests the validation error hook.
func TestExecuteValidationErrorHook(t *testing.T) {
	result := errors.ExecuteValidationErrorHook("0010,0010", "PN", "Invalid person name format")

	if result["error_type"] != "ValidationError" {
		t.Errorf("error_type = %v, want ValidationError", result["error_type"])
	}
	if result["tag"] != "0010,0010" {
		t.Errorf("tag = %v, want 0010,0010", result["tag"])
	}
	if result["vr"] != "PN" {
		t.Errorf("vr = %v, want PN", result["vr"])
	}
	if result["reason"] != "Invalid person name format" {
		t.Errorf("reason = %v, want Invalid person name format", result["reason"])
	}
}

// TestExecuteEncodingErrorHook tests the encoding error hook.
func TestExecuteEncodingErrorHook(t *testing.T) {
	result := errors.ExecuteEncodingErrorHook("UTF-8", 1024, "Failed to decode string")

	if result["error_type"] != "EncodingError" {
		t.Errorf("error_type = %v, want EncodingError", result["error_type"])
	}
	if result["encoding"] != "UTF-8" {
		t.Errorf("encoding = %v, want UTF-8", result["encoding"])
	}
	if result["data_length"] != 1024 {
		t.Errorf("data_length = %v, want 1024", result["data_length"])
	}
	if result["reason"] != "Failed to decode string" {
		t.Errorf("reason = %v, want Failed to decode string", result["reason"])
	}
}

// TestExecuteLengthErrorHook tests the length error hook.
func TestExecuteLengthErrorHook(t *testing.T) {
	result := errors.ExecuteLengthErrorHook(8, 6, "Float64 value")

	if result["error_type"] != "LengthError" {
		t.Errorf("error_type = %v, want LengthError", result["error_type"])
	}
	if result["expected"] != 8 {
		t.Errorf("expected = %v, want 8", result["expected"])
	}
	if result["actual"] != 6 {
		t.Errorf("actual = %v, want 6", result["actual"])
	}
	if result["difference"] != -2 {
		t.Errorf("difference = %v, want -2", result["difference"])
	}
	if result["context"] != "Float64 value" {
		t.Errorf("context = %v, want Float64 value", result["context"])
	}
}

// TestHookIntegrationWithInvalidDicomError tests hooks during DICOM error scenarios.
func TestHookIntegrationWithInvalidDicomError(t *testing.T) {
	err := errors.NewInvalidDicomError("File is missing DICM prefix", "preamble not found")

	// Simulate hook execution
	hookResult := errors.ExecuteErrorHook("InvalidDicomError", err.ErrorCode(), err.Error())

	if hookResult["error_code"] != "INVALID_DICOM" {
		t.Error("Hook did not capture correct error code")
	}
}

// TestHookIntegrationWithInvalidVRError tests hooks during VR error scenarios.
func TestHookIntegrationWithInvalidVRError(t *testing.T) {
	err := errors.NewInvalidVRError("0010,0010", "XX", "Unknown VR type")

	// Simulate hook execution
	hookResult := errors.ExecuteValidationErrorHook("0010,0010", "XX", err.Error())

	if hookResult["tag"] != "0010,0010" {
		t.Error("Hook did not capture tag correctly")
	}
	if hookResult["vr"] != "XX" {
		t.Error("Hook did not capture VR correctly")
	}
}

// TestHookIntegrationWithBytesLengthError tests hooks during length error scenarios.
func TestHookIntegrationWithBytesLengthError(t *testing.T) {
	_ = errors.NewDicomBytesLengthError(4, 2, "ReadUint32 requires 4 bytes")

	// Simulate hook execution
	hookResult := errors.ExecuteLengthErrorHook(4, 2, "ReadUint32 requires 4 bytes")

	if hookResult["expected"] != 4 {
		t.Error("Hook did not capture expected length")
	}
	if hookResult["actual"] != 2 {
		t.Error("Hook did not capture actual length")
	}
	if hookResult["difference"] != -2 {
		t.Error("Hook did not calculate difference correctly")
	}
}

// TestHookIntegrationWithUnicodeDecodeError tests hooks during encoding error scenarios.
func TestHookIntegrationWithUnicodeDecodeError(t *testing.T) {
	data := []byte{0xFF, 0xFE}
	err := errors.NewDicomUnicodeDecodeError("UTF-8", data, "Invalid UTF-8 sequence")

	// Simulate hook execution
	hookResult := errors.ExecuteEncodingErrorHook("UTF-8", len(data), err.Error())

	if hookResult["encoding"] != "UTF-8" {
		t.Error("Hook did not capture encoding")
	}
	if hookResult["data_length"] != 2 {
		t.Error("Hook did not capture data length")
	}
}

// TestHookDataStructures tests that all hooks return properly structured maps.
func TestHookDataStructures(t *testing.T) {
	// Test error hook structure
	errorHook := errors.ExecuteErrorHook("TestError", "TEST_CODE", "Test message")
	requiredFields := []string{"error_type", "error_code", "message", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := errorHook[field]; !ok {
			t.Errorf("Error hook missing field: %s", field)
		}
	}

	// Test validation error hook structure
	validationHook := errors.ExecuteValidationErrorHook("0008,0020", "DA", "Invalid date")
	requiredFields = []string{"error_type", "tag", "vr", "reason"}
	for _, field := range requiredFields {
		if _, ok := validationHook[field]; !ok {
			t.Errorf("Validation hook missing field: %s", field)
		}
	}

	// Test encoding error hook structure
	encodingHook := errors.ExecuteEncodingErrorHook("ISO-2022-IR 100", 512, "Decode failed")
	requiredFields = []string{"error_type", "encoding", "data_length", "reason"}
	for _, field := range requiredFields {
		if _, ok := encodingHook[field]; !ok {
			t.Errorf("Encoding hook missing field: %s", field)
		}
	}

	// Test length error hook structure
	lengthHook := errors.ExecuteLengthErrorHook(4, 3, "uint32 read")
	requiredFields = []string{"error_type", "expected", "actual", "difference", "context"}
	for _, field := range requiredFields {
		if _, ok := lengthHook[field]; !ok {
			t.Errorf("Length hook missing field: %s", field)
		}
	}
}

// TestMultipleErrorHooks tests creating multiple error hooks in sequence.
func TestMultipleErrorHooks(t *testing.T) {
	// Create multiple errors and their hooks
	errors1 := errors.NewInvalidDicomError("Missing preamble", "DICM not found")
	hook1 := errors.ExecuteErrorHook("InvalidDicomError", errors1.ErrorCode(), errors1.Error())

	errors2 := errors.NewInvalidVRError("0010,0010", "QQ", "Invalid VR")
	hook2 := errors.ExecuteValidationErrorHook("0010,0010", "QQ", errors2.Error())

	if hook1["error_code"] == hook2["error_type"] {
		t.Error("Different error hooks should have different error codes")
	}
}

// TestHookErrorCodeMapping tests that hook error codes match actual error codes.
func TestHookErrorCodeMapping(t *testing.T) {
	testCases := []struct {
		name          string
		err           errors.DicomError
		expectedCode  string
		hookErrorType string
	}{
		{
			name:          "InvalidDicomError",
			err:           errors.NewInvalidDicomError("test", "reason"),
			expectedCode:  "INVALID_DICOM",
			hookErrorType: "InvalidDicomError",
		},
		{
			name:          "InvalidVRError",
			err:           errors.NewInvalidVRError("0008,0008", "CS", "test"),
			expectedCode:  "INVALID_VR",
			hookErrorType: "InvalidVRError",
		},
		{
			name:          "InvalidValueError",
			err:           errors.NewInvalidValueError("0010,0010", "PN", "value", "test"),
			expectedCode:  "INVALID_VALUE",
			hookErrorType: "InvalidValueError",
		},
		{
			name:          "DicomKeyError",
			err:           errors.NewDicomKeyError("PatientName"),
			expectedCode:  "KEY_NOT_FOUND",
			hookErrorType: "DicomKeyError",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualCode := tc.err.ErrorCode()
			if actualCode != tc.expectedCode {
				t.Errorf("ErrorCode() = %v, want %v", actualCode, tc.expectedCode)
			}

			hookResult := errors.ExecuteErrorHook(tc.hookErrorType, actualCode, tc.err.Error())
			if hookResult["error_code"] != tc.expectedCode {
				t.Errorf("Hook error_code = %v, want %v", hookResult["error_code"], tc.expectedCode)
			}
		})
	}
}
