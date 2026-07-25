package filereader

import (
	"fmt"
	"io"
	"strings"

	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/hooks"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// FileMetaInfo contains DICOM file meta header information.
type FileMetaInfo struct {
	MediaStorageSOPClassUID         string
	MediaStorageSOPInstanceUID      string
	TransferSyntaxUID               string
	ImplementationClassUID          string
	ImplementationVersionName       string
	SourceApplicationEntityTitle    string
	SendingApplicationEntityTitle   string
	ReceivingApplicationEntityTitle string
	FileMetaInformationGroupLength  uint32
	FileMetaInformationVersion      []byte
}

// DCMFileReader reads DICOM files.
type DCMFileReader struct {
	reader       filebase.Reader
	fileMetaInfo *FileMetaInfo
	position     int64
	settings     *config.Settings // Configuration settings for reading behavior
	hookChain    *hooks.HookChain // Hook chain for element processing
	elementCount int              // Count of elements read
	metaWarnings []string         // Non-fatal issues found while reading the meta header
}

// NewDCMFileReader creates a new DICOM file reader.
func NewDCMFileReader(reader filebase.Reader) *DCMFileReader {
	return &DCMFileReader{
		reader:       reader,
		position:     0,
		settings:     config.Get(),         // Load global configuration settings
		hookChain:    hooks.NewHookChain(), // Initialize empty hook chain
		elementCount: 0,
	}
}

// ReadPreamble reads the 128-byte DICOM preamble.
func (dfr *DCMFileReader) ReadPreamble() error {
	preamble := make([]byte, 128)
	if err := dfr.reader.ReadBytes(preamble); err != nil {
		return fmt.Errorf("failed to read preamble: %w", err)
	}

	dfr.position += 128
	return nil
}

// ReadDICMPrefix reads and validates the "DICM" magic string.
func (dfr *DCMFileReader) ReadDICMPrefix() error {
	prefix := make([]byte, 4)
	if err := dfr.reader.ReadBytes(prefix); err != nil {
		return fmt.Errorf("failed to read DICM prefix: %w", err)
	}

	if string(prefix) != "DICM" {
		return fmt.Errorf("invalid DICM prefix: got %q, expected 'DICM'", string(prefix))
	}

	dfr.position += 4
	return nil
}

// ReadFileMetaInformationGroupLength reads the File Meta Information Group Length (0002,0000).
func (dfr *DCMFileReader) ReadFileMetaInformationGroupLength() (uint32, error) {
	tagValue, err := dfr.ReadTag()
	if err != nil {
		return 0, fmt.Errorf("failed to read meta info group length tag: %w", err)
	}

	expectedTag := tag.New(0x0002, 0x0000)
	if tagValue != expectedTag {
		return 0, fmt.Errorf("expected tag %s for file meta info group length, got %s", expectedTag.String(), tagValue.String())
	}

	vr := make([]byte, 2)
	if err := dfr.reader.ReadBytes(vr); err != nil {
		return 0, fmt.Errorf("failed to read VR: %w", err)
	}

	if string(vr) != "UL" {
		return 0, fmt.Errorf("expected VR 'UL' for file meta info group length, got %q", string(vr))
	}

	reserved := make([]byte, 2)
	if err := dfr.reader.ReadBytes(reserved); err != nil {
		return 0, fmt.Errorf("failed to read reserved bytes: %w", err)
	}

	length, err := dfr.reader.ReadUint32()
	if err != nil {
		return 0, fmt.Errorf("failed to read group length: %w", err)
	}

	dfr.position += 12
	return length, nil
}

// ReadFileMetaInfo reads the DICOM file meta information.
func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error) {
	metaInfo := &FileMetaInfo{}

	groupLength, err := dfr.ReadFileMetaInformationGroupLength()
	if err != nil {
		return nil, err
	}

	metaInfo.FileMetaInformationGroupLength = groupLength
	startPosition := dfr.position

	for dfr.position-startPosition < int64(groupLength) {
		tagValue, err := dfr.ReadTag()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read meta element tag: %w", err)
		}

		vrBytes := make([]byte, 2)
		if err := dfr.reader.ReadBytes(vrBytes); err != nil {
			return nil, fmt.Errorf("failed to read VR: %w", err)
		}
		vr := string(vrBytes)

		dfr.position += 6

		var valueLength uint32
		if isShortVR(vr) {
			length := make([]byte, 2)
			if err := dfr.reader.ReadBytes(length); err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = uint32(length[0]) | (uint32(length[1]) << 8)
			dfr.position += 2
		} else {
			reserved := make([]byte, 2)
			if err := dfr.reader.ReadBytes(reserved); err != nil {
				return nil, fmt.Errorf("failed to read reserved bytes: %w", err)
			}
			dfr.position += 2

			length, err := dfr.reader.ReadUint32()
			if err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = length
			dfr.position += 4
		}

		value := make([]byte, valueLength)
		if err := dfr.reader.ReadBytes(value); err != nil {
			// Handle read error gracefully - file meta info might be incomplete/corrupted.
			// Log a warning but continue with what we have so far; the rest of the
			// file may still be readable.
			dfr.metaWarnings = append(dfr.metaWarnings,
				fmt.Sprintf("skipped file meta element %s (expected %d bytes): %v",
					tagValue.String(), valueLength, err))
			return metaInfo, nil
		}
		dfr.position += int64(valueLength)

		if err := dfr.storeMetaValue(metaInfo, tagValue, vr, value); err != nil {
			return nil, err
		}
	}

	dfr.fileMetaInfo = metaInfo
	return metaInfo, nil
}

// ReadTag reads a DICOM tag (4 bytes: group + element).
func (dfr *DCMFileReader) ReadTag() (tag.Tag, error) {
	group, err := dfr.reader.ReadUint16()
	if err != nil {
		return tag.Tag(0), err
	}

	element, err := dfr.reader.ReadUint16()
	if err != nil {
		return tag.Tag(0), err
	}

	dfr.position += 4
	return tag.New(uint16(group), uint16(element)), nil
}

// UndefinedLength is the DICOM sentinel value (0xFFFFFFFF) used in the Value
// Length field to indicate that an element's extent is delimited by a
// Sequence/Item Delimitation Item rather than stated up front. It is legal for
// Sequences (SQ) and for encapsulated Pixel Data. See PS3.5 Section 7.1.
const UndefinedLength uint32 = 0xFFFFFFFF

// ReadDataElement reads a single data element from the dataset.
type DataElementValue struct {
	Tag    tag.Tag
	VR     string
	Value  []byte
	Length uint32

	// UndefinedLength reports whether the element declared length 0xFFFFFFFF.
	// Such elements carry no inline value; their content is delimited and is
	// parsed separately (see ReadSequence).
	UndefinedLength bool

	// Items holds the parsed child datasets of a Sequence (SQ) element.
	// It is nil for non-sequence elements.
	Items []*SequenceItemValue
}

// SequenceItemValue is a single item within a Sequence (SQ) element,
// holding the data elements nested inside that item.
type SequenceItemValue struct {
	Elements []*DataElementValue
}

// MaxSequenceDepth bounds how deeply nested sequences may be parsed. Real
// DICOM objects nest a handful of levels at most; the limit stops a crafted or
// corrupt file from driving unbounded recursion.
const MaxSequenceDepth = 64

// ReadDataElement reads a data element, including any nested sequence content.
func (dfr *DCMFileReader) ReadDataElement(explicitVR bool) (*DataElementValue, error) {
	return dfr.readDataElement(explicitVR, 0)
}

// readDataElement reads one data element at the given sequence nesting depth.
func (dfr *DCMFileReader) readDataElement(explicitVR bool, depth int) (*DataElementValue, error) {
	tagValue, err := dfr.ReadTag()
	if err != nil {
		return nil, err
	}

	element := &DataElementValue{
		Tag: tagValue,
	}

	// Item and delimitation items (group FFFE) are always encoded as
	// tag + 4-byte length with no VR, even inside explicit VR transfer
	// syntaxes. See PS3.5 Section 7.5.
	if tagValue.Group() == 0xFFFE {
		length, err := dfr.reader.ReadUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to read item length: %w", err)
		}
		dfr.position += 4
		element.Length = length
		element.UndefinedLength = length == UndefinedLength
		return element, nil
	}

	if explicitVR {
		vrBytes := make([]byte, 2)
		if err := dfr.reader.ReadBytes(vrBytes); err != nil {
			return nil, fmt.Errorf("failed to read VR: %w", err)
		}
		element.VR = string(vrBytes)
		dfr.position += 2

		var valueLength uint32
		if isShortVR(element.VR) {
			length := make([]byte, 2)
			if err := dfr.reader.ReadBytes(length); err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = uint32(length[0]) | (uint32(length[1]) << 8)
			dfr.position += 2
		} else {
			reserved := make([]byte, 2)
			if err := dfr.reader.ReadBytes(reserved); err != nil {
				return nil, fmt.Errorf("failed to read reserved bytes: %w", err)
			}
			dfr.position += 2

			length, err := dfr.reader.ReadUint32()
			if err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = length
			dfr.position += 4
		}
		element.Length = valueLength
	} else {
		length, err := dfr.reader.ReadUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to read value length: %w", err)
		}
		element.Length = length
		dfr.position += 4
		// Implicit VR carries no VR on the wire; recover it from the dictionary
		// so sequences can be recognized and descended into.
		element.VR = tagValue.GetVR()
	}

	element.UndefinedLength = element.Length == UndefinedLength

	// Sequences are parsed into items rather than read as an opaque value.
	if isSequenceVR(element.VR) {
		if depth >= MaxSequenceDepth {
			return nil, fmt.Errorf("sequence nesting exceeds maximum depth %d at tag %s",
				MaxSequenceDepth, element.Tag.String())
		}
		items, err := dfr.readSequenceItems(explicitVR, depth+1, element.Length)
		if err != nil {
			return nil, fmt.Errorf("failed to read sequence %s: %w", element.Tag.String(), err)
		}
		element.Items = items
		return element, nil
	}

	// An undefined length on a non-sequence element means encapsulated
	// (fragmented) pixel data: a run of items terminated by a Sequence
	// Delimitation Item. Concatenate the fragments into the element value.
	if element.UndefinedLength {
		value, err := dfr.readEncapsulatedValue()
		if err != nil {
			return nil, fmt.Errorf("failed to read encapsulated value for %s: %w",
				element.Tag.String(), err)
		}
		element.Value = value
		return element, nil
	}

	if element.Length > 0 {
		// Guard the allocation against a corrupt or hostile length field before
		// committing memory to it.
		if err := dfr.checkValueLength(element.Length); err != nil {
			return nil, fmt.Errorf("invalid length for tag %s: %w", element.Tag.String(), err)
		}
		value := make([]byte, element.Length)
		if err := dfr.reader.ReadBytes(value); err != nil {
			return nil, fmt.Errorf("failed to read data element value for tag %s (claimed %d bytes): %w",
				element.Tag.String(), element.Length, err)
		}
		element.Value = value
		dfr.position += int64(element.Length)
	}

	return element, nil
}

// lengthCheckThreshold is the declared value length above which the reader
// verifies the claim against the actual bytes remaining. Verification costs
// three seeks, so small elements — the overwhelming majority — skip it; an
// allocation of this size is harmless even when the length turns out to be
// wrong.
const lengthCheckThreshold = 16 << 20

// checkValueLength rejects declared value lengths that cannot be satisfied by
// the underlying stream. A corrupt or crafted file can claim a multi-gigabyte
// element; allocating for it before reading would exhaust memory.
func (dfr *DCMFileReader) checkValueLength(length uint32) error {
	if length < lengthCheckThreshold {
		return nil
	}
	remaining, err := dfr.remainingBytes()
	if err != nil {
		// Size is not knowable (non-seekable source); fall back to reading and
		// letting the short-read check report the truncation.
		return nil //nolint:nilerr // unknown size is not an error, just unverifiable
	}
	if int64(length) > remaining {
		return fmt.Errorf("declared length %d exceeds %d bytes remaining in stream", length, remaining)
	}
	return nil
}

// remainingBytes reports how many bytes are left in the underlying stream,
// restoring the stream position before returning.
func (dfr *DCMFileReader) remainingBytes() (int64, error) {
	current, err := dfr.reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	end, err := dfr.reader.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := dfr.reader.Seek(current, io.SeekStart); err != nil {
		return 0, err
	}
	return end - current, nil
}

// readSequenceItems reads the items of a Sequence (SQ) element. A declared
// length of UndefinedLength means the sequence runs until a Sequence
// Delimitation Item; otherwise it spans exactly declaredLength bytes.
func (dfr *DCMFileReader) readSequenceItems(explicitVR bool, depth int, declaredLength uint32) ([]*SequenceItemValue, error) {
	var items []*SequenceItemValue

	undefined := declaredLength == UndefinedLength
	if !undefined {
		if err := dfr.checkValueLength(declaredLength); err != nil {
			return nil, err
		}
	}
	start := dfr.position

	for {
		if !undefined && dfr.position-start >= int64(declaredLength) {
			return items, nil
		}

		marker, err := dfr.readItemHeader()
		if err != nil {
			if err == io.EOF {
				return items, nil
			}
			return nil, err
		}

		switch marker.tag {
		case tag.SequenceDelimiterTag:
			// End of an undefined-length sequence.
			return items, nil
		case tag.ItemTag:
			item, err := dfr.readSequenceItem(explicitVR, depth, marker.length)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		default:
			return nil, fmt.Errorf("unexpected tag %s inside sequence (expected item or delimiter)",
				marker.tag.String())
		}
	}
}

// itemHeader is the tag + length pair that prefixes a sequence item or delimiter.
type itemHeader struct {
	tag    tag.Tag
	length uint32
}

// readItemHeader reads the tag and 4-byte length of an item or delimitation item.
func (dfr *DCMFileReader) readItemHeader() (*itemHeader, error) {
	t, err := dfr.ReadTag()
	if err != nil {
		return nil, err
	}
	length, err := dfr.reader.ReadUint32()
	if err != nil {
		return nil, fmt.Errorf("failed to read item length: %w", err)
	}
	dfr.position += 4
	return &itemHeader{tag: t, length: length}, nil
}

// readSequenceItem reads the data elements contained in a single sequence item.
func (dfr *DCMFileReader) readSequenceItem(explicitVR bool, depth int, declaredLength uint32) (*SequenceItemValue, error) {
	item := &SequenceItemValue{}

	undefined := declaredLength == UndefinedLength
	if !undefined {
		if err := dfr.checkValueLength(declaredLength); err != nil {
			return nil, err
		}
	}
	start := dfr.position

	for {
		if !undefined && dfr.position-start >= int64(declaredLength) {
			return item, nil
		}

		elem, err := dfr.readDataElement(explicitVR, depth)
		if err != nil {
			if err == io.EOF {
				return item, nil
			}
			return nil, err
		}

		// An Item Delimitation Item closes an undefined-length item.
		if elem.Tag == tag.ItemDelimiterTag {
			return item, nil
		}
		// A Sequence Delimitation Item here means the enclosing sequence ended
		// without an explicit item delimiter; treat the item as complete.
		if elem.Tag == tag.SequenceDelimiterTag {
			return item, nil
		}

		item.Elements = append(item.Elements, elem)
	}
}

// readEncapsulatedValue reads the fragments of an undefined-length,
// non-sequence element (encapsulated Pixel Data) and returns them
// concatenated. The Basic Offset Table, when present, is the first item and is
// skipped since frame boundaries are recovered by the encaps package.
func (dfr *DCMFileReader) readEncapsulatedValue() ([]byte, error) {
	var value []byte
	first := true

	for {
		marker, err := dfr.readItemHeader()
		if err != nil {
			if err == io.EOF {
				return value, nil
			}
			return nil, err
		}

		if marker.tag == tag.SequenceDelimiterTag {
			return value, nil
		}
		if marker.tag != tag.ItemTag {
			return nil, fmt.Errorf("unexpected tag %s in encapsulated data (expected item or delimiter)",
				marker.tag.String())
		}
		if marker.length == UndefinedLength {
			return nil, fmt.Errorf("encapsulated data item has undefined length")
		}

		if marker.length > 0 {
			if err := dfr.checkValueLength(marker.length); err != nil {
				return nil, err
			}
			fragment := make([]byte, marker.length)
			if err := dfr.reader.ReadBytes(fragment); err != nil {
				return nil, fmt.Errorf("failed to read encapsulated fragment (%d bytes): %w",
					marker.length, err)
			}
			dfr.position += int64(marker.length)

			// The first item is the Basic Offset Table, not pixel data.
			if !first {
				value = append(value, fragment...)
			}
		}
		first = false
	}
}

// isSequenceVR reports whether a VR denotes a Sequence of Items.
func isSequenceVR(vr string) bool {
	return vr == "SQ"
}

// GetPosition returns the current position in the file.
func (dfr *DCMFileReader) GetPosition() int64 {
	return dfr.position
}

// GetFileMetaInfo returns the file meta information.
func (dfr *DCMFileReader) GetFileMetaInfo() *FileMetaInfo {
	return dfr.fileMetaInfo
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

// isShortVR checks if a VR uses short format (2-byte length instead of 4-byte).
// Short format VRs have a 2-byte reserved field and 2-byte length.
// Long format VRs (OB, OD, OF, OL, OW, SQ, UC, UN, UR, UT) have 2-byte reserved and 4-byte length.
func isShortVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OW", "SQ", "UC", "UN", "UR", "UT":
		return false
	default:
		return true
	}
}

// storeMetaValue stores a parsed meta information value into the appropriate FileMetaInfo field.
// It handles the standard DICOM file meta information tags (Group 0002).
func (dfr *DCMFileReader) storeMetaValue(metaInfo *FileMetaInfo, t tag.Tag, vr string, value []byte) error {
	group := t.Group()
	element := t.Element()

	// Helper function to convert bytes to string and trim null terminators
	toString := func(b []byte) string {
		// Find and remove null terminator if present
		s := string(b)
		if idx := strings.Index(s, "\x00"); idx >= 0 {
			return s[:idx]
		}
		return s
	}

	switch {
	case group == 0x0002 && element == 0x0001:
		metaInfo.FileMetaInformationVersion = value
	case group == 0x0002 && element == 0x0002:
		metaInfo.MediaStorageSOPClassUID = toString(value)
	case group == 0x0002 && element == 0x0003:
		metaInfo.MediaStorageSOPInstanceUID = toString(value)
	case group == 0x0002 && element == 0x0010:
		metaInfo.TransferSyntaxUID = toString(value)
	case group == 0x0002 && element == 0x0012:
		metaInfo.ImplementationClassUID = toString(value)
	case group == 0x0002 && element == 0x0013:
		metaInfo.ImplementationVersionName = toString(value)
	// Application Entity titles are (0002,0016..0018) per PS3.6; the 0x0100
	// range previously used here belongs to Private Information attributes.
	case group == 0x0002 && element == 0x0016:
		metaInfo.SourceApplicationEntityTitle = toString(value)
	case group == 0x0002 && element == 0x0017:
		metaInfo.SendingApplicationEntityTitle = toString(value)
	case group == 0x0002 && element == 0x0018:
		metaInfo.ReceivingApplicationEntityTitle = toString(value)
	}

	return nil
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

	// Warnings collects non-fatal issues found while parsing (unknown tags,
	// retired tags, VR mismatches, truncated meta elements). Parsing continues
	// past these; inspect the slice to surface them.
	Warnings []string
}

// GetDataset converts the parsed file into a Dataset, recursively materializing
// any nested sequences as sequence.Sequence values holding child Datasets.
func (df *DICOMFile) GetDataset() *dataset.Dataset {
	return elementsToDataset(df.DataElements)
}

// elementsToDataset builds a Dataset from parsed elements, descending into
// sequence items.
func elementsToDataset(elements []*DataElementValue) *dataset.Dataset {
	ds := dataset.NewDataset()

	for _, elem := range elements {
		if elem.Items != nil || isSequenceVR(elem.VR) {
			seq := sequence.New()
			for _, item := range elem.Items {
				_ = seq.Append(elementsToDataset(item.Elements))
			}
			_ = ds.AddSequence(elem.Tag, seq)
			continue
		}
		_ = ds.Add(dataelem.NewDataElement(elem.Tag, dataelem.VR(elem.VR), elem.Value))
	}

	return ds
}

// ReadDICOMFile reads an entire DICOM file.
func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error) {
	dfr := NewDCMFileReader(reader)

	dicomFile := &DICOMFile{
		DataElements: make([]*DataElementValue, 0),
	}

	if err := dfr.ReadPreamble(); err != nil {
		return nil, fmt.Errorf("failed to read preamble: %w", err)
	}

	if err := dfr.ReadDICMPrefix(); err != nil {
		return nil, fmt.Errorf("failed to read DICM prefix: %w", err)
	}

	metaInfo, err := dfr.ReadFileMetaInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to read file meta information: %w", err)
	}

	dicomFile.FileMetaInfo = metaInfo
	dicomFile.Warnings = append(dicomFile.Warnings, dfr.metaWarnings...)

	ts := metaInfo.TransferSyntaxUID
	dicomFile.ExplicitVR, dicomFile.IsLittleEndian = determineTransferSyntax(ts)

	if dicomFile.IsLittleEndian {
		reader.SetByteOrder(filebase.LittleEndian)
	} else {
		reader.SetByteOrder(filebase.BigEndian)
	}

	for {
		element, err := dfr.ReadDataElement(dicomFile.ExplicitVR)
		if err != nil {
			if err == io.EOF {
				break
			}
			errMsg := err.Error()
			if strings.Contains(errMsg, "reached end of file") {
				break
			}
			// Check if this is a length-related error (corrupted element)
			if strings.Contains(errMsg, "claimed") && strings.Contains(errMsg, "bytes") ||
				strings.Contains(errMsg, "exceeds") {
				dicomFile.Warnings = append(dicomFile.Warnings,
					fmt.Sprintf("stopped at corrupted element at position %d: %v", dfr.position, err))
				break
			}
			return nil, fmt.Errorf("failed to read data element: %w", err)
		}

		if err := validateDataElement(element); err != nil {
			dicomFile.Warnings = append(dicomFile.Warnings,
				fmt.Sprintf("tag %s: %v", element.Tag.String(), err))
		}

		// A delimitation item at the top level is stray — sequence content is
		// consumed by the sequence parser, so one appearing here marks the end
		// of readable data rather than an element to keep.
		if element.Tag == tag.SequenceDelimiterTag || element.Tag == tag.ItemDelimiterTag {
			break
		}

		dicomFile.DataElements = append(dicomFile.DataElements, element)
	}

	return dicomFile, nil
}

// determineTransferSyntax determines the VR type and byte order from a transfer syntax UID.
// Returns (explicitVR, littleEndian).
//
// Examples:
// - ImplicitVRLittleEndian (default): (false, true)
// - ExplicitVRLittleEndian: (true, true)
// - ExplicitVRBigEndian: (true, false)
// - Compressed formats (JPEG, RLE, etc.): (true, true)
func determineTransferSyntax(ts string) (bool, bool) {
	explicitVR := false
	littleEndian := true

	u := uid.New(ts)
	if !u.IsValid() {
		return explicitVR, littleEndian
	}

	if ts == uid.BigEndianTransferSyntax().String() {
		return true, false
	}

	if ts == uid.ImplicitVRLittleEndian().String() {
		return false, true
	}

	if ts == uid.ExplicitVRLittleEndian().String() {
		return true, true
	}

	if !uid.IsCompressed(u) {
		return true, true
	}

	if uid.IsCompressed(u) {
		return true, true
	}

	return explicitVR, littleEndian
}

// validateDataElement validates a data element against the DICOM dictionary.
// It checks that:
// 1. Standard tags are registered in the DICOM dictionary
// 2. The tag is not retired
// 3. The VR matches the dictionary entry (or is a valid variant like "OB or OW")
//
// Private tags (odd group numbers) always pass validation.
func validateDataElement(elem *DataElementValue) error {
	dict := tag.GlobalDictionary()
	info := dict.Get(elem.Tag)

	if info == nil {
		if elem.Tag.IsPrivate() {
			return nil
		}
		return fmt.Errorf("unknown standard tag: %s", elem.Tag)
	}

	if info.Retired {
		return fmt.Errorf("tag %s (%s) is retired", elem.Tag, info.Name)
	}

	if elem.VR != "" && info.VR != "" {
		if info.VR != elem.VR && !isValidVRVariant(elem.VR, info.VR) {
			return fmt.Errorf("VR mismatch for %s: expected %s, got %s",
				elem.Tag, info.VR, elem.VR)
		}
	}

	return nil
}

// isValidVRVariant checks if a VR is a valid variant of the expected VR.
// Some DICOM tags allow multiple VR types, represented as "X or Y" in the dictionary.
// This function validates that the actual VR matches one of the allowed variants.
func isValidVRVariant(actual, expected string) bool {
	switch expected {
	case "OB or OW":
		return actual == "OB" || actual == "OW"
	case "US or SS":
		return actual == "US" || actual == "SS"
	case "US or OW":
		return actual == "US" || actual == "OW"
	case "UL or OW":
		return actual == "UL" || actual == "OW"
	case "OB or OD or OF or OL or OW or UN":
		return actual == "OB" || actual == "OD" || actual == "OF" || actual == "OL" || actual == "OW" || actual == "UN"
	default:
		return false
	}
}
