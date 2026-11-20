# attributes

Reads the attributes of a DICOM file, the File Meta group and the whole data
set as the file holds them, without linking the packages that build data sets
or decode pixels.

How it differs from its neighbors: `filereader` reads the same files with the
same parser and adds `GetDataset`, frames and waveforms, which link the
codecs; `element` reads no files at all, it converts values already in memory
between their bytes and Go types. Use `attributes` to index or inspect files,
`filereader` to build a data set or reach the pixels.

Kept out of its dependencies: `compress`, `compress/jpeg2000`, `encaps`,
`pixels`, `waveforms`, `dataset` and `dataelem`. A test in this package runs
`go list -deps` and fails if any of them joins.

`DICOMJSON` writes the DICOM JSON Model (PS3.18 Annex F) of a file's elements
without them. It is the implementation `dataset.Dataset.ToDICOMJSON` uses, in
`internal/dicomjson`, so the two cannot drift: for every file of pydicom's
corpus and of this repository's test data, the bytes of the two documents are
equal (`TestDICOMJSONIsWhatDatasetWritesForEveryFile`). Text is decoded under
the Specific Character Set as `GetDataset` decodes it. A hook, `BulkData`,
chooses by VR, length or tag which values become a `BulkDataURI` or are left
out, and is told where each value is in the file, so a header of a file read
with `SkipBulkData` can be written with its pixels left where they are:

```go
file, _ := attributes.Read(reader, attributes.ReadOptions{SkipBulkData: true})
doc, err := attributes.DICOMJSON(file.DataElements, attributes.JSONOptions{
    BulkData: func(v attributes.BulkDataValue) attributes.BulkDataDecision {
        return attributes.BulkDataDecision{URI: fmt.Sprintf("bulk/%d+%d", v.Offset, v.Length)}
    },
})
```

A value left in the file that the hook gives neither a URI nor an omission is
an error, since there is nothing to write.

It is the parser `filereader` is built on, and returns what `filereader`
returns: `File` has the fields of `filereader.DICOMFile`, and the element
types are `filereader`'s under aliases. For every file of pydicom's corpus its
elements agree with the data set `GetDataset` builds, value for value.

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, nil)) // this read's own
f, _ := os.Open(path)
file, err := attributes.Read(filebase.NewFileReader(f), attributes.ReadOptions{
    SkipBulkData:   true,    // leave Pixel Data in the file
    MaxValueLength: 1 << 20, // refuse a longer value: errors.Is(err, attributes.ErrValueTooLong)
    Logger:         logger,
})
// Decoding logs a misspelled or unknown Specific Character Set; the
// ...WithContext forms log it to the logger ctx carries, the read's here,
// where TextEncodings and DecodeText log to config.Logger.
ctx := config.WithLogger(context.Background(), logger)
encodings := attributes.TextEncodingsWithContext(ctx, file.DataElements, nil)
for _, e := range file.DataElements {
    fmt.Println(e.Tag, e.VR, attributes.DecodeTextWithContext(ctx, e.VR, e.Value, encodings))
}
```

## API

```go
func Read(reader filebase.Reader, options ReadOptions) (*File, error)
func TextEncodings(elements []*DataElementValue, inherited []string) []string
func DecodeText(vr string, value []byte, encodings []string) string
func TextEncodingsWithContext(ctx context.Context, elements []*DataElementValue, inherited []string) []string
func DecodeTextWithContext(ctx context.Context, vr string, value []byte, encodings []string) string
func DICOMJSON(elements []*DataElementValue, opts JSONOptions) (map[string]JSONElement, error)
func DICOMJSONWithContext(ctx context.Context, elements []*DataElementValue, opts JSONOptions) (map[string]JSONElement, error)

type JSONElement       // = dataset.DICOMJSONElement
type JSONOptions       // BulkData func(BulkDataValue) BulkDataDecision; Warn func(string)
type BulkDataValue     // Tag, VR, Path []BulkDataStep, InFile, Offset, Length, Value
type BulkDataStep      // Sequence tag.Tag; Item int
type BulkDataDecision  // URI string; Omit bool

type File              // FileMetaInfo, DataElements, MetaElements, Warnings, ...
type ReadOptions       // = filereader.ReadOptions
type DataElementValue  // = filereader.DataElementValue
type ElementPosition, FragmentPosition, SequenceItemValue, FileMetaInfo
type ValueTooLongError
var  ErrValueTooLong
const UndefinedLength
```

For a data set, frames or waveforms, use `filereader`.
