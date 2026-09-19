package filereader_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// implicitElement encodes an element the implicit VR way: tag, then a 4-byte
// length, with no VR.
func implicitElement(group, element uint16, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, group)
	_ = binary.Write(&buf, binary.LittleEndian, element)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

// explicitElement encodes an element the explicit VR way, short form.
func explicitElement(group, element uint16, vr string, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, group)
	_ = binary.Write(&buf, binary.LittleEndian, element)
	buf.WriteString(vr)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

// TestImplicitElementInsideExplicitDataSet covers a writer that switches to
// implicit VR partway through a file declaring explicit VR.
//
// Real writers do this, most often inside sequences. Where a VR should be there
// are instead the low two bytes of a 4-byte length, so a reader expecting a VR
// takes those two bytes as the length, reads the value from the wrong offset,
// and every tag afterwards comes out of the middle of a value. pydicom's own
// SC_rgb_jpeg.dcm is encoded this way and yielded 1 element of the 34 it holds.
//
// dcmtk attempts a recovery here and gets it wrong — it assumes a 2-byte length
// — and fails on the same file.
func TestImplicitElementInsideExplicitDataSet(t *testing.T) {
	var ds bytes.Buffer
	// First element explicit, so the file genuinely starts as it declares.
	ds.Write(explicitElement(0x0008, 0x0060, "CS", []byte("MR")))
	// Then a switch to implicit, as a non-conformant writer would produce.
	ds.Write(implicitElement(0x0008, 0x0008, []byte("DERIVED\\SECONDARY  ")))
	// And back to explicit, so recovery is shown to resynchronize rather than
	// merely tolerate one bad element.
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))

	if len(df.DataElements) != 3 {
		t.Fatalf("read %d elements, want 3 — parsing did not resynchronize after the implicit element",
			len(df.DataElements))
	}

	got := df.GetDataset()
	for _, want := range []struct {
		tg    tag.Tag
		value string
	}{
		{tag.New(0x0008, 0x0060), "MR"},
		{tag.New(0x0008, 0x0008), "DERIVED\\SECONDARY  "},
		{tag.New(0x0010, 0x0010), "Doe^John"},
	} {
		elem, ok := got.Get(want.tg)
		if !ok {
			t.Errorf("%s missing", want.tg)
			continue
		}
		if v := string(elem.GetValue().([]byte)); v != want.value {
			t.Errorf("%s = %q, want %q", want.tg, v, want.value)
		}
	}

	// The VR of the implicit element has to come from the dictionary, since the
	// file did not supply one.
	elem, _ := got.Get(tag.New(0x0008, 0x0008))
	if elem.GetVR() != "CS" {
		t.Errorf("the implicitly encoded element has VR %q, want CS from the dictionary", elem.GetVR())
	}
}

// TestPrivateVRIsNotMistakenForALength guards the other side of that recovery.
//
// The check is on the shape of the two bytes — two uppercase letters — rather
// than membership in a list of known VRs. A private or future VR this build does
// not recognize is still a VR, and treating it as a length would corrupt the rest
// of the file rather than one element.
func TestPrivateVRIsNotMistakenForALength(t *testing.T) {
	var ds bytes.Buffer
	// "ZZ" is not a real VR, but it is shaped like one.
	ds.Write(explicitElement(0x0009, 0x0010, "ZZ", []byte("PRIVATE ")))
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))

	if len(df.DataElements) != 2 {
		t.Fatalf("read %d elements, want 2 — an unrecognized VR was treated as a length",
			len(df.DataElements))
	}
	elem, ok := df.GetDataset().Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("the element after the unrecognized VR was lost")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("value after an unrecognized VR = %q", got)
	}
}

// TestUndefinedLengthNonPixelElementIsASequence covers a private element written
// with VR UN holding a sequence.
//
// Only pixel data uses undefined length to mean encapsulation. Anything else
// carrying it holds items — most often a private element the writer had no
// dictionary entry for. Routing these to the encapsulation reader failed at the
// first item, because a sequence item may itself have undefined length and a
// pixel fragment never does. pydicom reads UN_sequence.dcm and
// nested_priv_SQ.dcm; go-dicom could not.
func TestUndefinedLengthNonPixelElementIsASequence(t *testing.T) {
	// A private tag, VR UN, undefined length, containing one item with one
	// element, closed by delimiters.
	var ds bytes.Buffer
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0009))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0010))
	ds.WriteString("UN")
	ds.Write([]byte{0x00, 0x00}) // reserved
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0xFFFFFFFF))

	// Item, undefined length.
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0xFFFFFFFF))
	ds.Write(explicitElement(0x0008, 0x0100, "SH", []byte("CODE01  ")))
	// Item delimiter.
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xE00D))
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0))
	// Sequence delimiter.
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0xE0DD))
	_ = binary.Write(&ds, binary.LittleEndian, uint32(0))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))

	if len(df.DataElements) != 1 {
		t.Fatalf("read %d elements, want 1", len(df.DataElements))
	}
	elem := df.DataElements[0]
	if elem.VR != "SQ" {
		t.Errorf("VR = %q, want SQ — an undefined-length UN element holds items", elem.VR)
	}
	if len(elem.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(elem.Items))
	}
	if len(elem.Items[0].Elements) != 1 {
		t.Fatalf("the item holds %d elements, want 1", len(elem.Items[0].Elements))
	}
	if got := string(elem.Items[0].Elements[0].Value); got != "CODE01  " {
		t.Errorf("nested value = %q", got)
	}
}

// TestPixelDataKeepsEncapsulationNotSequenceParsing guards the boundary: pixel
// data with undefined length is still encapsulated data, not a sequence.
func TestPixelDataKeepsEncapsulationNotSequenceParsing(t *testing.T) {
	frame := bytes.Repeat([]byte{0xA1}, 8)
	df := readFile(t, encapsulatedPixelData(t, [][]byte{frame}))

	elem, ok := df.GetDataset().Get(tag.New(0x7FE0, 0x0010))
	if !ok {
		t.Fatal("PixelData missing")
	}
	value := elem.GetValue().([]byte)
	if len(value) < 4 || !bytes.Equal(value[:4], []byte{0xFE, 0xFF, 0x00, 0xE0}) {
		t.Error("pixel data was parsed as a sequence instead of keeping its encapsulation")
	}
}

// TestMetaHeaderWithoutGroupLength covers a file whose meta header omits
// (0002,0000).
//
// The group length is the usual way to find the end of the header, but it is not
// always written. Requiring it rejected a file outright whose meta header is
// perfectly readable — the group marks its own end, since the data set that
// follows is not in group 0002. pydicom reads no_meta_group_length.dcm.
func TestMetaHeaderWithoutGroupLength(t *testing.T) {
	var meta bytes.Buffer
	meta.Write(explicitElement(0x0002, 0x0002, "UI", []byte("1.2.840.10008.5.1.4.1.1.4\x00")))
	meta.Write(explicitElement(0x0002, 0x0003, "UI", []byte("1.2.3.4.5\x00")))
	meta.Write(explicitElement(0x0002, 0x0010, "UI", []byte("1.2.840.10008.1.2.1\x00")))

	var ds bytes.Buffer
	ds.Write(explicitElement(0x0008, 0x0060, "CS", []byte("MR")))
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	var raw bytes.Buffer
	raw.Write(make([]byte, 128))
	raw.WriteString("DICM")
	// No (0002,0000) at all.
	raw.Write(meta.Bytes())
	raw.Write(ds.Bytes())

	df := readFile(t, raw.Bytes())

	if got := df.FileMetaInfo.TransferSyntaxUID; got != "1.2.840.10008.1.2.1" {
		t.Errorf("transfer syntax = %q, want the one in the header", got)
	}
	if len(df.DataElements) != 2 {
		t.Fatalf("read %d data set elements, want 2 — the meta header boundary was misplaced",
			len(df.DataElements))
	}
	elem, ok := df.GetDataset().Get(tag.New(0x0010, 0x0010))
	if !ok {
		t.Fatal("the data set element after the header was lost")
	}
	if got := string(elem.GetValue().([]byte)); got != "Doe^John" {
		t.Errorf("value = %q", got)
	}
}

// TestTruncatedFileKeepsWhatItCan verifies a file cut short yields the complete
// elements before the break, and says so.
//
// The incomplete element is dropped rather than returned short. pydicom returns
// it with whatever bytes were present; this is a deliberate difference, since a
// partially read pixel buffer handed back as PixelData can be rendered as an
// image with nothing looking wrong.
func TestTruncatedFileKeepsWhatItCan(t *testing.T) {
	var ds bytes.Buffer
	ds.Write(explicitElement(0x0008, 0x0060, "CS", []byte("MR")))
	ds.Write(explicitElement(0x0010, 0x0010, "PN", []byte("Doe^John")))

	// An element declaring far more than follows it.
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0028))
	_ = binary.Write(&ds, binary.LittleEndian, uint16(0x0010))
	ds.WriteString("US")
	_ = binary.Write(&ds, binary.LittleEndian, uint16(9999))
	ds.Write([]byte{0x40, 0x00}) // only 2 of the 9999 bytes

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))

	if len(df.DataElements) != 2 {
		t.Fatalf("kept %d elements, want the 2 complete ones", len(df.DataElements))
	}
	if len(df.Warnings) == 0 {
		t.Error("a truncated file produced no warning; the caller cannot tell data is missing")
	}
}
