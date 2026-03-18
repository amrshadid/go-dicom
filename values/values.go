package values

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// ConvertValue converts raw bytes to appropriate Go type based on VR.
func ConvertValue(vr string, rawValue []byte, isLittleEndian bool) (interface{}, error) {
	if len(rawValue) == 0 {
		return nil, nil
	}

	switch vr {
	case "AE", "AS", "CS", "LO", "LT", "SH", "ST", "UC", "UR", "UT", "DA", "DT", "TM", "UI":
		return string(bytes.TrimSpace(rawValue)), nil

	case "PN":
		return string(rawValue), nil

	case "DS":
		trimmed := strings.TrimSpace(string(rawValue))
		f, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return trimmed, nil
		}
		return f, nil

	case "IS":
		trimmed := strings.TrimSpace(string(rawValue))
		i, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return trimmed, nil
		}
		return i, nil

	case "FD":
		if len(rawValue) >= 8 {
			var f float64
			reader := bytes.NewReader(rawValue[:8])
			if isLittleEndian {
				if err := binary.Read(reader, binary.LittleEndian, &f); err != nil {
					return nil, fmt.Errorf("failed to read FD value: %w", err)
				}
			} else {
				if err := binary.Read(reader, binary.BigEndian, &f); err != nil {
					return nil, fmt.Errorf("failed to read FD value: %w", err)
				}
			}
			return f, nil
		}
		return nil, fmt.Errorf("FD value too short: %d bytes", len(rawValue))

	case "FL":
		if len(rawValue) >= 4 {
			var f float32
			reader := bytes.NewReader(rawValue[:4])
			if isLittleEndian {
				if err := binary.Read(reader, binary.LittleEndian, &f); err != nil {
					return nil, fmt.Errorf("failed to read FL value: %w", err)
				}
			} else {
				if err := binary.Read(reader, binary.BigEndian, &f); err != nil {
					return nil, fmt.Errorf("failed to read FL value: %w", err)
				}
			}
			return float64(f), nil
		}
		return nil, fmt.Errorf("FL value too short: %d bytes", len(rawValue))

	case "SL":
		if len(rawValue) >= 4 {
			var i int32
			reader := bytes.NewReader(rawValue[:4])
			if isLittleEndian {
				if err := binary.Read(reader, binary.LittleEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read SL value: %w", err)
				}
			} else {
				if err := binary.Read(reader, binary.BigEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read SL value: %w", err)
				}
			}
			return int64(i), nil
		}
		return nil, fmt.Errorf("SL value too short: %d bytes", len(rawValue))

	case "SS":
		if len(rawValue) >= 2 {
			var i int16
			reader := bytes.NewReader(rawValue[:2])
			if isLittleEndian {
				if err := binary.Read(reader, binary.LittleEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read SS value: %w", err)
				}
			} else {
				if err := binary.Read(reader, binary.BigEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read SS value: %w", err)
				}
			}
			return int64(i), nil
		}
		return nil, fmt.Errorf("SS value too short: %d bytes", len(rawValue))

	case "UL":
		if len(rawValue) >= 4 {
			var i uint32
			reader := bytes.NewReader(rawValue[:4])
			if isLittleEndian {
				if err := binary.Read(reader, binary.LittleEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read UL value: %w", err)
				}
			} else {
				if err := binary.Read(reader, binary.BigEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read UL value: %w", err)
				}
			}
			return uint64(i), nil
		}
		return nil, fmt.Errorf("UL value too short: %d bytes", len(rawValue))

	case "US":
		if len(rawValue) >= 2 {
			var i uint16
			reader := bytes.NewReader(rawValue[:2])
			if isLittleEndian {
				if err := binary.Read(reader, binary.LittleEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read US value: %w", err)
				}
			} else {
				if err := binary.Read(reader, binary.BigEndian, &i); err != nil {
					return nil, fmt.Errorf("failed to read US value: %w", err)
				}
			}
			return uint64(i), nil
		}
		return nil, fmt.Errorf("US value too short: %d bytes", len(rawValue))

	case "OB", "OD", "OF", "OL", "OW", "UN":
		return rawValue, nil

	case "AT":
		if len(rawValue) >= 4 {
			return rawValue, nil
		}
		return nil, fmt.Errorf("AT value too short: %d bytes", len(rawValue))

	case "SQ":
		return rawValue, nil

	default:
		return rawValue, nil
	}
}

// MultiString splits a multi-valued string separated by backslash.
func MultiString(value string, converter func(string) interface{}) []interface{} {
	if value == "" {
		return []interface{}{}
	}

	items := strings.Split(value, "\\")
	result := make([]interface{}, len(items))

	for i, item := range items {
		if converter != nil {
			result[i] = converter(strings.TrimSpace(item))
		} else {
			result[i] = strings.TrimSpace(item)
		}
	}

	return result
}

// MultiStringInt parses multi-valued integer strings separated by backslash.
func MultiStringInt(value string) []int64 {
	if value == "" {
		return []int64{}
	}

	items := strings.Split(value, "\\")
	result := make([]int64, 0, len(items))

	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			result = append(result, i)
		}
	}

	return result
}

// MultiStringFloat parses multi-valued decimal strings separated by backslash.
func MultiStringFloat(value string) []float64 {
	if value == "" {
		return []float64{}
	}

	items := strings.Split(value, "\\")
	result := make([]float64, 0, len(items))

	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			result = append(result, f)
		}
	}

	return result
}

// ConvertTag converts raw bytes to a DICOM tag group and element.
func ConvertTag(rawTag []byte, isLittleEndian bool) (uint16, uint16, error) {
	if len(rawTag) < 4 {
		return 0, 0, fmt.Errorf("tag bytes too short: %d", len(rawTag))
	}

	reader := bytes.NewReader(rawTag[:4])
	var group, element uint16

	if isLittleEndian {
		if err := binary.Read(reader, binary.LittleEndian, &group); err != nil {
			return 0, 0, fmt.Errorf("failed to read tag group: %w", err)
		}
		if err := binary.Read(reader, binary.LittleEndian, &element); err != nil {
			return 0, 0, fmt.Errorf("failed to read tag element: %w", err)
		}
	} else {
		if err := binary.Read(reader, binary.BigEndian, &group); err != nil {
			return 0, 0, fmt.Errorf("failed to read tag group: %w", err)
		}
		if err := binary.Read(reader, binary.BigEndian, &element); err != nil {
			return 0, 0, fmt.Errorf("failed to read tag element: %w", err)
		}
	}

	return group, element, nil
}

// EncodeValue converts a Go value to bytes for a given VR.
func EncodeValue(vr string, value interface{}, isLittleEndian bool) ([]byte, error) {
	switch vr {
	case "AE", "AS", "CS", "DA", "DT", "LO", "LT", "SH", "ST", "TM", "UI", "UC", "UR", "UT", "PN":
		if str, ok := value.(string); ok {
			return []byte(str), nil
		}
		return nil, fmt.Errorf("expected string for VR %s, got %T", vr, value)

	case "DS":
		if f, ok := value.(float64); ok {
			return []byte(fmt.Sprintf("%.6f", f)), nil
		}
		if str, ok := value.(string); ok {
			return []byte(str), nil
		}
		return nil, fmt.Errorf("expected float64 or string for VR DS, got %T", value)

	case "IS":
		if i, ok := value.(int64); ok {
			return []byte(fmt.Sprintf("%d", i)), nil
		}
		if str, ok := value.(string); ok {
			return []byte(str), nil
		}
		return nil, fmt.Errorf("expected int64 or string for VR IS, got %T", value)

	case "FD":
		if f, ok := value.(float64); ok {
			buf := new(bytes.Buffer)
			if isLittleEndian {
				if err := binary.Write(buf, binary.LittleEndian, f); err != nil {
					return nil, fmt.Errorf("failed to write FD value: %w", err)
				}
			} else {
				if err := binary.Write(buf, binary.BigEndian, f); err != nil {
					return nil, fmt.Errorf("failed to write FD value: %w", err)
				}
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("expected float64 for VR FD, got %T", value)

	case "FL":
		if f, ok := value.(float64); ok {
			buf := new(bytes.Buffer)
			f32 := float32(f)
			if isLittleEndian {
				if err := binary.Write(buf, binary.LittleEndian, f32); err != nil {
					return nil, fmt.Errorf("failed to write FL value: %w", err)
				}
			} else {
				if err := binary.Write(buf, binary.BigEndian, f32); err != nil {
					return nil, fmt.Errorf("failed to write FL value: %w", err)
				}
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("expected float64 for VR FL, got %T", value)

	case "SL":
		if i, ok := value.(int64); ok {
			buf := new(bytes.Buffer)
			if isLittleEndian {
				if err := binary.Write(buf, binary.LittleEndian, int32(i)); err != nil {
					return nil, fmt.Errorf("failed to write SL value: %w", err)
				}
			} else {
				if err := binary.Write(buf, binary.BigEndian, int32(i)); err != nil {
					return nil, fmt.Errorf("failed to write SL value: %w", err)
				}
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("expected int64 for VR SL, got %T", value)

	case "SS":
		if i, ok := value.(int64); ok {
			buf := new(bytes.Buffer)
			if isLittleEndian {
				if err := binary.Write(buf, binary.LittleEndian, int16(i)); err != nil {
					return nil, fmt.Errorf("failed to write SS value: %w", err)
				}
			} else {
				if err := binary.Write(buf, binary.BigEndian, int16(i)); err != nil {
					return nil, fmt.Errorf("failed to write SS value: %w", err)
				}
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("expected int64 for VR SS, got %T", value)

	case "UL":
		if i, ok := value.(uint64); ok {
			buf := new(bytes.Buffer)
			if isLittleEndian {
				if err := binary.Write(buf, binary.LittleEndian, uint32(i)); err != nil {
					return nil, fmt.Errorf("failed to write UL value: %w", err)
				}
			} else {
				if err := binary.Write(buf, binary.BigEndian, uint32(i)); err != nil {
					return nil, fmt.Errorf("failed to write UL value: %w", err)
				}
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("expected uint64 for VR UL, got %T", value)

	case "US":
		if i, ok := value.(uint64); ok {
			buf := new(bytes.Buffer)
			if isLittleEndian {
				if err := binary.Write(buf, binary.LittleEndian, uint16(i)); err != nil {
					return nil, fmt.Errorf("failed to write US value: %w", err)
				}
			} else {
				if err := binary.Write(buf, binary.BigEndian, uint16(i)); err != nil {
					return nil, fmt.Errorf("failed to write US value: %w", err)
				}
			}
			return buf.Bytes(), nil
		}
		return nil, fmt.Errorf("expected uint64 for VR US, got %T", value)

	case "OB", "OD", "OF", "OL", "OW", "UN", "AT", "SQ":
		if bytes, ok := value.([]byte); ok {
			return bytes, nil
		}
		return nil, fmt.Errorf("expected []byte for VR %s, got %T", vr, value)

	default:
		return nil, fmt.Errorf("unsupported VR: %s", vr)
	}
}

// RawDataElement represents raw DICOM data before value conversion.
type RawDataElement struct {
	Tag   string
	VR    *string
	Value []byte
}

// ParsedValue represents a parsed DICOM value.
type ParsedValue struct {
	VR      string
	Value   interface{}
	IsMulti bool
}

// SanitizeString removes null bytes and trailing spaces from a string.
func SanitizeString(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimRight(s, " ")
}

// PadString pads a string to a given length with spaces.
func PadString(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat(" ", length-len(s))
}

// ValidateNumericString checks if a string can be parsed as a number.
func ValidateNumericString(vr string, value string) error {
	switch vr {
	case "DS":
		_, err := strconv.ParseFloat(value, 64)
		return err
	case "IS":
		_, err := strconv.ParseInt(value, 10, 64)
		return err
	default:
		return fmt.Errorf("VR %s is not numeric", vr)
	}
}
