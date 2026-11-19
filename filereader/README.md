# FileReader

DICOM file reading with preamble/DICM validation, file meta information parsing, transfer syntax detection, and data element decoding. Supports both high-level and sequential low-level access.

To read attributes without linking the codecs, use [`filereader/attributes`](attributes/README.md): the same parser and element types, without `compress`, `encaps`, `pixels`, `waveforms` or `dataset`.

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

// The same, leaving bulk values (Pixel Data, long binary values) in the
// file: each such element has ValueSkipped set and keeps its Position.
func ReadDICOMFileWithOptions(reader filebase.Reader, options ReadOptions) (*DICOMFile, error)
type ReadOptions struct {
    SkipBulkData      bool
    BulkDataThreshold uint32 // the longest binary value still read
    MaxValueLength    uint32 // the longest value held; zero is no maximum
    Logger            *slog.Logger // where this read logs; nil is config.Logger
}

// A value longer than MaxValueLength ends the read before it is allocated,
// with an error naming the element. A value SkipBulkData leaves in the file
// is not held, so it is not refused.
var ErrValueTooLong error
type ValueTooLongError struct {
    Tag    tag.Tag
    Length uint64
    Max    uint32
    Offset int64
}

// A silent read:
//   filereader.ReadOptions{Logger: slog.New(slog.DiscardHandler)}
// With a logger given, nothing the read does reaches config.Logger, the
// validation GetDataset runs and the character set decisions included. A
// line names tags, VRs, lengths and offsets, never a value's text.

// A file read that way as a data set whose skipped values say where they
// are (dataelem.ValueInFile), for filewriter to copy them from the file
// with SetValueSource. A deflated file is refused.
func (df *DICOMFile) GetDatasetWithValuesInFile() (*dataset.Dataset, error)

// Where each frame lies in the file, and a reader of them by position: a
// frame, or part of a native one. A FrameReader is a dataset.FrameReader,
// so Dataset.RenderFramesFrom draws from the file a frame or a band at a
// time.
func (df *DICOMFile) FrameRanges(src io.ReaderAt) ([][]encaps.Range, error)
func (df *DICOMFile) FrameReader(src io.ReaderAt) (*FrameReader, error)
func (r *FrameReader) ReadFrame(i int) ([]byte, error)
func (r *FrameReader) ReadFrameBytes(i int, offset int64, p []byte) error

func NewDCMFileReader(reader filebase.Reader) *DCMFileReader

func (dfr *DCMFileReader) ReadPreamble() error
func (dfr *DCMFileReader) ReadDICMPrefix() error
func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error)
func (dfr *DCMFileReader) ReadTag() (tag.Tag, error)
func (dfr *DCMFileReader) ReadDataElement(explicitVR bool) (*DataElementValue, error)
func (dfr *DCMFileReader) GetPosition() int64

// For a program that shows a DICOMFile's elements as they are in the file:
// the character encodings a data set's text is in, and a value decoded
// under them. GetDataset applies the same rules itself.
func TextEncodings(elements []*DataElementValue, inherited []string) []string
func DecodeText(vr string, value []byte, encodings []string) string

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
