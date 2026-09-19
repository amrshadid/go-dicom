package filereader_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/amrshadid/go-dicom/tag"
)

// explicitUNElement encodes a data element with VR UN (4-byte length).
func explicitUNElement(group, element uint16, value []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, group)
	_ = binary.Write(&buf, binary.LittleEndian, element)
	buf.WriteString("UN")
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(value)))
	buf.Write(value)
	return buf.Bytes()
}

func implicitItem(inner []byte) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xFFFE))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0xE000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(inner)))
	buf.Write(inner)
	return buf.Bytes()
}

// TestUNReplacedWithDictionaryVR covers a standard tag encoded as UN.
//
// Writers that do not recognise a tag re-encode it as UN. The dictionary
// knows the real VR; without replacing it, GetTextValue fails because UN is
// not a text VR, and a nested sequence is never parsed. PS3.5 §6.2.2 Note 2
// says to treat the value as Implicit VR Little Endian. pydicom's
// replace_un_with_known_vr is on by default; rtdose_rle.dcm is the corpus
// file this shows up in.
func TestUNReplacedWithDictionaryVR(t *testing.T) {
	var ds bytes.Buffer
	ds.Write(explicitUNElement(0x0008, 0x0060, []byte("RTDOSE ")))
	ds.Write(explicitUNElement(0x0010, 0x0010, []byte("Lastname^Firstname")))

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))
	got := df.GetDataset()

	mod, err := got.GetTextValue(tag.New(0x0008, 0x0060))
	if err != nil {
		t.Fatalf("Modality GetTextValue: %v", err)
	}
	if strings.TrimSpace(mod) != "RTDOSE" {
		t.Errorf("Modality = %q, want RTDOSE", mod)
	}

	pn, err := got.GetPersonName(tag.New(0x0010, 0x0010))
	if err != nil {
		t.Fatalf("PatientName: %v", err)
	}
	if pn == nil || !strings.Contains(pn.Alphabetic, "Lastname") {
		t.Errorf("PatientName = %+v, want Lastname^Firstname", pn)
	}

	if len(df.DataElements) < 1 {
		t.Fatal("no data elements")
	}
	if df.DataElements[0].VR != "CS" {
		t.Errorf("Modality VR = %q, want CS (replaced from UN)", df.DataElements[0].VR)
	}
}

// TestUNSequenceIsParsed covers a known SQ tag encoded as defined-length UN.
//
// PS3.5 §6.2.2 Note 5: a UN value of defined or undefined length may hold a
// sequence encoded as Implicit VR. Without replacement the item count is 0.
func TestUNSequenceIsParsed(t *testing.T) {
	inner := implicitElement(0x0008, 0x1150, []byte("1.2.840.10008.5.1.4.1.1.4\x00"))
	item := implicitItem(inner)

	var ds bytes.Buffer
	ds.Write(explicitUNElement(0x0008, 0x1110, item)) // ReferencedStudySequence

	df := readFile(t, buildFile("1.2.840.10008.1.2.1", ds.Bytes(), false))
	if len(df.DataElements) != 1 {
		t.Fatalf("read %d elements, want 1", len(df.DataElements))
	}
	el := df.DataElements[0]
	if el.VR != "SQ" {
		t.Fatalf("VR = %q, want SQ", el.VR)
	}
	if len(el.Items) != 1 {
		t.Fatalf("sequence items = %d, want 1", len(el.Items))
	}
	if len(el.Items[0].Elements) != 1 {
		t.Fatalf("item elements = %d, want 1", len(el.Items[0].Elements))
	}
	child := el.Items[0].Elements[0]
	if child.VR != "UI" {
		t.Errorf("nested VR = %q, want UI", child.VR)
	}

	got := df.GetDataset()
	if _, ok := got.Get(tag.New(0x0008, 0x1110)); !ok {
		t.Fatal("sequence missing from dataset")
	}
}
