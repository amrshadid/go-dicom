# Errors

Specialized error types for DICOM operations implementing the `DicomError` interface with machine-readable error codes, detailed context, and error hooks.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/errors"

// Create specific errors
err := errors.NewInvalidDicomError("missing DICM prefix", "byte 132 invalid")
err := errors.NewInvalidVRError("(0010,0010)", "XX", "unsupported VR")
err := errors.NewDicomKeyError("PatientName")

// Handle by error code
if dErr, ok := err.(errors.DicomError); ok {
    switch dErr.ErrorCode() {
    case "INVALID_DICOM":  // file format error
    case "INVALID_VR":     // VR encoding error
    case "KEY_NOT_FOUND":  // missing dataset key
    }
}

// Type assertion
if invErr, ok := err.(*errors.InvalidDicomError); ok {
    fmt.Println(invErr.Reason)
}
```

## API Reference

```go
type DicomError interface {
    error
    ErrorCode() string
    Details() string
}

// Error constructors
func NewDicomException(msg string) *DicomException                                    // DICOM_EXCEPTION
func NewInvalidDicomError(msg, reason string) *InvalidDicomError                      // INVALID_DICOM
func NewInvalidVRError(tag, vr, msg string) *InvalidVRError                           // INVALID_VR
func NewInvalidValueError(tag, vr string, value interface{}, msg string) *InvalidValueError // INVALID_VALUE
func NewDicomKeyError(key string) *DicomKeyError                                      // KEY_NOT_FOUND
func NewDicomUnicodeDecodeError(encoding string, data []byte, msg string) *DicomUnicodeDecodeError // UNICODE_DECODE_ERROR
func NewDicomBytesLengthError(expected, actual int, msg string) *DicomBytesLengthError // BYTES_LENGTH_ERROR
func NewDicomRepresentationError(vr, msg string) *DicomRepresentationError             // REPRESENTATION_ERROR
func NewDicomInvalidTagError(tag, msg string) *DicomInvalidTagError                    // INVALID_TAG
func NewCompressionError(method, msg string) *CompressionError                         // COMPRESSION_ERROR
func NewEncapsulationError(msg, detail string) *EncapsulationError                     // ENCAPSULATION_ERROR

// Error hooks
func ExecuteErrorHook(errorType, code, message string) map[string]interface{}
func ExecuteValidationErrorHook(tag, vr, reason string) map[string]interface{}
```
