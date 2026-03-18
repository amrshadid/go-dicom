// Package errors provides comprehensive error handling for DICOM operations.
//
// This package defines a hierarchy of specialized error types for different failure
// scenarios in DICOM file processing. All error types implement the DicomError interface,
// providing consistent error handling with error codes and detailed information.
//
// # Core Concepts
//
// ## DicomError Interface
//
// Base interface for all DICOM-specific errors:
//   - Error() string: Returns formatted error message (implements error interface)
//   - ErrorCode() string: Returns machine-readable error code
//   - Details() string: Returns detailed error information
//
// ## Error Hierarchy
//
// The package provides 8 specialized error types for different scenarios:
//   - DicomException: Generic DICOM operation errors
//   - InvalidDicomError: File format violations
//   - InvalidVRError: Value Representation (VR) encoding issues
//   - InvalidValueError: Invalid element values
//   - DicomKeyError: Missing dataset keys
//   - DicomRepresentationError: Value representation issues
//   - DicomUnicodeDecodeError: Character encoding problems
//   - DicomBytesLengthError: Byte length mismatches
//   - DicomInvalidTagError: Tag format violations
//
// ## Error Codes
//
// Each error type has a standard error code for programmatic handling:
//   - DICOM_EXCEPTION: Generic exception
//   - INVALID_DICOM: Invalid file format
//   - INVALID_VR: Invalid Value Representation
//   - INVALID_VALUE: Invalid element value
//   - KEY_NOT_FOUND: Missing dictionary key
//   - REPRESENTATION_ERROR: Representation conversion error
//   - UNICODE_DECODE_ERROR: Character encoding failure
//   - BYTES_LENGTH_ERROR: Length mismatch
//   - INVALID_TAG: Tag format error
//
// # Basic Usage
//
// ## Creating Errors
//
//	import (
//	    "log"
//	    "github.com/amrshadid/go-dicom/errors"
//	)
//
//	func validateDicomFile() error {
//	    // Check for valid DICOM format
//	    if !isValidFormat() {
//	        return errors.NewInvalidDicomError(
//	            "file does not contain DICM prefix",
//	            "missing DICOM magic string",
//	        )
//	    }
//	    return nil
//	}
//
// ## Handling Errors
//
//	err := errors.NewInvalidVRError("(0010,0010)", "XX", "unsupported VR")
//	if err != nil {
//	    log.Printf("Error Code: %s\n", err.ErrorCode())
//	    log.Printf("Details: %s\n", err.Details())
//	}
//
// ## Type Assertions
//
//	if invErr, ok := err.(*errors.InvalidVRError); ok {
//	    fmt.Printf("VR: %s, Tag: %s\n", invErr.VR, invErr.Tag)
//	}
//
// # Advanced Usage
//
// ## Error Chaining
//
// Errors can be wrapped with fmt.Errorf:
//
//	baseErr := errors.NewInvalidDicomError("parse failed", "malformed header")
//	return fmt.Errorf("failed to read file: %w", baseErr)
//
// ## Custom Error Handling
//
// Check error codes for specific handling:
//
//	err := someOperation()
//	if dErr, ok := err.(errors.DicomError); ok {
//	    switch dErr.ErrorCode() {
//	    case "INVALID_DICOM":
//	        // Handle invalid DICOM format
//	    case "INVALID_VR":
//	        // Handle VR issues
//	    case "KEY_NOT_FOUND":
//	        // Handle missing key
//	    }
//	}
//
// ## Error Recovery
//
// Use detailed error information for recovery:
//
//	if bytesErr, ok := err.(*errors.DicomBytesLengthError); ok {
//	    fmt.Printf("Expected %d bytes, got %d\n", bytesErr.Expected, bytesErr.Actual)
//	    // Implement recovery logic
//	}
//
// # Data Structures
//
// ## DicomError Interface
//
//	type DicomError interface {
//	    error
//	    ErrorCode() string
//	    Details() string
//	}
//
// Base interface for all DICOM errors with three methods.
//
// ## DicomException
//
//	type DicomException struct {
//	    Message string  // Error message
//	    Code    string  // Error code (DICOM_EXCEPTION)
//	}
//
// Generic exception for general DICOM operation errors.
//
// ## InvalidDicomError
//
//	type InvalidDicomError struct {
//	    Message string  // Error message
//	    Reason  string  // Detailed reason
//	    Code    string  // Error code (INVALID_DICOM)
//	}
//
// Indicates DICOM file format is invalid or corrupted.
//
// ## InvalidVRError
//
//	type InvalidVRError struct {
//	    Tag     string  // DICOM tag (e.g., "(0010,0010)")
//	    VR      string  // Value Representation (e.g., "XX")
//	    Message string  // Error message
//	    Code    string  // Error code (INVALID_VR)
//	}
//
// Indicates Value Representation is invalid or unsupported.
//
// ## InvalidValueError
//
//	type InvalidValueError struct {
//	    Tag     string      // DICOM tag
//	    VR      string      // Value Representation
//	    Value   interface{} // The invalid value
//	    Message string      // Error message
//	    Code    string      // Error code (INVALID_VALUE)
//	}
//
// Indicates a DICOM element has an invalid value for its VR.
//
// ## DicomKeyError
//
//	type DicomKeyError struct {
//	    Key     string  // The missing key
//	    Message string  // Error message
//	    Code    string  // Error code (KEY_NOT_FOUND)
//	}
//
// Indicates a required key is missing from dataset.
//
// ## DicomRepresentationError
//
//	type DicomRepresentationError struct {
//	    VR      string  // Value Representation
//	    Message string  // Error message
//	    Code    string  // Error code (REPRESENTATION_ERROR)
//	}
//
// Indicates an issue converting between representations.
//
// ## DicomUnicodeDecodeError
//
//	type DicomUnicodeDecodeError struct {
//	    Encoding string  // Character encoding (e.g., "UTF-8")
//	    Data     []byte  // The invalid data
//	    Message  string  // Error message
//	    Code     string  // Error code (UNICODE_DECODE_ERROR)
//	}
//
// Indicates character encoding/decoding failure.
//
// ## DicomBytesLengthError
//
//	type DicomBytesLengthError struct {
//	    Expected int     // Expected number of bytes
//	    Actual   int     // Actual number of bytes
//	    Message  string  // Error message
//	    Code     string  // Error code (BYTES_LENGTH_ERROR)
//	}
//
// Indicates byte count mismatch during I/O operations.
//
// ## DicomInvalidTagError
//
//	type DicomInvalidTagError struct {
//	    Tag     string  // Invalid tag format
//	    Message string  // Error message
//	    Code    string  // Error code (INVALID_TAG)
//	}
//
// Indicates DICOM tag format is invalid.
//
// # API Reference
//
// ## Error Creation
//
// ### NewDicomException
//
//	func NewDicomException(msg string) *DicomException
//
// Creates a generic DICOM exception.
//
// **Parameters:**
// - `msg`: Error message
//
// **Returns:** DicomException pointer
//
// **Example:**
// ```go
// err := errors.NewDicomException("operation failed")
// ```
//
// ### NewInvalidDicomError
//
//	func NewInvalidDicomError(msg, reason string) *InvalidDicomError
//
// Creates error for invalid DICOM file format.
//
// **Parameters:**
// - `msg`: Error message
// - `reason`: Detailed reason for failure
//
// **Returns:** InvalidDicomError pointer
//
// **Example:**
// ```go
// err := errors.NewInvalidDicomError(
//
//	"invalid file format",
//	"missing DICM prefix",
//
// )
// ```
//
// ### NewInvalidVRError
//
//	func NewInvalidVRError(tag, vr, msg string) *InvalidVRError
//
// Creates error for invalid Value Representation.
//
// **Parameters:**
// - `tag`: DICOM tag
// - `vr`: Value Representation code
// - `msg`: Error message
//
// **Returns:** InvalidVRError pointer
//
// **Example:**
// ```go
// err := errors.NewInvalidVRError(
//
//	"(0010,0010)",
//	"XX",
//	"unsupported VR",
//
// )
// ```
//
// ### NewInvalidValueError
//
//	func NewInvalidValueError(tag, vr string, value interface{}, msg string) *InvalidValueError
//
// Creates error for invalid element value.
//
// **Parameters:**
// - `tag`: DICOM tag
// - `vr`: Value Representation
// - `value`: The invalid value
// - `msg`: Error message
//
// **Returns:** InvalidValueError pointer
//
// **Example:**
// ```go
// err := errors.NewInvalidValueError(
//
//	"(0010,0010)",
//	"PN",
//	12345,
//	"expected string value",
//
// )
// ```
//
// ### NewDicomKeyError
//
//	func NewDicomKeyError(key string) *DicomKeyError
//
// Creates error for missing key in dataset.
//
// **Parameters:**
// - `key`: The missing key
//
// **Returns:** DicomKeyError pointer
//
// **Example:**
// ```go
// err := errors.NewDicomKeyError("PatientName")
// ```
//
// ### NewDicomRepresentationError
//
//	func NewDicomRepresentationError(vr, msg string) *DicomRepresentationError
//
// Creates error for representation conversion failure.
//
// **Parameters:**
// - `vr`: Value Representation
// - `msg`: Error message
//
// **Returns:** DicomRepresentationError pointer
//
// **Example:**
// ```go
// err := errors.NewDicomRepresentationError(
//
//	"DS",
//	"invalid decimal string format",
//
// )
// ```
//
// ### NewDicomUnicodeDecodeError
//
//	func NewDicomUnicodeDecodeError(encoding string, data []byte, msg string) *DicomUnicodeDecodeError
//
// Creates error for character encoding failure.
//
// **Parameters:**
// - `encoding`: Character encoding name
// - `data`: The invalid data bytes
// - `msg`: Error message
//
// **Returns:** DicomUnicodeDecodeError pointer
//
// **Example:**
// ```go
// err := errors.NewDicomUnicodeDecodeError(
//
//	"UTF-8",
//	[]byte{0xFF, 0xFE},
//	"invalid UTF-8 sequence",
//
// )
// ```
//
// ### NewDicomBytesLengthError
//
//	func NewDicomBytesLengthError(expected, actual int, msg string) *DicomBytesLengthError
//
// Creates error for byte length mismatch.
//
// **Parameters:**
// - `expected`: Expected number of bytes
// - `actual`: Actual number of bytes
// - `msg`: Error message
//
// **Returns:** DicomBytesLengthError pointer
//
// **Example:**
// ```go
// err := errors.NewDicomBytesLengthError(4, 2, "incomplete tag data")
// ```
//
// ### NewDicomInvalidTagError
//
//	func NewDicomInvalidTagError(tag, msg string) *DicomInvalidTagError
//
// Creates error for invalid tag format.
//
// **Parameters:**
// - `tag`: The invalid tag
// - `msg`: Error message
//
// **Returns:** DicomInvalidTagError pointer
//
// **Example:**
// ```go
// err := errors.NewDicomInvalidTagError("GGGG,EEEE", "invalid tag format")
// ```
//
// ## Error Interface Methods
//
// ### Error()
//
// Returns formatted error message (implements error interface).
//
// **Returns:** String message
//
// ### ErrorCode()
//
// Returns machine-readable error code.
//
// **Returns:** Error code string
//
// ### Details()
//
// Returns detailed error information.
//
// **Returns:** Detailed string
//
// # Performance Characteristics
//
// | Operation | Complexity | Description |
// |-----------|-----------|-------------|
// | NewDicomException | O(1) | Simple object creation |
// | NewInvalidDicomError | O(1) | Simple object creation |
// | NewInvalidVRError | O(1) | Simple object creation |
// | NewInvalidValueError | O(1) | Simple object creation |
// | NewDicomKeyError | O(k) | k = string length for Key field |
// | NewDicomRepresentationError | O(1) | Simple object creation |
// | NewDicomUnicodeDecodeError | O(k) | k = data length (copies bytes) |
// | NewDicomBytesLengthError | O(1) | Simple object creation |
// | NewDicomInvalidTagError | O(1) | Simple object creation |
// | Error() | O(k) | k = formatted string length |
// | ErrorCode() | O(1) | Field access |
// | Details() | O(k) | k = details string length |
//
// # Error Codes Reference
//
// | Error Type | Code | Scenario |
// |------------|------|----------|
// | DicomException | DICOM_EXCEPTION | Generic DICOM operation error |
// | InvalidDicomError | INVALID_DICOM | File format not valid DICOM |
// | InvalidVRError | INVALID_VR | Value Representation not supported |
// | InvalidValueError | INVALID_VALUE | Element value invalid for VR |
// | DicomKeyError | KEY_NOT_FOUND | Required key missing from dataset |
// | DicomRepresentationError | REPRESENTATION_ERROR | Value representation conversion failed |
// | DicomUnicodeDecodeError | UNICODE_DECODE_ERROR | Character encoding/decoding failed |
// | DicomBytesLengthError | BYTES_LENGTH_ERROR | Byte count mismatch |
// | DicomInvalidTagError | INVALID_TAG | Tag format invalid |
//
// # Use Cases
//
// ## File Validation
//
// Validate DICOM file format during reading:
//
//	if !hasValidPreamble(file) {
//	    return errors.NewInvalidDicomError("preamble invalid", "not 128 zero bytes")
//	}
//
// ## VR Validation
//
// Check Value Representation during parsing:
//
//	if !isValidVR(vr) {
//	    return errors.NewInvalidVRError(tag.String(), vr, "unsupported VR")
//	}
//
// ## Value Validation
//
// Verify element values match expected types:
//
//	if !isValidPN(value) {
//	    return errors.NewInvalidValueError(tag, "PN", value, "invalid person name")
//	}
//
// ## Character Encoding
//
// Handle encoding errors:
//
//	if !isValidUTF8(data) {
//	    return errors.NewDicomUnicodeDecodeError("UTF-8", data, "invalid sequence")
//	}
//
// ## Data Integrity
//
// Verify byte counts during I/O:
//
//	if len(data) != expected {
//	    return errors.NewDicomBytesLengthError(expected, len(data), "short read")
//	}
//
// # Limitations
//
// - Error types carry only string/int/byte information (no nested errors)
// - No built-in error recovery mechanisms
// - Error codes are static strings (no enum type)
// - Limited context beyond fields in each error type
//
// # Related Packages
//
// - **filereader**: Uses these errors during DICOM file reading
// - **filewriter**: Uses these errors during DICOM file writing
// - **dataset**: Uses these errors for dataset operations
// - **valuerep**: Uses these errors for value representation
//
// # Best Practices
//
// ## Check Error Type
//
// Always check the concrete error type for specific handling:
//
//	if dErr, ok := err.(errors.DicomError); ok {
//	    code := dErr.ErrorCode()
//	    // Handle based on code
//	}
//
// ## Provide Context
//
// Include relevant context when creating errors:
//
//	return errors.NewInvalidVRError(tag.String(), vr, msg)
//
// ## Wrap Errors
//
// Use fmt.Errorf for error chaining:
//
//	return fmt.Errorf("failed to parse: %w", err)
//
// ## Log Details
//
// Use Details() method for comprehensive logging:
//
//	log.Printf("Error: %s, Details: %s", err, dErr.Details())
package errors
