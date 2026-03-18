# Values

Bidirectional conversion between raw DICOM bytes and Go types for all standard Value Representations, with endianness handling and multi-value parsing.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/values"

// Convert raw DICOM bytes to Go types
value, _ := values.ConvertValue("DS", []byte("123.45"), true) // float64
value, _ = values.ConvertValue("IS", []byte("789"), true)      // int64
value, _ = values.ConvertValue("US", rawBytes, true)            // uint64

// Encode Go values to DICOM bytes
encoded, _ := values.EncodeValue("PN", "Smith^John", true)
encoded, _ = values.EncodeValue("IS", int64(789), true)

// Multi-value parsing (backslash-separated)
numbers := values.MultiStringInt("123\\456\\789")     // []int64
floats := values.MultiStringFloat("1.23\\4.56\\7.89") // []float64

// Tag conversion
group, element, _ := values.ConvertTag(rawTag, true) // uint16, uint16

// String utilities
clean := values.SanitizeString("Hello\x00World   ")
padded := values.PadString("Hello", 10)
values.ValidateNumericString("DS", "123.45")
```

## API Reference

```go
// Value conversion
func ConvertValue(vr string, rawValue []byte, isLittleEndian bool) (interface{}, error)
func EncodeValue(vr string, value interface{}, isLittleEndian bool) ([]byte, error)

// Multi-value parsing
func MultiString(value string, converter func(string) interface{}) []interface{}
func MultiStringInt(value string) []int64
func MultiStringFloat(value string) []float64

// Tag conversion
func ConvertTag(rawTag []byte, isLittleEndian bool) (uint16, uint16, error)

// String utilities
func SanitizeString(s string) string
func PadString(s string, length int) string
func ValidateNumericString(vr string, value string) error

type RawDataElement struct { Tag string; VR *string; Value []byte }
type ParsedValue struct { VR string; Value interface{}; IsMulti bool }
```

## References

- [DICOM PS3.5 Section 5](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Value Representations and encoding rules
