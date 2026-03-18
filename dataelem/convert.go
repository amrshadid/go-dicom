package dataelem

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/amrshadid/go-dicom/tag"
)

// ConvertRawDataElement converts a RawDataElement to a fully-typed DataElement
// by parsing the raw bytes according to the VR type.
//
// This function implements lazy evaluation - raw data elements can be stored
// and only converted to typed values when needed.
func ConvertRawDataElement(raw *RawDataElement) (*DataElement, error) {
	if raw == nil {
		return nil, fmt.Errorf("raw data element is nil")
	}

	// Create base data element
	de := &DataElement{
		tag: raw.Tag(),
		VR:  raw.VR(),
	}

	// Populate keyword and description from dictionary
	dict := tag.GlobalDictionary()
	if info := dict.Get(raw.Tag()); info != nil {
		de.Keyword = info.Keyword
		de.Description = info.Name
	}

	// Convert value based on VR
	value, vm, err := convertValueByVR(raw.VR(), raw.Value(), raw.IsLittleEndian())
	if err != nil {
		return nil, fmt.Errorf("failed to convert value for VR %s: %w", raw.VR(), err)
	}

	de.Value = value
	de.VM = vm

	return de, nil
}

// convertValueByVR converts raw bytes to appropriate Go type based on VR.
// Returns the converted value and the Value Multiplicity (VM).
func convertValueByVR(vr VR, rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	// Handle nil/empty values
	if len(rawValue) == 0 {
		return emptyValueForVR(vr), 0, nil
	}

	switch vr {
	// Integer String - convert to int or []int
	case IS:
		return convertIS(rawValue)

	// Decimal String - convert to float64 or []float64
	case DS:
		return convertDS(rawValue)

	// Date - convert to time.Time or []time.Time
	case DA:
		return convertDA(rawValue)

	// DateTime - convert to time.Time or []time.Time
	case DT:
		return convertDT(rawValue)

	// Time - convert to time.Time or []time.Time (with date set to zero)
	case TM:
		return convertTM(rawValue)

	// Person Name - convert to PersonName or []PersonName
	case PN:
		return convertPN(rawValue)

	// Attribute Tag - convert to tag.Tag or []tag.Tag
	case AT:
		return convertAT(rawValue, isLittleEndian)

	// Sequence - special handling (returns empty for now, should be handled by parser)
	case SQ:
		return []SequenceItem{}, 0, nil

	// Text VRs - split by backslash for multi-value
	case AE, AS, CS, LO, SH, UC, UI:
		return convertTextVR(rawValue)

	// Long text VRs - no backslash splitting
	case LT, ST, UT:
		return string(rawValue), 1, nil

	// Binary numeric VRs - convert based on type
	case SS: // Signed Short (int16)
		return convertSS(rawValue, isLittleEndian)

	case US: // Unsigned Short (uint16) - represented as int for consistency
		return convertUS(rawValue, isLittleEndian)

	case SL: // Signed Long (int32)
		return convertSL(rawValue, isLittleEndian)

	case UL: // Unsigned Long (uint32) - represented as int64 for safety
		return convertUL(rawValue, isLittleEndian)

	case FL: // Float (float32)
		return convertFL(rawValue, isLittleEndian)

	case FD: // Double (float64)
		return convertFD(rawValue, isLittleEndian)

	// Binary data VRs - return raw bytes
	case OB, OD, OF, OL, OW, UN, UR:
		return rawValue, 1, nil

	default:
		// Unknown VR - return raw bytes
		return rawValue, 1, nil
	}
}

// convertIS converts Integer String to int or []int.
// Format: "123" or "123\\456\\789"
func convertIS(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash for multi-value
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		val, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid IS value: %s", parts[0])
		}
		return val, 1, nil
	}

	// Multi-value
	values := make([]int, len(parts))
	for i, part := range parts {
		val, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid IS value at index %d: %s", i, part)
		}
		values[i] = val
	}
	return values, len(values), nil
}

// convertDS converts Decimal String to float64 or []float64.
// Format: "123.45" or "123.45\\67.89"
func convertDS(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash for multi-value
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		val, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid DS value: %s", parts[0])
		}
		return val, 1, nil
	}

	// Multi-value
	values := make([]float64, len(parts))
	for i, part := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid DS value at index %d: %s", i, part)
		}
		values[i] = val
	}
	return values, len(values), nil
}

// convertDA converts Date to time.Time or []time.Time.
// Format: YYYYMMDD (e.g., "20231015")
func convertDA(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash for multi-value
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		val, err := parseDA(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, 0, err
		}
		return val, 1, nil
	}

	// Multi-value
	values := make([]time.Time, len(parts))
	for i, part := range parts {
		val, err := parseDA(strings.TrimSpace(part))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid DA value at index %d: %w", i, err)
		}
		values[i] = val
	}
	return values, len(values), nil
}

// parseDA parses a DICOM date string (YYYYMMDD).
// Supports full format (YYYYMMDD) and partial formats (YYYY, YYYYMM).
func parseDA(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	// Remove any trailing spaces
	s = strings.TrimSpace(s)

	var year, month, day int
	var err error

	switch len(s) {
	case 4: // YYYY
		year, err = strconv.Atoi(s)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DA year: %s", s)
		}
		month = 1
		day = 1

	case 6: // YYYYMM
		year, err = strconv.Atoi(s[0:4])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DA year: %s", s[0:4])
		}
		month, err = strconv.Atoi(s[4:6])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DA month: %s", s[4:6])
		}
		day = 1

	case 8: // YYYYMMDD
		year, err = strconv.Atoi(s[0:4])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DA year: %s", s[0:4])
		}
		month, err = strconv.Atoi(s[4:6])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DA month: %s", s[4:6])
		}
		day, err = strconv.Atoi(s[6:8])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DA day: %s", s[6:8])
		}

	default:
		return time.Time{}, fmt.Errorf("invalid DA length: %d (expected 4, 6, or 8)", len(s))
	}

	// Validate month and day ranges
	if month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("invalid DA month: %d (must be 1-12)", month)
	}
	if day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("invalid DA day: %d (must be 1-31)", day)
	}

	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}

// convertDT converts DateTime to time.Time or []time.Time.
// Format: YYYYMMDDHHMMSS.FFFFFF&ZZZZ (e.g., "20231015143000.123456-0400")
func convertDT(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash for multi-value
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		val, err := parseDT(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, 0, err
		}
		return val, 1, nil
	}

	// Multi-value
	values := make([]time.Time, len(parts))
	for i, part := range parts {
		val, err := parseDT(strings.TrimSpace(part))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid DT value at index %d: %w", i, err)
		}
		values[i] = val
	}
	return values, len(values), nil
}

// parseDT parses a DICOM datetime string.
// Format: YYYYMMDDHHMMSS.FFFFFF±ZZZZ
// Supports partial formats and optional timezone offsets.
func parseDT(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	s = strings.TrimSpace(s)

	// Extract timezone offset if present (+ or -)
	var tzOffset int // minutes offset from UTC
	var hasTZ bool
	tzIndex := strings.IndexAny(s, "+-")
	if tzIndex > 0 { // Make sure it's not at the start
		tzStr := s[tzIndex:]
		s = s[:tzIndex]

		if len(tzStr) >= 5 { // ±HHMM
			sign := 1
			if tzStr[0] == '-' {
				sign = -1
			}

			hours, err := strconv.Atoi(tzStr[1:3])
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid DT timezone hours: %s", tzStr[1:3])
			}
			mins, err := strconv.Atoi(tzStr[3:5])
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid DT timezone minutes: %s", tzStr[3:5])
			}

			tzOffset = sign * (hours*60 + mins)
			hasTZ = true
		}
	}

	// Extract fractional seconds if present
	var fracSec int // nanoseconds
	dotIndex := strings.Index(s, ".")
	if dotIndex > 0 {
		fracStr := s[dotIndex+1:]
		s = s[:dotIndex]

		// Pad or truncate to 9 digits (nanoseconds)
		for len(fracStr) < 9 {
			fracStr += "0"
		}
		if len(fracStr) > 9 {
			fracStr = fracStr[:9]
		}

		var err error
		fracSec, err = strconv.Atoi(fracStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DT fractional seconds: %s", fracStr)
		}
	}

	// Parse date and time components
	var year, month, day, hour, min, sec int
	var err error

	// Year (required minimum)
	if len(s) < 4 {
		return time.Time{}, fmt.Errorf("invalid DT: too short (minimum YYYY required)")
	}

	year, err = strconv.Atoi(s[0:4])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid DT year: %s", s[0:4])
	}
	month = 1
	day = 1

	// Parse remaining components based on length
	if len(s) >= 6 {
		month, err = strconv.Atoi(s[4:6])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DT month: %s", s[4:6])
		}
	}
	if len(s) >= 8 {
		day, err = strconv.Atoi(s[6:8])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DT day: %s", s[6:8])
		}
	}
	if len(s) >= 10 {
		hour, err = strconv.Atoi(s[8:10])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DT hour: %s", s[8:10])
		}
	}
	if len(s) >= 12 {
		min, err = strconv.Atoi(s[10:12])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DT minute: %s", s[10:12])
		}
	}
	if len(s) >= 14 {
		sec, err = strconv.Atoi(s[12:14])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid DT second: %s", s[12:14])
		}
	}

	// Validate ranges
	if month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("invalid DT month: %d", month)
	}
	if day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("invalid DT day: %d", day)
	}
	if hour < 0 || hour > 23 {
		return time.Time{}, fmt.Errorf("invalid DT hour: %d", hour)
	}
	if min < 0 || min > 59 {
		return time.Time{}, fmt.Errorf("invalid DT minute: %d", min)
	}
	if sec < 0 || sec > 59 {
		return time.Time{}, fmt.Errorf("invalid DT second: %d", sec)
	}

	// Create time in appropriate timezone
	var loc *time.Location
	if hasTZ {
		loc = time.FixedZone("", tzOffset*60) // Convert minutes to seconds
	} else {
		loc = time.UTC
	}

	return time.Date(year, time.Month(month), day, hour, min, sec, fracSec, loc), nil
}

// convertTM converts Time to time.Time or []time.Time (with date part set to zero).
// Format: HHMMSS.FFFFFF (e.g., "143000.123456")
func convertTM(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash for multi-value
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		val, err := parseTM(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, 0, err
		}
		return val, 1, nil
	}

	// Multi-value
	values := make([]time.Time, len(parts))
	for i, part := range parts {
		val, err := parseTM(strings.TrimSpace(part))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid TM value at index %d: %w", i, err)
		}
		values[i] = val
	}
	return values, len(values), nil
}

// parseTM parses a DICOM time string.
// Format: HHMMSS.FFFFFF
// Supports partial formats (HH, HHMM, HHMMSS) with optional fractional seconds.
// Date is set to zero (0000-01-01).
func parseTM(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	s = strings.TrimSpace(s)

	// Extract fractional seconds if present
	var fracSec int // nanoseconds
	dotIndex := strings.Index(s, ".")
	if dotIndex > 0 {
		fracStr := s[dotIndex+1:]
		s = s[:dotIndex]

		// Pad or truncate to 9 digits (nanoseconds)
		for len(fracStr) < 9 {
			fracStr += "0"
		}
		if len(fracStr) > 9 {
			fracStr = fracStr[:9]
		}

		var err error
		fracSec, err = strconv.Atoi(fracStr)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid TM fractional seconds: %s", fracStr)
		}
	}

	var hour, min, sec int
	var err error

	// Parse time components based on length
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("invalid TM: too short (minimum HH required)")
	}

	hour, err = strconv.Atoi(s[0:2])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid TM hour: %s", s[0:2])
	}

	if len(s) >= 4 {
		min, err = strconv.Atoi(s[2:4])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid TM minute: %s", s[2:4])
		}
	}

	if len(s) >= 6 {
		sec, err = strconv.Atoi(s[4:6])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid TM second: %s", s[4:6])
		}
	}

	// Validate ranges
	if hour < 0 || hour > 23 {
		return time.Time{}, fmt.Errorf("invalid TM hour: %d (must be 0-23)", hour)
	}
	if min < 0 || min > 59 {
		return time.Time{}, fmt.Errorf("invalid TM minute: %d (must be 0-59)", min)
	}
	if sec < 0 || sec > 59 {
		return time.Time{}, fmt.Errorf("invalid TM second: %d (must be 0-59)", sec)
	}

	// Use zero date (year 0, month 1, day 1) as per DICOM standard
	return time.Date(0, 1, 1, hour, min, sec, fracSec, time.UTC), nil
}

// PersonName represents a DICOM Person Name with its components.
type PersonName struct {
	Alphabetic  string // Alphabetic representation
	Ideographic string // Ideographic representation (for Asian languages)
	Phonetic    string // Phonetic representation
}

// String returns the alphabetic representation of the person name.
func (pn PersonName) String() string {
	return pn.Alphabetic
}

// convertPN converts Person Name to PersonName or []PersonName.
// Format: "LastName^FirstName^MiddleName^Prefix^Suffix"
// Multi-component: "Alpha=Ideo=Phone" where each component follows the above format.
func convertPN(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash for multi-value (multiple persons)
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		val := parsePN(strings.TrimSpace(parts[0]))
		return val, 1, nil
	}

	// Multi-value
	values := make([]PersonName, len(parts))
	for i, part := range parts {
		values[i] = parsePN(strings.TrimSpace(part))
	}
	return values, len(values), nil
}

// parsePN parses a single Person Name string.
func parsePN(s string) PersonName {
	pn := PersonName{}

	// Split by = for component groups (Alphabetic=Ideographic=Phonetic)
	components := strings.Split(s, "=")

	if len(components) > 0 {
		pn.Alphabetic = components[0]
	}
	if len(components) > 1 {
		pn.Ideographic = components[1]
	}
	if len(components) > 2 {
		pn.Phonetic = components[2]
	}

	return pn
}

// convertAT converts Attribute Tag to tag.Tag or []tag.Tag.
// Format: 2 uint16 values (group, element) per tag.
func convertAT(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%4 != 0 {
		return nil, 0, fmt.Errorf("invalid AT value length: %d (must be multiple of 4)", len(rawValue))
	}

	numTags := len(rawValue) / 4
	if numTags == 0 {
		return nil, 0, nil
	}

	if numTags == 1 {
		// Single tag
		var group, elem uint16
		if isLittleEndian {
			group = binary.LittleEndian.Uint16(rawValue[0:2])
			elem = binary.LittleEndian.Uint16(rawValue[2:4])
		} else {
			group = binary.BigEndian.Uint16(rawValue[0:2])
			elem = binary.BigEndian.Uint16(rawValue[2:4])
		}
		return tag.New(group, elem), 1, nil
	}

	// Multiple tags
	tags := make([]tag.Tag, numTags)
	for i := 0; i < numTags; i++ {
		offset := i * 4
		var group, elem uint16
		if isLittleEndian {
			group = binary.LittleEndian.Uint16(rawValue[offset : offset+2])
			elem = binary.LittleEndian.Uint16(rawValue[offset+2 : offset+4])
		} else {
			group = binary.BigEndian.Uint16(rawValue[offset : offset+2])
			elem = binary.BigEndian.Uint16(rawValue[offset+2 : offset+4])
		}
		tags[i] = tag.New(group, elem)
	}
	return tags, numTags, nil
}

// convertTextVR converts text VRs that support backslash-separated multi-value.
func convertTextVR(rawValue []byte) (interface{}, int, error) {
	str := strings.TrimSpace(string(rawValue))
	if str == "" {
		return nil, 0, nil
	}

	// Split by backslash
	parts := strings.Split(str, "\\")
	if len(parts) == 1 {
		// Single value
		return parts[0], 1, nil
	}

	// Multi-value
	return parts, len(parts), nil
}

// Binary numeric conversion functions

func convertSS(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%2 != 0 {
		return nil, 0, fmt.Errorf("invalid SS value length: %d (must be multiple of 2)", len(rawValue))
	}

	numValues := len(rawValue) / 2
	if numValues == 0 {
		return nil, 0, nil
	}

	if numValues == 1 {
		var val int16
		if isLittleEndian {
			val = int16(binary.LittleEndian.Uint16(rawValue))
		} else {
			val = int16(binary.BigEndian.Uint16(rawValue))
		}
		return int(val), 1, nil
	}

	values := make([]int, numValues)
	for i := 0; i < numValues; i++ {
		offset := i * 2
		var val int16
		if isLittleEndian {
			val = int16(binary.LittleEndian.Uint16(rawValue[offset : offset+2]))
		} else {
			val = int16(binary.BigEndian.Uint16(rawValue[offset : offset+2]))
		}
		values[i] = int(val)
	}
	return values, numValues, nil
}

func convertUS(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%2 != 0 {
		return nil, 0, fmt.Errorf("invalid US value length: %d (must be multiple of 2)", len(rawValue))
	}

	numValues := len(rawValue) / 2
	if numValues == 0 {
		return nil, 0, nil
	}

	if numValues == 1 {
		var val uint16
		if isLittleEndian {
			val = binary.LittleEndian.Uint16(rawValue)
		} else {
			val = binary.BigEndian.Uint16(rawValue)
		}
		return int(val), 1, nil
	}

	values := make([]int, numValues)
	for i := 0; i < numValues; i++ {
		offset := i * 2
		var val uint16
		if isLittleEndian {
			val = binary.LittleEndian.Uint16(rawValue[offset : offset+2])
		} else {
			val = binary.BigEndian.Uint16(rawValue[offset : offset+2])
		}
		values[i] = int(val)
	}
	return values, numValues, nil
}

func convertSL(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%4 != 0 {
		return nil, 0, fmt.Errorf("invalid SL value length: %d (must be multiple of 4)", len(rawValue))
	}

	numValues := len(rawValue) / 4
	if numValues == 0 {
		return nil, 0, nil
	}

	if numValues == 1 {
		var val int32
		if isLittleEndian {
			val = int32(binary.LittleEndian.Uint32(rawValue))
		} else {
			val = int32(binary.BigEndian.Uint32(rawValue))
		}
		return int(val), 1, nil
	}

	values := make([]int, numValues)
	for i := 0; i < numValues; i++ {
		offset := i * 4
		var val int32
		if isLittleEndian {
			val = int32(binary.LittleEndian.Uint32(rawValue[offset : offset+4]))
		} else {
			val = int32(binary.BigEndian.Uint32(rawValue[offset : offset+4]))
		}
		values[i] = int(val)
	}
	return values, numValues, nil
}

func convertUL(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%4 != 0 {
		return nil, 0, fmt.Errorf("invalid UL value length: %d (must be multiple of 4)", len(rawValue))
	}

	numValues := len(rawValue) / 4
	if numValues == 0 {
		return nil, 0, nil
	}

	if numValues == 1 {
		var val uint32
		if isLittleEndian {
			val = binary.LittleEndian.Uint32(rawValue)
		} else {
			val = binary.BigEndian.Uint32(rawValue)
		}
		return int64(val), 1, nil
	}

	values := make([]int64, numValues)
	for i := 0; i < numValues; i++ {
		offset := i * 4
		var val uint32
		if isLittleEndian {
			val = binary.LittleEndian.Uint32(rawValue[offset : offset+4])
		} else {
			val = binary.BigEndian.Uint32(rawValue[offset : offset+4])
		}
		values[i] = int64(val)
	}
	return values, numValues, nil
}

func convertFL(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%4 != 0 {
		return nil, 0, fmt.Errorf("invalid FL value length: %d (must be multiple of 4)", len(rawValue))
	}

	numValues := len(rawValue) / 4
	if numValues == 0 {
		return nil, 0, nil
	}

	if numValues == 1 {
		var bits uint32
		if isLittleEndian {
			bits = binary.LittleEndian.Uint32(rawValue)
		} else {
			bits = binary.BigEndian.Uint32(rawValue)
		}
		return float64(float32FromBits(bits)), 1, nil
	}

	values := make([]float64, numValues)
	for i := 0; i < numValues; i++ {
		offset := i * 4
		var bits uint32
		if isLittleEndian {
			bits = binary.LittleEndian.Uint32(rawValue[offset : offset+4])
		} else {
			bits = binary.BigEndian.Uint32(rawValue[offset : offset+4])
		}
		values[i] = float64(float32FromBits(bits))
	}
	return values, numValues, nil
}

func convertFD(rawValue []byte, isLittleEndian bool) (interface{}, int, error) {
	if len(rawValue)%8 != 0 {
		return nil, 0, fmt.Errorf("invalid FD value length: %d (must be multiple of 8)", len(rawValue))
	}

	numValues := len(rawValue) / 8
	if numValues == 0 {
		return nil, 0, nil
	}

	if numValues == 1 {
		var bits uint64
		if isLittleEndian {
			bits = binary.LittleEndian.Uint64(rawValue)
		} else {
			bits = binary.BigEndian.Uint64(rawValue)
		}
		return float64FromBits(bits), 1, nil
	}

	values := make([]float64, numValues)
	for i := 0; i < numValues; i++ {
		offset := i * 8
		var bits uint64
		if isLittleEndian {
			bits = binary.LittleEndian.Uint64(rawValue[offset : offset+8])
		} else {
			bits = binary.BigEndian.Uint64(rawValue[offset : offset+8])
		}
		values[i] = float64FromBits(bits)
	}
	return values, numValues, nil
}

// Helper functions for float conversions
func float32FromBits(bits uint32) float32 {
	return *(*float32)(unsafe.Pointer(&bits))
}

func float64FromBits(bits uint64) float64 {
	return *(*float64)(unsafe.Pointer(&bits))
}

// emptyValueForVR returns the appropriate empty value for a given VR.
func emptyValueForVR(vr VR) interface{} {
	switch vr {
	case IS, SS, US, SL, UL:
		return nil // Numeric types: nil for empty
	case DS, FL, FD:
		return nil // Float types: nil for empty
	case DA, DT, TM:
		return nil // Date/time types: nil for empty
	case PN:
		return nil // Person name: nil for empty
	case AT:
		return nil // Attribute tag: nil for empty
	case SQ:
		return []SequenceItem{} // Sequence: empty slice
	default:
		return nil // All others: nil for empty
	}
}
