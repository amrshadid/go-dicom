package element

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
)

// ValueEncoder encodes values to bytes according to DICOM standards.
type ValueEncoder struct {
	byteOrder filebase.ByteOrder
}

// NewValueEncoder creates a new ValueEncoder.
func NewValueEncoder(byteOrder filebase.ByteOrder) *ValueEncoder {
	return &ValueEncoder{
		byteOrder: byteOrder,
	}
}

// EncodeString encodes a string to bytes.
func (ve *ValueEncoder) EncodeString(value string) []byte {
	return []byte(value)
}

// EncodeUint16 encodes a uint16 to bytes.
func (ve *ValueEncoder) EncodeUint16(value uint16) []byte {
	buf := make([]byte, 2)
	if ve.byteOrder == filebase.LittleEndian {
		binary.LittleEndian.PutUint16(buf, value)
	} else {
		binary.BigEndian.PutUint16(buf, value)
	}
	return buf
}

// EncodeUint32 encodes a uint32 to bytes.
func (ve *ValueEncoder) EncodeUint32(value uint32) []byte {
	buf := make([]byte, 4)
	if ve.byteOrder == filebase.LittleEndian {
		binary.LittleEndian.PutUint32(buf, value)
	} else {
		binary.BigEndian.PutUint32(buf, value)
	}
	return buf
}

// EncodeInt16 encodes an int16 to bytes.
func (ve *ValueEncoder) EncodeInt16(value int16) []byte {
	return ve.EncodeUint16(uint16(value))
}

// EncodeInt32 encodes an int32 to bytes.
func (ve *ValueEncoder) EncodeInt32(value int32) []byte {
	return ve.EncodeUint32(uint32(value))
}

// EncodeFloat32 encodes a float32 to bytes.
func (ve *ValueEncoder) EncodeFloat32(value float32) []byte {
	return ve.EncodeUint32(math.Float32bits(value))
}

// EncodeFloat64 encodes a float64 to bytes.
func (ve *ValueEncoder) EncodeFloat64(value float64) []byte {
	buf := make([]byte, 8)
	if ve.byteOrder == filebase.LittleEndian {
		binary.LittleEndian.PutUint64(buf, math.Float64bits(value))
	} else {
		binary.BigEndian.PutUint64(buf, math.Float64bits(value))
	}
	return buf
}

// EncodeMultipleValues encodes multiple values separated by backslash.
func (ve *ValueEncoder) EncodeMultipleValues(values []string) []byte {
	return []byte(strings.Join(values, "\\"))
}

// DecodeString decodes bytes to a string.
func (ve *ValueEncoder) DecodeString(data []byte) string {
	return string(bytes.TrimSpace(data))
}

// DecodeUint16 decodes bytes to a uint16.
func (ve *ValueEncoder) DecodeUint16(data []byte) (uint16, error) {
	if len(data) < 2 {
		return 0, fmt.Errorf("insufficient data for uint16: got %d bytes", len(data))
	}
	if ve.byteOrder == filebase.LittleEndian {
		return binary.LittleEndian.Uint16(data), nil
	}
	return binary.BigEndian.Uint16(data), nil
}

// DecodeUint32 decodes bytes to a uint32.
func (ve *ValueEncoder) DecodeUint32(data []byte) (uint32, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("insufficient data for uint32: got %d bytes", len(data))
	}
	if ve.byteOrder == filebase.LittleEndian {
		return binary.LittleEndian.Uint32(data), nil
	}
	return binary.BigEndian.Uint32(data), nil
}

// DecodeInt16 decodes bytes to an int16.
func (ve *ValueEncoder) DecodeInt16(data []byte) (int16, error) {
	v, err := ve.DecodeUint16(data)
	return int16(v), err
}

// DecodeInt32 decodes bytes to an int32.
func (ve *ValueEncoder) DecodeInt32(data []byte) (int32, error) {
	v, err := ve.DecodeUint32(data)
	return int32(v), err
}

// DecodeFloat32 decodes bytes to a float32.
func (ve *ValueEncoder) DecodeFloat32(data []byte) (float32, error) {
	v, err := ve.DecodeUint32(data)
	return math.Float32frombits(v), err
}

// DecodeFloat64 decodes bytes to a float64.
func (ve *ValueEncoder) DecodeFloat64(data []byte) (float64, error) {
	if len(data) < 8 {
		return 0, fmt.Errorf("insufficient data for float64: got %d bytes", len(data))
	}

	var v uint64
	if ve.byteOrder == filebase.LittleEndian {
		v = binary.LittleEndian.Uint64(data)
	} else {
		v = binary.BigEndian.Uint64(data)
	}
	return math.Float64frombits(v), nil
}

// DecodeMultipleValues decodes bytes to multiple string values.
func (ve *ValueEncoder) DecodeMultipleValues(data []byte) []string {
	return strings.Split(ve.DecodeString(data), "\\")
}

// ValueParser parses DICOM values according to VR rules.
type ValueParser struct{}

// NewValueParser creates a new ValueParser.
func NewValueParser() *ValueParser {
	return &ValueParser{}
}

// ParseIntegerString parses an integer string (IS) value.
func (vp *ValueParser) ParseIntegerString(value string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(value), 10, 64)
}

// ParseDecimalString parses a decimal string (DS) value.
func (vp *ValueParser) ParseDecimalString(value string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(value), 64)
}

// ParseDate parses a date in format YYYYMMDD.
func (vp *ValueParser) ParseDate(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 8 {
		return "", fmt.Errorf("invalid date format: expected YYYYMMDD, got %s", trimmed)
	}
	// Validate it's all digits
	if _, err := strconv.ParseInt(trimmed, 10, 64); err != nil {
		return "", fmt.Errorf("invalid date: %w", err)
	}
	return trimmed, nil
}

// ParseTime parses a time in format HHMMSS or HHMMSSFFFFFF.
func (vp *ValueParser) ParseTime(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 6 {
		return "", fmt.Errorf("invalid time format: expected at least HHMMSS, got %s", trimmed)
	}
	return trimmed, nil
}

// ParsePersonName parses a person name (component groups separated by ^).
func (vp *ValueParser) ParsePersonName(value string) map[string]string {
	parts := strings.Split(value, "^")
	result := make(map[string]string)

	labels := []string{"FamilyName", "GivenName", "MiddleName", "NamePrefix", "NameSuffix"}
	for i, label := range labels {
		if i < len(parts) {
			result[label] = strings.TrimSpace(parts[i])
		}
	}

	return result
}

// ValuePadder handles padding of DICOM values according to VR rules.
type ValuePadder struct{}

// NewValuePadder creates a new ValuePadder.
func NewValuePadder() *ValuePadder {
	return &ValuePadder{}
}

// GetPadByte returns the padding byte for a VR.
func (vp *ValuePadder) GetPadByte(vr dataelem.VR) byte {
	switch vr {
	case dataelem.AE, dataelem.AS, dataelem.CS, dataelem.DA, dataelem.DS:
		return 0x20 // Space
	case dataelem.DT, dataelem.FD, dataelem.FL, dataelem.IS, dataelem.LO:
		return 0x20 // Space
	case dataelem.LT, dataelem.OB, dataelem.OD, dataelem.OF, dataelem.OL:
		return 0x00 // Null
	case dataelem.OW, dataelem.PN, dataelem.SH, dataelem.SL, dataelem.SQ:
		return 0x20 // Space
	case dataelem.SS, dataelem.ST, dataelem.TM, dataelem.UC, dataelem.UI:
		return 0x20 // Space
	case dataelem.UL, dataelem.UN, dataelem.UR, dataelem.UT:
		return 0x20 // Space
	default:
		return 0x00 // Null for unknown
	}
}

// Pad pads a value to even length.
func (vp *ValuePadder) Pad(value []byte, vr dataelem.VR) []byte {
	if len(value)%2 == 1 {
		padByte := vp.GetPadByte(vr)
		return append(value, padByte)
	}
	return value
}

// Unpad removes trailing padding from a value.
func (vp *ValuePadder) Unpad(value []byte, vr dataelem.VR) []byte {
	if len(value) == 0 {
		return value
	}

	padByte := vp.GetPadByte(vr)
	for len(value) > 0 && value[len(value)-1] == padByte {
		value = value[:len(value)-1]
	}
	return value
}

// ValueMultiplicity calculates the value multiplicity.
func (vp *ValuePadder) ValueMultiplicity(value []byte, vr dataelem.VR) int {
	// Backslash-separated values
	if isMultiValueVR(vr) {
		count := strings.Count(string(value), "\\") + 1
		if len(value) == 0 {
			return 0
		}
		return count
	}

	// Check if value is empty
	if len(value) == 0 {
		return 0
	}

	return 1
}

// isMultiValueVR checks if a VR can have multiple values.
func isMultiValueVR(vr dataelem.VR) bool {
	switch vr {
	case dataelem.AE, dataelem.AS, dataelem.AT, dataelem.CS, dataelem.DA, dataelem.DS,
		dataelem.DT, dataelem.FD, dataelem.FL, dataelem.IS, dataelem.LO, dataelem.LT,
		dataelem.OB, dataelem.OD, dataelem.OF, dataelem.OL, dataelem.OW, dataelem.PN,
		dataelem.SH, dataelem.SL, dataelem.SS, dataelem.ST, dataelem.TM, dataelem.UC,
		dataelem.UI, dataelem.UL, dataelem.UR, dataelem.UT:
		return true
	default:
		return false
	}
}

// ValueConverter converts values between different representations.
type ValueConverter struct {
	encoder *ValueEncoder
	parser  *ValueParser
	padder  *ValuePadder
}

// NewValueConverter creates a new ValueConverter.
func NewValueConverter(byteOrder filebase.ByteOrder) *ValueConverter {
	return &ValueConverter{
		encoder: NewValueEncoder(byteOrder),
		parser:  NewValueParser(),
		padder:  NewValuePadder(),
	}
}

// GetEncoder returns the encoder.
func (vc *ValueConverter) GetEncoder() *ValueEncoder {
	return vc.encoder
}

// GetParser returns the parser.
func (vc *ValueConverter) GetParser() *ValueParser {
	return vc.parser
}

// GetPadder returns the padder.
func (vc *ValueConverter) GetPadder() *ValuePadder {
	return vc.padder
}

// ConvertToString converts a value to string.
func (vc *ValueConverter) ConvertToString(value interface{}, vr dataelem.VR) (string, error) {
	if b, ok := value.([]byte); ok {
		return vc.encoder.DecodeString(b), nil
	}

	// Try to convert other types
	switch v := value.(type) {
	case string:
		return v, nil
	case int:
		return strconv.Itoa(v), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// ConvertToBytes converts a value to bytes.
func (vc *ValueConverter) ConvertToBytes(value interface{}, vr dataelem.VR) ([]byte, error) {
	if b, ok := value.([]byte); ok {
		return b, nil
	}

	switch v := value.(type) {
	case string:
		return vc.encoder.EncodeString(v), nil
	case int:
		return vc.encoder.EncodeInt32(int32(v)), nil
	case int32:
		return vc.encoder.EncodeInt32(v), nil
	case uint32:
		return vc.encoder.EncodeUint32(v), nil
	case float32:
		return vc.encoder.EncodeFloat32(v), nil
	case float64:
		return vc.encoder.EncodeFloat64(v), nil
	default:
		return []byte(fmt.Sprintf("%v", v)), nil
	}
}

// ValidateLength validates a value length for a VR.
func ValidateLength(value []byte, vr dataelem.VR) error {
	if len(value)%2 != 0 && vr != dataelem.OB && vr != dataelem.OW {
		return fmt.Errorf("odd length value not allowed for VR %s", vr)
	}

	// Check VR-specific limits
	switch vr {
	case dataelem.AE, dataelem.AS, dataelem.CS, dataelem.DA, dataelem.DS:
		if len(value) > 256 {
			return fmt.Errorf("value too long for VR %s (max 256 bytes)", vr)
		}
	case dataelem.FD, dataelem.FL, dataelem.IS, dataelem.LO, dataelem.OB:
		// No strict limit
	}

	return nil
}

// ExecuteEncodingHook executes a hook for value encoding operations.
func ExecuteEncodingHook(vr dataelem.VR, value interface{}, byteOrder filebase.ByteOrder) map[string]interface{} {
	result := make(map[string]interface{})
	result["vr"] = string(vr)
	result["value_type"] = fmt.Sprintf("%T", value)
	result["byte_order"] = byteOrder.String()
	return result
}

// ExecuteDecodingHook executes a hook for value decoding operations.
func ExecuteDecodingHook(vr dataelem.VR, bytesLength int, byteOrder filebase.ByteOrder) map[string]interface{} {
	result := make(map[string]interface{})
	result["vr"] = string(vr)
	result["bytes_length"] = bytesLength
	result["byte_order"] = byteOrder.String()
	return result
}

// ExecutePaddingHook executes a hook for padding operations.
func ExecutePaddingHook(vr dataelem.VR, originalLength, paddedLength int) map[string]interface{} {
	result := make(map[string]interface{})
	result["vr"] = string(vr)
	result["original_length"] = originalLength
	result["padded_length"] = paddedLength
	result["padding_bytes"] = paddedLength - originalLength
	return result
}

// ExecuteValidationHook executes a hook for validation operations.
func ExecuteValidationHook(vr dataelem.VR, value []byte, valid bool, errorMsg string) map[string]interface{} {
	result := make(map[string]interface{})
	result["vr"] = string(vr)
	result["value_length"] = len(value)
	result["valid"] = valid
	if errorMsg != "" {
		result["error"] = errorMsg
	}
	return result
}
