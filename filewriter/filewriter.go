package filewriter

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/config"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/filebase"
	"github.com/amrshadid/go-dicom/hooks"
	"github.com/amrshadid/go-dicom/tag"
	"github.com/amrshadid/go-dicom/uid"
)

// FileMetaInfo represents DICOM file meta information for writing.
type FileMetaInfo struct {
	MediaStorageSOPClassUID         string
	MediaStorageSOPInstanceUID      string
	TransferSyntaxUID               string
	ImplementationClassUID          string
	ImplementationVersionName       string
	SourceApplicationEntityTitle    string
	SendingApplicationEntityTitle   string
	ReceivingApplicationEntityTitle string

	// FileMetaInformationVersion is (0002,0001). Defaults to {0x00, 0x01} when
	// empty, which is the only value defined by PS3.10.
	FileMetaInformationVersion []byte
}

// isEncapsulatedSyntax reports whether a transfer syntax carries pixel data as
// fragments rather than as a contiguous value. compress.IsEncapsulated is the
// one answer the file writer and the network encoder share.
func isEncapsulatedSyntax(uid string) bool {
	return compress.IsEncapsulated(uid)
}

// bitsAllocatedTag is (0028,0100).
var bitsAllocatedTag = tag.New(0x0028, 0x0100)

// pixelDataTag is (7FE0,0010).
var pixelDataTag = tag.New(0x7FE0, 0x0010)

// writeSequenceDelimiter closes an undefined-length element with the
// (FFFE,E0DD) item that PS3.5 requires.
func (dfw *DCMFileWriter) writeSequenceDelimiter() error {
	if err := dfw.writer.WriteUint16(0xFFFE); err != nil {
		return fmt.Errorf("failed to write sequence delimiter group: %w", err)
	}
	if err := dfw.writer.WriteUint16(0xE0DD); err != nil {
		return fmt.Errorf("failed to write sequence delimiter element: %w", err)
	}
	if err := dfw.writer.WriteUint32(0); err != nil {
		return fmt.Errorf("failed to write sequence delimiter length: %w", err)
	}
	dfw.position += 8
	return nil
}

// undefinedLength is the DICOM sentinel (0xFFFFFFFF) marking an element whose
// extent is delimited rather than stated. Such elements are written through
// unchanged rather than having their length treated as a byte count.
const undefinedLength uint32 = 0xFFFFFFFF

// padValueForVR pads a value to even length using the VR's designated padding
// character, as required by PS3.5 Section 7.1.1. Returns the input unchanged
// when it is already even.
func padValueForVR(vr string, value []byte) []byte {
	if len(value)%2 == 0 {
		return value
	}

	pad := byte(0x00)
	if info := dataelem.GetVRInfo(dataelem.VR(vr)); info != nil {
		pad = info.PadValue
	}

	// Copy rather than append in place: the input may alias a caller's buffer,
	// which must not be mutated by writing.
	padded := make([]byte, len(value)+1)
	copy(padded, value)
	padded[len(value)] = pad
	return padded
}

// padMetaValue pads a file meta value to even length as DICOM requires:
// UI values pad with NUL, the text VRs pad with a space.
func padMetaValue(vr, value string) []byte {
	return padValueForVR(vr, []byte(value))
}

// DataElement represents a DICOM data element for writing.
type DataElement struct {
	Tag    tag.Tag
	VR     string
	Value  []byte
	Length uint32

	// Items holds the nested items of a Sequence (SQ) element. When set, Value
	// and Length are ignored and the sequence is serialized from these items.
	Items []*SequenceItem

	// InFile, when set, says the value is not in Value but in the writer's
	// value source (SetValueSource), at this offset and length, in the
	// source's byte order. Value and Length are then ignored. It is how a data
	// set read with filereader.ReadOptions.SkipBulkData is written back whole:
	// see filereader.DICOMFile.GetDatasetWithValuesInFile.
	InFile *dataelem.ValueInFile

	// Stream, when set, says the value is made as it is written: the writer
	// calls its WriteValue, with the value source (SetValueSource), and what
	// it writes is the value, as a data set holds it, little endian. Value
	// and Length are then ignored. It is how an image whose pixels are
	// redacted while they are copied from the file is written: see
	// dataset.Dataset.RedactPixelRegions.
	Stream *dataelem.StreamedValue
}

// SequenceItem is one item within a Sequence (SQ) element.
type SequenceItem struct {
	Elements []*DataElement
}

// DCMFileWriter writes DICOM files.
type DCMFileWriter struct {
	writer       filebase.Writer
	position     int64
	explicitVR   bool
	littleEndian bool
	settings     *config.Settings // Configuration settings for writing behavior
	hookChain    *hooks.HookChain // Hook chain for element processing
	elementCount int              // Count of elements written

	// encapsulated records that the target transfer syntax carries pixel data
	// as fragments, which PS3.5 A.4 requires be written with undefined length.
	encapsulated bool

	// bitsAllocated is (0028,0100) as written, for the Pixel Data byte-order
	// swap when the target syntax is big endian.
	bitsAllocated int

	// source is where the values of elements with InFile set are copied
	// from, and sourceLittleEndian the byte order they are in there.
	source             io.ReaderAt
	sourceLittleEndian bool
}

// NewDCMFileWriter creates a new DICOM file writer.
func NewDCMFileWriter(writer filebase.Writer) *DCMFileWriter {
	return &DCMFileWriter{
		writer:       writer,
		position:     0,
		explicitVR:   true,
		littleEndian: true,
		settings:     config.Get(),         // Load global configuration settings
		hookChain:    hooks.NewHookChain(), // Initialize empty hook chain
		elementCount: 0,
	}
}

// SetExplicitVR sets whether to use explicit VR.
func (dfw *DCMFileWriter) SetExplicitVR(explicit bool) {
	dfw.explicitVR = explicit
}

// SetEncapsulated records that the target transfer syntax carries pixel data as
// encapsulated fragments, which changes how (7FE0,0010) is written.
func (dfw *DCMFileWriter) SetEncapsulated(encapsulated bool) {
	dfw.encapsulated = encapsulated
}

// SetValueSource names the file the values of elements with InFile set are
// copied from, and the byte order its data set is in: the file the data set
// was read from with filereader.ReadOptions.SkipBulkData. Values are copied as
// the element is written, a chunk at a time, so they are never in memory whole
// except inside a sequence, whose items are encoded in memory to be measured.
//
// The source must not change between reading and writing: the offsets are
// those it had when it was read.
func (dfw *DCMFileWriter) SetValueSource(source io.ReaderAt, littleEndian bool) {
	dfw.source = source
	dfw.sourceLittleEndian = littleEndian
}

// SetLittleEndian sets the byte order (little-endian or big-endian).
func (dfw *DCMFileWriter) SetLittleEndian(littleEndian bool) {
	dfw.littleEndian = littleEndian
	if littleEndian {
		dfw.writer.SetByteOrder(filebase.LittleEndian)
	} else {
		dfw.writer.SetByteOrder(filebase.BigEndian)
	}
}

// WritePreamble writes the 128-byte DICOM preamble.
func (dfw *DCMFileWriter) WritePreamble() error {
	preamble := make([]byte, 128)
	if err := dfw.writer.WriteBytes(preamble); err != nil {
		return fmt.Errorf("failed to write preamble: %w", err)
	}
	dfw.position += 128
	return nil
}

// WriteDICMPrefix writes the "DICM" magic string.
func (dfw *DCMFileWriter) WriteDICMPrefix() error {
	if err := dfw.writer.WriteBytes([]byte("DICM")); err != nil {
		return fmt.Errorf("failed to write DICM prefix: %w", err)
	}
	dfw.position += 4
	return nil
}

// WriteFileMetaInfo writes the DICOM file meta information.
func (dfw *DCMFileWriter) WriteFileMetaInfo(metaInfo *FileMetaInfo) error {
	if metaInfo == nil {
		return fmt.Errorf("file meta info is nil")
	}
	elements := make([]*DataElement, 0)

	// addMeta appends a group-0002 element, padding the value to even length as
	// PS3.5 Section 7.1.1 requires. An odd-length value misaligns every element
	// that follows it, so this is not optional.
	addMeta := func(element uint16, vr, value string) {
		padded := padMetaValue(vr, value)
		elements = append(elements, &DataElement{
			Tag:    tag.New(0x0002, element),
			VR:     vr,
			Value:  padded,
			Length: uint32(len(padded)),
		})
	}

	// File Meta Information Version (0002,0001) is Type 1 — always present,
	// two bytes with the value 0x0001 big-endian per PS3.10 Section 7.1.
	version := metaInfo.FileMetaInformationVersion
	if len(version) == 0 {
		version = []byte{0x00, 0x01}
	}
	elements = append(elements, &DataElement{
		Tag:    tag.New(0x0002, 0x0001),
		VR:     "OB",
		Value:  version,
		Length: uint32(len(version)),
	})

	// Tag assignments follow PS3.6 (the DICOM data dictionary, group 0002).
	if metaInfo.MediaStorageSOPClassUID != "" {
		addMeta(0x0002, "UI", metaInfo.MediaStorageSOPClassUID)
	}
	if metaInfo.MediaStorageSOPInstanceUID != "" {
		addMeta(0x0003, "UI", metaInfo.MediaStorageSOPInstanceUID)
	}
	if metaInfo.TransferSyntaxUID != "" {
		addMeta(0x0010, "UI", metaInfo.TransferSyntaxUID)
	}
	// Implementation Class UID is Type 1 (PS3.10 Table 7.1-1), so a meta
	// that does not name one gets this library's, with its version name.
	classUID, versionName := metaInfo.ImplementationClassUID, metaInfo.ImplementationVersionName
	if classUID == "" {
		classUID, versionName = uid.ImplementationClassUID, uid.ImplementationVersionName
	}
	addMeta(0x0012, "UI", classUID)
	if versionName != "" {
		addMeta(0x0013, "SH", versionName)
	}
	if metaInfo.SourceApplicationEntityTitle != "" {
		addMeta(0x0016, "AE", metaInfo.SourceApplicationEntityTitle)
	}
	if metaInfo.SendingApplicationEntityTitle != "" {
		addMeta(0x0017, "AE", metaInfo.SendingApplicationEntityTitle)
	}
	if metaInfo.ReceivingApplicationEntityTitle != "" {
		addMeta(0x0018, "AE", metaInfo.ReceivingApplicationEntityTitle)
	}

	// Checked before anything is written: a value too long for its 16-bit
	// length field was written with its length truncated while the group
	// length counted it whole, so the header described a different file
	// from the one that followed it.
	for _, elem := range elements {
		if isShortVR(elem.VR) && elem.Length > 0xFFFF {
			return fmt.Errorf("file meta %s is %d bytes; a %s value holds at most 65535",
				elem.Tag, elem.Length, elem.VR)
		}
	}

	// Calculate total group length (all elements after group length tag)
	groupLength := uint32(0)
	for _, elem := range elements {
		// Tag (4) + VR (2) + Length field size + Value
		groupLength += 4 + 2
		if isShortVR(elem.VR) {
			// Short VR: 2-byte length
			groupLength += 2 + elem.Length
		} else {
			// Long VR: 2-byte reserved + 4-byte length
			groupLength += 2 + 4 + elem.Length
		}
	}

	// Write Group Length (0002,0000) - UL is a short VR with 2-byte length
	if err := dfw.WriteTag(tag.New(0x0002, 0x0000)); err != nil {
		return err
	}

	if err := dfw.writer.WriteBytes([]byte("UL")); err != nil {
		return fmt.Errorf("failed to write VR: %w", err)
	}

	// UL is a short VR, so write 2-byte length directly (not reserved + 4-byte)
	if err := dfw.writer.WriteUint16(4); err != nil { // Length of value (always 4 for UL)
		return fmt.Errorf("failed to write value length: %w", err)
	}

	if err := dfw.writer.WriteUint32(groupLength); err != nil {
		return fmt.Errorf("failed to write group length value: %w", err)
	}

	dfw.position += 4 + 2 + 2 + 4 // tag + VR + length + value

	// Write all other meta elements
	for _, elem := range elements {
		if err := dfw.WriteDataElement(elem, true); err != nil {
			return err
		}
	}

	return nil
}

// WriteTag writes a DICOM tag (4 bytes: group + element).
func (dfw *DCMFileWriter) WriteTag(t tag.Tag) error {
	if err := dfw.writer.WriteUint16(t.Group()); err != nil {
		return fmt.Errorf("failed to write tag group: %w", err)
	}

	if err := dfw.writer.WriteUint16(t.Element()); err != nil {
		return fmt.Errorf("failed to write tag element: %w", err)
	}

	dfw.position += 4
	return nil
}

// WriteDataElement writes a single data element.
//
// Values are padded to even length as DICOM requires (PS3.5 Section 7.1.1)
// using the VR's designated padding character. Padding is applied here rather
// than left to callers because an odd-length value misaligns every element
// after it, silently corrupting the file.
func (dfw *DCMFileWriter) WriteDataElement(elem *DataElement, forceExplicitVR bool) (err error) {
	if elem == nil {
		return fmt.Errorf("data element is nil")
	}

	// A sequence is serialized from its items rather than from a byte value.
	if elem.VR == "SQ" || elem.Items != nil {
		return dfw.writeSequence(elem, forceExplicitVR)
	}
	if elem.InFile != nil {
		return dfw.writeValueInFile(elem, forceExplicitVR)
	}
	if elem.Stream != nil {
		return dfw.writeStreamed(elem, forceExplicitVR)
	}

	// Bits Allocated decides the width Pixel Data is byte-swapped in below, and
	// precedes it in tag order. A nested writer has its own, so an icon inside a
	// 32-bit image is not swapped at the parent's width.
	if elem.Tag == bitsAllocatedTag && len(elem.Value) >= 2 {
		dfw.bitsAllocated = int(binary.LittleEndian.Uint16(elem.Value))
	}

	// The VR field is exactly two bytes (PS3.5 6.2), and this writes elem.VR into
	// it as given. A VR of any other length — a dictionary's "OB or OW", or none
	// at all — shifts every byte after it, and the file reads as corrupt from
	// that element on. ElementsFromDataset resolves the VR first; this is for a
	// DataElement built by hand, which should produce a readable file whatever it
	// holds. UN is what the standard says an element of unknown VR is.
	if len(elem.VR) != 2 {
		elem = &DataElement{Tag: elem.Tag, VR: "UN", Value: elem.Value, Length: elem.Length}
	}

	// Values are held little endian in memory, so numeric ones must be
	// converted when the target syntax is big endian. Swap a copy: the caller's
	// value must not be mutated by writing.
	if !dfw.littleEndian && dataelem.IsByteOrderSensitive(dataelem.VR(elem.VR)) {
		swapped := make([]byte, len(elem.Value))
		copy(swapped, elem.Value)
		if elem.Tag == pixelDataTag {
			// At the sample width, not the width OW implies: a 32-bit sample
			// reversed in two-byte units keeps its halves transposed (#124).
			dataelem.SwapBytes(swapped, dataelem.PixelDataSwapWidth(dataelem.VR(elem.VR), dfw.bitsAllocated))
		} else {
			dataelem.SwapByteOrder(dataelem.VR(elem.VR), swapped)
		}
		elem = &DataElement{
			Tag: elem.Tag, VR: elem.VR, Value: swapped, Length: uint32(len(swapped)),
		}
	}

	// Encapsulated pixel data is a sequence of fragments, not a value with a
	// byte count. PS3.5 A.4 requires undefined length and a closing sequence
	// delimiter, and dcmtk refuses a file without them:
	//
	//   Found explicit length Pixel Data in top level dataset with transfer
	//   syntax RLE Lossless: Only undefined length permitted
	//
	// The fragments themselves were already correct, so every compressed file
	// this wrote held the right bytes behind a length field that made it
	// unreadable to a strict parser. pydicom accepted them, which is why it went
	// unnoticed.
	if dfw.encapsulated && elem.Tag == pixelDataTag && elem.Length != undefinedLength {
		elem = &DataElement{
			Tag:    elem.Tag,
			VR:     elem.VR,
			Value:  elem.Value,
			Length: undefinedLength,
		}
		defer func() {
			if err == nil {
				err = dfw.writeSequenceDelimiter()
			}
		}()
	}

	// Undefined length (0xFFFFFFFF) marks a delimited element and must be
	// written through as-is rather than treated as a byte count.
	if elem.Length != undefinedLength && int(elem.Length) == len(elem.Value) && len(elem.Value)%2 != 0 {
		padded := padValueForVR(elem.VR, elem.Value)
		elem = &DataElement{
			Tag:    elem.Tag,
			VR:     elem.VR,
			Value:  padded,
			Length: uint32(len(padded)),
		}
	}

	if err := dfw.writeHeader(elem.Tag, elem.VR, elem.Length, dfw.explicitVR || forceExplicitVR); err != nil {
		return err
	}

	// Write value
	if elem.Length > 0 {
		if err := dfw.writer.WriteBytes(elem.Value); err != nil {
			return fmt.Errorf("failed to write data element value: %w", err)
		}
		dfw.position += int64(elem.Length)
	}

	return nil
}

// writeHeader writes an element's tag, its VR when explicit, and its length.
func (dfw *DCMFileWriter) writeHeader(t tag.Tag, vr string, length uint32, explicitVR bool) error {
	// A short VR states its length in 16 bits when the VR is explicit, and a
	// longer value cannot be stated in it. Writing the low 16 bits of the
	// length put every element after it in the wrong place. Refused before
	// the tag is written, so a refusal leaves nothing of the element behind.
	if explicitVR && isShortVR(vr) && length > 0xFFFF {
		return fmt.Errorf("%s value of %s is %d bytes; its explicit VR length field holds at most 65535",
			vr, t, length)
	}

	if err := dfw.WriteTag(t); err != nil {
		return err
	}

	if explicitVR {
		// Write VR (2 bytes)
		if err := dfw.writer.WriteBytes([]byte(vr)); err != nil {
			return fmt.Errorf("failed to write VR: %w", err)
		}
		dfw.position += 2

		// Write length based on VR type
		if isShortVR(vr) {
			// Short VR format: 2-byte length
			if err := dfw.writer.WriteUint16(uint16(length)); err != nil {
				return fmt.Errorf("failed to write value length: %w", err)
			}
			dfw.position += 2
		} else {
			// Long VR format: 2-byte reserved + 4-byte length
			if err := dfw.writer.WriteBytes([]byte{0x00, 0x00}); err != nil {
				return fmt.Errorf("failed to write reserved bytes: %w", err)
			}
			if err := dfw.writer.WriteUint32(length); err != nil {
				return fmt.Errorf("failed to write value length: %w", err)
			}
			dfw.position += 6
		}
	} else {
		// Implicit VR: 4-byte length
		if err := dfw.writer.WriteUint32(length); err != nil {
			return fmt.Errorf("failed to write value length: %w", err)
		}
		dfw.position += 4
	}
	return nil
}

// copyChunk is how much of a value left in the source is held at once. A
// multiple of eight, so a byte-order swap never splits a number across two
// chunks.
const copyChunk = 1 << 20

// writeValueInFile writes an element whose value is copied from the value
// source. It makes the same choices WriteDataElement makes for a value in
// memory, in the same order, so the two write the same bytes: the VR, the
// byte order, undefined length for encapsulated Pixel Data, and padding.
func (dfw *DCMFileWriter) writeValueInFile(elem *DataElement, forceExplicitVR bool) (err error) {
	ref := *elem.InFile
	if dfw.source == nil {
		// Writing nothing in its place would make an image with no pixels
		// that reads as complete.
		return fmt.Errorf("element %s: its value is %s, and the writer has no value source to copy it from",
			elem.Tag.String(), ref)
	}
	if ref.Offset < 0 || ref.Length < 0 || ref.Length >= int64(undefinedLength) {
		return fmt.Errorf("element %s: %s cannot be written as one value", elem.Tag.String(), ref)
	}

	vr := elem.VR
	if len(vr) != 2 {
		vr = "UN"
	}

	// The source holds the value in its own byte order and memory holds it
	// little endian. A whole read swaps a big endian value into memory, and
	// the writer swaps it out again for a big endian target, both at the
	// same width; with the value never in memory, a swap is needed only when
	// the two orders differ, and then once.
	swapWidth := 0
	if dfw.sourceLittleEndian != dfw.littleEndian && dataelem.IsByteOrderSensitive(dataelem.VR(vr)) {
		swapWidth = dataelem.PixelDataSwapWidth(dataelem.VR(vr), 0)
		if elem.Tag == pixelDataTag {
			swapWidth = dataelem.PixelDataSwapWidth(dataelem.VR(vr), dfw.bitsAllocated)
		}
	}

	length := uint32(ref.Length)
	pad := length%2 != 0
	if dfw.encapsulated && elem.Tag == pixelDataTag {
		// As WriteDataElement: undefined length and a closing delimiter
		// (PS3.5 A.4). The items are the value, and are never padded.
		length, pad = undefinedLength, false
		defer func() {
			if err == nil {
				err = dfw.writeSequenceDelimiter()
			}
		}()
	} else if pad {
		length++
	}

	if err := dfw.writeHeader(elem.Tag, vr, length, dfw.explicitVR || forceExplicitVR); err != nil {
		return err
	}

	buf := make([]byte, min(int64(copyChunk), ref.Length))
	for done := int64(0); done < ref.Length; {
		chunk := buf[:min(int64(len(buf)), ref.Length-done)]
		if _, err := dfw.source.ReadAt(chunk, ref.Offset+done); err != nil {
			return fmt.Errorf("element %s: copying its value from the source at offset %d: %w",
				elem.Tag.String(), ref.Offset+done, err)
		}
		dataelem.SwapBytes(chunk, swapWidth)
		if err := dfw.writer.WriteBytes(chunk); err != nil {
			return fmt.Errorf("failed to write data element value: %w", err)
		}
		done += int64(len(chunk))
		dfw.position += int64(len(chunk))
	}

	if pad {
		// The VR's padding byte, as padValueForVR gives a value in memory.
		if err := dfw.writer.WriteBytes(padValueForVR(vr, []byte{0})[1:]); err != nil {
			return fmt.Errorf("failed to write data element value: %w", err)
		}
		dfw.position++
	}
	return nil
}

// streamWriter is where a streamed value is written: it reverses what it is
// given to the byte order of the target, in whole numbers, and passes it on.
type streamWriter struct {
	dfw *DCMFileWriter
	// swap is the width numbers are reversed at, or 0 for none; held is the
	// start of a number whose end has not come yet.
	swap    int
	held    []byte
	written int64
	err     error
}

func (s *streamWriter) Write(p []byte) (int, error) {
	n := len(p)
	if s.swap > 1 {
		// Reversed on a copy: the producer may reuse what it passed.
		whole := append(append([]byte(nil), s.held...), p...)
		keep := len(whole) % s.swap
		s.held = append(s.held[:0], whole[len(whole)-keep:]...)
		whole = whole[:len(whole)-keep]
		dataelem.SwapBytes(whole, s.swap)
		p = whole
	}
	return n, s.put(p)
}

// put writes bytes to the output.
func (s *streamWriter) put(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	if err := s.dfw.writer.WriteBytes(p); err != nil {
		s.err = fmt.Errorf("failed to write data element value: %w", err)
		return s.err
	}
	s.written += int64(len(p))
	s.dfw.position += int64(len(p))
	return nil
}

// writeStreamed writes an element whose value is made as it is written. It
// makes the choices writeValueInFile makes, in the same order: the VR, the
// byte order, and padding.
func (dfw *DCMFileWriter) writeStreamed(elem *DataElement, forceExplicitVR bool) error {
	stream := elem.Stream
	if stream.WriteValue == nil || stream.Length < 0 || stream.Length >= int64(undefinedLength) {
		return fmt.Errorf("element %s: %s cannot be written as one value", elem.Tag.String(), stream)
	}
	if dfw.encapsulated && elem.Tag == pixelDataTag {
		// A streamed value is the samples themselves. Written as fragments
		// it would be an image that decodes to nothing.
		return fmt.Errorf("element %s: %s is native samples, and the target transfer syntax encapsulates Pixel Data",
			elem.Tag.String(), stream)
	}

	vr := elem.VR
	if len(vr) != 2 {
		vr = "UN"
	}
	// The value arrives little endian, as a data set holds it.
	swapWidth := 0
	if !dfw.littleEndian && dataelem.IsByteOrderSensitive(dataelem.VR(vr)) {
		swapWidth = dataelem.PixelDataSwapWidth(dataelem.VR(vr), 0)
		if elem.Tag == pixelDataTag {
			swapWidth = dataelem.PixelDataSwapWidth(dataelem.VR(vr), dfw.bitsAllocated)
		}
	}

	length := uint32(stream.Length)
	pad := length%2 != 0
	if pad {
		length++
	}
	if err := dfw.writeHeader(elem.Tag, vr, length, dfw.explicitVR || forceExplicitVR); err != nil {
		return err
	}

	out := &streamWriter{dfw: dfw, swap: swapWidth}
	if err := stream.WriteValue(dfw.source, dfw.sourceLittleEndian, out); err != nil {
		if out.err != nil {
			return out.err
		}
		return fmt.Errorf("element %s: making its value: %w", elem.Tag.String(), err)
	}
	// A number that did not end is written as it is, which is what reversing
	// a value does to bytes short of a whole number.
	if err := out.put(out.held); err != nil {
		return err
	}
	if out.written != stream.Length {
		return fmt.Errorf("element %s: its value is %d bytes and %d were made", elem.Tag.String(), stream.Length, out.written)
	}

	if pad {
		if err := dfw.writer.WriteBytes(padValueForVR(vr, []byte{0})[1:]); err != nil {
			return fmt.Errorf("failed to write data element value: %w", err)
		}
		dfw.position++
	}
	return nil
}

// seekBuffer is an in-memory io.WriteSeeker used to serialize nested sequence
// content so its length can be stated before it is written. Only append-style
// writing is used, so Seek reports the current end.
type seekBuffer struct {
	buf bytes.Buffer
}

func (s *seekBuffer) Write(p []byte) (int, error) { return s.buf.Write(p) }

func (s *seekBuffer) Seek(_ int64, _ int) (int64, error) { return int64(s.buf.Len()), nil }

func (s *seekBuffer) Bytes() []byte { return s.buf.Bytes() }

// EncodeFileHeader returns what comes before the data set in a Part 10 file:
// the 128-byte preamble, "DICM", and the File Meta Information that
// WriteFileMetaInfo writes for meta (PS3.10 7.1). The data set follows it,
// encoded in meta.TransferSyntaxUID.
//
// It is for a writer that has the data set as bytes already encoded, such as
// a C-STORE received over the network, and copies them after the header
// without decoding them.
func EncodeFileHeader(meta *FileMetaInfo) ([]byte, error) {
	buf := &seekBuffer{}
	w := NewDCMFileWriter(filebase.NewFileWriter(buf))
	if err := w.WritePreamble(); err != nil {
		return nil, err
	}
	if err := w.WriteDICMPrefix(); err != nil {
		return nil, err
	}
	if err := w.WriteFileMetaInfo(meta); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeSequence serializes a Sequence (SQ) element and its nested items.
//
// Items are written with explicit lengths rather than delimiters, so the
// encoding is self-describing. Item tags always use the implicit-style header
// — tag then 4-byte length, no VR — even inside an explicit VR transfer
// syntax (PS3.5 Section 7.5).
//
// The sequence itself has an explicit length too, except in Implicit VR when
// the dictionary does not call its tag a sequence: a private sequence above
// all. There nothing but an undefined length tells a reader the element holds
// items (PS3.5 6.2.2 Note 5, CP-246), so it is written with one and closed by
// a Sequence Delimitation Item (PS3.5 7.5.1). With its byte count instead,
// dcmtk and pydicom read the whole sequence as one opaque value, and so did
// this library's own reader: a vendor's private sequence did not survive one
// pass through anonymize or modify as a sequence. The items inside may keep
// their lengths; both readers parse them once they know they are items.
func (dfw *DCMFileWriter) writeSequence(elem *DataElement, forceExplicitVR bool) error {
	// Serialize the items first so the sequence length is known up front.
	itemBytes, err := dfw.encodeSequenceItems(elem.Items, forceExplicitVR)
	if err != nil {
		return err
	}

	explicitVR := dfw.explicitVR || forceExplicitVR
	length := uint32(len(itemBytes))
	if elem.Tag.SequenceNeedsUndefinedLength(explicitVR) {
		length = undefinedLength
	}

	if err := dfw.WriteTag(elem.Tag); err != nil {
		return err
	}

	if explicitVR {
		if err := dfw.writer.WriteBytes([]byte("SQ")); err != nil {
			return fmt.Errorf("failed to write SQ VR: %w", err)
		}
		if err := dfw.writer.WriteBytes([]byte{0x00, 0x00}); err != nil {
			return fmt.Errorf("failed to write reserved bytes: %w", err)
		}
		dfw.position += 4
	}

	if err := dfw.writer.WriteUint32(length); err != nil {
		return fmt.Errorf("failed to write sequence length: %w", err)
	}
	dfw.position += 4

	if len(itemBytes) > 0 {
		if err := dfw.writer.WriteBytes(itemBytes); err != nil {
			return fmt.Errorf("failed to write sequence items: %w", err)
		}
		dfw.position += int64(len(itemBytes))
	}

	if length == undefinedLength {
		return dfw.writeSequenceDelimiter()
	}
	return nil
}

// encodeSequenceItems serializes sequence items into a buffer so the enclosing
// sequence can state its length.
func (dfw *DCMFileWriter) encodeSequenceItems(items []*SequenceItem, forceExplicitVR bool) ([]byte, error) {
	var out []byte

	for _, item := range items {
		if item == nil {
			continue
		}

		body, err := dfw.encodeElements(item.Elements, forceExplicitVR)
		if err != nil {
			return nil, err
		}

		// Item header: (FFFE,E000) then a 4-byte length, no VR.
		header := make([]byte, 8)
		copy(header[0:4], tag.ItemTag.ToBytes(dfw.littleEndian))
		if dfw.littleEndian {
			header[4] = byte(len(body))
			header[5] = byte(len(body) >> 8)
			header[6] = byte(len(body) >> 16)
			header[7] = byte(len(body) >> 24)
		} else {
			header[4] = byte(len(body) >> 24)
			header[5] = byte(len(body) >> 16)
			header[6] = byte(len(body) >> 8)
			header[7] = byte(len(body))
		}

		out = append(out, header...)
		out = append(out, body...)
	}

	return out, nil
}

// encodeElements serializes a list of elements to bytes using a scratch
// writer, so nested content can be sized before being written.
func (dfw *DCMFileWriter) encodeElements(elements []*DataElement, forceExplicitVR bool) ([]byte, error) {
	buf := &seekBuffer{}
	inner := filebase.NewFileWriter(buf)
	if dfw.littleEndian {
		inner.SetByteOrder(filebase.LittleEndian)
	} else {
		inner.SetByteOrder(filebase.BigEndian)
	}

	nested := NewDCMFileWriter(inner)
	nested.explicitVR = dfw.explicitVR
	nested.littleEndian = dfw.littleEndian
	nested.source, nested.sourceLittleEndian = dfw.source, dfw.sourceLittleEndian

	for _, e := range elements {
		if e == nil {
			continue
		}
		if err := nested.WriteDataElement(e, forceExplicitVR); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// WriteDataElements writes multiple data elements.
func (dfw *DCMFileWriter) WriteDataElements(elements []*DataElement) error {
	for _, elem := range elements {
		if err := dfw.WriteDataElement(elem, false); err != nil {
			return err
		}
	}
	return nil
}

// GetPosition returns the current position in the file.
func (dfw *DCMFileWriter) GetPosition() int64 {
	return dfw.position
}

// RegisterHook registers a hook function at the specified processing level.
// Hooks allow custom processing of data elements before writing.
// Multiple hooks can be registered at the same level; they execute in order.
func (dfw *DCMFileWriter) RegisterHook(level hooks.HookLevel, fn hooks.AdvancedHookFunc) error {
	if dfw.hookChain == nil {
		dfw.hookChain = hooks.NewHookChain()
	}
	return dfw.hookChain.RegisterHook(level, fn)
}

// GetHookChain returns the hook chain for this writer.
// Allows direct access for advanced hook chain operations.
func (dfw *DCMFileWriter) GetHookChain() *hooks.HookChain {
	if dfw.hookChain == nil {
		dfw.hookChain = hooks.NewHookChain()
	}
	return dfw.hookChain
}

// GetElementCount returns the number of data elements that have been written.
func (dfw *DCMFileWriter) GetElementCount() int {
	return dfw.elementCount
}

// Flush flushes any buffered data.
func (dfw *DCMFileWriter) Flush() error {
	return dfw.writer.Flush()
}

// Close closes the writer.
func (dfw *DCMFileWriter) Close() error {
	if err := dfw.Flush(); err != nil {
		return err
	}
	return dfw.writer.Close()
}

// Helper functions

// ConvertDataElementToRaw converts a DataElement to a RawDataElement for hook processing.
func ConvertDataElementToRaw(elem *DataElement) (*hooks.RawDataElement, error) {
	if elem == nil {
		return nil, fmt.Errorf("data element cannot be nil")
	}
	vr := elem.VR
	raw := &hooks.RawDataElement{
		Tag:   elem.Tag.String(),
		VR:    &vr,
		Value: elem.Value,
	}
	return raw, nil
}

func isShortVR(vr string) bool {
	switch vr {
	case "OB", "OD", "OF", "OL", "OV", "OW", "SQ", "SV", "UC", "UN", "UR", "UT", "UV":
		return false
	default:
		return true
	}
}

// WriteDICOMFile writes a complete DICOM file.
type DICOMFileWriter struct {
	writer       *DCMFileWriter
	metaInfo     *FileMetaInfo
	dataElements []*DataElement
}

// NewDICOMFileWriter creates a new DICOM file writer.
func NewDICOMFileWriter(writer filebase.Writer) *DICOMFileWriter {
	return &DICOMFileWriter{
		writer:       NewDCMFileWriter(writer),
		dataElements: make([]*DataElement, 0),
	}
}

// SetFileMetaInfo sets the file meta information.
func (dfw *DICOMFileWriter) SetFileMetaInfo(metaInfo *FileMetaInfo) {
	dfw.metaInfo = metaInfo

	// Set transfer syntax properties
	if metaInfo != nil && metaInfo.TransferSyntaxUID != "" {
		dfw.writer.SetEncapsulated(isEncapsulatedSyntax(metaInfo.TransferSyntaxUID))
		explicitVR, littleEndian := determineTransferSyntax(metaInfo.TransferSyntaxUID)
		dfw.writer.SetExplicitVR(explicitVR)
		dfw.writer.SetLittleEndian(littleEndian)
	}
}

// SetValueSource names the file the values of elements with InFile set are
// copied from, and the byte order of its data set; see
// DCMFileWriter.SetValueSource.
func (dfw *DICOMFileWriter) SetValueSource(source io.ReaderAt, littleEndian bool) {
	dfw.writer.SetValueSource(source, littleEndian)
}

// AddDataElement adds a data element to the dataset.
func (dfw *DICOMFileWriter) AddDataElement(elem *DataElement) error {
	if elem == nil {
		return fmt.Errorf("data element is nil")
	}
	dfw.dataElements = append(dfw.dataElements, elem)
	return nil
}

// AddDataElements adds multiple data elements to the dataset.
func (dfw *DICOMFileWriter) AddDataElements(elements []*DataElement) error {
	for _, elem := range elements {
		if err := dfw.AddDataElement(elem); err != nil {
			return err
		}
	}
	return nil
}

// Write writes the complete DICOM file.
func (dfw *DICOMFileWriter) Write() error {
	// Write preamble
	if err := dfw.writer.WritePreamble(); err != nil {
		return fmt.Errorf("failed to write preamble: %w", err)
	}

	// Write DICM magic
	if err := dfw.writer.WriteDICMPrefix(); err != nil {
		return fmt.Errorf("failed to write DICM prefix: %w", err)
	}

	// The file meta header is always Explicit VR Little Endian, whatever the
	// transfer syntax it declares for the data set that follows (PS3.10
	// Section 7.1). Writing it in the data set's byte order produced a header
	// whose very first tag read back as (0200,0000).
	if dfw.metaInfo != nil {
		explicitVR, littleEndian := dfw.writer.explicitVR, dfw.writer.littleEndian
		dfw.writer.SetExplicitVR(true)
		dfw.writer.SetLittleEndian(true)

		err := dfw.writer.WriteFileMetaInfo(dfw.metaInfo)

		// Restore the data set's encoding before writing it.
		dfw.writer.SetExplicitVR(explicitVR)
		dfw.writer.SetLittleEndian(littleEndian)

		if err != nil {
			return fmt.Errorf("failed to write file meta information: %w", err)
		}
	}

	// Under the Deflated syntax the data set that follows the meta header is
	// raw DEFLATE. Without this the writer produced a file declaring
	// 1.2.840.10008.1.2.1.99 whose body was uncompressed, so nothing —
	// including this library's own reader — could read it back.
	if dfw.metaInfo != nil && dfw.metaInfo.TransferSyntaxUID == DeflatedExplicitVRLittleEndianUID {
		return dfw.writeDeflatedDataSet()
	}

	// Write dataset elements
	if err := dfw.writer.WriteDataElements(dfw.dataElements); err != nil {
		return fmt.Errorf("failed to write data elements: %w", err)
	}

	return nil
}

// DeflatedExplicitVRLittleEndianUID is the transfer syntax whose data set is
// DEFLATE-compressed after the file meta header (PS3.5 Annex A.5).
const DeflatedExplicitVRLittleEndianUID = "1.2.840.10008.1.2.1.99"

// writeDeflatedDataSet serializes the elements, compresses them, and writes the
// result after the meta header.
//
// The elements are encoded to memory first because DEFLATE has to see the whole
// data set: it is a single stream over the body, not a per-element encoding.
func (dfw *DICOMFileWriter) writeDeflatedDataSet() error {
	raw, err := dfw.writer.encodeElements(dfw.dataElements, false)
	if err != nil {
		return fmt.Errorf("failed to encode data set for deflating: %w", err)
	}

	var compressed bytes.Buffer
	zw, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		return fmt.Errorf("failed to start the deflate stream: %w", err)
	}
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return fmt.Errorf("failed to deflate the data set: %w", err)
	}
	// Close flushes the final block; a stream left unclosed decompresses short.
	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to finish the deflate stream: %w", err)
	}

	if err := dfw.writer.writer.WriteBytes(compressed.Bytes()); err != nil {
		return fmt.Errorf("failed to write the deflated data set: %w", err)
	}
	return nil
}

// Close closes the writer.
func (dfw *DICOMFileWriter) Close() error {
	return dfw.writer.Close()
}

// determineTransferSyntax determines if a transfer syntax uses explicit VR and little-endian.
// Uses the uid module for transfer syntax classification.
func determineTransferSyntax(ts string) (bool, bool) {
	// Default: Implicit VR Little Endian
	explicitVR := false
	littleEndian := true

	// Validate and classify transfer syntax using uid module
	u := uid.New(ts)
	if !u.IsValid() {
		// Invalid UID, use default
		return explicitVR, littleEndian
	}

	// Check for big-endian (explicit VR big endian)
	if ts == uid.BigEndianTransferSyntax().String() {
		return true, false
	}

	// Check for implicit VR little endian (default)
	if ts == uid.ImplicitVRLittleEndian().String() {
		return false, true
	}

	// Check for explicit VR little endian
	if ts == uid.ExplicitVRLittleEndian().String() {
		return true, true
	}

	// Check for any other uncompressed native format
	if !uid.IsCompressed(u) {
		// Any other uncompressed syntax is explicit VR little endian
		return true, true
	}

	// All compressed transfer syntaxes are explicit VR, little endian
	if uid.IsCompressed(u) {
		return true, true
	}

	// Default fallback
	return explicitVR, littleEndian
}
