package network

import "fmt"

// NetworkError is the base interface for all network-specific errors.
type NetworkError interface {
	error
	ErrorCode() string
	Details() string
}

// PDUError represents errors in PDU encoding/decoding.
type PDUError struct {
	Message string
	Code    string
	Detail  string
}

func (e *PDUError) Error() string     { return fmt.Sprintf("PDU error [%s]: %s", e.Code, e.Message) }
func (e *PDUError) ErrorCode() string { return e.Code }
func (e *PDUError) Details() string   { return e.Detail }

// NewPDUError creates a new PDU error.
func NewPDUError(code, message string) *PDUError {
	return &PDUError{Code: code, Message: message}
}

// NewPDUErrorf creates a new PDU error with formatted message.
func NewPDUErrorf(code, format string, args ...interface{}) *PDUError {
	return &PDUError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AssociationError represents errors during association negotiation.
type AssociationError struct {
	Message string
	Code    string
	Detail  string
	Result  byte // A-ASSOCIATE-RJ result field
	Source  byte // A-ASSOCIATE-RJ source field
	Reason  byte // A-ASSOCIATE-RJ reason field
}

func (e *AssociationError) Error() string {
	return fmt.Sprintf("association error [%s]: %s", e.Code, e.Message)
}
func (e *AssociationError) ErrorCode() string { return e.Code }
func (e *AssociationError) Details() string   { return e.Detail }

// NewAssociationError creates a new association error.
func NewAssociationError(code, message string) *AssociationError {
	return &AssociationError{Code: code, Message: message}
}

// NewAssociationRejection creates an error from A-ASSOCIATE-RJ PDU fields.
func NewAssociationRejection(result, source, reason byte) *AssociationError {
	return &AssociationError{
		Code:    "ASSOCIATION_REJECTED",
		Message: fmt.Sprintf("association rejected: result=%d, source=%d, reason=%d", result, source, reason),
		Result:  result,
		Source:  source,
		Reason:  reason,
	}
}

// TimeoutError represents a network operation timeout.
type TimeoutError struct {
	Message   string
	Code      string
	Detail    string
	Operation string
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timeout [%s]: %s", e.Operation, e.Message)
}
func (e *TimeoutError) ErrorCode() string { return e.Code }
func (e *TimeoutError) Details() string   { return e.Detail }

// NewTimeoutError creates a new timeout error.
func NewTimeoutError(operation, message string) *TimeoutError {
	return &TimeoutError{Code: "TIMEOUT", Operation: operation, Message: message}
}

// CommunicationError represents a transport-level communication error.
type CommunicationError struct {
	Message string
	Code    string
	Detail  string
	Cause   error
}

func (e *CommunicationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("communication error [%s]: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("communication error [%s]: %s", e.Code, e.Message)
}
func (e *CommunicationError) ErrorCode() string { return e.Code }
func (e *CommunicationError) Details() string   { return e.Detail }
func (e *CommunicationError) Unwrap() error     { return e.Cause }

// NewCommunicationError creates a new communication error.
func NewCommunicationError(code, message string, cause error) *CommunicationError {
	return &CommunicationError{Code: code, Message: message, Cause: cause}
}

// DIMSEError represents an error in DIMSE message processing.
type DIMSEError struct {
	Message string
	Code    string
	Detail  string
	Status  uint16
}

func (e *DIMSEError) Error() string {
	return fmt.Sprintf("DIMSE error [%s]: %s (status=0x%04X)", e.Code, e.Message, e.Status)
}
func (e *DIMSEError) ErrorCode() string { return e.Code }
func (e *DIMSEError) Details() string   { return e.Detail }

// NewDIMSEError creates a new DIMSE error.
func NewDIMSEError(code, message string, status uint16) *DIMSEError {
	return &DIMSEError{Code: code, Message: message, Status: status}
}
