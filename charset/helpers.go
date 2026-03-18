package charset

import (
	"context"
	"fmt"
)

// DecodeString is a convenience function that decodes a byte slice with a single encoding.
func DecodeString(value []byte, encoding string) (string, error) {
	encodings, err := ConvertEncodings([]string{encoding})
	if err != nil {
		return "", err
	}

	return DecodeBytes(value, encodings, DefaultTextDelimiters)
}

// EncodeToBytes is a convenience function that encodes a string with a single encoding.
func EncodeToBytes(value, encoding string) ([]byte, error) {
	encodings, err := ConvertEncodings([]string{encoding})
	if err != nil {
		return nil, err
	}

	return EncodeString(value, encodings)
}

// DecodeWithCharset decodes bytes using a CharacterSet object.
func DecodeWithCharset(value []byte, cs *CharacterSet) (string, error) {
	if cs == nil {
		return DecodeBytes(value, []string{DefaultEncoding}, DefaultTextDelimiters)
	}
	return DecodeBytes(value, cs.Encodings, DefaultTextDelimiters)
}

// EncodeWithCharset encodes a string using a CharacterSet object.
func EncodeWithCharset(value string, cs *CharacterSet) ([]byte, error) {
	if cs == nil {
		return EncodeString(value, []string{DefaultEncoding})
	}
	return EncodeString(value, cs.Encodings)
}

// ValidateEncoding checks if a DICOM encoding name is valid and supported.
func ValidateEncoding(encoding string) error {
	if encoding == "" {
		return nil // Empty is valid (default)
	}

	info := GetEncodingInfo(encoding)
	if info == nil {
		// Try as Go encoding name
		if !isValidGoEncoding(encoding) {
			return fmt.Errorf("unknown or unsupported encoding: %s", encoding)
		}
	}

	return nil
}

// ValidateEncodings checks if multiple DICOM encoding names are valid.
func ValidateEncodings(encodings []string) error {
	if len(encodings) == 0 {
		return nil
	}

	// Validate each encoding
	for _, enc := range encodings {
		if err := ValidateEncoding(enc); err != nil {
			return err
		}
	}

	// Validate stand-alone encoding rules
	if len(encodings) > 1 {
		// Check if first is stand-alone
		if IsStandAloneEncoding(encodings[0]) {
			return fmt.Errorf("stand-alone encoding %s cannot be used with code extensions", encodings[0])
		}

		// Check for stand-alone in other positions
		for i := 1; i < len(encodings); i++ {
			if IsStandAloneEncoding(encodings[i]) {
				return fmt.Errorf("stand-alone encoding %s cannot be used in code extensions (position %d)", encodings[i], i+1)
			}
		}
	}

	return nil
}

// GetSupportedEncodings returns a list of all supported DICOM encoding names.
func GetSupportedEncodings() []string {
	initializeEncodingMaps()

	encodings := make([]string, 0, len(encodingInfoMap))
	seen := make(map[string]bool)

	for _, info := range encodingInfoList {
		if info.DicomName != "" && !seen[info.DicomName] {
			encodings = append(encodings, info.DicomName)
			seen[info.DicomName] = true
		}
	}

	return encodings
}

// GetEncodingDescription returns a human-readable description of an encoding.
func GetEncodingDescription(encoding string) string {
	info := GetEncodingInfo(encoding)
	if info == nil {
		return "Unknown encoding"
	}
	return info.Description
}

// IsMultiByteEncoding checks if an encoding is a multi-byte encoding.
func IsMultiByteEncoding(encoding string) bool {
	info := GetEncodingInfo(encoding)
	return info != nil && info.IsMultiByte()
}

// NormalizePersonName normalizes a PersonName string for searching/comparison.
func NormalizePersonName(pn string) string {
	// Trim whitespace
	result := ""
	for _, char := range pn {
		if char != ' ' && char != '\t' && char != '\n' && char != '\r' {
			result += string(char)
		}
	}

	// Remove trailing delimiters
	for len(result) > 0 {
		lastChar := result[len(result)-1]
		if lastChar == '^' || lastChar == '=' {
			result = result[:len(result)-1]
		} else {
			break
		}
	}

	return result
}

// SplitMultiValue splits a multi-valued text string by backslash delimiter.
func SplitMultiValue(value string) []string {
	if value == "" {
		return []string{}
	}
	return splitByBackslash(value)
}

// JoinMultiValue joins multiple values into a multi-valued string.
func JoinMultiValue(values []string) string {
	result := ""
	for i, v := range values {
		if i > 0 {
			result += "\\"
		}
		result += v
	}
	return result
}

// splitByBackslash splits a string by backslash, handling escaped backslashes.
func splitByBackslash(s string) []string {
	var parts []string
	current := ""

	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(s[i])
		}
	}

	if current != "" || len(parts) > 0 {
		parts = append(parts, current)
	}

	return parts
}

// DecodeElement decodes a DICOM element value based on its VR.
func DecodeElement(ctx context.Context, value []byte, vr string, cs *CharacterSet) (interface{}, error) {
	if len(value) == 0 {
		return "", nil
	}

	encodings := []string{DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	// PersonName VR uses special decoding
	if vr == "PN" {
		return DecodePersonNameWithContext(ctx, value, encodings)
	}

	// Text VRs use standard decoding
	return DecodeBytesWithContext(ctx, value, encodings, DefaultTextDelimiters)
}

// EncodeElement encodes a value for a DICOM element based on its VR.
func EncodeElement(ctx context.Context, value interface{}, vr string, cs *CharacterSet) ([]byte, error) {
	encodings := []string{DefaultEncoding}
	if cs != nil {
		encodings = cs.Encodings
	}

	// Handle PersonName
	if vr == "PN" {
		if pn, ok := value.(*PersonName); ok {
			return EncodePersonNameWithContext(ctx, pn, encodings)
		}
		// If it's a string, convert to PersonName first
		if str, ok := value.(string); ok {
			pn := &PersonName{Alphabetic: str}
			return EncodePersonNameWithContext(ctx, pn, encodings)
		}
	}

	// Handle string values
	if str, ok := value.(string); ok {
		return EncodeStringWithContext(ctx, str, encodings)
	}

	return nil, fmt.Errorf("unsupported value type: %T", value)
}
