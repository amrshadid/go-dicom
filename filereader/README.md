# FileReader

DICOM file reading with preamble/DICM validation, file meta information parsing, transfer syntax detection, and data element decoding. Supports both high-level and sequential low-level access.

## Quick Start

```go
import (
    "github.com/amrshadid/go-dicom/filebase"
    "github.com/amrshadid/go-dicom/filereader"
)

// High-level: read entire file
file, _ := os.Open("patient.dcm")
reader := filebase.NewFileReader(file)
dicom, err := filereader.ReadDICOMFile(reader)

fmt.Println(dicom.FileMetaInfo.TransferSyntaxUID)
for _, elem := range dicom.DataElements {
    fmt.Printf("Tag: %s, VR: %s\n", elem.Tag, elem.VR)
}

// Low-level: sequential reading
dfr := filereader.NewDCMFileReader(reader)
dfr.ReadPreamble()
dfr.ReadDICMPrefix()
metaInfo, _ := dfr.ReadFileMetaInfo()
elem, _ := dfr.ReadDataElement(true) // explicit VR
```

## API Reference

```go
func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error)
func NewDCMFileReader(reader filebase.Reader) *DCMFileReader

func (dfr *DCMFileReader) ReadPreamble() error
func (dfr *DCMFileReader) ReadDICMPrefix() error
func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error)
func (dfr *DCMFileReader) ReadTag() (tag.Tag, error)
func (dfr *DCMFileReader) ReadDataElement(explicitVR bool) (*DataElementValue, error)
func (dfr *DCMFileReader) GetPosition() int64

type DICOMFile struct {
    FileMetaInfo   *FileMetaInfo
    DataElements   []*DataElementValue
    ExplicitVR     bool
    IsLittleEndian bool
}

type FileMetaInfo struct {
    MediaStorageSOPClassUID, MediaStorageSOPInstanceUID, TransferSyntaxUID string
    ImplementationClassUID, ImplementationVersionName string
    // ...
}
```

## References

- [DICOM PS3.10 Section 7](https://dicom.nema.org/medical/dicom/current/output/html/part10.html) - File Format (preamble, DICM prefix, meta information)
- [DICOM PS3.5 Section 5.1](https://dicom.nema.org/medical/dicom/current/output/html/part05.html) - Data Element Structure
