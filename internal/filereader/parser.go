// Package filereader (internal) is the DICOM file parser: the preamble, the File Meta
// group and the data set, read into the elements as the file holds them. It
// builds no data set and decodes no pixels, so it imports nothing that does:
// filereader adds those on top, and filereader/attributes gives the parser
// alone to a program that reads attributes and must not link the codecs.
//
// The types here are filereader's and attributes', by alias: a DataElementValue
// from any of the three packages is the same type. The package is named
// filereader so that the types print as they always have: %T of a
// *filereader.DataElementValue is still "*filereader.DataElementValue", and
// an error or a log line that names one reads as before. Its import path,
// reflect.Type.PkgPath, is this internal one.
package filereader

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/amrshadid/go-dicom/charset"
	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/internal/inflate"
	"github.com/amrshadid/go-dicom/internal/vr"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// Reader reads a DICOM file element by element. filereader.DCMFileReader is
// built on it.
type Reader struct {
	reader       filebase.Reader
	fileMetaInfo *FileMetaInfo
	position     int64
	metaWarnings []string // Non-fatal issues found while reading the meta header

	// implicitElements counts the elements found written without a VR in a
	// data set that declares explicit VR, and firstImplicit is the first.
	// They become one warning: a file written that way throughout has
	// thousands of them.
	implicitElements int
	firstImplicit    string

	// orderViolations counts the elements, in a data set or in the items of
	// a sequence, whose tag is not greater than the one before it. The first
	// maxOrderWarnings are warned about each; a file out of order throughout
	// has thousands.
	orderViolations int

	// orderKept holds the first maxOrderKept of those violations, for
	// File.TagOrderViolations.
	orderKept []TagOrderViolation

	// insideUnknown is how many elements of VR UN and undefined length the
	// reader is inside. What such an element holds is implicit VR by rule
	// (PS3.5 6.2.2 Note 5), so an element without a VR there is as it
	// should be and is not counted.
	insideUnknown int

	// Total size of the underlying stream, measured once and reused to bound
	// every declared element length. streamSizeErr records a non-seekable
	// source so the measurement is not retried per element.
	streamSize      int64
	streamSizeKnown bool
	streamSizeErr   error

	// metaElements holds the group-0002 elements exactly as they appeared,
	// including any not represented by a FileMetaInfo field.
	metaElements []*DataElementValue

	// metaGroupLength is the (0002,0000) element, which metaElements leaves
	// out, or nil when the file has none.
	metaGroupLength *DataElementValue

	// options are those the file is being read with.
	options ReadOptions

	// ctx carries options.Logger (config.WithLogger) to everything the
	// read calls that logs. It is nil for a reader made by NewReader, which
	// logs to config.Logger.
	ctx context.Context
}

// NewReader returns a Reader over reader, positioned at its start.
func NewReader(reader filebase.Reader) *Reader {
	return &Reader{reader: reader}
}

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

// ReadOptions choose how ReadDICOMFileWithOptions reads a file.
type ReadOptions struct {
	// SkipBulkData leaves bulk values in the file instead of reading them
	// into memory: Pixel Data in every form, its fragments when it is
	// encapsulated, and any value of a binary VR (OB, OD, OF, OL, OV, OW or
	// UN) longer than BulkDataThreshold. A value whose VR nobody knows, in
	// an implicit VR file, may be text or may be bulk: it is read up to
	// UnknownVRBulkLength bytes, or BulkDataThreshold when that is more, and
	// left in the file beyond. Each such element has ValueSkipped set and a
	// nil Value, and keeps its Length and its Position, so the bytes can be
	// found again.
	//
	// It is for reading the structure of a file: what it holds and where.
	// Memory then follows the metadata and not the image. A data set built
	// from such a file with GetDataset has those values empty.
	SkipBulkData bool

	// BulkDataThreshold is the longest value of a binary VR still read
	// when SkipBulkData is set. Zero reads none of them. Pixel Data is
	// skipped whatever its length.
	BulkDataThreshold uint32

	// MaxValueLength is the longest value the read holds in memory. A
	// longer one ends the read with a *ValueTooLongError naming it, before
	// any of it is allocated, whether or not the size of the source is
	// known. Zero is no maximum, as before it existed.
	//
	// It bounds a value as it is held: an element's value, a File Meta
	// element's, and encapsulated Pixel Data as a whole, its items and
	// their headers counted. A sequence is not a value and is not bounded
	// by it; its items are read element by element. A value SkipBulkData
	// leaves in the file is not held, so it is not refused either: read
	// with both, Pixel Data of any length is passed over while a value read
	// into memory, text or a binary value under BulkDataThreshold, is held
	// to the maximum.
	MaxValueLength uint32

	// Logger is where the read logs, and everything it calls: the
	// character set decisions and the validation run as GetDataset builds
	// the data set, later, for the File it returns. Nothing the read
	// does then reaches config.Logger. Nil is config.Logger, as before.
	//
	// To read silently, give it a logger that discards:
	//
	//	filereader.ReadOptions{Logger: slog.New(slog.DiscardHandler)}
	//
	// What is logged names tags, VRs, lengths and offsets, never a value's
	// bytes or text: a file from an untrusted source writes what it likes
	// in its values. What a program should act on is in File.Warnings
	// as well; the log is for someone watching the read.
	Logger *slog.Logger
}

// logger is where this reader logs: its read's logger, or config.Logger.
func (dfr *Reader) logger() *slog.Logger {
	return config.LoggerFromContext(dfr.ctx)
}

// ErrValueTooLong is what a read refused by ReadOptions.MaxValueLength
// matches with errors.Is.
var ErrValueTooLong = errors.New("value is longer than the read allows")

// ValueTooLongError is a value refused because it states more bytes than
// ReadOptions.MaxValueLength. It says which element and where; nothing of
// the value itself is in it, since none of it was read.
type ValueTooLongError struct {
	// Tag is the element whose value it is.
	Tag tag.Tag
	// Length is the length the file states, or for encapsulated Pixel Data
	// the length it would have reached with the next item.
	Length uint64
	// Max is the maximum the read was given.
	Max uint32
	// Offset is where the element's tag begins.
	Offset int64
}

// Error names the element by its dictionary name and tag.
func (e *ValueTooLongError) Error() string {
	name := e.Tag.GetName()
	if name == "" {
		name = "the element"
	}
	return fmt.Sprintf("%s %s at offset %d states a value of %d bytes, more than the maximum of %d this read holds",
		name, e.Tag.String(), e.Offset, e.Length, e.Max)
}

// Is makes the error match ErrValueTooLong.
func (e *ValueTooLongError) Is(target error) bool { return target == ErrValueTooLong }

// ReadPreamble reads the 128-byte DICOM preamble.
func (dfr *Reader) ReadPreamble() error {
	preamble := make([]byte, 128)
	if err := dfr.reader.ReadBytes(preamble); err != nil {
		return fmt.Errorf("failed to read preamble: %w", err)
	}

	dfr.position += 128
	return nil
}

// ReadDICMPrefix reads and validates the "DICM" magic string.
func (dfr *Reader) ReadDICMPrefix() error {
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
func (dfr *Reader) detectPreamble() (bool, error) {
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
func (dfr *Reader) ReadFileMetaInformationGroupLength() (uint32, error) {
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
func (dfr *Reader) ReadFileMetaInfo() (*FileMetaInfo, error) {
	metaInfo := &FileMetaInfo{}

	// (0002,0000) File Meta Information Group Length is the usual way to know
	// where the meta header ends, but it is not always written. A file without
	// it was rejected outright, though its meta header is perfectly readable —
	// the group itself marks its own end, since every element in it is in group
	// 0002 and the data set that follows is not.
	groupLengthStart := dfr.position
	groupLength, haveGroupLength, err := dfr.tryReadMetaGroupLength()
	if err != nil {
		return nil, err
	}
	if haveGroupLength {
		metaInfo.FileMetaInformationGroupLength = groupLength
		// Tag, "UL", a 2-byte length of 4, and the value.
		value := make([]byte, 4)
		binary.LittleEndian.PutUint32(value, groupLength)
		dfr.metaGroupLength = &DataElementValue{
			Tag: tag.New(0x0002, 0x0000), VR: "UL", Value: value, Length: 4,
			Position: ElementPosition{
				Offset: groupLengthStart, ExplicitVR: true,
				LengthOffset: groupLengthStart + 6, LengthSize: 2,
				ValueOffset: groupLengthStart + 8, End: dfr.position,
			},
		}
	}

	startPosition := dfr.position

	// A stated group length bounds the header. Without one the loop relies on the
	// group check inside it, which is also a useful guard when a stated length is
	// wrong.
	for !haveGroupLength || dfr.position-startPosition < int64(groupLength) {
		elementStart := dfr.position
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

		if err := dfr.checkMaxValueLength(tagValue, uint64(valueLength), elementStart); err != nil {
			return nil, err
		}
		// The meta group's lengths were allocated with no check at all, so a
		// 200-byte file stating a 1 GiB element here cost 1 GiB even from a
		// file whose size was known (#458). checkValueLength holds it to the
		// bytes there are, and readValue to the bytes that arrive.
		// Refused, the value is passed over to the end of the file, as reading
		// it did: what follows the header is inside the value and is not a
		// data set to be read.
		var value []byte
		err = dfr.checkValueLength(valueLength)
		if err == nil {
			value, err = dfr.readValue(valueLength)
		} else if _, seekErr := dfr.reader.Seek(0, io.SeekEnd); seekErr != nil {
			return nil, fmt.Errorf("failed to pass over a truncated file meta element: %w", seekErr)
		}
		if err != nil {
			// Handle read error gracefully - file meta info might be incomplete/corrupted.
			// Log a warning but continue with what we have so far; the rest of the
			// file may still be readable.
			dfr.logger().Warn("filereader: skipping file meta element",
				"tag", tagValue.String(), "expectedBytes", valueLength, "err", err)
			dfr.metaWarnings = append(dfr.metaWarnings,
				fmt.Sprintf("skipped file meta element %s (expected %d bytes): %v",
					tagValue.String(), valueLength, err))
			return metaInfo, nil
		}
		valueStart := dfr.position - int64(valueLength)

		lengthSize := 4
		if isShortVR(vr) {
			lengthSize = 2
		}
		dfr.metaElements = append(dfr.metaElements, &DataElementValue{
			Tag: tagValue, VR: vr, Value: value, Length: valueLength,
			Position: ElementPosition{
				Offset: elementStart, ExplicitVR: true,
				LengthOffset: valueStart - int64(lengthSize), LengthSize: lengthSize,
				ValueOffset: valueStart, End: dfr.position,
			},
		})

		if err := dfr.storeMetaValue(metaInfo, tagValue, vr, value); err != nil {
			return nil, err
		}
	}

	dfr.fileMetaInfo = metaInfo
	return metaInfo, nil
}

// ReadTag reads a DICOM tag (4 bytes: group + element).
func (dfr *Reader) ReadTag() (tag.Tag, error) {
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

	// Position is where the element was read from. It is the zero value for
	// an element that was not read from a file.
	Position ElementPosition

	// ValueSkipped reports that the value was left unread, because the file
	// was read with ReadOptions.SkipBulkData. Value is then nil, Length is
	// still the length the element states, and Position says where the bytes
	// are.
	ValueSkipped bool
}

// ElementPosition is where a data element sits in the bytes it was read
// from, so that a decoded attribute can be tied back to them.
//
// Offsets count from the first byte of the file. Under the Deflated transfer
// syntax the data set is read after inflating it, and the offsets of its
// elements count from the first byte of the inflated data set instead; see
// File.Deflated.
type ElementPosition struct {
	// Offset is the first byte of the tag.
	Offset int64
	// ExplicitVR reports whether a VR was read from the file. When it was,
	// it is the two bytes after the tag. It is false for an element written
	// implicitly, including one inside a data set that declares explicit VR.
	ExplicitVR bool
	// LengthOffset is the first byte of the length field, and LengthSize
	// its width: 2 for a short explicit VR, otherwise 4.
	LengthOffset int64
	LengthSize   int
	// ValueOffset is the first byte after the length field: the value, or
	// the first item of a sequence or of encapsulated pixel data.
	ValueOffset int64
	// End is the first byte after the element, the Sequence Delimitation
	// Item of an undefined-length element included.
	End int64
	// DelimiterOffset is the first byte of the Sequence Delimitation Item
	// (FFFE,E0DD) that closes an undefined-length element, 8 bytes long. It
	// is 0 when there is none.
	DelimiterOffset int64
	// Fragments are the items of encapsulated pixel data, the Basic Offset
	// Table first. It is nil for anything else.
	Fragments []FragmentPosition
}

// FragmentPosition is one item of encapsulated pixel data: an (FFFE,E000)
// tag, a 4-byte length, and that many bytes.
type FragmentPosition struct {
	// Offset is the first byte of the item's tag, ValueOffset the first
	// byte of its content, and End the first byte after it.
	Offset, ValueOffset, End int64
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

	// DeclaredLength is the length the item's header states, UndefinedLength
	// when the item is closed by a delimiter instead.
	DeclaredLength uint32
	// End is the first byte after the item, its Item Delimitation Item
	// included.
	End int64
	// DelimiterOffset is the first byte of the Item Delimitation Item
	// (FFFE,E00D) that closes the item, 8 bytes long, or 0 when it has none.
	DelimiterOffset int64
}

// MaxSequenceDepth bounds how deeply nested sequences may be parsed. Real
// DICOM objects nest a handful of levels at most; the limit stops a crafted or
// corrupt file from driving unbounded recursion.
const MaxSequenceDepth = 64

// ReadDataElement reads a data element, including any nested sequence content.
func (dfr *Reader) ReadDataElement(explicitVR bool) (*DataElementValue, error) {
	return dfr.readDataElement(explicitVR, 0)
}

// readDataElement reads one data element at the given sequence nesting depth.
func (dfr *Reader) readDataElement(explicitVR bool, depth int) (element *DataElementValue, err error) {
	start := dfr.position
	tagValue, err := dfr.ReadTag()
	if err != nil {
		return nil, err
	}

	element = &DataElementValue{
		Tag: tagValue,
	}
	element.Position.Offset = start
	// Wherever the element turns out to end, that is where the reader is.
	defer func() {
		if err == nil {
			element.Position.End = dfr.position
		}
	}()

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
		element.Position.LengthOffset, element.Position.LengthSize = start+4, 4
		element.Position.ValueOffset = dfr.position
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

			// Counted for the file's warnings, which is where a program
			// learns of it. The log has each one, for someone reading a
			// file element by element.
			if dfr.insideUnknown == 0 {
				if dfr.implicitElements == 0 {
					dfr.firstImplicit = element.Tag.String()
				}
				dfr.implicitElements++
			}
			dfr.logger().Debug("filereader: element is implicitly encoded in an explicit VR data set",
				"tag", element.Tag.String(), "vr", element.VR)

			// An element encoded this way sits in a data set the writer was
			// treating as implicit, so anything nested inside it is implicit
			// too. Parsing its items as explicit would fail the same way one
			// level down.
			elementIsImplicit = true
		} else {
			element.VR = string(vrBytes)
			element.Position.ExplicitVR = true

			if err := dfr.readExplicitLength(element); err != nil {
				return nil, err
			}
			// Tag, VR and a 2-byte length, or two reserved bytes and a
			// 4-byte one.
			if dfr.position-start == 8 {
				element.Position.LengthSize = 2
			}

			// An intermediary that does not know a tag re-encodes it as UN.
			// PS3.5 6.2.2 Note 2 lets a receiver that does know it read the
			// value as Implicit VR Little Endian, whatever the transfer syntax,
			// and pydicom does so by default. Without this the typed accessors
			// refuse the value and a UN-encoded sequence is never parsed:
			// rtdose_rle.dcm has 35 such elements and its Referenced RT Plan
			// Sequence stayed an opaque blob.
			// Defined length only: an undefined length means items whatever the
			// dictionary says (Note 5), and the branch below reads them.
			if known := dictionaryVRForUN(element.VR, element.Tag, dfr.reader.GetByteOrder()); known != "" &&
				element.Length != UndefinedLength {
				element.VR = known
				// Note 2 again: the value is implicit VR little endian, so
				// anything nested inside it is too.
				elementIsImplicit = true
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
	if element.Position.LengthSize == 0 {
		element.Position.LengthSize = 4
	}
	element.Position.ValueOffset = dfr.position
	element.Position.LengthOffset = dfr.position - int64(element.Position.LengthSize)

	// Sequences are parsed into items rather than read as an opaque value.
	if IsSequenceVR(element.VR) {
		if depth >= MaxSequenceDepth {
			return nil, fmt.Errorf("sequence nesting exceeds maximum depth %d at tag %s",
				MaxSequenceDepth, element.Tag.String())
		}
		items, delimiter, err := dfr.readSequenceItems(explicitVR && !elementIsImplicit, depth+1, element.Length)
		if err != nil {
			return nil, fmt.Errorf("failed to read sequence %s: %w", element.Tag.String(), err)
		}
		element.Items = items
		element.Position.DelimiterOffset = delimiter
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
	if element.UndefinedLength && !element.Tag.IsPixelData() {
		if depth >= MaxSequenceDepth {
			return nil, fmt.Errorf("sequence nesting exceeds maximum depth %d at tag %s",
				MaxSequenceDepth, element.Tag.String())
		}
		if element.VR == "UN" {
			dfr.insideUnknown++
		}
		items, delimiter, err := dfr.readSequenceItems(explicitVR && !elementIsImplicit, depth+1, element.Length)
		if element.VR == "UN" {
			dfr.insideUnknown--
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read undefined-length sequence %s: %w",
				element.Tag.String(), err)
		}
		element.VR = "SQ"
		element.Items = items
		element.Position.DelimiterOffset = delimiter
		return element, nil
	}

	// An undefined length on pixel data means encapsulated (fragmented) pixel
	// data: a run of items terminated by a Sequence Delimitation Item.
	if element.UndefinedLength {
		element.ValueSkipped = dfr.options.SkipBulkData
		value, fragments, delimiter, err := dfr.readEncapsulatedValue(element.Tag, start)
		if err != nil {
			return nil, fmt.Errorf("failed to read encapsulated value for %s: %w",
				element.Tag.String(), err)
		}
		element.Value = value
		element.Position.Fragments = fragments
		element.Position.DelimiterOffset = delimiter
		return element, nil
	}

	if element.Length > 0 {
		bulk := dfr.isBulk(element)
		// The caller's maximum comes before the check against the stream: a
		// value too long to hold is refused as that, not dropped as a
		// truncation, whatever is left of the file. A value left in the file
		// is not held, so it is not refused.
		if !bulk {
			if err := dfr.checkMaxValueLength(element.Tag, uint64(element.Length), start); err != nil {
				return nil, err
			}
		}
		// Guard the allocation against a corrupt or hostile length field before
		// committing memory to it.
		if err := dfr.checkValueLength(element.Length); err != nil {
			return nil, fmt.Errorf("invalid length for tag %s: %w", element.Tag.String(), err)
		}
		if bulk {
			if err := dfr.skip(element.Length); err != nil {
				return nil, fmt.Errorf("failed to pass over the value of tag %s (claimed %d bytes): %w",
					element.Tag.String(), element.Length, err)
			}
			element.ValueSkipped = true
			return element, nil
		}
		value, err := dfr.readValue(element.Length)
		if err != nil {
			return nil, fmt.Errorf("failed to read data element value for tag %s (claimed %d bytes): %w",
				element.Tag.String(), element.Length, err)
		}
		element.Value = value
	}

	return element, nil
}

// UnknownVRBulkLength is how long a value of no known VR may be and still
// be read when bulk data is skipped. Such a value is often a line of text.
const UnknownVRBulkLength = 1024

// binaryVRs are the VRs whose values are bytes, words and floats in bulk,
// never text or a few numbers.
var binaryVRs = map[string]bool{"OB": true, "OD": true, "OF": true, "OL": true, "OV": true, "OW": true, "UN": true}

// isBulk reports whether an element's value is left unread under the options
// the file is being read with.
func (dfr *Reader) isBulk(element *DataElementValue) bool {
	if !dfr.options.SkipBulkData {
		return false
	}
	if element.Tag.IsPixelData() {
		return true
	}
	if element.VR == "" {
		return element.Length > max(dfr.options.BulkDataThreshold, UnknownVRBulkLength)
	}
	if element.Length <= dfr.options.BulkDataThreshold {
		return false
	}
	// An implicit VR file has the dictionary's VR, which may offer a choice,
	// "OB or OW". It is bulk when every choice is binary: "US or OW" may
	// yet be a short list of numbers.
	for _, vr := range vr.Choices(element.VR) {
		if !binaryVRs[vr] {
			return false
		}
	}
	return true
}

// skip passes over n bytes of the stream without keeping them: by seeking
// where the stream can, by reading and discarding where it cannot.
func (dfr *Reader) skip(n uint32) error {
	if n == 0 {
		return nil
	}
	if _, err := dfr.reader.Seek(int64(n), io.SeekCurrent); err == nil {
		dfr.position += int64(n)
		return nil
	}
	scratch := make([]byte, 64<<10)
	for left := int64(n); left > 0; {
		chunk := scratch
		if left < int64(len(chunk)) {
			chunk = chunk[:left]
		}
		if err := dfr.reader.ReadBytes(chunk); err != nil {
			return err
		}
		left -= int64(len(chunk))
		dfr.position += int64(len(chunk))
	}
	return nil
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

func (dfr *Reader) checkValueLength(length uint32) error {
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

// checkMaxValueLength refuses a value longer than ReadOptions.MaxValueLength,
// before anything is allocated for it. offset is where the element's tag
// begins.
func (dfr *Reader) checkMaxValueLength(t tag.Tag, length uint64, offset int64) error {
	limit := dfr.options.MaxValueLength
	if limit == 0 || length <= uint64(limit) {
		return nil
	}
	return &ValueTooLongError{Tag: t, Length: length, Max: limit, Offset: offset}
}

// valuePieceSize is the first piece a value of unknown bound is read into;
// see readValue.
const valuePieceSize = 64 << 10

// readValue reads a value of n bytes and advances the position past it.
//
// When the size of the source is known, checkValueLength has already held n
// to the bytes that are there, and the value is one allocation. When it is
// not, as for a socket or an upload body, n is the file's word alone: it was
// allocated whole, so a stream stating 1 GiB and sending 16 bytes cost 1 GiB
// (#458). It is read in pieces instead, the first of 64 KiB and each after
// doubling what has arrived, so what is held is never more than twice what
// the source has sent, plus the first piece. network's readPDUBody does the
// same for a PDU.
func (dfr *Reader) readValue(n uint32) ([]byte, error) {
	if _, err := dfr.streamSizeOnce(); err == nil || n <= valuePieceSize {
		value := make([]byte, n)
		if err := dfr.reader.ReadBytes(value); err != nil {
			return nil, err
		}
		dfr.position += int64(n)
		return value, nil
	}
	// The position moves only once the whole value is in, as it does for one
	// read: a value cut short leaves it at the value's start, which is where
	// the warning about the truncation says the file ends.
	size := int(n)
	value := make([]byte, valuePieceSize)
	if err := dfr.reader.ReadBytes(value); err != nil {
		return nil, err
	}
	for len(value) < size {
		grown := make([]byte, min(size, 2*len(value)))
		copy(grown, value)
		if err := dfr.reader.ReadBytes(grown[len(value):]); err != nil {
			return nil, fmt.Errorf("after %d bytes: %w", len(value), err)
		}
		value = grown
	}
	dfr.position += int64(n)
	return value, nil
}

// maxOrderWarnings is how many elements out of tag order a read warns about
// one by one.
const maxOrderWarnings = 10

// maxOrderKept is how many violations File.TagOrderViolations holds; the
// count beyond them is in File.TagOrderViolationCount. A file out of order
// throughout would otherwise cost memory in proportion to its elements.
const maxOrderKept = 1000

// TagOrderViolation is a data element whose tag is not greater than the tag
// of the element before it in the same data set, which PS3.5 7.1 does not
// allow. The element is kept as read; this records where the order broke.
type TagOrderViolation struct {
	// Tag is the element's tag and Previous the tag of the element before it.
	Tag, Previous tag.Tag
	// Offset is where the element begins, in the file, or in the inflated
	// data set of a deflated file.
	Offset int64
	// InItem reports that the two are in a sequence item, not the top-level
	// data set.
	InItem bool
	// Repeated reports that Tag equals Previous: the same tag twice. Otherwise
	// Tag is the smaller.
	Repeated bool
}

// tagOrder follows the tags of one data set, a top-level one or an item of a
// sequence, as they are read.
type tagOrder struct {
	last uint32
	any  bool
}

// checkTagOrder warns when an element's tag is not greater than the tag of
// the element before it in the same data set. PS3.5 7.1 puts the data
// elements of a data set in increasing tag order, each tag once. The element
// is kept as read: the warning is the only effect, as a file written this way
// is common enough and its elements are all there.
func (dfr *Reader) checkTagOrder(o *tagOrder, t tag.Tag, offset int64, inItem bool) {
	defer func() { o.last, o.any = uint32(t), true }()
	if !o.any || uint32(t) > o.last {
		return
	}
	dfr.orderViolations++
	before := tag.Tag(o.last)
	if len(dfr.orderKept) < maxOrderKept {
		dfr.orderKept = append(dfr.orderKept, TagOrderViolation{
			Tag: t, Previous: before, Offset: offset, InItem: inItem, Repeated: t == before,
		})
	}
	if dfr.orderViolations > maxOrderWarnings {
		return
	}
	where := "of the data set"
	if inItem {
		where = "of a sequence item"
	}
	if t == before {
		dfr.warn("data element %s at offset %d repeats %s, the tag of the element before it; "+
			"PS3.5 7.1 puts the data elements %s in increasing tag order, each once; both are kept",
			t, offset, before, where)
		return
	}
	dfr.warn("data element %s at offset %d follows %s, a greater tag; "+
		"PS3.5 7.1 puts the data elements %s in increasing tag order; kept as read",
		t, offset, before, where)
}

// orderSummary says how many elements beyond the ones warned about were out
// of tag order, once the read is done.
func (dfr *Reader) orderSummary() {
	if dfr.orderViolations > maxOrderWarnings {
		dfr.warn("%d more data elements are out of increasing tag order (PS3.5 7.1); "+
			"%d in all", dfr.orderViolations-maxOrderWarnings, dfr.orderViolations)
	}
}

// warn records a non-fatal problem found while reading the data set.
func (dfr *Reader) warn(format string, args ...any) {
	dfr.metaWarnings = append(dfr.metaWarnings, fmt.Sprintf(format, args...))
}

// streamSizeOnce returns the total size of the underlying stream, measuring it
// on first use and caching the result. Measuring costs three seeks, which is
// why it is not repeated per element.
func (dfr *Reader) streamSizeOnce() (int64, error) {
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
//
// It also returns where the Sequence Delimitation Item began, or 0 when the
// sequence ended without one.
func (dfr *Reader) readSequenceItems(explicitVR bool, depth int, declaredLength uint32) ([]*SequenceItemValue, int64, error) {
	var items []*SequenceItemValue

	undefined := declaredLength == UndefinedLength
	if !undefined {
		if err := dfr.checkValueLength(declaredLength); err != nil {
			return nil, 0, err
		}
	}
	start := dfr.position

	for {
		if !undefined && dfr.position-start >= int64(declaredLength) {
			return items, 0, nil
		}

		marker, err := dfr.readItemHeader()
		if err != nil {
			if err == io.EOF {
				return items, 0, nil
			}
			return nil, 0, err
		}

		switch marker.tag {
		case tag.SequenceDelimiterTag:
			// End of an undefined-length sequence.
			return items, dfr.position - 8, nil
		case tag.ItemTag:
			// Where the item tag started, which is 8 bytes back: 4 for the tag
			// and 4 for the length that readItemHeader has already consumed.
			itemStart := dfr.position - 8

			// An item whose declared body runs past the end of its sequence is
			// still an item: its elements are all there, and only the length
			// field is wrong. Dropping it loses real data — pydicom's
			// DICOMDIR-nooffset declares 248 bytes for its last directory
			// record with 224 left, and the record it holds is complete, so the
			// file came back with 51 of its 52 records and an IMAGE missing
			// (#122).
			//
			// The item is read to the end of the sequence instead. A header
			// sitting exactly at that end has nothing to read and is not an
			// item at all: recording one would invent a record the file does
			// not contain.
			length := marker.length
			if !undefined && length != UndefinedLength {
				remaining := int64(declaredLength) - (dfr.position - start)
				if remaining <= 0 {
					dfr.warn("sequence item header at the end of the sequence, "+
						"declaring %d bytes with none left; ending the sequence", length)
					return items, 0, nil
				}
				if int64(length) > remaining {
					dfr.warn("sequence item declares %d bytes with %d left in the sequence; "+
						"keeping the complete elements it holds", length, remaining)
					length = uint32(remaining)
				}
			}

			item, err := dfr.readSequenceItem(explicitVR, depth, length)
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
					return items, 0, nil
				}
				return nil, 0, err
			}
			item.Offset = itemStart
			item.DeclaredLength = marker.length
			item.End = dfr.position
			items = append(items, item)
		default:
			return nil, 0, fmt.Errorf("unexpected tag %s inside sequence (expected item or delimiter)",
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
func (dfr *Reader) readItemHeader() (*itemHeader, error) {
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
func (dfr *Reader) readSequenceItem(explicitVR bool, depth int, declaredLength uint32) (*SequenceItemValue, error) {
	item := &SequenceItemValue{}

	undefined := declaredLength == UndefinedLength
	if !undefined {
		if err := dfr.checkValueLength(declaredLength); err != nil {
			return nil, err
		}
	}
	start := dfr.position

	var order tagOrder
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
			item.DelimiterOffset = elem.Position.Offset
			return item, nil
		}
		// A Sequence Delimitation Item here means the enclosing sequence ended
		// without an explicit item delimiter; treat the item as complete.
		if elem.Tag == tag.SequenceDelimiterTag {
			return item, nil
		}

		dfr.checkTagOrder(&order, elem.Tag, elem.Position.Offset, true)
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
func (dfr *Reader) readEncapsulatedValue(t tag.Tag, start int64) ([]byte, []FragmentPosition, int64, error) {
	var raw bytes.Buffer
	var fragments []FragmentPosition

	for {
		marker, err := dfr.readItemHeader()
		if err != nil {
			if err == io.EOF {
				return raw.Bytes(), fragments, 0, nil
			}
			return nil, nil, 0, err
		}

		if marker.tag == tag.SequenceDelimiterTag {
			// The delimiter closes the encapsulation and is not part of it.
			return raw.Bytes(), fragments, dfr.position - 8, nil
		}
		if marker.tag != tag.ItemTag {
			return nil, nil, 0, fmt.Errorf("unexpected tag %s in encapsulated data (expected item or delimiter)",
				marker.tag.String())
		}
		if marker.length == UndefinedLength {
			return nil, nil, 0, fmt.Errorf("encapsulated data item has undefined length")
		}

		// The value held is the whole encapsulation, each item's header
		// included, so that is what the maximum bounds; skipped, nothing of
		// it is held.
		if !dfr.options.SkipBulkData {
			if err := dfr.checkMaxValueLength(t, uint64(raw.Len())+8+uint64(marker.length), start); err != nil {
				return nil, nil, 0, err
			}
		}
		if marker.length > 0 {
			if err := dfr.checkValueLength(marker.length); err != nil {
				return nil, nil, 0, err
			}
		}
		// Left in the file: where each fragment is, and nothing of it.
		if dfr.options.SkipBulkData {
			if err := dfr.skip(marker.length); err != nil {
				return nil, nil, 0, fmt.Errorf("failed to pass over an encapsulated fragment (%d bytes): %w",
					marker.length, err)
			}
			fragments = append(fragments, FragmentPosition{
				Offset:      dfr.position - int64(marker.length) - 8,
				ValueOffset: dfr.position - int64(marker.length),
				End:         dfr.position,
			})
			continue
		}

		writeItemHeader(&raw, dfr.reader.GetByteOrder(), marker)

		if marker.length > 0 {
			fragment, err := dfr.readValue(marker.length)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("failed to read encapsulated fragment (%d bytes): %w",
					marker.length, err)
			}
			raw.Write(fragment)
		}
		fragments = append(fragments, FragmentPosition{
			Offset:      dfr.position - int64(marker.length) - 8,
			ValueOffset: dfr.position - int64(marker.length),
			End:         dfr.position,
		})
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
func IsSequenceVR(vr string) bool {
	return vr == "SQ"
}

// GetPosition returns the current position in the file.
func (dfr *Reader) GetPosition() int64 {
	return dfr.position
}

// GetFileMetaInfo returns the file meta information.
func (dfr *Reader) GetFileMetaInfo() *FileMetaInfo {
	return dfr.fileMetaInfo
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
func (dfr *Reader) storeMetaValue(metaInfo *FileMetaInfo, t tag.Tag, vr string, value []byte) error {
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

// SpecificCharacterSetTag is (0008,0005).
const SpecificCharacterSetTag = tag.Tag(0x00080005)

// TextEncodings returns the character encodings in force for the text of a
// data set: those its Specific Character Set (0008,0005) declares, or, when
// it has none, inherited, which is what the data set it is an item of uses
// (PS3.3 C.12.1.1.2). Pass nil for the top of a file. The result is what
// DecodeText takes.
//
// It is for a program that shows the elements of a File as they are in
// the file. GetDataset applies the same rules and returns text already
// decoded.
func TextEncodings(elements []*DataElementValue, inherited []string) []string {
	encodings, _ := EncodingsFor(context.Background(), elements, inherited)
	return encodings
}

// DecodeText returns a value of the given VR as UTF-8 text, decoded under
// encodings as TextEncodings returns them. Only the VRs a character set
// applies to are decoded with it; a UI or a DA is in the default repertoire
// whatever the data set declares. A value that does not decode is returned
// as it is.
func DecodeText(vrName string, value []byte, encodings []string) string {
	return string(DecodeTextValue(context.Background(), vrName, value, encodings))
}

// EncodingsFor settles the character set for a data set, and reports whether it
// is a fallback rather than what the file asked for.
//
// PS3.5 6.1.2.3: an absent or empty Specific Character Set means the default
// repertoire, which is ASCII. Files holding Latin-1 names under no declaration
// are common from older equipment, and pydicom decodes them with iso8859 and
// warns. Doing the same rescues the names; leaving the bytes alone and then
// declaring ISO_IR 192 over them, which is what happened before, destroyed them
// on the next write.
func EncodingsFor(ctx context.Context, elements []*DataElementValue, inherited []string) (encodings []string, fellBack bool) {
	for _, elem := range elements {
		if elem.Tag != SpecificCharacterSetTag {
			continue
		}
		declared := ParseSpecificCharacterSet(strings.TrimSpace(string(elem.Value)))
		if len(declared) == 0 {
			return []string{charset.DefaultEncoding}, true
		}
		converted, err := charset.ConvertEncodingsWithContext(ctx, declared)
		if err != nil || len(converted) == 0 {
			return []string{charset.DefaultEncoding}, true
		}
		return converted, isFallbackEncoding(declared, converted)
	}
	if inherited != nil {
		return inherited, false
	}
	return []string{charset.DefaultEncoding}, true
}

// isFallbackEncoding reports whether the conversion gave the default because it
// did not recognize what was declared, rather than because Latin-1 was asked
// for. ISO_IR 999 decodes correctly today through this path; the caller is
// still entitled to know a guess was made.
func isFallbackEncoding(declared, converted []string) bool {
	if len(converted) != 1 || converted[0] != charset.DefaultEncoding {
		return false
	}
	for _, term := range declared {
		switch strings.TrimSpace(term) {
		case "", "ISO_IR 6", "ISO 2022 IR 6", "ISO_IR 100", "ISO 2022 IR 100":
			return false
		}
	}
	return true
}

// DecodeTextValue converts a text value from its declared character set to
// UTF-8, leaving everything else alone.
//
// Without this the ordinary accessors hand back whatever bytes were in the file,
// so a name written in Greek, Hebrew, Japanese or plain accented Latin comes out
// as mojibake unless the caller knows to reach for the decoding API. That is
// most of the world, and it is not something a caller can be expected to
// discover from a wrong-looking string.
func DecodeTextValue(ctx context.Context, vrName string, value []byte, encodings []string) []byte {
	if len(value) == 0 || len(encodings) == 0 || !vr.IsText(vrName) {
		return value
	}

	delimiters := charset.DefaultTextDelimiters
	if vrName == "PN" {
		delimiters = charset.PersonNameDelimiters
	}
	// The declaration covers only some text VRs. A CS, UI or DA is in the
	// default repertoire whatever it says, so decoding one with the declared
	// set read the bytes E9 58 as "щX" under ISO_IR 144, where pydicom reads
	// "éX" (#198).
	if !vr.IsCharacterSet(vrName) {
		encodings = []string{charset.DefaultEncoding}
	}

	decoded, err := charset.DecodeBytesWithContext(ctx, value, encodings, delimiters)
	if err != nil {
		// A value that will not decode is left as it was found. Dropping it
		// would lose data that is merely in an encoding this build does not
		// know, and the caller can still reach the bytes through the file.
		return value
	}
	return []byte(decoded)
}

// Read reads a whole file, as options say. It logs to options.Logger, or
// when that is nil to the logger ctx carries (config.WithLogger). filereader.ReadDICOMFileWithOptions and
// attributes.Read are this.
func Read(ctx context.Context, reader filebase.Reader, options ReadOptions) (*File, error) {
	dfr := NewReader(reader)
	dfr.options = options
	// The logger travels in a context rather than through a global, so two
	// reads at once each log where they were told to (config.WithLogger).
	dfr.ctx = config.WithLogger(ctx, options.Logger)

	dicomFile := &File{
		DataElements: make([]*DataElementValue, 0),
	}
	// Whether the encoding has to be worked out from the data set itself.
	sniffed := false
	// Bits Allocated as it is read, for the Pixel Data byte-order swap below.
	bitsAllocated := 0
	// How many warnings belonged to the meta header. Warnings recorded while the
	// data set is parsed are appended after it, and were never copied to the
	// file at all: the copy below happens before the data set is read, so a
	// truncated or overrunning sequence warned into a slice nobody looked at.
	metaWarningCount := 0

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
		dicomFile.MetaGroupLength = dfr.metaGroupLength
		dicomFile.Warnings = append(dicomFile.Warnings, dfr.metaWarnings...)
		metaWarningCount = len(dfr.metaWarnings)
	} else {
		// Without a meta header there is no stated transfer syntax. Implicit VR
		// Little Endian is the DICOM default (PS3.5 Section 10.1), but the first
		// element usually says otherwise plainly enough to read, so look before
		// falling back to it.
		metaInfo = &FileMetaInfo{}
		dicomFile.FileMetaInfo = metaInfo
		sniffed = true
	}

	dicomFile.DataSetOffset = dfr.position

	ts := metaInfo.TransferSyntaxUID
	dicomFile.ExplicitVR, dicomFile.IsLittleEndian = determineTransferSyntax(ts)

	// Why the encoding is not known from the file meta group, when it is not.
	// Transfer Syntax UID (0002,0010) is Type 1, and files are written
	// without it, or with a UID that is not a transfer syntax at all. Either
	// way the group says nothing of how the data set is encoded, and a
	// reader that assumed implicit VR lost every element of one that was
	// explicit (#273). The data set's first element says, as it does for a
	// file with no meta group, and the reader says that it looked.
	//
	// unstated is what is said, up to the encoding the data set is read as.
	unstated := ""
	switch {
	case sniffed:
		unstated = "no file meta header; parsing as a raw data set in"
	case ts == "":
		unstated = "the file meta has no Transfer Syntax UID (0002,0010); reading the data set as"
	case !isStandardTransferSyntax(ts):
		unstated = "Transfer Syntax UID " + ts + " is not one the standard registers; reading the data set as"
	}
	if unstated != "" {
		explicit, little, looked := dfr.sniffDataSetHead()
		if looked {
			dicomFile.ExplicitVR, dicomFile.IsLittleEndian = explicit, little
		}
		warning := unstated + " " + describeEncoding(dicomFile.ExplicitVR, dicomFile.IsLittleEndian)
		// A file with a meta group was expected to say. Whether the
		// encoding was read off the data set or assumed is worth knowing.
		switch {
		case sniffed:
		case looked:
			warning += ", judged from its first element"
		default:
			warning += ", the default"
		}
		dicomFile.Warnings = append(dicomFile.Warnings, warning)
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

		if dfr.options.SkipBulkData {
			// A read that leaves the bulk values where they are is a read that
			// does not want them in memory, and inflating the whole data set
			// into a slice to find where they end put them there: a deflated
			// file with a 200 MB value took 200 MB to read. The inflated bytes
			// are a stream instead, measured once (the bound against a bomb
			// holds exactly as it does below) and read forward, a skipped
			// value being inflated and dropped. The data set is then read
			// with the memory of its attributes.
			stream, err := inflate.Open(reader, compressedSize, MaxInflatedDatasetSize)
			if err != nil {
				return nil, fmt.Errorf("failed to inflate deflated data set: %w", err)
			}
			reader = filebase.NewFileReader(stream)
		} else {
			inflated, err := inflateRemainder(reader, compressedSize)
			if err != nil {
				return nil, fmt.Errorf("failed to inflate deflated data set: %w", err)
			}
			reader = filebase.NewFileReader(bytes.NewReader(inflated))
		}
		dfr.reader = reader
		// Offsets now refer to the inflated data set, which begins at zero.
		dicomFile.Deflated = true
		dfr.position = 0
		dfr.streamSizeKnown = false
	}

	if dicomFile.IsLittleEndian {
		reader.SetByteOrder(filebase.LittleEndian)
	} else {
		reader.SetByteOrder(filebase.BigEndian)
	}

	var topOrder tagOrder
	for {
		element, err := dfr.ReadDataElement(dicomFile.ExplicitVR)
		if err != nil {
			if err == io.EOF {
				break
			}
			// The caller's maximum ends the read. It is not a truncation, and
			// the tests on the text below must never take it for one.
			if errors.Is(err, ErrValueTooLong) {
				return nil, err
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
				dfr.logger().Warn("filereader: file is truncated; dropping the incomplete element",
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
			normalizeByteOrder(element, bitsAllocated)
		}
		// Bits Allocated decides how wide a native Pixel Data sample is, and it
		// precedes Pixel Data in every conforming file (group 0028 before 7FE0).
		if element.Tag == bitsAllocatedTag && len(element.Value) >= 2 {
			bitsAllocated = int(binary.LittleEndian.Uint16(element.Value))
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

		dfr.checkTagOrder(&topOrder, element.Tag, element.Position.Offset, false)
		dicomFile.DataElements = append(dicomFile.DataElements, element)
	}
	dfr.orderSummary()

	// A file that declares explicit VR and holds elements without one is
	// not conformant, and readable: the reader took each as implicit, with
	// its VR from the dictionary, as pydicom does. Said once, with how many.
	if dfr.implicitElements > 0 {
		warning := fmt.Sprintf("%d element(s) are written without a VR in a data set that declares "+
			"explicit VR, the first at %s; each was read as implicit VR, with its VR from the dictionary",
			dfr.implicitElements, dfr.firstImplicit)
		dfr.logger().Warn("filereader: " + warning)
		dfr.warn("%s", warning)
	}

	// The warnings recorded while the data set was parsed, which the copy above
	// could not have seen.
	if len(dfr.metaWarnings) > metaWarningCount {
		dicomFile.Warnings = append(dicomFile.Warnings, dfr.metaWarnings[metaWarningCount:]...)
	}
	dicomFile.TagOrderViolations = dfr.orderKept
	dicomFile.TagOrderViolationCount = dfr.orderViolations

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
	limit := inflate.LimitFor(compressedSize, MaxInflatedDatasetSize)

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

// isStandardTransferSyntax reports whether a UID is in the part of the
// registry the standard keeps for transfer syntaxes: 1.2.840.10008.1.2 and
// everything under it (PS3.6 Annex A). One it adds later is there too, which
// a list would not know.
func isStandardTransferSyntax(ts string) bool {
	const root = "1.2.840.10008.1.2"
	return ts == root || strings.HasPrefix(ts, root+".")
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
		if !vr.MatchesDictionary(elem.VR, info.VR) {
			return fmt.Errorf("VR mismatch for %s: expected %s, got %s",
				elem.Tag, info.VR, elem.VR)
		}
	}

	return nil
}

// dictionaryVRForUN gives the VR to read a UN element with, or "" to keep UN.
//
// Only a public tag the dictionary knows unambiguously: a private tag has no
// entry to take a VR from, and an entry offering a choice ("US or SS") cannot be
// settled without the attributes that decide it, which is the writer's job
// (dataset.ResolveVR). Guessing either would describe bytes as something they
// may not be.
//
// Little endian only. Note 2 says a UN value is little endian whatever the
// transfer syntax, so in a big endian file a resolved value would be the one
// value in the data set that must not be byte-swapped — and normalizeByteOrder
// swaps by VR. Keeping UN there is correct rather than conservative: the bytes
// stay as they are, which is what UN means.
func dictionaryVRForUN(elemVR string, t tag.Tag, order filebase.ByteOrder) string {
	if elemVR != "UN" || order == filebase.BigEndian {
		return ""
	}
	if t.Group()%2 == 1 {
		// A private creator is LO by definition — its value is the vendor's own
		// name (PS3.5 7.8.1) — so it needs no dictionary. Every other private
		// tag keeps UN: there is no entry to take a VR from.
		if e := t.Element(); e >= 0x0010 && e <= 0x00FF {
			return "LO"
		}
		return ""
	}
	known := t.GetVR()
	if known == "" || known == "UN" || vr.OffersChoice(known) {
		return ""
	}
	return known
}

// normalizeByteOrder converts an element's value from big endian to the little
// endian representation the rest of the library assumes.
//
// Dataset stores values as opaque bytes with no record of how to interpret
// them, and everything downstream — pixel access, numeric conversion, the JSON
// model — reads them as little endian. Rather than thread the file's byte order
// through all of that, big endian values are normalised once here, so a data
// set means the same thing regardless of how the file was encoded.
func normalizeByteOrder(elem *DataElementValue, bitsAllocated int) {
	// Native Pixel Data is reversed at its sample width, not at the width OW
	// implies: at 32 or 64 bits the two-byte swap leaves each sample's halves
	// transposed, which is #124. Encapsulated Pixel Data is fragments, not
	// samples, and is never reversed.
	if elem.Tag == pixelDataTag && !elem.UndefinedLength {
		vr.SwapBytes(elem.Value, vr.PixelDataSwapWidth(elem.VR, bitsAllocated))
	} else {
		// Swap in place: the value was allocated by this reader and is not shared.
		vr.SwapByteOrder(elem.VR, elem.Value)
	}

	// Sequence items carry their own elements, encoded the same way, and an
	// item describing its own image — an icon — has its own Bits Allocated.
	for _, item := range elem.Items {
		itemBits := 0
		for _, nested := range item.Elements {
			normalizeByteOrder(nested, itemBits)
			if nested.Tag == bitsAllocatedTag && len(nested.Value) >= 2 {
				itemBits = int(binary.LittleEndian.Uint16(nested.Value))
			}
		}
	}
}

// bitsAllocatedTag is (0028,0100) and pixelDataTag is (7FE0,0010).
var (
	bitsAllocatedTag = tag.New(0x0028, 0x0100)
	pixelDataTag     = tag.New(0x7FE0, 0x0010)
)

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
func (dfr *Reader) readExplicitLength(element *DataElementValue) error {
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

// tryReadMetaGroupLength reads (0002,0000) if it is the next element.
//
// It reports whether the element was there. A file that omits it is
// non-conformant but readable, so the absence is not an error — the tag is put
// back and the caller finds the end of the header by watching the group instead.
func (dfr *Reader) tryReadMetaGroupLength() (uint32, bool, error) {
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
		dfr.logger().Warn("filereader: file meta header has no group length element",
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

	if !vr.IsValid(string(head[4:6])) {
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
func (dfr *Reader) sniffDataSetHead() (explicitVR, littleEndian, ok bool) {
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
