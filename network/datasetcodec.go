package network

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/amrshadid/go-dicom/dataelem"
	"github.com/amrshadid/go-dicom/dataset"
	"github.com/amrshadid/go-dicom/tag"
)

// maxDatasetElementLength bounds a single declared element length when decoding
// a received data set. The length field is peer-controlled, so it is checked
// against the bytes actually remaining before any allocation is made.
const maxDatasetElementLength = 1 << 30

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

		// An undefined length here means a sequence or encapsulated pixel data.
		// Those are delimited rather than sized; consume the remainder as an
		// opaque value so the surrounding elements still decode.
		if length == 0xFFFFFFFF {
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

// inflateBytes decompresses a data set encoded with the Deflated syntax.
func inflateBytes(data []byte) ([]byte, error) {
	r := flate.NewReader(bytes.NewReader(data))
	defer r.Close()
	return io.ReadAll(r)
}
