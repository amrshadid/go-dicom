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

It is the parser `filereader` is built on, and returns what `filereader`
returns: `File` has the fields of `filereader.DICOMFile`, and the element
types are `filereader`'s under aliases. For every file of pydicom's corpus its
elements agree with the data set `GetDataset` builds, value for value.

```go
f, _ := os.Open(path)
file, err := attributes.Read(filebase.NewFileReader(f), attributes.ReadOptions{
    SkipBulkData:   true,    // leave Pixel Data in the file
    MaxValueLength: 1 << 20, // refuse a longer value: errors.Is(err, attributes.ErrValueTooLong)
    Logger:         slog.New(slog.DiscardHandler),
})
encodings := attributes.TextEncodings(file.DataElements, nil)
for _, e := range file.DataElements {
    fmt.Println(e.Tag, e.VR, attributes.DecodeText(e.VR, e.Value, encodings))
}
```

## API

```go
func Read(reader filebase.Reader, options ReadOptions) (*File, error)
func TextEncodings(elements []*DataElementValue, inherited []string) []string
func DecodeText(vr string, value []byte, encodings []string) string

type File              // FileMetaInfo, DataElements, MetaElements, Warnings, ...
type ReadOptions       // = filereader.ReadOptions
type DataElementValue  // = filereader.DataElementValue
type ElementPosition, FragmentPosition, SequenceItemValue, FileMetaInfo
type ValueTooLongError
var  ErrValueTooLong
const UndefinedLength
```

For a data set, frames or waveforms, use `filereader`.
