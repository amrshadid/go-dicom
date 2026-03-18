# FileWriter

DICOM file writing with proper preamble, DICM prefix, file meta information, transfer syntax configuration, charset encoding, and validation modes.

## Quick Start

```go
import (
    "github.com/amrshadid/go-dicom/filebase"
    "github.com/amrshadid/go-dicom/filewriter"
    "github.com/amrshadid/go-dicom/tag"
)

writer := filebase.NewWriter(outputFile)
dfw := filewriter.NewDCMFileWriter(writer)

dfw.WritePreamble()
dfw.WriteDICMPrefix()
dfw.WriteFileMetaInfo(&filewriter.FileMetaInfo{
    MediaStorageSOPClassUID:    "1.2.840.10008.5.1.4.1.1.2",
    MediaStorageSOPInstanceUID: "1.2.3.4.5",
    TransferSyntaxUID:          "1.2.840.10008.1.2.1",
})

dfw.WriteDataElement(&filewriter.DataElement{
    Tag: tag.New(0x0010, 0x0010), VR: "PN",
    Value: []byte("Doe^John"), Length: 8,
})
```

## API Reference

```go
func NewDCMFileWriter(writer filebase.Writer) *DCMFileWriter

func (dfw *DCMFileWriter) WritePreamble() error
func (dfw *DCMFileWriter) WriteDICMPrefix() error
func (dfw *DCMFileWriter) WriteFileMetaInfo(metaInfo *FileMetaInfo) error
func (dfw *DCMFileWriter) WriteDataElement(elem *DataElement) error
func (dfw *DCMFileWriter) WriteDataElements(elements []*DataElement) error
func (dfw *DCMFileWriter) SetExplicitVR(explicit bool)
func (dfw *DCMFileWriter) SetLittleEndian(littleEndian bool)
func (dfw *DCMFileWriter) GetTransferSyntaxUID() string
func (dfw *DCMFileWriter) SetCharacterSet(charset string) error
func (dfw *DCMFileWriter) WriteWaveformSequence(waveform *Waveform) error
func (dfw *DCMFileWriter) GetPosition() int64

func ValidateElement(elem *DataElement) error
func SetValidationMode(mode ValidationMode) // ValidationNone, ValidationWarn, ValidationStrict

type FileMetaInfo struct {
    MediaStorageSOPClassUID, MediaStorageSOPInstanceUID string
    TransferSyntaxUID, ImplementationClassUID string
    // ...
}

type DataElement struct { Tag tag.Tag; VR string; Value []byte; Length uint32 }
```

## References

- [DICOM PS3.10](https://dicom.nema.org/medical/dicom/current/output/html/part10.html) - Media Storage and File Format
