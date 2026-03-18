package valuerep

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// VRMetadata contains metadata about a Value Representation type.
type VRMetadata struct {
	Code       string
	Name       string
	MaxLength  int
	Padding    string
	IsNumeric  bool
	IsString   bool
	IsBinary   bool
	IsDateTime bool
}

// VRMetadataMap contains metadata for all standard DICOM Value Representations.
var VRMetadataMap = map[string]VRMetadata{
	"AE": {Code: "AE", Name: "Application Entity", MaxLength: 16, Padding: " ", IsString: true},
	"AS": {Code: "AS", Name: "Age String", MaxLength: 4, Padding: " ", IsString: true},
	"CS": {Code: "CS", Name: "Code String", MaxLength: 16, Padding: " ", IsString: true},
	"DA": {Code: "DA", Name: "Date", MaxLength: 8, Padding: " ", IsString: true, IsDateTime: true},
	"DS": {Code: "DS", Name: "Decimal String", MaxLength: 16, Padding: " ", IsString: true, IsNumeric: true},
	"DT": {Code: "DT", Name: "DateTime", MaxLength: 26, Padding: " ", IsString: true, IsDateTime: true},
	"IS": {Code: "IS", Name: "Integer String", MaxLength: 12, Padding: " ", IsString: true, IsNumeric: true},
	"LO": {Code: "LO", Name: "Long String", MaxLength: 64, Padding: " ", IsString: true},
	"LT": {Code: "LT", Name: "Long Text", MaxLength: 10240, Padding: " ", IsString: true},
	"PN": {Code: "PN", Name: "Person Name", MaxLength: 64, Padding: " ", IsString: true},
	"SH": {Code: "SH", Name: "Short String", MaxLength: 16, Padding: " ", IsString: true},
	"ST": {Code: "ST", Name: "Short Text", MaxLength: 1024, Padding: " ", IsString: true},
	"TM": {Code: "TM", Name: "Time", MaxLength: 14, Padding: " ", IsString: true, IsDateTime: true},
	"UC": {Code: "UC", Name: "Unlimited Characters", MaxLength: 0, Padding: " ", IsString: true},
	"UI": {Code: "UI", Name: "Unique Identifier", MaxLength: 64, Padding: "\x00", IsString: true},
	"UR": {Code: "UR", Name: "Universal Resource Identifier", MaxLength: 0, Padding: " ", IsString: true},
	"UT": {Code: "UT", Name: "Unlimited Text", MaxLength: 0, Padding: " ", IsString: true},
	"OB": {Code: "OB", Name: "Other Byte", MaxLength: 0, IsBinary: true},
	"OD": {Code: "OD", Name: "Other Double", MaxLength: 0, IsBinary: true},
	"OF": {Code: "OF", Name: "Other Float", MaxLength: 0, IsBinary: true},
	"OL": {Code: "OL", Name: "Other Long", MaxLength: 0, IsBinary: true},
	"OW": {Code: "OW", Name: "Other Word", MaxLength: 0, IsBinary: true},
	"FD": {Code: "FD", Name: "Floating Point Double", MaxLength: 8, IsNumeric: true, IsBinary: true},
	"FL": {Code: "FL", Name: "Floating Point Single", MaxLength: 4, IsNumeric: true, IsBinary: true},
	"SL": {Code: "SL", Name: "Signed Long", MaxLength: 4, IsNumeric: true, IsBinary: true},
	"SS": {Code: "SS", Name: "Signed Short", MaxLength: 2, IsNumeric: true, IsBinary: true},
	"UL": {Code: "UL", Name: "Unsigned Long", MaxLength: 4, IsNumeric: true, IsBinary: true},
	"US": {Code: "US", Name: "Unsigned Short", MaxLength: 2, IsNumeric: true, IsBinary: true},
	"AT": {Code: "AT", Name: "Attribute Tag", MaxLength: 4, IsBinary: true},
	"SQ": {Code: "SQ", Name: "Sequence of Items", MaxLength: 0, IsBinary: true},
	"UN": {Code: "UN", Name: "Unknown", MaxLength: 0, IsBinary: true},
}

// Validation regular expressions for VR formats.
var (
	reAE = regexp.MustCompile(`^[\x20-\x7e]*$`)
	reAS = regexp.MustCompile(`^\d\d\d[DWMY]$`)
	reCS = regexp.MustCompile(`^[A-Z0-9 _]*$`)
	reDS = regexp.MustCompile(`^ *[+\-]?(\d+|\d+\.\d*|\.\d+)([eE][+\-]?\d+)? *$`)
	reIS = regexp.MustCompile(`^ *[+\-]?\d+ *$`)
	reDA = regexp.MustCompile(`^\d{4}(0[1-9]|1[0-2])([0-2]\d|3[01])$`)
	reTM = regexp.MustCompile(`^([01]\d|2[0-3])([0-5]\d((60|[0-5]\d))?)?$`)
	reUI = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*))*$`)
	reUR = regexp.MustCompile(`^[A-Za-z_\d:/?#\[\]@!$&'()*+,;=%\-.~]* *$`)
)

// ValidateType checks if a value is of the correct type for a VR.
func ValidateType(vr string, value interface{}) (bool, string) {
	metadata, exists := VRMetadataMap[vr]
	if !exists {
		return false, fmt.Sprintf("Unknown VR: %s", vr)
	}

	if value == nil {
		return true, ""
	}

	switch {
	case metadata.IsString:
		if _, ok := value.(string); !ok {
			if _, ok := value.([]byte); !ok {
				return false, fmt.Sprintf("VR %s requires string or []byte, got %T", vr, value)
			}
		}
	case metadata.IsNumeric && metadata.IsBinary:
		if _, ok := value.([]byte); !ok {
			return false, fmt.Sprintf("VR %s requires []byte, got %T", vr, value)
		}
	case metadata.IsBinary:
		if _, ok := value.([]byte); !ok {
			return false, fmt.Sprintf("VR %s requires []byte, got %T", vr, value)
		}
	}

	return true, ""
}

// ValidateVRLength checks if a value length is within the VR's constraints.
func ValidateVRLength(vr string, value interface{}) (bool, string) {
	metadata, exists := VRMetadataMap[vr]
	if !exists {
		return false, fmt.Sprintf("Unknown VR: %s", vr)
	}

	if metadata.MaxLength == 0 {
		return true, ""
	}

	var length int
	switch v := value.(type) {
	case string:
		length = len(v)
	case []byte:
		length = len(v)
	default:
		return true, ""
	}

	if length > metadata.MaxLength {
		return false, fmt.Sprintf("VR %s value length %d exceeds maximum %d", vr, length, metadata.MaxLength)
	}

	return true, ""
}

// ValidateRegex validates a value against the VR's regex pattern.
func ValidateRegex(vr string, value interface{}) (bool, string) {
	var strValue string
	switch v := value.(type) {
	case string:
		strValue = v
	case []byte:
		strValue = string(v)
	default:
		return true, ""
	}

	var re *regexp.Regexp
	switch vr {
	case "AE":
		re = reAE
	case "AS":
		re = reAS
	case "CS":
		re = reCS
	case "DS":
		re = reDS
	case "IS":
		re = reIS
	case "DA":
		re = reDA
	case "TM":
		re = reTM
	case "UI":
		re = reUI
	case "UR":
		re = reUR
	default:
		return true, ""
	}

	if !re.MatchString(strValue) {
		return false, fmt.Sprintf("VR %s value '%s' does not match expected format", vr, strValue)
	}

	return true, ""
}

// ValidateValue performs complete validation (type + length + regex).
func ValidateValue(vr string, value interface{}) error {
	valid, msg := ValidateType(vr, value)
	if !valid {
		return fmt.Errorf("type validation: %s", msg)
	}

	valid, msg = ValidateVRLength(vr, value)
	if !valid {
		return fmt.Errorf("length validation: %s", msg)
	}

	valid, msg = ValidateRegex(vr, value)
	if !valid {
		return fmt.Errorf("format validation: %s", msg)
	}

	return nil
}

// PersonName represents a DICOM Person Name value with components.
type PersonName struct {
	Alphabetic  string
	Ideographic string
	Phonetic    string
}

// GetComponent returns a specific component of the person name.
func (pn *PersonName) GetComponent(which string) string {
	switch which {
	case "alphabetic":
		return pn.Alphabetic
	case "ideographic":
		return pn.Ideographic
	case "phonetic":
		return pn.Phonetic
	default:
		return ""
	}
}

// ParsePersonName parses a PN value into alphabetic, ideographic, and phonetic components.
func ParsePersonName(value string) *PersonName {
	parts := strings.Split(value, "^")
	pn := &PersonName{}

	if len(parts) > 0 {
		pn.Alphabetic = parts[0]
	}
	if len(parts) > 1 {
		pn.Ideographic = parts[1]
	}
	if len(parts) > 2 {
		pn.Phonetic = parts[2]
	}

	return pn
}

// DecimalString represents a DICOM DS (Decimal String) value.
type DecimalString struct {
	Value float64
	Raw   string
}

// ParseDecimalString parses a DS value into a float64.
func ParseDecimalString(value string) (*DecimalString, error) {
	trimmed := strings.TrimSpace(value)
	f, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid decimal string: %s", value)
	}

	return &DecimalString{
		Value: f,
		Raw:   trimmed,
	}, nil
}

// IntegerString represents a DICOM IS (Integer String) value.
type IntegerString struct {
	Value int64
	Raw   string
}

// ParseIntegerString parses an IS value into an int64.
func ParseIntegerString(value string) (*IntegerString, error) {
	trimmed := strings.TrimSpace(value)
	i, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid integer string: %s", value)
	}

	return &IntegerString{
		Value: i,
		Raw:   trimmed,
	}, nil
}

// Date represents a DICOM DA (Date) value.
type Date struct {
	Value time.Time
	Raw   string
}

// ParseDate parses a DA value in YYYYMMDD format.
func ParseDate(value string) (*Date, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 8 {
		return nil, fmt.Errorf("invalid date format: %s (expected YYYYMMDD)", value)
	}

	t, err := time.Parse("20060102", trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid date: %s", value)
	}

	return &Date{
		Value: t,
		Raw:   trimmed,
	}, nil
}

// Time represents a DICOM TM (Time) value.
type Time struct {
	Value time.Time
	Raw   string
}

// ParseTime parses a TM value in HHMMSS.ffffff format.
func ParseTime(value string) (*Time, error) {
	trimmed := strings.TrimSpace(value)

	// Handle various time formats
	var t time.Time
	var err error

	switch len(trimmed) {
	case 2:
		t, err = time.Parse("15", trimmed)
	case 4:
		t, err = time.Parse("1504", trimmed)
	case 6:
		t, err = time.Parse("150405", trimmed)
	default:
		// Try with fractional seconds
		if strings.Contains(trimmed, ".") {
			t, err = time.Parse("150405.000000", trimmed)
		} else {
			return nil, fmt.Errorf("invalid time format: %s", value)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("invalid time: %s", value)
	}

	return &Time{
		Value: t,
		Raw:   trimmed,
	}, nil
}

// UniqueIdentifier represents a DICOM UI (UID) value.
type UniqueIdentifier struct {
	Value string
}

// ValidateUID checks if a UID is valid.
func ValidateUID(uid string) error {
	if !reUI.MatchString(uid) {
		return fmt.Errorf("invalid UID format: %s", uid)
	}
	return nil
}

// GetVRMetadata returns metadata for a VR code.
func GetVRMetadata(vrCode string) (VRMetadata, error) {
	metadata, exists := VRMetadataMap[vrCode]
	if !exists {
		return VRMetadata{}, fmt.Errorf("unknown VR: %s", vrCode)
	}
	return metadata, nil
}

// IsValidVR checks if a VR code is valid.
func IsValidVR(vrCode string) bool {
	_, exists := VRMetadataMap[vrCode]
	return exists
}

// GetAllVRCodes returns all valid VR codes.
func GetAllVRCodes() []string {
	codes := make([]string, 0, len(VRMetadataMap))
	for code := range VRMetadataMap {
		codes = append(codes, code)
	}
	return codes
}
