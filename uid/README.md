# UID

DICOM Unique Identifier management with creation, validation, database lookup, transfer syntax classification (compressed/lossless), and SOP class utilities.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/uid"

// Create and validate
u := uid.New("1.2.840.10008.1.2")
fmt.Println(u.IsValid())  // true

// Lookup metadata
info := uid.GetUIDInfo("1.2.840.10008.1.2")
fmt.Println(info.Name) // "Implicit VR Little Endian"
fmt.Println(info.Type) // "TransferSyntax"

// Transfer syntax queries
uid.IsTransferSyntax("1.2.840.10008.1.2")       // true
uid.IsCompressed(uid.New("1.2.840.10008.1.2.4.50")) // true (JPEG)
uid.IsLossless(uid.New("1.2.840.10008.1.2.5"))      // true (RLE)

// Browse by type
transferSyntaxes := uid.GetByType("TransferSyntax")
sopClasses := uid.GetByType("SOPClass")
ts := uid.GetByName("CT Image Storage")
```

## API Reference

```go
type UID struct { ... }
type UIDInfo struct { UID, Name, Type, Description string; IsRetired bool }

// Creation
func New(value string) UID
func (u UID) String() string
func (u UID) IsValid() bool
func (u UID) IsEmpty() bool
func (u UID) Equals(other UID) bool
func (u UID) Info() *UIDInfo

// Database queries
func GetUIDInfo(uid string) *UIDInfo
func AllUIDs() []UID
func GetByName(name string) *UID
func GetByType(typeStr string) []UID

// Type classification
func IsTransferSyntax(uid string) bool
func IsSOPClass(uid string) bool

// Transfer syntax utilities
func IsCompressed(uid UID) bool
func IsLossless(uid UID) bool
func CompressedTransferSyntaxes() []UID
func LittleEndianTransferSyntaxes() []UID
func ImplicitVRLittleEndian() UID
func ExplicitVRLittleEndian() UID
func BigEndianTransferSyntax() UID

// SOP class utilities
func VerificationSOPClass() UID
func CTImageStorage() UID
func MRImageStorage() UID
func SupportsMultipleFrames(uid UID) bool
```

## References

- [DICOM PS3.5](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Value Representations (UID format)
- [DICOM PS3.6](https://dicom.nema.org/medical/dicom/current/output/html/part06.html) - UID Registry
