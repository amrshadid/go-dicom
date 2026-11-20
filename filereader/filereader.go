package filereader

import (
	"context"
	"fmt"
	"unicode/utf8"

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

// specificCharacterSetTag is (0008,0005).
const specificCharacterSetTag = fileparse.SpecificCharacterSetTag

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
// inherited is the character set in force from the enclosing data set, which an
// item may override: PS3.5 allows Specific Character Set inside a sequence item,
// and it applies to that item and anything below it. Passing nil at the top
// level means "read it from these elements".
//
// inFile keeps a skipped value as a dataelem.ValueInFile. warn records a
// decision the caller should know about; it may be nil.
func elementsToDataset(ctx context.Context, elements []*DataElementValue, inherited []string, inFile bool, warn func(string)) *dataset.Dataset {
	ds := dataset.NewDataset()

	encodings, fellBack := fileparse.EncodingsFor(ctx, elements, inherited)

	// Decode first, decide afterwards. Whether this data set may declare UTF-8
	// depends on how the values came out, and the declaration has to be written
	// in tag order, before the values it describes.
	type decodedElement struct {
		elem  *DataElementValue
		value []byte
	}
	decoded := make([]decodedElement, 0, len(elements))
	guessed, allUTF8 := false, true
	for _, elem := range elements {
		if elem.Items != nil || fileparse.IsSequenceVR(elem.VR) || elem.Tag == specificCharacterSetTag {
			decoded = append(decoded, decodedElement{elem, elem.Value})
			continue
		}
		value := fileparse.DecodeTextValue(ctx, elem.VR, elem.Value, encodings)
		if dataelem.IsTextVR(dataelem.VR(elem.VR)) {
			if fellBack && !isASCII(elem.Value) {
				// Latin-1 is a guess: the file did not say, or named something
				// this build does not know, and another single-byte set is
				// possible. The caller is told rather than left to notice.
				guessed = true
			}
			if !utf8.Valid(value) {
				allUTF8 = false
			}
		}
		decoded = append(decoded, decodedElement{elem, value})
	}

	if guessed && warn != nil {
		warn("text with no usable Specific Character Set (0008,0005) was decoded as ISO-8859-1, " +
			"which is a guess; the default repertoire is ASCII (PS3.5 6.1.2.3)")
	}
	// Reached when a decoder returns an error and decodeTextValue keeps the
	// bytes as they were found. The decoders here substitute U+FFFD for input
	// they cannot map, so this is insurance rather than a path a corpus file
	// takes today — and it is the difference between a data set that is wrong
	// and one that lies about being right.
	if !allUTF8 && warn != nil {
		warn("keeping the declared Specific Character Set (0008,0005): some text did not decode, " +
			"so the data set cannot claim ISO_IR 192 over it")
	}

	// A declaration is added when text was converted and the file had none.
	// Without it, writing the data set back produces UTF-8 bytes that a reader
	// decodes as the default repertoire — the same corruption, one file later.
	addDeclaration := guessed && allUTF8 && !hasSpecificCharacterSet(elements)

	for _, d := range decoded {
		if addDeclaration && d.elem.Tag > specificCharacterSetTag {
			_ = ds.AddWithContext(ctx, dataelem.NewDataElement(specificCharacterSetTag, dataelem.CS,
				[]byte(utf8CharacterSet)))
			addDeclaration = false
		}

		if d.elem.Items != nil || fileparse.IsSequenceVR(d.elem.VR) {
			seq := sequence.New()
			for _, item := range d.elem.Items {
				_ = seq.Append(elementsToDataset(ctx, item.Elements, encodings, inFile, warn))
			}
			_ = ds.AddSequenceWithContext(ctx, d.elem.Tag, seq)
			continue
		}

		if inFile && d.elem.ValueSkipped {
			_ = ds.AddWithContext(ctx, dataelem.NewDataElement(d.elem.Tag, dataelem.VR(d.elem.VR), valueInFile(d.elem)))
			continue
		}

		value := d.value
		if d.elem.Tag == specificCharacterSetTag && allUTF8 {
			// The values below are UTF-8 now, so the attribute has to say so or
			// the data set contradicts itself — and anything writing it back out
			// would label UTF-8 bytes as something else. Only when they really
			// are UTF-8: this used to be unconditional, so a file whose text did
			// not decode was labeled ISO_IR 192 over the bytes it came with.
			value = []byte(utf8CharacterSet)
		}
		_ = ds.AddWithContext(ctx, dataelem.NewDataElement(d.elem.Tag, dataelem.VR(d.elem.VR), value))
	}
	if addDeclaration {
		_ = ds.AddWithContext(ctx, dataelem.NewDataElement(specificCharacterSetTag, dataelem.CS,
			[]byte(utf8CharacterSet)))
	}

	return ds
}

// valueInFile is where a skipped value's bytes are. Encapsulated Pixel Data
// runs from its first item to the delimiter that closes it, or to where the
// reader stopped in a file that ends without one: the bytes a whole read puts
// in the value.
func valueInFile(elem *DataElementValue) dataelem.ValueInFile {
	if !elem.UndefinedLength {
		return dataelem.ValueInFile{Offset: elem.Position.ValueOffset, Length: int64(elem.Length)}
	}
	end := elem.Position.DelimiterOffset
	if end == 0 {
		end = elem.Position.End
	}
	return dataelem.ValueInFile{Offset: elem.Position.ValueOffset, Length: end - elem.Position.ValueOffset}
}

// utf8CharacterSet is the defined term for UTF-8, which every decoded data set
// declares.
const utf8CharacterSet = "ISO_IR 192"

// isASCII reports whether every byte is in the default repertoire, which needs
// no decoding whatever the declaration says.
func isASCII(value []byte) bool {
	for _, b := range value {
		if b > 0x7F {
			return false
		}
	}
	return true
}

// hasSpecificCharacterSet reports whether (0008,0005) is present at all.
func hasSpecificCharacterSet(elements []*DataElementValue) bool {
	for _, elem := range elements {
		if elem.Tag == specificCharacterSetTag {
			return true
		}
	}
	return false
}

// ReadDICOMFile reads an entire DICOM file.
func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error) {
	return ReadDICOMFileWithOptions(reader, ReadOptions{})
}
