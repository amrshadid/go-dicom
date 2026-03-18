package errors

import (
	"fmt"
)

// DicomError is the base interface for all DICOM-related errors.
type DicomError interface {
	error
	ErrorCode() string
	Details() string
}

// DicomException is the base DICOM exception type.
type DicomException struct {
	Message string
	Code    string
}

func (e *DicomException) Error() string {
	return e.Message
}

func (e *DicomException) ErrorCode() string {
	return e.Code
}

func (e *DicomException) Details() string {
	return e.Message
}

// NewDicomException creates a new DicomException.
func NewDicomException(msg string) *DicomException {
	return &DicomException{
		Message: msg,
		Code:    "DICOM_EXCEPTION",
	}
}

// InvalidDicomError indicates an invalid DICOM file format.
type InvalidDicomError struct {
	Message string
	Reason  string
	Code    string
}

func (e *InvalidDicomError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("InvalidDicomError: %s (reason: %s)", e.Message, e.Reason)
	}
	return fmt.Sprintf("InvalidDicomError: %s", e.Message)
}

func (e *InvalidDicomError) ErrorCode() string {
	return e.Code
}

func (e *InvalidDicomError) Details() string {
	return fmt.Sprintf("Message: %s, Reason: %s", e.Message, e.Reason)
}

// NewInvalidDicomError creates a new InvalidDicomError.
func NewInvalidDicomError(msg, reason string) *InvalidDicomError {
	return &InvalidDicomError{
		Message: msg,
		Reason:  reason,
		Code:    "INVALID_DICOM",
	}
}

// InvalidVRError indicates an invalid Value Representation.
type InvalidVRError struct {
	Tag     string
	VR      string
	Message string
	Code    string
}

func (e *InvalidVRError) Error() string {
	return fmt.Sprintf("InvalidVRError for tag %s (VR: %s): %s", e.Tag, e.VR, e.Message)
}

func (e *InvalidVRError) ErrorCode() string {
	return e.Code
}

func (e *InvalidVRError) Details() string {
	return fmt.Sprintf("Tag: %s, VR: %s, Message: %s", e.Tag, e.VR, e.Message)
}

// NewInvalidVRError creates a new InvalidVRError.
func NewInvalidVRError(tag, vr, msg string) *InvalidVRError {
	return &InvalidVRError{
		Tag:     tag,
		VR:      vr,
		Message: msg,
		Code:    "INVALID_VR",
	}
}

// InvalidValueError indicates an invalid value for a DICOM element.
type InvalidValueError struct {
	Tag     string
	VR      string
	Value   interface{}
	Message string
	Code    string
}

func (e *InvalidValueError) Error() string {
	return fmt.Sprintf("InvalidValueError for tag %s (VR: %s): %s (value: %v)",
		e.Tag, e.VR, e.Message, e.Value)
}

func (e *InvalidValueError) ErrorCode() string {
	return e.Code
}

func (e *InvalidValueError) Details() string {
	return fmt.Sprintf("Tag: %s, VR: %s, Value: %v, Message: %s",
		e.Tag, e.VR, e.Value, e.Message)
}

// NewInvalidValueError creates a new InvalidValueError.
func NewInvalidValueError(tag, vr string, value interface{}, msg string) *InvalidValueError {
	return &InvalidValueError{
		Tag:     tag,
		VR:      vr,
		Value:   value,
		Message: msg,
		Code:    "INVALID_VALUE",
	}
}

// DicomKeyError indicates a key was not found in the dataset.
type DicomKeyError struct {
	Key     string
	Message string
	Code    string
}

func (e *DicomKeyError) Error() string {
	return fmt.Sprintf("DicomKeyError: key '%s' not found", e.Key)
}

func (e *DicomKeyError) ErrorCode() string {
	return e.Code
}

func (e *DicomKeyError) Details() string {
	return fmt.Sprintf("Key: %s, Message: %s", e.Key, e.Message)
}

// NewDicomKeyError creates a new DicomKeyError.
func NewDicomKeyError(key string) *DicomKeyError {
	return &DicomKeyError{
		Key:     key,
		Message: fmt.Sprintf("Key '%s' not found in dataset", key),
		Code:    "KEY_NOT_FOUND",
	}
}

// DicomRepresentationError indicates an issue with value representation.
type DicomRepresentationError struct {
	VR      string
	Message string
	Code    string
}

func (e *DicomRepresentationError) Error() string {
	return fmt.Sprintf("DicomRepresentationError (VR: %s): %s", e.VR, e.Message)
}

func (e *DicomRepresentationError) ErrorCode() string {
	return e.Code
}

func (e *DicomRepresentationError) Details() string {
	return fmt.Sprintf("VR: %s, Message: %s", e.VR, e.Message)
}

// NewDicomRepresentationError creates a new DicomRepresentationError.
func NewDicomRepresentationError(vr, msg string) *DicomRepresentationError {
	return &DicomRepresentationError{
		VR:      vr,
		Message: msg,
		Code:    "REPRESENTATION_ERROR",
	}
}

// DicomUnicodeDecodeError indicates a character encoding issue.
type DicomUnicodeDecodeError struct {
	Encoding string
	Data     []byte
	Message  string
	Code     string
}

func (e *DicomUnicodeDecodeError) Error() string {
	return fmt.Sprintf("DicomUnicodeDecodeError (%s): %s", e.Encoding, e.Message)
}

func (e *DicomUnicodeDecodeError) ErrorCode() string {
	return e.Code
}

func (e *DicomUnicodeDecodeError) Details() string {
	return fmt.Sprintf("Encoding: %s, Message: %s, Data length: %d bytes",
		e.Encoding, e.Message, len(e.Data))
}

// NewDicomUnicodeDecodeError creates a new DicomUnicodeDecodeError.
func NewDicomUnicodeDecodeError(encoding string, data []byte, msg string) *DicomUnicodeDecodeError {
	return &DicomUnicodeDecodeError{
		Encoding: encoding,
		Data:     data,
		Message:  msg,
		Code:     "UNICODE_DECODE_ERROR",
	}
}

// DicomBytesLengthError indicates an invalid byte length.
type DicomBytesLengthError struct {
	Expected int
	Actual   int
	Message  string
	Code     string
}

func (e *DicomBytesLengthError) Error() string {
	return fmt.Sprintf("DicomBytesLengthError: expected %d bytes, got %d", e.Expected, e.Actual)
}

func (e *DicomBytesLengthError) ErrorCode() string {
	return e.Code
}

func (e *DicomBytesLengthError) Details() string {
	return fmt.Sprintf("Expected: %d, Actual: %d, Message: %s", e.Expected, e.Actual, e.Message)
}

// NewDicomBytesLengthError creates a new DicomBytesLengthError.
func NewDicomBytesLengthError(expected, actual int, msg string) *DicomBytesLengthError {
	return &DicomBytesLengthError{
		Expected: expected,
		Actual:   actual,
		Message:  msg,
		Code:     "BYTES_LENGTH_ERROR",
	}
}

// DicomInvalidTagError indicates an invalid tag format.
type DicomInvalidTagError struct {
	Tag     string
	Message string
	Code    string
}

func (e *DicomInvalidTagError) Error() string {
	return fmt.Sprintf("DicomInvalidTagError: invalid tag '%s'", e.Tag)
}

func (e *DicomInvalidTagError) ErrorCode() string {
	return e.Code
}

func (e *DicomInvalidTagError) Details() string {
	return fmt.Sprintf("Tag: %s, Message: %s", e.Tag, e.Message)
}

// NewDicomInvalidTagError creates a new DicomInvalidTagError.
func NewDicomInvalidTagError(tag, msg string) *DicomInvalidTagError {
	return &DicomInvalidTagError{
		Tag:     tag,
		Message: msg,
		Code:    "INVALID_TAG",
	}
}

// ExecuteErrorHook executes a hook for error operations.
func ExecuteErrorHook(errorType string, code string, message string) map[string]interface{} {
	result := make(map[string]interface{})
	result["error_type"] = errorType
	result["error_code"] = code
	result["message"] = message
	result["timestamp"] = fmt.Sprintf("%v", struct{}{})
	return result
}

// ExecuteValidationErrorHook executes a hook for validation errors.
func ExecuteValidationErrorHook(tag string, vr string, reason string) map[string]interface{} {
	result := make(map[string]interface{})
	result["error_type"] = "ValidationError"
	result["tag"] = tag
	result["vr"] = vr
	result["reason"] = reason
	return result
}

// ExecuteEncodingErrorHook executes a hook for encoding errors.
func ExecuteEncodingErrorHook(encoding string, dataLength int, reason string) map[string]interface{} {
	result := make(map[string]interface{})
	result["error_type"] = "EncodingError"
	result["encoding"] = encoding
	result["data_length"] = dataLength
	result["reason"] = reason
	return result
}

// ExecuteLengthErrorHook executes a hook for byte length errors.
func ExecuteLengthErrorHook(expected int, actual int, context string) map[string]interface{} {
	result := make(map[string]interface{})
	result["error_type"] = "LengthError"
	result["expected"] = expected
	result["actual"] = actual
	result["difference"] = actual - expected
	result["context"] = context
	return result
}

// CompressionError indicates a compression/decompression failure.
type CompressionError struct {
	Method  string
	Message string
	Code    string
}

func (e *CompressionError) Error() string {
	return fmt.Sprintf("CompressionError (%s): %s", e.Method, e.Message)
}

func (e *CompressionError) ErrorCode() string {
	return e.Code
}

func (e *CompressionError) Details() string {
	return fmt.Sprintf("Method: %s, Message: %s", e.Method, e.Message)
}

// NewCompressionError creates a new CompressionError.
func NewCompressionError(method, msg string) *CompressionError {
	return &CompressionError{
		Method:  method,
		Message: msg,
		Code:    "COMPRESSION_ERROR",
	}
}

// EncapsulationError indicates an encapsulation parsing failure.
type EncapsulationError struct {
	Detail  string
	Message string
	Code    string
}

func (e *EncapsulationError) Error() string {
	return fmt.Sprintf("EncapsulationError: %s", e.Message)
}

func (e *EncapsulationError) ErrorCode() string {
	return e.Code
}

func (e *EncapsulationError) Details() string {
	return fmt.Sprintf("Detail: %s, Message: %s", e.Detail, e.Message)
}

// NewEncapsulationError creates a new EncapsulationError.
func NewEncapsulationError(msg, detail string) *EncapsulationError {
	return &EncapsulationError{
		Detail:  detail,
		Message: msg,
		Code:    "ENCAPSULATION_ERROR",
	}
}
