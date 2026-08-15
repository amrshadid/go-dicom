package filereader

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/compress"
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

	// Total size of the underlying stream, measured once and reused to bound
	// every declared element length. streamSizeErr records a non-seekable
	// source so the measurement is not retried per element.
	streamSize      int64
	streamSizeKnown bool
	streamSizeErr   error

	// metaElements holds the group-0002 elements exactly as they appeared,
	// including any not represented by a FileMetaInfo field.
	metaElements []*DataElementValue
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

// detectPreamble determines whether the stream begins with a DICOM Part 10
// preamble and DICM prefix, consuming them if so and rewinding to the start if
// not.
//
// A short stream is not an error here: an empty or truncated file simply has no
// preamble, and the caller then finds no elements.
func (dfr *DCMFileReader) detectPreamble() (bool, error) {
	start, err := dfr.reader.Seek(0, io.SeekCurrent)
	if err != nil {
		// Not seekable, so the preamble cannot be tested for; assume Part 10,
		// which is what a non-seekable DICOM source almost always is.
		if err := dfr.ReadPreamble(); err != nil {
			return false, fmt.Errorf("failed to read preamble: %w", err)
		}
		if err := dfr.ReadDICMPrefix(); err != nil {
			return false, fmt.Errorf("failed to read DICM prefix: %w", err)
		}
		return true, nil
	}

	header := make([]byte, 132)
	if err := dfr.reader.ReadBytes(header); err != nil {
		// Fewer than 132 bytes: there is no preamble to consume.
		if _, seekErr := dfr.reader.Seek(start, io.SeekStart); seekErr != nil {
			return false, seekErr
		}
		return false, nil
	}

	if string(header[128:132]) == "DICM" {
		dfr.position = start + 132
		return true, nil
	}

	if _, err := dfr.reader.Seek(start, io.SeekStart); err != nil {
		return false, err
	}
	dfr.position = start
	return false, nil
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

	// ReadTag already accounted for the 4-byte tag; add only the VR (2),
	// reserved (2), and length (4) read here.
	dfr.position += 8
	return length, nil
}

// ReadFileMetaInfo reads the DICOM file meta information.
func (dfr *DCMFileReader) ReadFileMetaInfo() (*FileMetaInfo, error) {
	metaInfo := &FileMetaInfo{}

	// (0002,0000) File Meta Information Group Length is the usual way to know
	// where the meta header ends, but it is not always written. A file without
	// it was rejected outright, though its meta header is perfectly readable —
	// the group itself marks its own end, since every element in it is in group
	// 0002 and the data set that follows is not.
	groupLength, haveGroupLength, err := dfr.tryReadMetaGroupLength()
	if err != nil {
		return nil, err
	}
	if haveGroupLength {
		metaInfo.FileMetaInformationGroupLength = groupLength
	}

	startPosition := dfr.position

	// A stated group length bounds the header. Without one the loop relies on the
	// group check inside it, which is also a useful guard when a stated length is
	// wrong.
	for !haveGroupLength || dfr.position-startPosition < int64(groupLength) {
		tagValue, err := dfr.ReadTag()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read meta element tag: %w", err)
		}

		// The first element outside group 0002 belongs to the data set. Put it
		// back so the data set parser reads it.
		if tagValue.Group() != 0x0002 {
			if _, seekErr := dfr.reader.Seek(-4, io.SeekCurrent); seekErr != nil {
				return nil, fmt.Errorf("failed to rewind past the end of the meta header: %w", seekErr)
			}
			dfr.position -= 4
			break
		}

		vrBytes := make([]byte, 2)
		if err := dfr.reader.ReadBytes(vrBytes); err != nil {
			return nil, fmt.Errorf("failed to read VR: %w", err)
		}
		vr := string(vrBytes)

		// ReadTag already accounted for the 4-byte tag; add only the VR.
		dfr.position += 2

		var valueLength uint32
		if isShortVR(vr) {
			// ReadUint16 honors the reader's byte order; assembling the two
			// bytes by hand would hardcode little endian and corrupt every
			// length in an explicit VR big endian file.
			short, err := dfr.reader.ReadUint16()
			if err != nil {
				return nil, fmt.Errorf("failed to read value length: %w", err)
			}
			valueLength = uint32(short)
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
			config.Logger.Warn("filereader: skipping file meta element",
				"tag", tagValue.String(), "expectedBytes", valueLength, "err", err)
			dfr.metaWarnings = append(dfr.metaWarnings,
				fmt.Sprintf("skipped file meta element %s (expected %d bytes): %v",
					tagValue.String(), valueLength, err))
			return metaInfo, nil
		}
		dfr.position += int64(valueLength)

		dfr.metaElements = append(dfr.metaElements, &DataElementValue{
			Tag: tagValue, VR: vr, Value: value, Length: valueLength,
		})

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

	// Offset is where the item's tag begins, counted from the first byte of the
	// file. It is not a parsing detail: a DICOMDIR's directory records form a
	// tree, and the only thing linking a record to its children is a byte
	// offset stored in the parent (PS3.3 F.2.2). Discarding it makes the
	// hierarchy unrecoverable — the flat sequence order is not the tree, which
	// is what pydicom's DICOMDIR-reordered fixture exists to demonstrate.
	Offset int64
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

	// Set when this element turns out to be implicitly encoded despite the data
	// set declaring explicit VR, so its nested items are parsed the same way.
	elementIsImplicit := false

	if explicitVR {
		vrBytes := make([]byte, 2)
		if err := dfr.reader.ReadBytes(vrBytes); err != nil {
			return nil, fmt.Errorf("failed to read VR: %w", err)
		}
		dfr.position += 2

		// Where a VR should be, something that is not one means this element was
		// written implicitly inside a file that declares explicit VR. Writers do
		// it — most often inside sequences — and the result is non-conformant
		// but common enough that both pydicom and dcmtk try to cope.
		//
		// pydicom's approach is taken here: those two bytes are not a VR at all,
		// they are the low half of a 4-byte length, so the element is read as
		// implicit and its VR comes from the dictionary. dcmtk instead assumes a
		// 2-byte length and still loses the file.
		//
		// Without this, parsing stops at the first such element: the two bytes
		// are read as a length, the value is taken from the wrong offset, and
		// every tag after it is read out of the middle of a value.
		// SC_rgb_jpeg.dcm yielded 1 element of the 34 it holds.
		if !isPlausibleVR(vrBytes) {
			high := make([]byte, 2)
			if err := dfr.reader.ReadBytes(high); err != nil {
				return nil, fmt.Errorf("failed to read the rest of an implicit length for %s: %w",
					element.Tag.String(), err)
			}
			dfr.position += 2

			combined := []byte{vrBytes[0], vrBytes[1], high[0], high[1]}
			if dfr.reader.GetByteOrder() == filebase.BigEndian {
				element.Length = binary.BigEndian.Uint32(combined)
			} else {
				element.Length = binary.LittleEndian.Uint32(combined)
			}
			element.VR = tagValue.GetVR()

			config.Logger.Warn("filereader: element is implicitly encoded in an explicit VR data set",
				"tag", element.Tag.String(), "vr", element.VR)

			// An element encoded this way sits in a data set the writer was
			// treating as implicit, so anything nested inside it is implicit
			// too. Parsing its items as explicit would fail the same way one
			// level down.
			elementIsImplicit = true
		} else {
			element.VR = string(vrBytes)

			if err := dfr.readExplicitLength(element); err != nil {
				return nil, err
			}
		}
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
		items, err := dfr.readSequenceItems(explicitVR && !elementIsImplicit, depth+1, element.Length)
		if err != nil {
			return nil, fmt.Errorf("failed to read sequence %s: %w", element.Tag.String(), err)
		}
		element.Items = items
		return element, nil
	}

	// Undefined length on an element the dictionary does not call a sequence is
	// still almost always a sequence. Only pixel data uses undefined length to
	// mean encapsulation, so anything else carrying it holds items — most often
	// a private element written with VR UN because the writer had no dictionary
	// entry for it, which PS3.5 §6.2.2 says to read as a sequence.
	//
	// Routing these to the encapsulation reader failed at the first item: a
	// sequence item may itself have undefined length, which a pixel fragment
	// never does. UN_sequence.dcm and nested_priv_SQ.dcm were both unreadable
	// for that reason while pydicom read them.
	if element.UndefinedLength && !isEncapsulatedPixelDataTag(element.Tag) {
		if depth >= MaxSequenceDepth {
			return nil, fmt.Errorf("sequence nesting exceeds maximum depth %d at tag %s",
				MaxSequenceDepth, element.Tag.String())
		}
		items, err := dfr.readSequenceItems(explicitVR && !elementIsImplicit, depth+1, element.Length)
		if err != nil {
			return nil, fmt.Errorf("failed to read undefined-length sequence %s: %w",
				element.Tag.String(), err)
		}
		element.VR = "SQ"
		element.Items = items
		return element, nil
	}

	// An undefined length on pixel data means encapsulated (fragmented) pixel
	// data: a run of items terminated by a Sequence Delimitation Item.
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

// checkValueLength rejects declared value lengths that cannot be satisfied by
// the underlying stream, so a corrupt or crafted file cannot make the reader
// allocate for an element far larger than the data that exists.
//
// Every element is checked, not only large ones. An earlier version skipped
// the check below 16 MiB on the theory that a small allocation is harmless,
// but a 200-byte file declaring a 15 MiB element still allocated 15 MiB before
// discovering the stream was short — cheap once, ruinous in a loop. The stream
// size is measured once and cached, so the check costs nothing per element.
// errTruncatedValue marks a length that runs past the end of the stream, so a
// caller can tell a file that was cut short from one whose bytes did not mean
// what they claimed. The two call for different handling: the first loses what
// is missing, the second invalidates what was already read.
var errTruncatedValue = errors.New("value runs past the end of the stream")

func (dfr *DCMFileReader) checkValueLength(length uint32) error {
	size, err := dfr.streamSizeOnce()
	if err != nil {
		// Size is not knowable (non-seekable source); fall back to reading and
		// letting the short-read check report the truncation.
		return nil //nolint:nilerr // unknown size is not an error, just unverifiable
	}

	remaining := size - dfr.position
	if remaining < 0 {
		remaining = 0
	}
	if int64(length) > remaining {
		return fmt.Errorf("%w: declared length %d exceeds %d bytes remaining",
			errTruncatedValue, length, remaining)
	}
	return nil
}

// warn records a non-fatal problem found while reading the data set.
func (dfr *DCMFileReader) warn(format string, args ...any) {
	dfr.metaWarnings = append(dfr.metaWarnings, fmt.Sprintf(format, args...))
}

// streamSizeOnce returns the total size of the underlying stream, measuring it
// on first use and caching the result. Measuring costs three seeks, which is
// why it is not repeated per element.
func (dfr *DCMFileReader) streamSizeOnce() (int64, error) {
	if dfr.streamSizeKnown {
		if dfr.streamSizeErr != nil {
			return 0, dfr.streamSizeErr
		}
		return dfr.streamSize, nil
	}
	dfr.streamSizeKnown = true

	current, err := dfr.reader.Seek(0, io.SeekCurrent)
	if err != nil {
		dfr.streamSizeErr = err
		return 0, err
	}
	end, err := dfr.reader.Seek(0, io.SeekEnd)
	if err != nil {
		dfr.streamSizeErr = err
		return 0, err
	}
	if _, err := dfr.reader.Seek(current, io.SeekStart); err != nil {
		dfr.streamSizeErr = err
		return 0, err
	}

	dfr.streamSize = end
	return end, nil
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
			// Where the item tag started, which is 8 bytes back: 4 for the tag
			// and 4 for the length that readItemHeader has already consumed.
			itemStart := dfr.position - 8
			item, err := dfr.readSequenceItem(explicitVR, depth, marker.length)
			if err != nil {
				// A file cut short loses its last item, not the sequence. The
				// items already read are complete and were parsed from bytes
				// that are all there; discarding them because the next one is
				// short throws away good data to punish a defect it had no part
				// in. pydicom's DICOMDIR-nooffset ends 24 bytes into its last
				// directory record, and dropping the sequence loses the other
				// 51 records with it.
				//
				// Only for truncation. Any other error means the bytes did not
				// mean what they claimed, and continuing past that would build
				// a data set out of a misreading.
				if errors.Is(err, errTruncatedValue) && len(items) > 0 {
					dfr.warn("sequence is truncated; keeping the %d complete items before it: %v",
						len(items), err)
					return items, nil
				}
				return nil, err
			}
			item.Offset = itemStart
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

// readEncapsulatedValue reads an undefined-length, non-sequence element
// (encapsulated Pixel Data) and returns the encapsulation exactly as it appears
// in the file: the Basic Offset Table item, every fragment item, and each of
// their (FFFE,E000) headers.
//
// An earlier version skipped the offset table and concatenated the fragment
// payloads, on the stated reasoning that frame boundaries would be recovered by
// the encaps package. They could not be. That package parses the item
// structure, and by the time it saw the value there was no structure left —
// ExtractEncapsulatedFrames failed with "failed to parse basic offset table"
// because there was no longer a table to parse, and multi-frame compressed
// images could not be split at all.
//
// Keeping the encapsulation also matches what pydicom exposes as PixelData for
// a compressed instance, so a value read here means the same thing as the value
// a user would see there.
func (dfr *DCMFileReader) readEncapsulatedValue() ([]byte, error) {
	var raw bytes.Buffer

	for {
		marker, err := dfr.readItemHeader()
		if err != nil {
			if err == io.EOF {
				return raw.Bytes(), nil
			}
			return nil, err
		}

		if marker.tag == tag.SequenceDelimiterTag {
			// The delimiter closes the encapsulation and is not part of it.
			return raw.Bytes(), nil
		}
		if marker.tag != tag.ItemTag {
			return nil, fmt.Errorf("unexpected tag %s in encapsulated data (expected item or delimiter)",
				marker.tag.String())
		}
		if marker.length == UndefinedLength {
			return nil, fmt.Errorf("encapsulated data item has undefined length")
		}

		writeItemHeader(&raw, dfr.reader.GetByteOrder(), marker)

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
			raw.Write(fragment)
		}
	}
}

// writeItemHeader re-serializes an item header into buf.
//
// The header is rebuilt rather than captured because the reader is consumed
// forward and may not be seekable. Using the reader's own byte order is what
// makes the result byte-identical to the file: readItemHeader decoded the tag
// and length with that order, so encoding them back with it round-trips.
func writeItemHeader(buf *bytes.Buffer, order filebase.ByteOrder, marker *itemHeader) {
	var b [8]byte
	group, element := marker.tag.Group(), marker.tag.Element()

	if order == filebase.BigEndian {
		binary.BigEndian.PutUint16(b[0:2], group)
		binary.BigEndian.PutUint16(b[2:4], element)
		binary.BigEndian.PutUint32(b[4:8], marker.length)
	} else {
		binary.LittleEndian.PutUint16(b[0:2], group)
		binary.LittleEndian.PutUint16(b[2:4], element)
		binary.LittleEndian.PutUint32(b[4:8], marker.length)
	}
	buf.Write(b[:])
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
	case "OB", "OD", "OF", "OL", "OV", "OW", "SQ", "SV", "UC", "UN", "UR", "UT", "UV":
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

	// HasPreamble reports whether the file began with the 128-byte preamble and
	// the DICM prefix. False means a raw data set with no file meta header,
	// which is parsed as implicit VR little endian.
	HasPreamble bool

	// MetaElements holds the group-0002 file meta elements as they appeared in
	// the file, including any without a corresponding FileMetaInfo field.
	// FileMetaInfo carries the recognized ones in typed form.
	MetaElements []*DataElementValue

	// Warnings collects non-fatal issues found while parsing (unknown tags,
	// retired tags, VR mismatches, truncated meta elements). Parsing continues
	// past these; inspect the slice to surface them.
	Warnings []string
}

// GetDataset converts the parsed file into a Dataset, recursively materializing
// any nested sequences as sequence.Sequence values holding child Datasets.
func (df *DICOMFile) GetDataset() *dataset.Dataset {
	ds := elementsToDataset(df.DataElements, nil)

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
func elementsToDataset(elements []*DataElementValue, inherited []string) *dataset.Dataset {
	ds := dataset.NewDataset()

	encodings := inherited
	if declared := specificCharacterSetOf(elements); declared != nil {
		encodings = declared
	}

	for _, elem := range elements {
		if elem.Items != nil || isSequenceVR(elem.VR) {
			seq := sequence.New()
			for _, item := range elem.Items {
				_ = seq.Append(elementsToDataset(item.Elements, encodings))
			}
			_ = ds.AddSequence(elem.Tag, seq)
			continue
		}

		value := elem.Value
		if elem.Tag == specificCharacterSetTag {
			// The values below are UTF-8 now, so the attribute has to say so or
			// the data set contradicts itself — and anything writing it back out
			// would label UTF-8 bytes as something else.
			value = []byte(utf8CharacterSet)
		} else {
			value = decodeTextValue(dataelem.VR(elem.VR), value, encodings)
		}
		_ = ds.Add(dataelem.NewDataElement(elem.Tag, dataelem.VR(elem.VR), value))
	}

	return ds
}

// specificCharacterSetTag is (0008,0005).
const specificCharacterSetTag = tag.Tag(0x00080005)

// utf8CharacterSet is the defined term for UTF-8, which every decoded data set
// declares.
const utf8CharacterSet = "ISO_IR 192"

// specificCharacterSetOf reads (0008,0005) from a flat element list, or nil when
// it is absent.
func specificCharacterSetOf(elements []*DataElementValue) []string {
	for _, elem := range elements {
		if elem.Tag != specificCharacterSetTag {
			continue
		}
		if strings.TrimSpace(string(elem.Value)) == "" {
			return nil
		}
		// DecodeBytes works in Go's encoding names; the file names them the way
		// DICOM does. Passing the DICOM name straight through finds no decoder,
		// and the failure is silent — the bytes come back unchanged, which reads
		// as "this text needed no decoding" rather than as an error.
		declared := parseSpecificCharacterSetValue(strings.TrimSpace(string(elem.Value)))
		converted, err := charset.ConvertEncodings(declared)
		if err != nil {
			return nil
		}
		return converted
	}
	return nil
}

// decodeTextValue converts a text value from its declared character set to
// UTF-8, leaving everything else alone.
//
// Without this the ordinary accessors hand back whatever bytes were in the file,
// so a name written in Greek, Hebrew, Japanese or plain accented Latin comes out
// as mojibake unless the caller knows to reach for the decoding API. That is
// most of the world, and it is not something a caller can be expected to
// discover from a wrong-looking string.
func decodeTextValue(vr dataelem.VR, value []byte, encodings []string) []byte {
	if len(value) == 0 || len(encodings) == 0 || !dataelem.IsTextVR(vr) {
		return value
	}

	delimiters := charset.DefaultTextDelimiters
	if vr == dataelem.PN {
		delimiters = charset.PersonNameDelimiters
	}

	decoded, err := charset.DecodeBytes(value, encodings, delimiters)
	if err != nil {
		// A value that will not decode is left as it was found. Dropping it
		// would lose data that is merely in an encoding this build does not
		// know, and the caller can still reach the bytes through the file.
		return value
	}
	return []byte(decoded)
}

// ReadDICOMFile reads an entire DICOM file.
func ReadDICOMFile(reader filebase.Reader) (*DICOMFile, error) {
	dfr := NewDCMFileReader(reader)

	dicomFile := &DICOMFile{
		DataElements: make([]*DataElementValue, 0),
	}
	// Whether the encoding has to be worked out from the data set itself.
	sniffed := false

	// A DICOM Part 10 file opens with a 128-byte preamble and the characters
	// DICM. A raw DICOM stream — as produced by some modalities, and what
	// travels on the network — has neither and begins directly with the data
	// set. Detect which this is rather than assuming.
	hasPreamble, err := dfr.detectPreamble()
	if err != nil {
		return nil, err
	}
	dicomFile.HasPreamble = hasPreamble

	var metaInfo *FileMetaInfo
	if hasPreamble {
		metaInfo, err = dfr.ReadFileMetaInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to read file meta information: %w", err)
		}
		dicomFile.FileMetaInfo = metaInfo
		dicomFile.MetaElements = dfr.metaElements
		dicomFile.Warnings = append(dicomFile.Warnings, dfr.metaWarnings...)
	} else {
		// Without a meta header there is no stated transfer syntax. Implicit VR
		// Little Endian is the DICOM default (PS3.5 Section 10.1), but the first
		// element usually says otherwise plainly enough to read, so look before
		// falling back to it.
		metaInfo = &FileMetaInfo{}
		dicomFile.FileMetaInfo = metaInfo
		sniffed = true
	}

	ts := metaInfo.TransferSyntaxUID
	dicomFile.ExplicitVR, dicomFile.IsLittleEndian = determineTransferSyntax(ts)

	if sniffed {
		explicit, little, ok := dfr.sniffDataSetHead()
		if ok {
			dicomFile.ExplicitVR, dicomFile.IsLittleEndian = explicit, little
		}
		dicomFile.Warnings = append(dicomFile.Warnings,
			fmt.Sprintf("no file meta header; parsing as a raw data set in %s",
				describeEncoding(dicomFile.ExplicitVR, dicomFile.IsLittleEndian)))
	}

	// Under the Deflated syntax the meta header is uncompressed but everything
	// after it is raw DEFLATE. Inflate the remainder and continue from that,
	// since the element parser reads a plain data set.
	if ts == DeflatedExplicitVRLittleEndianUID {
		// How many compressed bytes are left bounds how far they may expand.
		// A non-seekable source cannot answer, and inflateRemainder falls back
		// to the absolute ceiling when told a non-positive size.
		compressedSize := int64(-1)
		if size, sizeErr := dfr.streamSizeOnce(); sizeErr == nil {
			compressedSize = size - dfr.position
		}

		inflated, err := inflateRemainder(reader, compressedSize)
		if err != nil {
			return nil, fmt.Errorf("failed to inflate deflated data set: %w", err)
		}
		reader = filebase.NewFileReader(bytes.NewReader(inflated))
		dfr.reader = reader
		// Offsets now refer to the inflated data set, which begins at zero.
		dfr.position = 0
		dfr.streamSizeKnown = false
	}

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
			// An element declaring more bytes than the file holds means the file
			// is truncated. Everything read so far is kept and the incomplete
			// element is dropped, with a warning naming it.
			//
			// pydicom instead returns the element with whatever bytes were
			// present — 8130 of a declared 8192 for MR_truncated.dcm. That is a
			// deliberate difference rather than an oversight: a partially read
			// pixel buffer handed back as PixelData can be rendered as an image
			// without anything looking wrong, and for a value the caller cannot
			// tell is short, dropping it is safer than shortening it. Callers
			// wanting the partial bytes have the file and the offset from the
			// warning.
			if strings.Contains(errMsg, "claimed") && strings.Contains(errMsg, "bytes") ||
				strings.Contains(errMsg, "exceeds") {
				config.Logger.Warn("filereader: file is truncated; dropping the incomplete element",
					"position", dfr.position, "err", err)
				dicomFile.Warnings = append(dicomFile.Warnings,
					fmt.Sprintf("file is truncated at position %d; the incomplete element was "+
						"dropped and the %d elements before it are intact: %v",
						dfr.position, len(dicomFile.DataElements), err))
				break
			}
			return nil, fmt.Errorf("failed to read data element: %w", err)
		}

		// Big endian values are converted once here so that everything
		// downstream can assume little endian; see normalizeByteOrder.
		if !dicomFile.IsLittleEndian {
			normalizeByteOrder(element)
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

// DeflatedExplicitVRLittleEndianUID is the transfer syntax whose data set is
// DEFLATE-compressed after the file meta header (PS3.5 Annex A.5).
const DeflatedExplicitVRLittleEndianUID = "1.2.840.10008.1.2.1.99"

// MaxInflatedDatasetSize bounds how far a deflated data set may expand.
//
// DEFLATE reaches ratios above 1000:1 on repetitive input, so a small file can
// otherwise expand without limit. 256 MiB is far above any legitimate DICOM
// data set while keeping a hostile one cheap to reject.
const MaxInflatedDatasetSize int64 = 256 << 20

// inflateRemainder reads everything left in the stream and inflates it,
// refusing input that expands past what a stream of compressedSize bytes is
// allowed to produce.
//
// Pass a non-positive compressedSize when the remaining length is unknown, as
// it is for a non-seekable source; MaxInflatedDatasetSize is then the only
// bound available.
func inflateRemainder(reader filebase.Reader, compressedSize int64) ([]byte, error) {
	zr := flate.NewReader(reader)
	defer zr.Close()

	// Scaling the limit to the compressed size is what keeps rejecting a bomb
	// cheap. Against the absolute ceiling alone, a 300 KB file allocated
	// 600 MiB before being refused — io.ReadAll grows by doubling, so the peak
	// is roughly twice the limit, and the attacker picks the input size.
	limit := compress.InflateLimitFor(compressedSize, MaxInflatedDatasetSize)

	// Read one byte past the limit: if it materializes, the input expands
	// beyond what is allowed and the rest is not worth decompressing.
	out, err := io.ReadAll(io.LimitReader(zr, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > limit {
		return nil, fmt.Errorf("data set of %d compressed bytes expands beyond the %d byte limit allowed for its size",
			compressedSize, limit)
	}
	return out, nil
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
		// Group lengths, (gggg,0000), are standard but not enumerated in the
		// dictionary. See tag.IsGroupLength.
		if elem.Tag.IsGroupLength() {
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

// normalizeByteOrder converts an element's value from big endian to the little
// endian representation the rest of the library assumes.
//
// Dataset stores values as opaque bytes with no record of how to interpret
// them, and everything downstream — pixel access, numeric conversion, the JSON
// model — reads them as little endian. Rather than thread the file's byte order
// through all of that, big endian values are normalised once here, so a data
// set means the same thing regardless of how the file was encoded.
func normalizeByteOrder(elem *DataElementValue) {
	// Swap in place: the value was allocated by this reader and is not shared.
	dataelem.SwapByteOrder(dataelem.VR(elem.VR), elem.Value)

	// Sequence items carry their own elements, encoded the same way.
	for _, item := range elem.Items {
		for _, nested := range item.Elements {
			normalizeByteOrder(nested)
		}
	}
}

// isPlausibleVR reports whether two bytes could be a Value Representation.
//
// Every VR is two uppercase ASCII letters (PS3.5 §6.2). Checking the shape
// rather than membership in the list of known VRs is deliberate: a private or
// future VR this build does not recognize is still a VR, and treating it as a
// length would corrupt the rest of the file. The check only has to separate
// "this is a VR" from "this is the low half of a length".
func isPlausibleVR(b []byte) bool {
	if len(b) != 2 {
		return false
	}
	return b[0] >= 'A' && b[0] <= 'Z' && b[1] >= 'A' && b[1] <= 'Z'
}

// readExplicitLength reads the value length of an explicit VR element.
//
// Short-form VRs carry a 2-byte length; the rest carry two reserved bytes and
// then a 4-byte one (PS3.5 §7.1.2). Both are read through the reader so its byte
// order applies — assembling them by hand hardcodes little endian and corrupts
// every length in a big endian file.
func (dfr *DCMFileReader) readExplicitLength(element *DataElementValue) error {
	if isShortVR(element.VR) {
		short, err := dfr.reader.ReadUint16()
		if err != nil {
			return fmt.Errorf("failed to read value length: %w", err)
		}
		element.Length = uint32(short)
		dfr.position += 2
		return nil
	}

	reserved := make([]byte, 2)
	if err := dfr.reader.ReadBytes(reserved); err != nil {
		return fmt.Errorf("failed to read reserved bytes: %w", err)
	}
	dfr.position += 2

	length, err := dfr.reader.ReadUint32()
	if err != nil {
		return fmt.Errorf("failed to read value length: %w", err)
	}
	element.Length = length
	dfr.position += 4
	return nil
}

// isEncapsulatedPixelDataTag reports whether a tag may carry encapsulated pixel
// data, which is the only legitimate use of undefined length outside a sequence.
//
// PixelData is the familiar one; the float variants were added for pixel data
// that cannot be represented as integers and use the same encapsulation.
func isEncapsulatedPixelDataTag(t tag.Tag) bool {
	switch t {
	case tag.New(0x7FE0, 0x0010), // PixelData
		tag.New(0x7FE0, 0x0008), // FloatPixelData
		tag.New(0x7FE0, 0x0009): // DoubleFloatPixelData
		return true
	default:
		return false
	}
}

// tryReadMetaGroupLength reads (0002,0000) if it is the next element.
//
// It reports whether the element was there. A file that omits it is
// non-conformant but readable, so the absence is not an error — the tag is put
// back and the caller finds the end of the header by watching the group instead.
func (dfr *DCMFileReader) tryReadMetaGroupLength() (uint32, bool, error) {
	start, err := dfr.reader.Seek(0, io.SeekCurrent)
	if err != nil {
		// Not seekable, so the tag cannot be put back: require the element,
		// which is what a conformant file has anyway.
		length, err := dfr.ReadFileMetaInformationGroupLength()
		return length, err == nil, err
	}

	tagValue, err := dfr.ReadTag()
	if err != nil {
		return 0, false, fmt.Errorf("failed to read the first meta element tag: %w", err)
	}

	if tagValue != tag.New(0x0002, 0x0000) {
		if _, seekErr := dfr.reader.Seek(start, io.SeekStart); seekErr != nil {
			return 0, false, seekErr
		}
		dfr.position = start
		config.Logger.Warn("filereader: file meta header has no group length element",
			"firstTag", tagValue.String())
		return 0, false, nil
	}

	// Rewind and let the dedicated reader consume the whole element, so its
	// VR and length checks still apply.
	if _, err := dfr.reader.Seek(start, io.SeekStart); err != nil {
		return 0, false, err
	}
	dfr.position = start

	length, err := dfr.ReadFileMetaInformationGroupLength()
	if err != nil {
		return 0, false, err
	}
	return length, true, nil
}

// sniffDataSetEncoding reports how a data set with no file meta header is
// encoded, by looking at its first element.
//
// PS3.5 Section 10.1 makes Implicit VR Little Endian the default, and assuming
// it is what this used to do. But the default applies to a stream that carries
// no other information, and the first element carries plenty: an explicit VR
// data set puts two ASCII letters where an implicit one puts the low half of a
// length. The two are distinguishable, and the cost of not distinguishing them
// is total — reading an explicit VR stream as implicit takes the VR characters
// as part of the length, which then runs past the end of the file, and every
// element is dropped. pydicom's corpus has two such files, and both produced an
// empty data set and no error.
//
// Endianness follows from the group number. A data set begins at group 0x0002
// or 0x0008, so of the two readings the correct one is the smaller: 0x0008 one
// way is 0x0800 the other. Implicit VR is always little endian — the standard
// defines no implicit big-endian syntax — so only the explicit case has a
// choice to make.
//
// Returns false if the head is too short to tell, leaving the default in place.
func sniffDataSetEncoding(head []byte) (explicitVR, littleEndian, ok bool) {
	if len(head) < 8 {
		return false, true, false
	}

	if !dataelem.IsValidVR(dataelem.VR(head[4:6])) {
		// No VR where an explicit data set would put one, so implicit — which
		// exists only in little endian.
		return false, true, true
	}

	groupLE := uint16(head[0]) | uint16(head[1])<<8
	groupBE := uint16(head[0])<<8 | uint16(head[1])
	return true, groupLE <= groupBE, true
}

// sniffDataSetHead peeks at the first element and reports how it is encoded,
// leaving the stream where it found it.
func (dfr *DCMFileReader) sniffDataSetHead() (explicitVR, littleEndian, ok bool) {
	start, err := dfr.reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, true, false // not seekable: cannot look without consuming
	}
	head := make([]byte, 8)
	readErr := dfr.reader.ReadBytes(head)
	if _, seekErr := dfr.reader.Seek(start, io.SeekStart); seekErr != nil {
		return false, true, false
	}
	dfr.position = start
	if readErr != nil {
		return false, true, false
	}
	return sniffDataSetEncoding(head)
}

// describeEncoding names an encoding for a warning message.
func describeEncoding(explicitVR, littleEndian bool) string {
	vr, order := "implicit VR", "little endian"
	if explicitVR {
		vr = "explicit VR"
	}
	if !littleEndian {
		order = "big endian"
	}
	return vr + " " + order
}
