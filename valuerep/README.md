# ValueRep

DICOM Value Representation metadata, multi-level validation (type, length, format), and specialized parsers for PersonName, Date, Time, DecimalString, IntegerString, and UID.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/valuerep"

// Complete validation
err := valuerep.ValidateValue("AE", "MyApplication")

// Individual validation levels
valid, msg := valuerep.ValidateType("AE", "MyApplication")
valid, msg = valuerep.ValidateVRLength("AE", "MyApplication")
valid, msg = valuerep.ValidateRegex("CS", "CODE")

// Specialized parsers
pn := valuerep.ParsePersonName("Smith^John^Michael^Dr.^Jr.")
d, _ := valuerep.ParseDate("20230615")
t, _ := valuerep.ParseTime("153045.123456")
ds, _ := valuerep.ParseDecimalString("123.45")
is, _ := valuerep.ParseIntegerString("789")
valuerep.ValidateUID("1.2.840.10008.5.1.4.1.1.2")

// VR metadata
metadata, _ := valuerep.GetVRMetadata("PN")
codes := valuerep.GetAllVRCodes()
valuerep.IsValidVR("PN") // true
```

## API Reference

```go
// Validation
func ValidateValue(vr string, value interface{}) error
func ValidateType(vr string, value interface{}) (bool, string)
func ValidateVRLength(vr string, value interface{}) (bool, string)
func ValidateRegex(vr string, value interface{}) (bool, string)

// Parsers
func ParsePersonName(value string) *PersonName
func ParseDate(value string) (*Date, error)
func ParseTime(value string) (*Time, error)
func ParseDecimalString(value string) (*DecimalString, error)
func ParseIntegerString(value string) (*IntegerString, error)
func ValidateUID(uid string) error

// VR metadata
func GetVRMetadata(vrCode string) (VRMetadata, error)
func IsValidVR(vrCode string) bool
func GetAllVRCodes() []string

type VRMetadata struct {
    Code string; Name string; MaxLength int; Padding string
    IsNumeric, IsString, IsBinary, IsDateTime bool
}

type PersonName struct { Alphabetic, Ideographic, Phonetic string }
type DecimalString struct { Value float64; Raw string }
type IntegerString struct { Value int64; Raw string }
type Date struct { Value time.Time; Raw string }
type Time struct { Value time.Time; Raw string }

var VRMetadataMap map[string]VRMetadata
```

## References

- [DICOM PS3.5 Section 6.2](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Value Representation definitions and constraints
