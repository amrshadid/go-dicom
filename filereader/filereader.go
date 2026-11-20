package filereader

import (
	"context"
	"fmt"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/hooks"
	fileparse "github.com/amrshadid/go-dicom/internal/filereader"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// The parser itself is internal/filereader, which imports nothing that
// builds a data set or decodes pixels, so that filereader/attributes can give
// it alone to a program that reads attributes. These are its types under
// the names they have always had here: an alias is the same type, so a
// value from one package is a value of the other.

// FileMetaInfo contains DICOM file meta header information.
type FileMetaInfo = fileparse.FileMetaInfo

// ReadOptions choose how ReadDICOMFileWithOptions reads a file.
type ReadOptions = fileparse.ReadOptions

// DataElementValue is one data element as it was read: its tag, VR,
// length and value bytes, the items of a sequence, and where it is.
type DataElementValue = fileparse.DataElementValue

// ElementPosition is where a data element sits in the bytes it was read
// from, so that a decoded attribute can be tied back to them.
type ElementPosition = fileparse.ElementPosition

// FragmentPosition is one item of encapsulated pixel data: an (FFFE,E000)
// tag, a 4-byte length, and that many bytes.
type FragmentPosition = fileparse.FragmentPosition

// SequenceItemValue is a single item within a Sequence (SQ) element,
// holding the data elements nested inside that item.
type SequenceItemValue = fileparse.SequenceItemValue

// ValueTooLongError is a value refused because it states more bytes than
// ReadOptions.MaxValueLength.
type ValueTooLongError = fileparse.ValueTooLongError

// TagOrderViolation is a data element whose tag is not greater than the tag of
// the element before it in the same data set (PS3.5 7.1), as
// DICOMFile.TagOrderViolations lists them.
type TagOrderViolation = fileparse.TagOrderViolation

// ErrValueTooLong is what a read refused by ReadOptions.MaxValueLength
// matches with errors.Is.
var ErrValueTooLong = fileparse.ErrValueTooLong

// UndefinedLength is the DICOM sentinel value (0xFFFFFFFF) used in the Value
// Length field to indicate that an element's extent is delimited by a
// Sequence/Item Delimitation Item rather than stated up front. It is legal for
// Sequences (SQ) and for encapsulated Pixel Data. See PS3.5 Section 7.1.
const UndefinedLength uint32 = fileparse.UndefinedLength

// MaxSequenceDepth bounds how deeply nested sequences may be parsed. Real
// DICOM objects nest a handful of levels at most; the limit stops a crafted or
// corrupt file from driving unbounded recursion.
const MaxSequenceDepth = fileparse.MaxSequenceDepth

// UnknownVRBulkLength is how long a value of no known VR may be and still
// be read when bulk data is skipped. Such a value is often a line of text.
const UnknownVRBulkLength = fileparse.UnknownVRBulkLength

// DeflatedExplicitVRLittleEndianUID is the transfer syntax whose data set is
// DEFLATE-compressed after the file meta header (PS3.5 Annex A.5).
const DeflatedExplicitVRLittleEndianUID = fileparse.DeflatedExplicitVRLittleEndianUID

// MaxInflatedDatasetSize bounds how far a deflated data set may expand.
//
// DEFLATE reaches ratios above 1000:1 on repetitive input, so a small file can
// otherwise expand without limit. 256 MiB is far above any legitimate DICOM
// data set while keeping a hostile one cheap to reject.
const MaxInflatedDatasetSize int64 = fileparse.MaxInflatedDatasetSize

// DCMFileReader reads DICOM files.
type DCMFileReader struct {
	parser       *fileparse.Reader
	settings     *config.Settings // Configuration settings for reading behavior
	hookChain    *hooks.HookChain // Hook chain for element processing
	elementCount int              // Count of elements read
}

// NewDCMFileReader creates a new DICOM file reader.
func NewDCMFileReader(reader filebase.Reader) *DCMFileReader {
	return &DCMFileReader{
		parser:       fileparse.NewReader(reader),
		settings:     config.Get(),         // Load global configuration settings
		hookChain:    hooks.NewHookChain(), // Initialize empty hook chain
		elementCount: 0,
	}
}

// ReadPreamble reads the 128-byte DICOM preamble.
func (dfr *DCMFileReader) ReadPreamble() error { return dfr.parser.ReadPreamble() }

// ReadDICMPrefix reads and validates the "DICM" magic string.
func (dfr *DCMFileReader) ReadDICMPrefix() error { return dfr.parser.ReadDICMPrefix() }

// ReadFileMetaInformationGroupLength reads the File Meta Information Group Length (0002,0000).
func (dfr *DCMFileReader) ReadFileMetaInformationGroupLength() (uint32, error) {
	return dfr.parser.ReadFileMetaInformationGroupLength()
}

// ReadFileMetaInfo reads the DICOM file meta information.
func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error) {
	return dfr.parser.ReadFileMetaInfo()
}

// ReadTag reads a DICOM tag (4 bytes: group + element).
func (dfr *DCMFileReader) ReadTag() (tag.Tag, error) { return dfr.parser.ReadTag() }

// ReadDataElement reads a data element, including any nested sequence content.
func (dfr *DCMFileReader) ReadDataElement(explicitVR bool) (*DataElementValue, error) {
	return dfr.parser.ReadDataElement(explicitVR)
}

// GetPosition returns the current position in the file.
func (dfr *DCMFileReader) GetPosition() int64 { return dfr.parser.GetPosition() }

// GetFileMetaInfo returns the file meta information.
func (dfr *DCMFileReader) GetFileMetaInfo() *FileMetaInfo { return dfr.parser.GetFileMetaInfo() }

// TextEncodings returns the character encodings in force for the text of a
// data set: those its Specific Character Set (0008,0005) declares, or, when
// it has none, inherited, which is what the data set it is an item of uses
// (PS3.3 C.12.1.1.2). Pass nil for the top of a file. The result is what
// DecodeText takes.
//
// It is for a program that shows the elements of a DICOMFile as they are in
// the file. GetDataset applies the same rules and returns text already
// decoded.
func TextEncodings(elements []*DataElementValue, inherited []string) []string {
	return fileparse.TextEncodings(elements, inherited)
}

// DecodeText returns a value of the given VR as UTF-8 text, decoded under
// encodings as TextEncodings returns them. Only the VRs a character set
// applies to are decoded with it; a UI or a DA is in the default repertoire
// whatever the data set declares. A value that does not decode is returned
// as it is.
func DecodeText(vr string, value []byte, encodings []string) string {
	return fileparse.DecodeText(vr, value, encodings)
}

// ReadDICOMFileWithOptions is ReadDICOMFile with a choice of how to read:
// see ReadOptions.
func ReadDICOMFileWithOptions(reader filebase.Reader, options ReadOptions) (*DICOMFile, error) {
	// The logger travels in a context rather than through a global, so two
	// reads at once each log where they were told to (config.WithLogger),
	// and GetDataset, later, logs where the read did.
	ctx := config.WithLogger(context.Background(), options.Logger)
	parsed, err := fileparse.Read(ctx, reader, options)
	if err != nil {
		return nil, err
	}
	return &DICOMFile{
		FileMetaInfo:    parsed.FileMetaInfo,
		DataElements:    parsed.DataElements,
		ExplicitVR:      parsed.ExplicitVR,
		IsLittleEndian:  parsed.IsLittleEndian,
		HasPreamble:     parsed.HasPreamble,
		MetaElements:    parsed.MetaElements,
		MetaGroupLength: parsed.MetaGroupLength,
		DataSetOffset:   parsed.DataSetOffset,
		Deflated:        parsed.Deflated,
		Warnings:        parsed.Warnings,
		ctx:             ctx,

		TagOrderViolations:     parsed.TagOrderViolations,
		TagOrderViolationCount: parsed.TagOrderViolationCount,
	}, nil
}

// context carries the logger the file was read with, for what it does
// afterwards on the read's behalf: GetDataset and the character set
// methods. A DICOMFile not made by a read has none, and logs to
// config.Logger.
func (df *DICOMFile) context() context.Context {
	if df.ctx == nil {
		return context.Background()
	}
	return df.ctx
}

// RegisterHook registers a hook function at the specified processing level.
// Hooks allow custom processing of data elements during reading.
// Multiple hooks can be registered at the same level; they execute in order.
func (dfr *DCMFileReader) RegisterHook(level hooks.HookLevel, fn hooks.AdvancedHookFunc) error {
	if dfr.hookChain == nil {
		dfr.hookChain = hooks.NewHookChain()
	}
	return dfr.hookChain.RegisterHook(level, fn)
}

// GetHookChain returns the hook chain for this reader.
// Allows direct access for advanced hook chain operations.
func (dfr *DCMFileReader) GetHookChain() *hooks.HookChain {
	if dfr.hookChain == nil {
		dfr.hookChain = hooks.NewHookChain()
	}
	return dfr.hookChain
}

// GetElementCount returns the number of data elements that have been read.
func (dfr *DCMFileReader) GetElementCount() int {
	return dfr.elementCount
}

// ProcessElementThroughHooks converts a raw DataElementValue to DataElement and processes it
// through the registered hook chain. This allows custom processing at various stages
// (PreValidation, PostValidation, PreCompression, PostCompression, etc.).
// Returns the processed element or an error if hook processing fails.
func (dfr *DCMFileReader) ProcessElementThroughHooks(elem *DataElementValue) (*dataelem.DataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("element cannot be nil")
	}

	// Create DataElement from raw value (elem.Tag is already tag.Tag)
	dataElem := dataelem.NewDataElement(elem.Tag, dataelem.VR(elem.VR), elem.Value)
	if dataElem == nil {
		return nil, fmt.Errorf("failed to create data element from tag %s", elem.Tag.String())
	}

	// Process through hook chain at PostValidation level by default
	// This allows hooks to access the converted element
	if dfr.hookChain != nil && dfr.hookChain.HookCount() > 0 {
		processed, err := dfr.hookChain.ExecuteHooks(dataElem, hooks.PostValidation)
		if err != nil {
			return nil, fmt.Errorf("hook processing failed for tag %s: %w", elem.Tag.String(), err)
		}
		if processed != nil {
			dataElem = processed
		}
	}

	// Increment element count after successful processing
	dfr.elementCount++

	return dataElem, nil
}

// ConvertRawDataElement converts a raw data element using hooks.
// This integrates with the hooks system for custom VR lookup and value conversion.
func ConvertRawDataElement(elem *DataElementValue, encoding string) (*hooks.RawDataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("element cannot be nil")
	}

	// Convert to RawDataElement format
	vrPtr := &elem.VR
	raw := &hooks.RawDataElement{
		Tag:   elem.Tag.String(),
		VR:    vrPtr,
		Value: elem.Value,
	}

	return raw, nil
}

// DICOMFile is a parsed DICOM file: preamble, meta header, and dataset.
type DICOMFile struct {
	FileMetaInfo   *FileMetaInfo
	DataElements   []*DataElementValue
	ExplicitVR     bool
	IsLittleEndian bool

	// HasPreamble reports whether the file began with the 128-byte preamble and
	// the DICM prefix. False means a raw data set with no file meta header,
	// which is parsed as implicit VR little endian.
	HasPreamble bool

	// MetaElements holds the group-0002 file meta elements as they appeared in
	// the file, including any without a corresponding FileMetaInfo field.
	// FileMetaInfo carries the recognized ones in typed form.
	MetaElements []*DataElementValue

	// MetaGroupLength is the File Meta Information Group Length element
	// (0002,0000), which MetaElements leaves out, or nil for a file written
	// without one.
	MetaGroupLength *DataElementValue

	// DataSetOffset is where the data set begins in the file: after the
	// file meta group, or 0 for a file with none.
	DataSetOffset int64

	// Deflated reports that the data set was inflated before it was read
	// (PS3.5 A.5). The positions of its elements then count from the first
	// byte of the inflated data set, not of the file.
	Deflated bool

	// Warnings collects non-fatal issues found while parsing (unknown tags,
	// retired tags, VR mismatches, truncated meta elements, and data elements
	// that repeat the tag before them or follow a greater one, which PS3.5 7.1
	// does not allow, in the data set and in each sequence item). Parsing
	// continues past these; inspect the slice to surface them.
	Warnings []string

	// TagOrderViolations are the data elements, up to a thousand, whose tag is
	// not greater than the tag of the element before them in the same data set
	// or sequence item (PS3.5 7.1), in the order read; TagOrderViolationCount
	// counts them all, so a count above the length means some were not kept.
	// Each is also among Warnings, in words. A program that acts on the order
	// reads these and not the text of a warning.
	TagOrderViolations     []TagOrderViolation
	TagOrderViolationCount int

	// ctx carries the logger the file was read with; see context.
	ctx context.Context
}

// GetDataset converts the parsed file into a Dataset, recursively materializing
// any nested sequences as sequence.Sequence values holding child Datasets.
func (df *DICOMFile) GetDataset() *dataset.Dataset {
	return df.dataset(false)
}

// GetDatasetWithValuesInFile is GetDataset for a file read with
// ReadOptions.SkipBulkData, keeping where each skipped value is: its element
// holds a dataelem.ValueInFile instead of nothing. filewriter, given the file
// as its value source, writes the data set back whole with the bytes copied
// from the file, so an image can be rewritten without its pixels in memory.
//
// A deflated file is refused: its values are in the inflated bytes, which are
// not in the file to be copied from (PS3.5 A.5).
func (df *DICOMFile) GetDatasetWithValuesInFile() (*dataset.Dataset, error) {
	if df.Deflated {
		return nil, fmt.Errorf("the data set is deflated (%s), so its values are not in the file "+
			"to be copied from; read it whole", DeflatedExplicitVRLittleEndianUID)
	}
	return df.dataset(true), nil
}

// dataset builds the data set, with values left in the file as references
// when inFile is set and as nothing otherwise.
func (df *DICOMFile) dataset(inFile bool) *dataset.Dataset {
	// Character set decisions are recorded on the file, not only logged: a
	// caller writing the data set back is the one who needs to know a guess was
	// made. GetDataset may be called more than once, so each is recorded once.
	ds := elementsToDataset(df.context(), df.DataElements, nil, inFile, func(warning string) {
		for _, existing := range df.Warnings {
			if existing == warning {
				return
			}
		}
		config.LoggerFromContext(df.context()).Warn("filereader: " + warning)
		df.Warnings = append(df.Warnings, warning)
	})

	// The transfer syntax lives in the file meta header, which is not part of
	// the data set. Carrying it across is what lets pixel access know whether
	// PixelData is raw or encapsulated, and which codec to use.
	if df.FileMetaInfo != nil {
		ds.SetTransferSyntaxUID(df.FileMetaInfo.TransferSyntaxUID)
	}
	return ds
}

// elementsToDataset builds a Dataset from parsed elements, descending into
// sequence items and decoding text into UTF-8.
//
// The decoding, and what the data set then declares, is fileparse.DecodeItem,
// which attributes.DICOMJSON reads the same elements through, so a data set and
// the JSON of the same file cannot disagree about the text.
//
// inherited is the character set in force from the enclosing data set, which an
// item may override. Passing nil at the top level means "read it from these
// elements". inFile keeps a skipped value as a dataelem.ValueInFile. warn records
// a decision the caller should know about; it may be nil.
func elementsToDataset(ctx context.Context, elements []*DataElementValue, inherited []string, inFile bool, warn func(string)) *dataset.Dataset {
	ds := dataset.NewDataset()
	item := fileparse.DecodeItem(ctx, elements, inherited, warn)

	for _, d := range item.Elements {
		if d.Source != nil && fileparse.IsSequenceElement(d.Source) {
			seq := sequence.New()
			for _, child := range d.Source.Items {
				_ = seq.Append(elementsToDataset(ctx, child.Elements, item.Encodings, inFile, warn))
			}
			_ = ds.AddSequenceWithContext(ctx, d.Tag, seq)
			continue
		}

		if inFile && d.Source != nil && d.Source.ValueSkipped {
			_ = ds.AddWithContext(ctx, dataelem.NewDataElement(d.Tag, dataelem.VR(d.VR), valueInFile(d.Source)))
			continue
		}

		_ = ds.AddWithContext(ctx, dataelem.NewDataElement(d.Tag, dataelem.VR(d.VR), d.Value))
	}
	return ds
}

// valueInFile is where a skipped value's bytes are.
func valueInFile(elem *DataElementValue) dataelem.ValueInFile {
	offset, length := fileparse.ValueExtent(elem)
	return dataelem.ValueInFile{Offset: offset, Length: length}
}

// ReadDICOMFile reads an entire DICOM file.
func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error) {
	return ReadDICOMFileWithOptions(reader, ReadOptions{})
}
