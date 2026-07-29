package network

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/compress"
	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/sequence"
	"github.com/amrshadid/go-dicom/tag"
)

// maxDatasetElementLength bounds a single declared element length when decoding
// a received data set. The length field is peer-controlled, so it is checked
// against the bytes actually remaining before any allocation is made.
const maxDatasetElementLength = 1 << 30

// undefinedLength is the DICOM sentinel marking an element whose extent is
// delimited rather than stated (PS3.5 Section 7.1).
const undefinedLength uint32 = 0xFFFFFFFF

// maxSequenceDepth bounds nesting while decoding, so a crafted data set cannot
// drive unbounded recursion.
const maxSequenceDepth = 64

// transferSyntaxEncoding describes how a data set is laid out on the wire for a
// given transfer syntax.
type transferSyntaxEncoding struct {
	ExplicitVR bool
	BigEndian  bool
	Deflated   bool
}

// encodingForTransferSyntax maps a transfer syntax UID onto its wire encoding.
//
// Every compressed syntax (JPEG, JPEG-LS, JPEG 2000, RLE, MPEG, ...) encodes the
// surrounding data set as Explicit VR Little Endian and compresses only the
// pixel data, so they all fall through to the explicit little-endian default.
func encodingForTransferSyntax(ts string) transferSyntaxEncoding {
	switch ts {
	case ImplicitVRLittleEndianUID:
		return transferSyntaxEncoding{ExplicitVR: false, BigEndian: false}
	case ExplicitVRBigEndianUID:
		return transferSyntaxEncoding{ExplicitVR: true, BigEndian: true}
	case DeflatedExplicitVRLittleEndianUID:
		return transferSyntaxEncoding{ExplicitVR: true, BigEndian: false, Deflated: true}
	case "":
		// No negotiated syntax known; DICOM's default encoding is implicit VR LE.
		return transferSyntaxEncoding{ExplicitVR: false, BigEndian: false}
	default:
		return transferSyntaxEncoding{ExplicitVR: true, BigEndian: false}
	}
}

// byteOrder returns the binary.ByteOrder for this encoding.
func (e transferSyntaxEncoding) byteOrder() binary.ByteOrder {
	if e.BigEndian {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// EncodeDataset serializes a data set using the given transfer syntax.
//
// The transfer syntax must be the one negotiated for the presentation context
// the data will be sent on; encoding with a different syntax than the peer
// agreed to produces a data set the peer cannot parse.
func EncodeDataset(ds *dataset.Dataset, transferSyntax string) ([]byte, error) {
	if ds == nil {
		return nil, nil
	}

	enc := encodingForTransferSyntax(transferSyntax)
	var buf bytes.Buffer

	for _, elem := range ds.GetAll() {
		t, ok := elem.GetTag().(tag.Tag)
		if !ok {
			continue
		}

		// A sequence holds nested data sets rather than a byte value, so it is
		// serialized recursively. Skipping it here would transmit the element
		// as empty and silently drop every nested item.
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			if err := writeSequence(&buf, enc, t, seq); err != nil {
				return nil, err
			}
			continue
		}

		data, ok := elementValueBytes(elem)
		if !ok {
			continue
		}
		if err := writeElement(&buf, enc, t, elem.GetVR(), data); err != nil {
			return nil, err
		}
	}

	if enc.Deflated {
		return deflateBytes(buf.Bytes())
	}
	return buf.Bytes(), nil
}

// DecodeDataset parses a data set encoded with the given transfer syntax.
func DecodeDataset(data []byte, transferSyntax string) (*dataset.Dataset, error) {
	enc := encodingForTransferSyntax(transferSyntax)

	if enc.Deflated {
		inflated, err := inflateBytes(data)
		if err != nil {
			return nil, NewPDUErrorf("DECODE_DS", "failed to inflate deflated data set: %v", err)
		}
		data = inflated
	}

	ds := dataset.NewDataset()
	r := bytes.NewReader(data)
	order := enc.byteOrder()

	for r.Len() > 0 {
		t, vr, length, err := readElementHeader(r, enc, order)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		// Sequences hold nested data sets and are parsed recursively, so the
		// items survive rather than being flattened into an opaque value.
		if vr == dataelem.SQ {
			seq, err := readSequence(r, enc, order, length, 0)
			if err != nil {
				return nil, err
			}
			_ = ds.AddSequence(t, seq)
			continue
		}

		// An undefined length on a non-sequence element means encapsulated
		// pixel data, which is delimited rather than sized. Consume the
		// remainder as an opaque value so the surrounding elements still decode.
		if length == undefinedLength {
			value := make([]byte, r.Len())
			_, _ = io.ReadFull(r, value)
			_ = ds.Add(dataelem.NewDataElement(t, vr, value))
			break
		}

		if uint64(length) > uint64(r.Len()) {
			return nil, NewPDUErrorf("DECODE_DS",
				"element %s declares %d bytes but only %d remain", t.String(), length, r.Len())
		}
		if length > maxDatasetElementLength {
			return nil, NewPDUErrorf("DECODE_DS",
				"element %s length %d exceeds maximum %d", t.String(), length, maxDatasetElementLength)
		}

		value := make([]byte, length)
		if _, err := io.ReadFull(r, value); err != nil {
			return nil, NewPDUErrorf("DECODE_DS", "failed to read value for %s: %v", t.String(), err)
		}
		_ = ds.Add(dataelem.NewDataElement(t, vr, value))
	}

	return ds, nil
}

// readElementHeader reads one element's tag, VR, and value length.
func readElementHeader(r *bytes.Reader, enc transferSyntaxEncoding, order binary.ByteOrder) (tag.Tag, dataelem.VR, uint32, error) {
	var group, element uint16
	if err := binary.Read(r, order, &group); err != nil {
		return 0, "", 0, io.EOF
	}
	if err := binary.Read(r, order, &element); err != nil {
		return 0, "", 0, NewPDUError("DECODE_DS", "truncated element tag")
	}
	t := tag.New(group, element)

	// Item and delimitation items always use implicit-style headers.
	if group == 0xFFFE {
		var length uint32
		if err := binary.Read(r, order, &length); err != nil {
			return 0, "", 0, NewPDUError("DECODE_DS", "truncated item length")
		}
		return t, dataelem.UN, length, nil
	}

	if !enc.ExplicitVR {
		var length uint32
		if err := binary.Read(r, order, &length); err != nil {
			return 0, "", 0, NewPDUError("DECODE_DS", "truncated element length")
		}
		return t, dataelem.VR(t.GetVR()), length, nil
	}

	vrBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, vrBytes); err != nil {
		return 0, "", 0, NewPDUError("DECODE_DS", "truncated VR")
	}
	vr := dataelem.VR(vrBytes)

	if isLongFormVR(vr) {
		if _, err := r.Seek(2, io.SeekCurrent); err != nil { // reserved
			return 0, "", 0, NewPDUError("DECODE_DS", "truncated reserved bytes")
		}
		var length uint32
		if err := binary.Read(r, order, &length); err != nil {
			return 0, "", 0, NewPDUError("DECODE_DS", "truncated element length")
		}
		return t, vr, length, nil
	}

	var shortLen uint16
	if err := binary.Read(r, order, &shortLen); err != nil {
		return 0, "", 0, NewPDUError("DECODE_DS", "truncated element length")
	}
	return t, vr, uint32(shortLen), nil
}

// writeElement serializes a single data element.
func writeElement(buf *bytes.Buffer, enc transferSyntaxEncoding, t tag.Tag, vr dataelem.VR, data []byte) error {
	order := enc.byteOrder()

	// Resolve the VR first: it determines the pad byte as well as the header form.
	if vr == "" {
		vr = dataelem.VR(t.GetVR())
	}
	if len(vr) != 2 {
		vr = dataelem.UN
	}

	// DICOM requires every Data Element value to have an even length
	// (PS3.5 Section 7.1.1). Pad with the VR's designated padding character:
	// NUL for UI and the binary VRs, space for the text VRs.
	data = padToEvenLength(vr, data)

	if err := binary.Write(buf, order, t.Group()); err != nil {
		return err
	}
	if err := binary.Write(buf, order, t.Element()); err != nil {
		return err
	}

	if !enc.ExplicitVR {
		if err := binary.Write(buf, order, uint32(len(data))); err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}

	buf.WriteString(string(vr))

	if isLongFormVR(vr) {
		buf.Write([]byte{0x00, 0x00}) // reserved
		if err := binary.Write(buf, order, uint32(len(data))); err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}

	if len(data) > 0xFFFF {
		return fmt.Errorf("element %s: value of %d bytes exceeds the 16-bit length field for VR %s",
			t.String(), len(data), vr)
	}
	if err := binary.Write(buf, order, uint16(len(data))); err != nil {
		return err
	}
	buf.Write(data)
	return nil
}

// writeSequence serializes a Sequence (SQ) element and the data sets it holds.
//
// Items are written with explicit lengths rather than delimiters, which keeps
// the encoding self-describing and is accepted by every conforming peer. Item
// tags always use the implicit-style header — tag then 4-byte length, no VR —
// even inside an explicit VR transfer syntax (PS3.5 Section 7.5).
func writeSequence(buf *bytes.Buffer, enc transferSyntaxEncoding, t tag.Tag, seq *sequence.Sequence) error {
	order := enc.byteOrder()

	// Serialize the items first so the sequence length is known up front.
	var itemsBuf bytes.Buffer
	for _, item := range seq.Items() {
		child, ok := item.(*dataset.Dataset)
		if !ok {
			// A sequence item that is not a data set cannot be encoded; skip it
			// rather than emitting a malformed item.
			continue
		}

		childBytes, err := encodeDatasetBody(child, enc)
		if err != nil {
			return err
		}

		if err := binary.Write(&itemsBuf, order, tag.ItemTag.Group()); err != nil {
			return err
		}
		if err := binary.Write(&itemsBuf, order, tag.ItemTag.Element()); err != nil {
			return err
		}
		if err := binary.Write(&itemsBuf, order, uint32(len(childBytes))); err != nil {
			return err
		}
		itemsBuf.Write(childBytes)
	}

	// Sequence element header.
	if err := binary.Write(buf, order, t.Group()); err != nil {
		return err
	}
	if err := binary.Write(buf, order, t.Element()); err != nil {
		return err
	}
	if enc.ExplicitVR {
		buf.WriteString("SQ")
		buf.Write([]byte{0x00, 0x00}) // reserved
	}
	if err := binary.Write(buf, order, uint32(itemsBuf.Len())); err != nil {
		return err
	}
	buf.Write(itemsBuf.Bytes())
	return nil
}

// encodeDatasetBody serializes a data set's elements without applying the
// deflate wrapper, so it can be nested inside a sequence item.
func encodeDatasetBody(ds *dataset.Dataset, enc transferSyntaxEncoding) ([]byte, error) {
	var buf bytes.Buffer

	for _, elem := range ds.GetAll() {
		t, ok := elem.GetTag().(tag.Tag)
		if !ok {
			continue
		}
		if seq, ok := elem.GetValue().(*sequence.Sequence); ok {
			if err := writeSequence(&buf, enc, t, seq); err != nil {
				return nil, err
			}
			continue
		}
		data, ok := elementValueBytes(elem)
		if !ok {
			continue
		}
		if err := writeElement(&buf, enc, t, elem.GetVR(), data); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// readSequence parses the items of a Sequence (SQ) element. A declaredLength of
// 0xFFFFFFFF means the sequence is delimited rather than sized.
func readSequence(r *bytes.Reader, enc transferSyntaxEncoding, order binary.ByteOrder,
	declaredLength uint32, depth int) (*sequence.Sequence, error) {

	if depth > maxSequenceDepth {
		return nil, NewPDUErrorf("DECODE_DS",
			"sequence nesting exceeds maximum depth %d", maxSequenceDepth)
	}

	seq := sequence.New()
	undefined := declaredLength == undefinedLength

	if !undefined {
		if uint64(declaredLength) > uint64(r.Len()) {
			return nil, NewPDUErrorf("DECODE_DS",
				"sequence declares %d bytes but only %d remain", declaredLength, r.Len())
		}
	}
	start := r.Len()

	for {
		if !undefined && start-r.Len() >= int(declaredLength) {
			return seq, nil
		}
		if r.Len() < 8 {
			return seq, nil
		}

		itemTag, itemLen, err := readItemHeader(r, order)
		if err != nil {
			return seq, nil //nolint:nilerr // a truncated item ends the sequence
		}

		if itemTag == tag.SequenceDelimiterTag {
			return seq, nil
		}
		if itemTag != tag.ItemTag {
			return nil, NewPDUErrorf("DECODE_DS",
				"unexpected tag %s inside sequence (expected item or delimiter)", itemTag)
		}

		child, err := readSequenceItem(r, enc, order, itemLen, depth)
		if err != nil {
			return nil, err
		}
		_ = seq.Append(child)
	}
}

// readSequenceItem parses the elements of one sequence item into a Dataset.
func readSequenceItem(r *bytes.Reader, enc transferSyntaxEncoding, order binary.ByteOrder,
	declaredLength uint32, depth int) (*dataset.Dataset, error) {

	child := dataset.NewDataset()
	undefined := declaredLength == undefinedLength

	if !undefined && uint64(declaredLength) > uint64(r.Len()) {
		return nil, NewPDUErrorf("DECODE_DS",
			"sequence item declares %d bytes but only %d remain", declaredLength, r.Len())
	}
	start := r.Len()

	for {
		if !undefined && start-r.Len() >= int(declaredLength) {
			return child, nil
		}
		if r.Len() == 0 {
			return child, nil
		}

		t, vr, length, err := readElementHeader(r, enc, order)
		if err != nil {
			return child, nil //nolint:nilerr // a truncated element ends the item
		}

		// An Item Delimitation Item closes an undefined-length item.
		if t == tag.ItemDelimiterTag {
			return child, nil
		}
		if t == tag.SequenceDelimiterTag {
			return child, nil
		}

		if vr == dataelem.SQ {
			nested, err := readSequence(r, enc, order, length, depth+1)
			if err != nil {
				return nil, err
			}
			_ = child.AddSequence(t, nested)
			continue
		}

		if length == undefinedLength {
			return child, nil
		}
		if uint64(length) > uint64(r.Len()) {
			return nil, NewPDUErrorf("DECODE_DS",
				"element %s declares %d bytes but only %d remain", t, length, r.Len())
		}

		value := make([]byte, length)
		if _, err := io.ReadFull(r, value); err != nil {
			return child, nil //nolint:nilerr // a truncated value ends the item
		}
		_ = child.Add(dataelem.NewDataElement(t, vr, value))
	}
}

// readItemHeader reads an item or delimitation item: a tag and a 4-byte length,
// with no VR regardless of the transfer syntax.
func readItemHeader(r *bytes.Reader, order binary.ByteOrder) (tag.Tag, uint32, error) {
	var group, element uint16
	if err := binary.Read(r, order, &group); err != nil {
		return 0, 0, err
	}
	if err := binary.Read(r, order, &element); err != nil {
		return 0, 0, err
	}
	var length uint32
	if err := binary.Read(r, order, &length); err != nil {
		return 0, 0, err
	}
	return tag.New(group, element), length, nil
}

// padToEvenLength appends the VR's padding byte when a value has odd length.
// Returns the input unchanged when it is already even.
func padToEvenLength(vr dataelem.VR, data []byte) []byte {
	if len(data)%2 == 0 {
		return data
	}

	pad := byte(0x00)
	if info := dataelem.GetVRInfo(vr); info != nil {
		pad = info.PadValue
	}

	// Copy rather than append in place: the caller's slice may alias the
	// element's stored value, which must not be mutated by encoding.
	padded := make([]byte, len(data)+1)
	copy(padded, data)
	padded[len(data)] = pad
	return padded
}

// isLongFormVR reports whether a VR uses the 12-byte explicit header
// (2-byte reserved + 4-byte length) rather than the 8-byte short form.
func isLongFormVR(vr dataelem.VR) bool {
	switch vr {
	case dataelem.OB, dataelem.OD, dataelem.OF, dataelem.OL, dataelem.OW,
		dataelem.SQ, dataelem.UC, dataelem.UN, dataelem.UR, dataelem.UT:
		return true
	default:
		return false
	}
}

// elementValueBytes extracts an element's value as raw bytes.
func elementValueBytes(elem *dataelem.DataElement) ([]byte, bool) {
	switch v := elem.GetValue().(type) {
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	default:
		return nil, false
	}
}

// deflateBytes compresses a data set for the Deflated Explicit VR LE syntax.
func deflateBytes(data []byte) ([]byte, error) {
	var out bytes.Buffer
	w, err := flate.NewWriter(&out, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// MaxInflatedDatasetSize bounds how much a Deflated Explicit VR Little Endian
// data set may expand to.
//
// Deflate reaches ratios above 1000:1 on repetitive input, so a peer that
// negotiates the deflated transfer syntax can send a few kilobytes that expand
// without limit — a decompression bomb. The transfer syntax is negotiable by
// any peer, so this path is reachable before authentication. 256 MiB is far
// above any legitimate DICOM data set while keeping a hostile one cheap to
// reject.
const MaxInflatedDatasetSize int64 = 256 << 20

// inflateBytes decompresses a data set encoded with the Deflated syntax,
// refusing input that expands beyond MaxInflatedDatasetSize.
func inflateBytes(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()

	// The limit is scaled to what the peer actually sent. The absolute ceiling
	// alone lets a peer spend a few hundred kilobytes to make this allocate
	// hundreds of megabytes — and this path is reachable before authentication,
	// so the cost of rejecting a bomb should track the cost of building one.
	limit := compress.InflateLimitFor(int64(len(data)), MaxInflatedDatasetSize)

	// Read one byte past the limit: if that byte materializes, the input
	// expands beyond what is allowed and the rest is not worth decompressing.
	limited := io.LimitReader(r, limit+1)
	out, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > limit {
		return nil, NewPDUErrorf("DECOMPRESSION_LIMIT",
			"deflated data set of %d bytes expands beyond the %d byte limit allowed for its size",
			len(data), limit)
	}
	return out, nil
}
