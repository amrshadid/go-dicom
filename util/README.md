# Util

DICOM data manipulation utilities: hex conversion, formatted hex dumps, dataset information extraction, pretty-printing, and repair of invalid multi-value separators.

## Quick Start

```go
import "github.com/amrshadid/go-dicom/util"

// Hex conversion
bytes, _ := util.Hex2Bytes("48 65 6c 6c 6f")
hex := util.Bytes2Hex(bytes) // "48 65 6c 6c 6f"

// Hex dump
dump := util.HexDump(data, 0, 0)

// Dataset info extraction
info, _ := util.GetDatasetInfo(ds)
fmt.Printf("Patient: %s, Modality: %s\n", info.PatientName, info.Modality)
util.PrintDatasetInfo(info)

// Pretty print
util.PrettyPrint(ds, 0)

// Fix invalid separators (space -> backslash)
config := util.DefaultFixSeparatorConfig()
fixedDS, _ := util.FixSeparator(ds, config)
```

## API Reference

```go
// Hex utilities
func Hex2Bytes(hexString string) ([]byte, error)
func Bytes2Hex(data []byte) string
func PrintCharacter(b byte) string

// Hex dump
func HexDump(data []byte, startAddress, stopAddress int) string
func HexDumpReader(r io.Reader, startAddress, stopAddress int, showAddress bool) string

// Dataset utilities
func GetDatasetInfo(ds *dataset.Dataset) (*DatasetInfo, error)
func PrintDatasetInfo(info *DatasetInfo)
func PrettyPrint(ds *dataset.Dataset, indentLevel int)
func DumpDataset(ds *dataset.Dataset) string

type DatasetInfo struct {
    PatientName, PatientID, StudyDate, Modality string
    SeriesNumber, InstanceNumber, Rows, Columns int
    BitsAllocated, BitsStored int
    SOPClassUID, SOPInstanceUID string
}

// Separator fixer
func FixSeparator(ds *dataset.Dataset, config *FixSeparatorConfig) (*dataset.Dataset, error)
func DefaultFixSeparatorConfig() *FixSeparatorConfig

type FixSeparatorConfig struct {
    InvalidSeparator byte      // default: ' '
    ForVRs           []string  // default: ["DS", "IS"]
    ProcessUnknownVRs bool     // default: true
}
```

## References

- [DICOM PS3.5 Section 6.2](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Multi-value separator rules
- [DICOM PS3.6](https://dicom.nema.org/medical/dicom/current/output/html/part06.html) - Standard tag definitions
