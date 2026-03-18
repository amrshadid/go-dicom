# Tag

DICOM tag representation as 32-bit integers with O(1) dictionary lookup for metadata (name, VR, VM, keyword), private tag support, byte conversion, and comparison operations.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/tag"

// Create tags
t := tag.New(0x0010, 0x0010)         // from group/element
t = tag.FromInt(0x00020010)           // from integer
t, _ = tag.FromBytes(data, true)      // from bytes (little-endian)
t, _ = tag.ParseTag("(0010,0020)")    // from string

// Access metadata
fmt.Println(t.GetName())     // "Patient's Name"
fmt.Println(t.GetVR())       // "PN"
fmt.Println(t.GetKeyword())  // "PatientName"
fmt.Println(t.IsRetired())   // false
fmt.Println(t.IsPrivate())   // false

// Byte conversion
bytes := t.ToBytes(true)     // little-endian
fmt.Println(t.String())      // "(0010,0010)"
```

## API Reference

```go
// Creation
func New(group, element uint16) Tag
func FromInt(val uint32) Tag
func FromBytes(data []byte, littleEndian bool) (Tag, error)
func ParseTag(s string) (Tag, error)

// Tag methods
func (t Tag) Group() uint16
func (t Tag) Element() uint16
func (t Tag) ToBytes(littleEndian bool) []byte
func (t Tag) String() string          // "(GGGG,EEEE)"
func (t Tag) Hex() string             // "GGGGEEEE"
func (t Tag) Uint32() uint32
func (t Tag) IsPrivate() bool
func (t Tag) IsPrivateCreator() bool
func (t Tag) IsSpecial() bool
func (t Tag) Equals(other Tag) bool
func (t Tag) Less(other Tag) bool

// Dictionary lookup
func (t Tag) GetInfo() *TagInfo
func (t Tag) GetName() string
func (t Tag) GetVR() string
func (t Tag) GetVM() string
func (t Tag) GetKeyword() string
func (t Tag) IsRetired() bool
func (t Tag) Exists() bool

// Special tag constants
var ItemTag, ItemDelimiterTag, SequenceDelimiterTag Tag

type TagInfo struct { DicomName, Keyword, VR, VM string; IsRetired bool }
```

## References

- [DICOM PS3.5 Section 7](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Data Element Structure and Tags
- [DICOM PS3.6](https://dicom.nema.org/medical/dicom/current/output/html/part06.html) - Data Dictionary
