// This file contains examples and utilities for integrating multival with other modules.
// These are optional integration examples and helper functions.

package multival

import (
	"fmt"
	"strconv"
	"strings"
)

// DICOM VR Constructors
// These constructors implement DICOM value representation rules

// DSConstructor creates a constructor for Decimal String (DS) values.
// DS values are numeric strings that convert to float64.
// Empty strings are preserved (DICOM Type 2 behavior).
func DSConstructor(v interface{}) interface{} {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		if val == "" {
			return "" // Preserve empty string for DICOM Type 2
		}
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
		return 0.0
	default:
		return 0.0
	}
}

// ISConstructor creates a constructor for Integer String (IS) values.
// IS values are numeric strings that convert to int.
// Empty strings are preserved (DICOM Type 2 behavior).
func ISConstructor(v interface{}) interface{} {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case int32:
		return int(val)
	case float64:
		return int(val)
	case string:
		if val == "" {
			return 0 // Or could preserve as ""
		}
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
		return 0
	default:
		return 0
	}
}

// CSConstructor creates a constructor for Code String (CS) values.
// CS values are uppercase, left-padded, maximum 16 characters.
func CSConstructor(v interface{}) interface{} {
	s := fmt.Sprintf("%v", v)
	return strings.ToUpper(strings.TrimSpace(s))
}

// LOConstructor creates a constructor for Long String (LO) values.
// LO values are strings with maximum 64 characters.
func LOConstructor(v interface{}) interface{} {
	s := fmt.Sprintf("%v", v)
	if len(s) > 64 {
		return s[:64]
	}
	return s
}

// SHConstructor creates a constructor for Short String (SH) values.
// SH values are strings with maximum 16 characters.
func SHConstructor(v interface{}) interface{} {
	s := fmt.Sprintf("%v", v)
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

// STConstructor creates a constructor for Short Text (ST) values.
// ST values are strings with no length limit.
func STConstructor(v interface{}) interface{} {
	return fmt.Sprintf("%v", v)
}

// ParseDICOMString parses a DICOM multi-valued string (backslash-separated) into a ConstrainedList.
// This function bridges DICOM string format to multival containers.
// Example: "100.5\\200.3\\150.8" -> FloatList with 3 items
func ParseDICOMString(dicomString string, constructor func(interface{}) interface{}) (*ConstrainedList, error) {
	if constructor == nil {
		return nil, fmt.Errorf("constructor is nil")
	}

	if dicomString == "" {
		return New(constructor), nil
	}

	// DICOM uses backslash as multi-value separator
	values := strings.Split(dicomString, "\\")

	cl := New(constructor)
	for _, v := range values {
		if err := cl.Append(v); err != nil {
			return nil, err
		}
	}

	return cl, nil
}

// MultiValueToString converts a ConstrainedList back to DICOM multi-value string format.
// This function bridges multival containers to DICOM string format.
// Example: FloatList with [100.5, 200.3] -> "100.5\\200.3"
func MultiValueToString(cl *ConstrainedList, separator string) string {
	if cl == nil {
		return ""
	}

	if cl.Length() == 0 {
		return ""
	}

	items := cl.Items()
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = fmt.Sprintf("%v", item)
	}

	return strings.Join(parts, separator)
}

// DSList is a convenient type alias for Decimal String lists.
// Use this for DICOM elements with DS VR.
func NewDSList() *ConstrainedList {
	return New(DSConstructor)
}

// ISList is a convenient type alias for Integer String lists.
// Use this for DICOM elements with IS VR.
func NewISList() *ConstrainedList {
	return New(ISConstructor)
}

// CSList is a convenient type alias for Code String lists.
// Use this for DICOM elements with CS VR.
func NewCSList() *ConstrainedList {
	return New(CSConstructor)
}

// LOList is a convenient type alias for Long String lists.
// Use this for DICOM elements with LO VR.
func NewLOList() *ConstrainedList {
	return New(LOConstructor)
}

// SHList is a convenient type alias for Short String lists.
// Use this for DICOM elements with SH VR.
func NewSHList() *ConstrainedList {
	return New(SHConstructor)
}

// STList is a convenient type alias for Short Text lists.
// Use this for DICOM elements with ST VR.
func NewSTList() *ConstrainedList {
	return New(STConstructor)
}

// DatasetMultiValue represents a DICOM element with multi-valued support.
// This is an example structure for future dataset module integration.
type DatasetMultiValue struct {
	Tag    uint32           // DICOM tag (e.g., 0x00100010 for PatientName)
	VR     string           // Value Representation (e.g., "PN")
	Values *ConstrainedList // The actual multi-valued data
}

// NewDatasetMultiValue creates a new multi-valued DICOM element.
func NewDatasetMultiValue(tag uint32, vr string) *DatasetMultiValue {
	return &DatasetMultiValue{
		Tag: tag,
		VR:  vr,
	}
}

// SetValuesFromString parses a DICOM multi-value string and populates the element.
func (dmv *DatasetMultiValue) SetValuesFromString(dicomString string) error {
	constructor := GetConstructorForVR(dmv.VR)
	if constructor == nil {
		constructor = StringConstructor // Fallback to string
	}

	cl, err := ParseDICOMString(dicomString, constructor)
	if err != nil {
		return err
	}

	dmv.Values = cl
	return nil
}

// GetDICOMString returns the values as a DICOM multi-value string.
func (dmv *DatasetMultiValue) GetDICOMString() string {
	if dmv.Values == nil {
		return ""
	}
	return MultiValueToString(dmv.Values, "\\")
}

// GetConstructorForVR returns the appropriate constructor for a DICOM VR type.
// This function can be expanded as more VRs are needed.
func GetConstructorForVR(vr string) func(interface{}) interface{} {
	switch vr {
	case "DS", "FD", "FL":
		return DSConstructor
	case "IS", "US", "UL", "SL", "SS":
		return ISConstructor
	case "CS":
		return CSConstructor
	case "LO":
		return LOConstructor
	case "SH":
		return SHConstructor
	case "ST", "LT", "UT", "PN":
		return STConstructor
	default:
		return StringConstructor
	}
}

// StringConstructor is a simple string converter (already defined in main file).
// Kept here for reference in integration examples.
var StringConstructor = func(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// Example usage in comments:
/*
// Example 1: Create a list for DICOM measurements (DS - Decimal String)
measurements := NewDSList()
measurements.Append(100.5)
measurements.Append(200.3)
measurements.Append(150.8)

// Example 2: Parse from DICOM string
dicomString := "100.5\\200.3\\150.8"
measurements, err := ParseDICOMString(dicomString, DSConstructor)
if err != nil {
    log.Fatal(err)
}

// Example 3: Convert back to DICOM string
result := MultiValueToString(measurements, "\\")
fmt.Println(result)  // Output: 100.5\200.3\150.8

// Example 4: Use with DatasetMultiValue (future dataset integration)
elem := NewDatasetMultiValue(0x00100010, "PN")  // PatientName
err := elem.SetValuesFromString("Doe^John\\Smith^Jane")
if err != nil {
    log.Fatal(err)
}

// Example 5: Sort measurements
measurements.Sort(func(i, j interface{}) bool {
    return i.(float64) < j.(float64)
})

// Example 6: Check equality
ds1 := NewDSList()
ds1.Append(1.0)
ds1.Append(2.0)

ds2 := NewDSList()
ds2.Append(1.0)
ds2.Append(2.0)

if ds1.Equal(ds2) {
    fmt.Println("Lists are equal")
}
*/
