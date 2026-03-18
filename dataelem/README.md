# DataElem

DICOM data element handling with all 29 Value Representations, type conversions, DICOM JSON Model (Part 18) serialization, and lazy evaluation via RawDataElement.

## Quick Start

```go
import (
    "github.com/amrshadid/go-dicom/dataelem"
    "github.com/amrshadid/go-dicom/tag"
)

// Create a data element
elem := dataelem.NewDataElement(tag.New(0x0010, 0x0010), dataelem.PN, "CITIZEN^Joan")
fmt.Println(elem.GetKeyword())  // "PatientName"
fmt.Println(elem.GetValue())    // "CITIZEN^Joan"

// Convert from raw bytes (during file parsing)
raw := dataelem.NewRawDataElement(tag.New(0x0008, 0x0020), dataelem.DA, 8, []byte("20231015"), 0, false, true, false)
de, _ := dataelem.ConvertRawDataElement(raw)

// JSON serialization (DICOM Part 18)
jsonStr, _ := elem.ToJSON()
de, _ := dataelem.FromJSON(jsonData)
```

## API Reference

```go
// Construction
func NewDataElement(tag, vr, value) *DataElement
func NewRawDataElement(tag, vr, length, value, offset, implicitVR, littleEndian, undefinedLength) *RawDataElement
func ConvertRawDataElement(raw *RawDataElement) (*DataElement, error)

// DataElement methods
func (de *DataElement) GetTag() / SetTag()
func (de *DataElement) GetVR() / SetVR()
func (de *DataElement) GetValue() / SetValue()
func (de *DataElement) GetKeyword() string
func (de *DataElement) GetVM() int
func (de *DataElement) IsPrivate() / IsRetired() / IsEmpty() bool
func (de *DataElement) Clone() *DataElement
func (de *DataElement) ValidateWithConfig(isReading bool) error
func (de *DataElement) ToJSON() (string, error)
func FromJSON(jsonStr string) (*DataElement, error)

// VR utilities
func IsValidVR(vr VR) bool
func IsTextVR(vr VR) / IsNumericVR(vr VR) bool
func GetVRInfo(vr VR) *VRInfo

// VR constants: AE, AS, AT, CS, DA, DS, DT, FD, FL, IS, LO, LT, OB, OD, OF, OL, OW, PN, SH, SL, SQ, SS, ST, TM, UC, UI, UL, UN, UR, US, UT
```

## References

- [DICOM PS3.5 Section 7.1](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Data Element Structure
- [DICOM PS3.18 Annex F](https://dicom.nema.org/medical/dicom/current/output/html/part18.html) - DICOM JSON Model
