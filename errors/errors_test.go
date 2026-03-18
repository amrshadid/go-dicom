package errors_test

import (
	"fmt"
	"testing"

	"github.com/amrshadid/go-dicom/errors"
)

func TestDicomException(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantCode string
	}{
		{
			name:     "basic exception",
			msg:      "test exception",
			wantCode: "DICOM_EXCEPTION",
		},
		{
			name:     "exception with special chars",
			msg:      "test: error @ position 123",
			wantCode: "DICOM_EXCEPTION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.NewDicomException(tt.msg)

			if err.Error() != tt.msg {
				t.Errorf("got Error() = %s, want %s", err.Error(), tt.msg)
			}
			if err.ErrorCode() != tt.wantCode {
				t.Errorf("got ErrorCode() = %s, want %s", err.ErrorCode(), tt.wantCode)
			}
			if err.Details() != tt.msg {
				t.Errorf("got Details() = %s, want %s", err.Details(), tt.msg)
			}
		})
	}
}

func TestInvalidDicomError(t *testing.T) {
	tests := []struct {
		name       string
		msg        string
		reason     string
		wantCode   string
		wantInText string
	}{
		{
			name:       "with reason",
			msg:        "invalid format",
			reason:     "missing preamble",
			wantCode:   "INVALID_DICOM",
			wantInText: "missing preamble",
		},
		{
			name:       "without reason",
			msg:        "invalid format",
			reason:     "",
			wantCode:   "INVALID_DICOM",
			wantInText: "InvalidDicomError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.NewInvalidDicomError(tt.msg, tt.reason)

			if err.ErrorCode() != tt.wantCode {
				t.Errorf("got ErrorCode() = %s, want %s", err.ErrorCode(), tt.wantCode)
			}

			errStr := err.Error()
			if !contains(errStr, tt.wantInText) {
				t.Errorf("Error() should contain '%s', got: %s", tt.wantInText, errStr)
			}
		})
	}
}

func TestInvalidVRError(t *testing.T) {
	tag := "(0010,0010)"
	vr := "INVALID"
	msg := "unknown VR"

	err := errors.NewInvalidVRError(tag, vr, msg)

	if !contains(err.Error(), tag) {
		t.Errorf("Error() should contain tag %s, got: %s", tag, err.Error())
	}
	if !contains(err.Error(), vr) {
		t.Errorf("Error() should contain VR %s, got: %s", vr, err.Error())
	}
	if err.ErrorCode() != "INVALID_VR" {
		t.Errorf("got ErrorCode() = %s, want INVALID_VR", err.ErrorCode())
	}
}

func TestInvalidValueError(t *testing.T) {
	tag := "(0010,0010)"
	vr := "PN"
	value := 12345 // Invalid for PN
	msg := "expected string"

	err := errors.NewInvalidValueError(tag, vr, value, msg)

	if !contains(err.Error(), tag) {
		t.Errorf("Error() should contain tag, got: %s", err.Error())
	}
	if !contains(err.Error(), vr) {
		t.Errorf("Error() should contain VR, got: %s", err.Error())
	}
	if err.ErrorCode() != "INVALID_VALUE" {
		t.Errorf("got ErrorCode() = %s, want INVALID_VALUE", err.ErrorCode())
	}

	details := err.Details()
	if !contains(details, tag) || !contains(details, vr) {
		t.Errorf("Details should include tag and VR, got: %s", details)
	}
}

func TestDicomKeyError(t *testing.T) {
	key := "PatientName"
	err := errors.NewDicomKeyError(key)

	if !contains(err.Error(), key) {
		t.Errorf("Error() should contain key, got: %s", err.Error())
	}
	if err.ErrorCode() != "KEY_NOT_FOUND" {
		t.Errorf("got ErrorCode() = %s, want KEY_NOT_FOUND", err.ErrorCode())
	}
}

func TestDicomRepresentationError(t *testing.T) {
	vr := "DS"
	msg := "invalid decimal string"

	err := errors.NewDicomRepresentationError(vr, msg)

	if !contains(err.Error(), vr) {
		t.Errorf("Error() should contain VR, got: %s", err.Error())
	}
	if !contains(err.Error(), msg) {
		t.Errorf("Error() should contain message, got: %s", err.Error())
	}
	if err.ErrorCode() != "REPRESENTATION_ERROR" {
		t.Errorf("got ErrorCode() = %s, want REPRESENTATION_ERROR", err.ErrorCode())
	}
}

func TestDicomUnicodeDecodeError(t *testing.T) {
	encoding := "UTF-8"
	data := []byte{0xFF, 0xFE, 0xFD}
	msg := "invalid UTF-8 sequence"

	err := errors.NewDicomUnicodeDecodeError(encoding, data, msg)

	if !contains(err.Error(), encoding) {
		t.Errorf("Error() should contain encoding, got: %s", err.Error())
	}
	if err.ErrorCode() != "UNICODE_DECODE_ERROR" {
		t.Errorf("got ErrorCode() = %s, want UNICODE_DECODE_ERROR", err.ErrorCode())
	}

	details := err.Details()
	if !contains(details, encoding) {
		t.Errorf("Details should include encoding, got: %s", details)
	}
}

func TestDicomBytesLengthError(t *testing.T) {
	expected := 4
	actual := 2
	msg := "incomplete tag data"

	err := errors.NewDicomBytesLengthError(expected, actual, msg)

	errStr := err.Error()
	if !contains(errStr, "4") || !contains(errStr, "2") {
		t.Errorf("Error() should contain byte counts, got: %s", errStr)
	}
	if err.ErrorCode() != "BYTES_LENGTH_ERROR" {
		t.Errorf("got ErrorCode() = %s, want BYTES_LENGTH_ERROR", err.ErrorCode())
	}
}

func TestDicomInvalidTagError(t *testing.T) {
	tag := "GGGG,EEEE"
	msg := "invalid tag format"

	err := errors.NewDicomInvalidTagError(tag, msg)

	if !contains(err.Error(), tag) {
		t.Errorf("Error() should contain tag, got: %s", err.Error())
	}
	if err.ErrorCode() != "INVALID_TAG" {
		t.Errorf("got ErrorCode() = %s, want INVALID_TAG", err.ErrorCode())
	}
}

// Test that all error types implement the DicomError interface.
func TestErrorInterfaces(t *testing.T) {
	errs := []errors.DicomError{
		errors.NewDicomException("test"),
		errors.NewInvalidDicomError("test", "reason"),
		errors.NewInvalidVRError("(0010,0010)", "XX", "test"),
		errors.NewInvalidValueError("(0010,0010)", "PN", "value", "test"),
		errors.NewDicomKeyError("key"),
		errors.NewDicomRepresentationError("PN", "test"),
		errors.NewDicomUnicodeDecodeError("UTF-8", []byte{}, "test"),
		errors.NewDicomBytesLengthError(4, 2, "test"),
		errors.NewDicomInvalidTagError("(0010,0010)", "test"),
	}

	for _, err := range errs {
		if err == nil {
			t.Error("error should not be nil")
		}
		if err.Error() == "" {
			t.Error("Error() should not be empty")
		}
		if err.ErrorCode() == "" {
			t.Error("ErrorCode() should not be empty")
		}
		if err.Details() == "" {
			t.Error("Details() should not be empty")
		}
	}
}

// Test error chaining with fmt.Errorf.
func TestErrorWrapping(t *testing.T) {
	baseErr := errors.NewInvalidDicomError("test", "reason")
	wrappedErr := fmt.Errorf("wrapped: %w", baseErr)

	if wrappedErr == nil {
		t.Error("wrapped error should not be nil")
	}
	if !contains(wrappedErr.Error(), "wrapped") {
		t.Errorf("wrapped error should contain 'wrapped', got: %s", wrappedErr.Error())
	}
}

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
